package pte_test

import (
	"bytes"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	pte "github.com/lemadane/piped-template-engine-in-go"
)

func TestIssue3ExactNumericEndToEnd(t *testing.T) {
	engine := pte.NewEngine("")

	t.Run("1. Mixed integer/float conditional comparison", func(t *testing.T) {
		data := map[string]any{
			"largeUint":    uint64(9007199254740993),
			"roundedFloat": float64(9007199254740992),
		}

		tests := []struct {
			name     string
			template string
			expected string
		}{
			{
				name:     "== operator",
				template: "|if largeUint == roundedFloat|wrong|else|correct|/if|",
				expected: "correct",
			},
			{
				name:     "!= operator",
				template: "|if largeUint != roundedFloat|correct|else|wrong|/if|",
				expected: "correct",
			},
			{
				name:     "< operator",
				template: "|if largeUint < roundedFloat|wrong|else|correct|/if|",
				expected: "correct",
			},
			{
				name:     "<= operator",
				template: "|if largeUint <= roundedFloat|wrong|else|correct|/if|",
				expected: "correct",
			},
			{
				name:     "> operator",
				template: "|if largeUint > roundedFloat|correct|else|wrong|/if|",
				expected: "correct",
			},
			{
				name:     ">= operator",
				template: "|if largeUint >= roundedFloat|correct|else|wrong|/if|",
				expected: "correct",
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				var buf bytes.Buffer
				err := engine.RenderString(&buf, tt.template, data)
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if got := strings.TrimSpace(buf.String()); got != tt.expected {
					t.Errorf("expected %q, got %q", tt.expected, got)
				}
			})
		}
	})

	t.Run("2. Exact equality with a representable float", func(t *testing.T) {
		data := map[string]any{
			"largeUint":  uint64(9007199254740992),
			"exactFloat": float64(9007199254740992),
		}

		tmpl := "|if largeUint == exactFloat|correct|else|wrong|/if|"
		var buf bytes.Buffer
		err := engine.RenderString(&buf, tmpl, data)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got := strings.TrimSpace(buf.String()); got != "correct" {
			t.Errorf("expected 'correct', got %q", got)
		}
	})

	t.Run("3. Negative signed integer versus unsigned integer", func(t *testing.T) {
		data := map[string]any{
			"negInt":            int64(-1),
			"posUint":           uint64(1),
			"maxInt":            int64(math.MaxInt64),
			"maxIntUint":        uint64(math.MaxInt64),
			"maxIntUintPlusOne": uint64(math.MaxInt64) + 1,
		}

		t.Run("-1 < 1", func(t *testing.T) {
			tmpl := "|if negInt < posUint|correct|else|wrong|/if|"
			var buf bytes.Buffer
			if err := engine.RenderString(&buf, tmpl, data); err != nil {
				t.Fatal(err)
			}
			if got := strings.TrimSpace(buf.String()); got != "correct" {
				t.Errorf("expected 'correct', got %q", got)
			}
		})

		t.Run("MaxInt64 == MaxInt64Uint", func(t *testing.T) {
			tmpl := "|if maxInt == maxIntUint|correct|else|wrong|/if|"
			var buf bytes.Buffer
			if err := engine.RenderString(&buf, tmpl, data); err != nil {
				t.Fatal(err)
			}
			if got := strings.TrimSpace(buf.String()); got != "correct" {
				t.Errorf("expected 'correct', got %q", got)
			}
		})

		t.Run("MaxInt64 < MaxInt64UintPlusOne", func(t *testing.T) {
			tmpl := "|if maxInt < maxIntUintPlusOne|correct|else|wrong|/if|"
			var buf bytes.Buffer
			if err := engine.RenderString(&buf, tmpl, data); err != nil {
				t.Fatal(err)
			}
			if got := strings.TrimSpace(buf.String()); got != "correct" {
				t.Errorf("expected 'correct', got %q", got)
			}
		})
	})

	t.Run("4. Switch uses exact numeric equality", func(t *testing.T) {
		t.Run("unequal large uint vs rounded float goes to default", func(t *testing.T) {
			tmpl := `|switch largeUint|
|case roundedFloat|wrong
|default|correct
|/switch|`
			data := map[string]any{
				"largeUint":    uint64(9007199254740993),
				"roundedFloat": float64(9007199254740992),
			}
			var buf bytes.Buffer
			if err := engine.RenderString(&buf, tmpl, data); err != nil {
				t.Fatal(err)
			}
			if got := strings.TrimSpace(buf.String()); got != "correct" {
				t.Errorf("expected 'correct', got %q", got)
			}
		})

		t.Run("equal representable uint vs float matches case", func(t *testing.T) {
			tmpl := `|switch largeUint|
|case exactFloat|matched
|default|wrong
|/switch|`
			data := map[string]any{
				"largeUint":  uint64(9007199254740992),
				"exactFloat": float64(9007199254740992),
			}
			var buf bytes.Buffer
			if err := engine.RenderString(&buf, tmpl, data); err != nil {
				t.Fatal(err)
			}
			if got := strings.TrimSpace(buf.String()); got != "matched" {
				t.Errorf("expected 'matched', got %q", got)
			}
		})
	})

	t.Run("5. Exact unsigned addition", func(t *testing.T) {
		tmpl := "|largeUint + oneUint|"
		data := map[string]any{
			"largeUint": uint64(9007199254740993),
			"oneUint":   uint64(1),
		}
		var buf bytes.Buffer
		if err := engine.RenderString(&buf, tmpl, data); err != nil {
			t.Fatal(err)
		}
		if got := strings.TrimSpace(buf.String()); got != "9007199254740994" {
			t.Errorf("expected '9007199254740994', got %q", got)
		}
	})

	t.Run("6. Exact unsigned subtraction and multiplication", func(t *testing.T) {
		t.Run("subtraction", func(t *testing.T) {
			tmpl := "|largeUint - oneUint|"
			data := map[string]any{
				"largeUint": uint64(9007199254740993),
				"oneUint":   uint64(1),
			}
			var buf bytes.Buffer
			if err := engine.RenderString(&buf, tmpl, data); err != nil {
				t.Fatal(err)
			}
			if got := strings.TrimSpace(buf.String()); got != "9007199254740992" {
				t.Errorf("expected '9007199254740992', got %q", got)
			}
		})

		t.Run("multiplication", func(t *testing.T) {
			tmpl := "|halfUint * twoUint|"
			data := map[string]any{
				"halfUint": uint64(4503599627370497),
				"twoUint":  uint64(2),
			}
			var buf bytes.Buffer
			if err := engine.RenderString(&buf, tmpl, data); err != nil {
				t.Fatal(err)
			}
			if got := strings.TrimSpace(buf.String()); got != "9007199254740994" {
				t.Errorf("expected '9007199254740994', got %q", got)
			}
		})
	})

	t.Run("7. Exact large numeric literals", func(t *testing.T) {
		t.Run("addition expression literal", func(t *testing.T) {
			tmpl := "|9007199254740993 + 1|"
			var buf bytes.Buffer
			if err := engine.RenderString(&buf, tmpl, nil); err != nil {
				t.Fatal(err)
			}
			if got := strings.TrimSpace(buf.String()); got != "9007199254740994" {
				t.Errorf("expected '9007199254740994', got %q", got)
			}
		})

		t.Run("largest uint64 literal", func(t *testing.T) {
			tmpl := "|18446744073709551615|"
			var buf bytes.Buffer
			if err := engine.RenderString(&buf, tmpl, nil); err != nil {
				t.Fatal(err)
			}
			if got := strings.TrimSpace(buf.String()); got != "18446744073709551615" {
				t.Errorf("expected '18446744073709551615', got %q", got)
			}
		})
	})

	t.Run("8. Signed addition overflow", func(t *testing.T) {
		tmpl := "prefix|maxInt + oneInt|suffix"
		data := map[string]any{
			"maxInt": int64(math.MaxInt64),
			"oneInt": int64(1),
		}
		var buf bytes.Buffer
		err := engine.RenderString(&buf, tmpl, data)
		if err == nil {
			t.Fatalf("expected overflow error, got nil")
		}
		if !strings.Contains(strings.ToLower(err.Error()), "overflow") && !strings.Contains(strings.ToLower(err.Error()), "out of range") {
			t.Errorf("expected overflow error message, got %v", err)
		}
		if buf.Len() > 0 {
			t.Errorf("expected 0 bytes committed on error, got %q", buf.String())
		}
	})

	t.Run("9. Signed subtraction overflow", func(t *testing.T) {
		tmpl := "prefix|minInt - oneInt|suffix"
		data := map[string]any{
			"minInt": int64(math.MinInt64),
			"oneInt": int64(1),
		}
		var buf bytes.Buffer
		err := engine.RenderString(&buf, tmpl, data)
		if err == nil {
			t.Fatalf("expected overflow error, got nil")
		}
		if !strings.Contains(strings.ToLower(err.Error()), "overflow") && !strings.Contains(strings.ToLower(err.Error()), "underflow") && !strings.Contains(strings.ToLower(err.Error()), "out of range") {
			t.Errorf("expected overflow/underflow error message, got %v", err)
		}
		if buf.Len() > 0 {
			t.Errorf("expected 0 bytes committed on error, got %q", buf.String())
		}
	})

	t.Run("10. Signed multiplication overflow", func(t *testing.T) {
		t.Run("positive overflow", func(t *testing.T) {
			tmpl := "|maxInt * twoInt|"
			data := map[string]any{
				"maxInt": int64(math.MaxInt64),
				"twoInt": int64(2),
			}
			var buf bytes.Buffer
			err := engine.RenderString(&buf, tmpl, data)
			if err == nil {
				t.Fatalf("expected overflow error, got nil")
			}
			if buf.Len() > 0 {
				t.Errorf("expected 0 bytes committed on error, got %q", buf.String())
			}
		})

		t.Run("negative minInt * -1 overflow", func(t *testing.T) {
			tmpl := "|minInt * negOneInt|"
			data := map[string]any{
				"minInt":    int64(math.MinInt64),
				"negOneInt": int64(-1),
			}
			var buf bytes.Buffer
			err := engine.RenderString(&buf, tmpl, data)
			if err == nil {
				t.Fatalf("expected overflow error, got nil")
			}
		})
	})

	t.Run("11. Unsigned overflow and underflow", func(t *testing.T) {
		t.Run("unsigned overflow", func(t *testing.T) {
			tmpl := "|maxUint + oneUint|"
			data := map[string]any{
				"maxUint": uint64(math.MaxUint64),
				"oneUint": uint64(1),
			}
			var buf bytes.Buffer
			err := engine.RenderString(&buf, tmpl, data)
			if err == nil {
				t.Fatalf("expected overflow error, got nil")
			}
		})

		t.Run("unsigned underflow", func(t *testing.T) {
			tmpl := "|zeroUint - oneUint|"
			data := map[string]any{
				"zeroUint": uint64(0),
				"oneUint":  uint64(1),
			}
			var buf bytes.Buffer
			err := engine.RenderString(&buf, tmpl, data)
			if err == nil {
				t.Fatalf("expected underflow error, got nil")
			}
		})
	})

	t.Run("12. Valid boundary arithmetic", func(t *testing.T) {
		data := map[string]any{
			"maxInt":  int64(math.MaxInt64),
			"minInt":  int64(math.MinInt64),
			"maxUint": uint64(math.MaxUint64),
			"one":     int64(1),
			"oneUint": uint64(1),
		}

		t.Run("MaxInt64 - 1", func(t *testing.T) {
			var buf bytes.Buffer
			if err := engine.RenderString(&buf, "|maxInt - one|", data); err != nil {
				t.Fatal(err)
			}
			expected := fmt.Sprintf("%d", int64(math.MaxInt64)-1)
			if got := strings.TrimSpace(buf.String()); got != expected {
				t.Errorf("expected %q, got %q", expected, got)
			}
		})

		t.Run("MinInt64 + 1", func(t *testing.T) {
			var buf bytes.Buffer
			if err := engine.RenderString(&buf, "|minInt + one|", data); err != nil {
				t.Fatal(err)
			}
			expected := fmt.Sprintf("%d", int64(math.MinInt64)+1)
			if got := strings.TrimSpace(buf.String()); got != expected {
				t.Errorf("expected %q, got %q", expected, got)
			}
		})

		t.Run("MaxUint64 - 1", func(t *testing.T) {
			var buf bytes.Buffer
			if err := engine.RenderString(&buf, "|maxUint - oneUint|", data); err != nil {
				t.Fatal(err)
			}
			expected := fmt.Sprintf("%d", uint64(math.MaxUint64)-1)
			if got := strings.TrimSpace(buf.String()); got != expected {
				t.Errorf("expected %q, got %q", expected, got)
			}
		})
	})

	t.Run("13. Fragment rendering pipeline", func(t *testing.T) {
		tmpl := `|fragment result|
|largeUint + oneUint|
|/fragment|`
		data := map[string]any{
			"largeUint": uint64(9007199254740993),
			"oneUint":   uint64(1),
		}
		var buf bytes.Buffer
		if err := engine.RenderFragment(&buf, tmpl, "result", data); err != nil {
			t.Fatal(err)
		}
		if got := strings.TrimSpace(buf.String()); got != "9007199254740994" {
			t.Errorf("expected '9007199254740994', got %q", got)
		}

		tmplErr := `|fragment result|
prefix|maxInt + oneInt|suffix
|/fragment|`
		dataErr := map[string]any{
			"maxInt": int64(math.MaxInt64),
			"oneInt": int64(1),
		}
		var bufErr bytes.Buffer
		err := engine.RenderFragment(&bufErr, tmplErr, "result", dataErr)
		if err == nil {
			t.Fatalf("expected overflow error in fragment, got nil")
		}
		if bufErr.Len() > 0 {
			t.Errorf("expected 0 bytes committed on error in fragment, got %q", bufErr.String())
		}
	})

	t.Run("14. Named-template rendering pipeline", func(t *testing.T) {
		templates := map[string]string{
			"math/test": `|if largeUint > roundedFloat|OK|/if|-|switch largeUint||case exactFloat|MATCHED|/switch|-|largeUint + oneUint|`,
		}
		namedEngine := pte.NewEngine("", pte.WithInMemoryTemplates(templates))

		data := map[string]any{
			"largeUint":    uint64(9007199254740993),
			"roundedFloat": float64(9007199254740992),
			"exactFloat":   float64(9007199254740993),
			"oneUint":      uint64(1),
		}

		var buf bytes.Buffer
		if err := namedEngine.Render(&buf, "math/test", data); err != nil {
			t.Fatal(err)
		}
		if got := strings.TrimSpace(buf.String()); got != "OK--9007199254740994" {
			t.Errorf("expected 'OK--9007199254740994', got %q", got)
		}
	})

	t.Run("15. Concurrent rendering", func(t *testing.T) {
		tmplValid := "|largeUint + oneUint|"
		dataValid := map[string]any{
			"largeUint": uint64(9007199254740993),
			"oneUint":   uint64(1),
		}
		tmplErr := "|maxInt + oneInt|"
		dataErr := map[string]any{
			"maxInt": int64(math.MaxInt64),
			"oneInt": int64(1),
		}

		var wg sync.WaitGroup
		errChan := make(chan error, 200)

		for i := 0; i < 50; i++ {
			wg.Add(2)
			go func() {
				defer wg.Done()
				var buf bytes.Buffer
				if err := engine.RenderString(&buf, tmplValid, dataValid); err != nil {
					errChan <- err
					return
				}
				if got := strings.TrimSpace(buf.String()); got != "9007199254740994" {
					errChan <- fmt.Errorf("concurrent valid: expected '9007199254740994', got %q", got)
				}
			}()
			go func() {
				defer wg.Done()
				var buf bytes.Buffer
				err := engine.RenderString(&buf, tmplErr, dataErr)
				if err == nil {
					errChan <- fmt.Errorf("concurrent overflow: expected error, got nil")
					return
				}
				if buf.Len() > 0 {
					errChan <- fmt.Errorf("concurrent overflow: expected 0 bytes committed, got %q", buf.String())
				}
			}()
		}

		wg.Wait()
		close(errChan)

		for err := range errChan {
			t.Errorf("concurrency error: %v", err)
		}
	})

	t.Run("16. RenderStream end-to-end pipeline", func(t *testing.T) {
		templates := map[string]string{
			"stream/calc": "|largeUint + oneUint|",
		}
		streamEngine := pte.NewEngine("", pte.WithInMemoryTemplates(templates))

		data := map[string]any{
			"largeUint": uint64(9007199254740993),
			"oneUint":   uint64(1),
		}

		reader := streamEngine.RenderStream("stream/calc", data)
		out, err := io.ReadAll(reader)
		if err != nil {
			t.Fatalf("RenderStream failed: %v", err)
		}
		if got := strings.TrimSpace(string(out)); got != "9007199254740994" {
			t.Errorf("expected '9007199254740994', got %q", got)
		}
	})

	t.Run("17. HTTP Server end-to-end response pipeline", func(t *testing.T) {
		templates := map[string]string{
			"pages/home": `<!DOCTYPE html><html><body><h1>|largeUint + oneUint|</h1></body></html>`,
			"pages/err":  `<!DOCTYPE html><html><body><h1>|maxInt + oneInt|</h1></body></html>`,
		}
		httpEngine := pte.NewEngine("", pte.WithInMemoryTemplates(templates))

		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			data := map[string]any{
				"largeUint": uint64(9007199254740993),
				"oneUint":   uint64(1),
				"maxInt":    int64(math.MaxInt64),
				"oneInt":    int64(1),
			}
			tmplName := r.URL.Query().Get("page")
			var buf bytes.Buffer
			if err := httpEngine.Render(&buf, tmplName, data); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write(buf.Bytes())
		})

		server := httptest.NewServer(handler)
		defer server.Close()

		t.Run("successful exact numeric page response", func(t *testing.T) {
			resp, err := http.Get(server.URL + "?page=pages/home")
			if err != nil {
				t.Fatalf("HTTP GET failed: %v", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				t.Errorf("expected status 200, got %d", resp.StatusCode)
			}

			body, _ := io.ReadAll(resp.Body)
			expected := `<!DOCTYPE html><html><body><h1>9007199254740994</h1></body></html>`
			if got := strings.TrimSpace(string(body)); got != expected {
				t.Errorf("expected %q, got %q", expected, got)
			}
		})

		t.Run("overflow error page response", func(t *testing.T) {
			resp, err := http.Get(server.URL + "?page=pages/err")
			if err != nil {
				t.Fatalf("HTTP GET failed: %v", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusInternalServerError {
				t.Errorf("expected status 500 on overflow, got %d", resp.StatusCode)
			}
		})
	})
}
