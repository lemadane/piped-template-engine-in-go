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

func TestAllRecentFeaturesCombinedEndToEnd(t *testing.T) {
	t.Run("1. Unified Numeric Contract Invariants", func(t *testing.T) {
		engine := pte.NewEngine("")

		t.Run("Large Integer Division (> 2^53)", func(t *testing.T) {
			tmpl := "|largeUint / oneUint|"
			data := map[string]any{
				"largeUint": uint64(9007199254740993),
				"oneUint":   uint64(1),
			}
			var buf bytes.Buffer
			if err := engine.RenderString(&buf, tmpl, data); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got := strings.TrimSpace(buf.String()); got != "9007199254740993" {
				t.Errorf("expected '9007199254740993', got %q", got)
			}
		})

		t.Run("Fractional Division", func(t *testing.T) {
			tmpl := "|5 / 2|"
			var buf bytes.Buffer
			if err := engine.RenderString(&buf, tmpl, nil); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got := strings.TrimSpace(buf.String()); got != "2.5" {
				t.Errorf("expected '2.5', got %q", got)
			}
		})

		t.Run("String 'NaN' Type Preservation", func(t *testing.T) {
			tmpl := "|if str == 'NaN'|correct|else|wrong|/if|"
			data := map[string]any{"str": "NaN"}
			var buf bytes.Buffer
			if err := engine.RenderString(&buf, tmpl, data); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got := strings.TrimSpace(buf.String()); got != "correct" {
				t.Errorf("expected 'correct', got %q", got)
			}
		})

		t.Run("Typed Float NaN IEEE Comparison", func(t *testing.T) {
			tmpl := "|if floatNaN == floatNaN|wrong|else|correct|/if|"
			data := map[string]any{"floatNaN": math.NaN()}
			var buf bytes.Buffer
			if err := engine.RenderString(&buf, tmpl, data); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got := strings.TrimSpace(buf.String()); got != "correct" {
				t.Errorf("expected 'correct', got %q", got)
			}
		})
	})

	t.Run("2. Semantic JS & CSS Directives", func(t *testing.T) {
		engine := pte.NewEngine("")

		t.Run("Inline & Block JS Directives", func(t *testing.T) {
			tmpl := `|js "console.log('init');"||js|const id = |userId|;|/js|`
			data := map[string]any{"userId": 1001}
			var buf bytes.Buffer
			if err := engine.RenderString(&buf, tmpl, data); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			expected := "<script>console.log('init');</script><script>const id = 1001;</script>"
			if got := buf.String(); got != expected {
				t.Errorf("expected %q, got %q", expected, got)
			}
		})

		t.Run("Inline & Block CSS Directives", func(t *testing.T) {
			tmpl := `|css "body { margin: 0; }"||css|.card { color: |color|; }|/css|`
			data := map[string]any{"color": "navy"}
			var buf bytes.Buffer
			if err := engine.RenderString(&buf, tmpl, data); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			expected := "<style>body { margin: 0; }</style><style>.card { color: navy; }</style>"
			if got := buf.String(); got != expected {
				t.Errorf("expected %q, got %q", expected, got)
			}
		})

		t.Run("Syntax Error Enforcement", func(t *testing.T) {
			for _, tmpl := range []string{"|js|const x = 1;", "|css|body {}", "hello |/js|", "hello |/css|"} {
				var buf bytes.Buffer
				if err := engine.RenderString(&buf, tmpl, nil); err == nil {
					t.Errorf("expected error for invalid syntax %q, got nil", tmpl)
				}
			}
		})
	})

	t.Run("3. Full Engine Master Integration Pipeline", func(t *testing.T) {
		templates := map[string]string{
			"app/master": `|js|const userId = |uid|;|/js|
|css|.user { color: |theme|; }|/css|
<div id="calc">|calcResult|</div>
|if statusStr == 'NaN'|<span>Text String NaN</span>|/if|
|fragment body_frag|
  <main>User ID: |uid|</main>
|/fragment|`,
		}

		appEngine := pte.NewEngine("", pte.WithInMemoryTemplates(templates))
		data := map[string]any{
			"uid":        uint64(9007199254740993),
			"theme":      "blue",
			"calcResult": uint64(9007199254740993) / uint64(1),
			"statusStr":  "NaN",
		}

		t.Run("Named Template Render", func(t *testing.T) {
			var buf bytes.Buffer
			if err := appEngine.Render(&buf, "app/master", data); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			out := buf.String()
			if !strings.Contains(out, `<script>const userId = 9007199254740993;</script>`) {
				t.Errorf("missing JS directive output: %s", out)
			}
			if !strings.Contains(out, `<style>.user { color: blue; }</style>`) {
				t.Errorf("missing CSS directive output: %s", out)
			}
			if !strings.Contains(out, `<div id="calc">9007199254740993</div>`) {
				t.Errorf("missing exact integer division output: %s", out)
			}
			if !strings.Contains(out, `<span>Text String NaN</span>`) {
				t.Errorf("missing string NaN evaluation output: %s", out)
			}
		})

		t.Run("Fragment Render", func(t *testing.T) {
			var buf bytes.Buffer
			if err := appEngine.RenderFragment(&buf, templates["app/master"], "body_frag", data); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got := strings.TrimSpace(buf.String()); got != "<main>User ID: 9007199254740993</main>" {
				t.Errorf("fragment expected '<main>User ID: 9007199254740993</main>', got %q", got)
			}
		})

		t.Run("RenderStream Pipeline", func(t *testing.T) {
			reader := appEngine.RenderStream("app/master", data)
			outBytes, err := io.ReadAll(reader)
			if err != nil {
				t.Fatalf("unexpected streaming error: %v", err)
			}
			out := string(outBytes)
			if !strings.Contains(out, `<script>const userId = 9007199254740993;</script>`) {
				t.Errorf("stream missing JS directive output: %s", out)
			}
		})

		t.Run("HTTP Web Server Response Pipeline", func(t *testing.T) {
			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var buf bytes.Buffer
				if err := appEngine.Render(&buf, "app/master", data); err != nil {
					http.Error(w, err.Error(), 500)
					return
				}
				w.Write(buf.Bytes())
			})
			server := httptest.NewServer(handler)
			defer server.Close()

			resp, err := http.Get(server.URL)
			if err != nil {
				t.Fatalf("HTTP request failed: %v", err)
			}
			defer resp.Body.Close()

			bodyBytes, _ := io.ReadAll(resp.Body)
			out := string(bodyBytes)
			if !strings.Contains(out, `<div id="calc">9007199254740993</div>`) {
				t.Errorf("HTTP response missing exact calculation output: %s", out)
			}
		})

		t.Run("Multi-Goroutine Race Detector Check", func(t *testing.T) {
			var wg sync.WaitGroup
			errChan := make(chan error, 100)

			for i := 0; i < 50; i++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					var buf bytes.Buffer
					if err := appEngine.Render(&buf, "app/master", data); err != nil {
						errChan <- err
						return
					}
					out := buf.String()
					if !strings.Contains(out, `9007199254740993`) {
						errChan <- fmt.Errorf("concurrent render missing expected data: %s", out)
					}
				}()
			}
			wg.Wait()
			close(errChan)

			for err := range errChan {
				t.Errorf("concurrent pipeline error: %v", err)
			}
		})
	})
}
