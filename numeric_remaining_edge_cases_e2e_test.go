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

func TestIssue3RemainingNumericEdgeCasesEndToEnd(t *testing.T) {
	engine := pte.NewEngine("")

	t.Run("1. Defect 1: NaN comparison semantics", func(t *testing.T) {
		data := map[string]any{
			"nan":        math.NaN(),
			"anotherNaN": math.NaN(),
			"finite":     123.45,
		}

		tests := []struct {
			name     string
			template string
			expected string
		}{
			{
				name:     "NaN == NaN is false",
				template: "|if nan == nan|wrong|else|correct|/if|",
				expected: "correct",
			},
			{
				name:     "NaN != NaN is true",
				template: "|if nan != nan|correct|else|wrong|/if|",
				expected: "correct",
			},
			{
				name:     "NaN < NaN is false",
				template: "|if nan < nan|wrong|else|correct|/if|",
				expected: "correct",
			},
			{
				name:     "NaN <= NaN is false",
				template: "|if nan <= nan|wrong|else|correct|/if|",
				expected: "correct",
			},
			{
				name:     "NaN > NaN is false",
				template: "|if nan > nan|wrong|else|correct|/if|",
				expected: "correct",
			},
			{
				name:     "NaN >= NaN is false",
				template: "|if nan >= nan|wrong|else|correct|/if|",
				expected: "correct",
			},
			{
				name:     "NaN == finite is false",
				template: "|if nan == finite|wrong|else|correct|/if|",
				expected: "correct",
			},
			{
				name:     "NaN != finite is true",
				template: "|if nan != finite|correct|else|wrong|/if|",
				expected: "correct",
			},
			{
				name:     "NaN < finite is false",
				template: "|if nan < finite|wrong|else|correct|/if|",
				expected: "correct",
			},
			{
				name:     "NaN > finite is false",
				template: "|if nan > finite|wrong|else|correct|/if|",
				expected: "correct",
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				var buf bytes.Buffer
				if err := engine.RenderString(&buf, tt.template, data); err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if got := strings.TrimSpace(buf.String()); got != tt.expected {
					t.Errorf("expected %q, got %q", tt.expected, got)
				}
			})
		}

		t.Run("NaN switch does not match NaN case", func(t *testing.T) {
			tmpl := `|switch nan|
|case anotherNaN|wrong
|default|correct
|/switch|`
			var buf bytes.Buffer
			if err := engine.RenderString(&buf, tmpl, data); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got := strings.TrimSpace(buf.String()); got != "correct" {
				t.Errorf("expected 'correct', got %q", got)
			}
		})
	})

	t.Run("2. Defect 2: Infinity ordering semantics", func(t *testing.T) {
		data := map[string]any{
			"positiveInfinity": math.Inf(1),
			"negativeInfinity": math.Inf(-1),
			"maxUint":          uint64(math.MaxUint64),
			"maxInt":           int64(math.MaxInt64),
			"minInt":           int64(math.MinInt64),
			"zeroUint":         uint64(0),
			"floatVal":         100.5,
		}

		t.Run("Positive infinity > maxUint", func(t *testing.T) {
			var buf bytes.Buffer
			if err := engine.RenderString(&buf, "|if positiveInfinity > maxUint|correct|else|wrong|/if|", data); err != nil {
				t.Fatal(err)
			}
			if got := strings.TrimSpace(buf.String()); got != "correct" {
				t.Errorf("expected 'correct', got %q", got)
			}
		})

		t.Run("Negative infinity < minInt", func(t *testing.T) {
			var buf bytes.Buffer
			if err := engine.RenderString(&buf, "|if negativeInfinity < minInt|correct|else|wrong|/if|", data); err != nil {
				t.Fatal(err)
			}
			if got := strings.TrimSpace(buf.String()); got != "correct" {
				t.Errorf("expected 'correct', got %q", got)
			}
		})

		t.Run("Positive infinity equality and ordering", func(t *testing.T) {
			tests := []struct {
				name     string
				template string
				expected string
			}{
				{
					name:     "+Inf == +Inf",
					template: "|if positiveInfinity == positiveInfinity|correct|else|wrong|/if|",
					expected: "correct",
				},
				{
					name:     "-Inf == -Inf",
					template: "|if negativeInfinity == negativeInfinity|correct|else|wrong|/if|",
					expected: "correct",
				},
				{
					name:     "+Inf != -Inf",
					template: "|if positiveInfinity != negativeInfinity|correct|else|wrong|/if|",
					expected: "correct",
				},
				{
					name:     "+Inf > -Inf",
					template: "|if positiveInfinity > negativeInfinity|correct|else|wrong|/if|",
					expected: "correct",
				},
			}
			for _, tt := range tests {
				t.Run(tt.name, func(t *testing.T) {
					var buf bytes.Buffer
					if err := engine.RenderString(&buf, tt.template, data); err != nil {
						t.Fatal(err)
					}
					if got := strings.TrimSpace(buf.String()); got != tt.expected {
						t.Errorf("expected %q, got %q", tt.expected, got)
					}
				})
			}
		})

		t.Run("Positive infinity switch matches case", func(t *testing.T) {
			tmpl := `|switch positiveInfinity|
|case positiveInfinity|matched
|default|wrong
|/switch|`
			var buf bytes.Buffer
			if err := engine.RenderString(&buf, tmpl, data); err != nil {
				t.Fatal(err)
			}
			if got := strings.TrimSpace(buf.String()); got != "matched" {
				t.Errorf("expected 'matched', got %q", got)
			}
		})
	})

	t.Run("3. Non-finite arithmetic policy", func(t *testing.T) {
		data := map[string]any{
			"posInf": math.Inf(1),
			"negInf": math.Inf(-1),
			"nan":    math.NaN(),
		}

		t.Run("posInf + 1 returns error", func(t *testing.T) {
			var buf bytes.Buffer
			err := engine.RenderString(&buf, "prefix|posInf + 1|suffix", data)
			if err == nil {
				t.Fatalf("expected error for non-finite arithmetic, got nil")
			}
			if buf.Len() > 0 {
				t.Errorf("expected 0 bytes committed, got %q", buf.String())
			}
		})

		t.Run("negInf * 2 returns error", func(t *testing.T) {
			var buf bytes.Buffer
			err := engine.RenderString(&buf, "prefix|negInf * 2|suffix", data)
			if err == nil {
				t.Fatalf("expected error for non-finite arithmetic, got nil")
			}
			if buf.Len() > 0 {
				t.Errorf("expected 0 bytes committed, got %q", buf.String())
			}
		})

		t.Run("nan + 1 returns error", func(t *testing.T) {
			var buf bytes.Buffer
			err := engine.RenderString(&buf, "prefix|nan + 1|suffix", data)
			if err == nil {
				t.Fatalf("expected error for non-finite arithmetic, got nil")
			}
			if buf.Len() > 0 {
				t.Errorf("expected 0 bytes committed, got %q", buf.String())
			}
		})
	})

	t.Run("4. Defect 3: Full-width unsigned modulo", func(t *testing.T) {
		t.Run("MaxUint64 % MaxUint64", func(t *testing.T) {
			data := map[string]any{
				"maxUint": uint64(math.MaxUint64),
			}
			var buf bytes.Buffer
			if err := engine.RenderString(&buf, "|maxUint % maxUint|", data); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got := strings.TrimSpace(buf.String()); got != "0" {
				t.Errorf("expected '0', got %q", got)
			}
		})

		t.Run("MaxUint64 % (MaxUint64 - 1)", func(t *testing.T) {
			data := map[string]any{
				"maxUint":     uint64(math.MaxUint64),
				"maxUintSub1": uint64(math.MaxUint64) - 1,
			}
			var buf bytes.Buffer
			if err := engine.RenderString(&buf, "|maxUint % maxUintSub1|", data); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got := strings.TrimSpace(buf.String()); got != "1" {
				t.Errorf("expected '1', got %q", got)
			}
		})

		t.Run("MaxUint64 % (MaxInt64Uint + 1)", func(t *testing.T) {
			data := map[string]any{
				"maxUint":        uint64(math.MaxUint64),
				"maxIntUintPlus": uint64(math.MaxInt64) + 1,
			}
			var buf bytes.Buffer
			if err := engine.RenderString(&buf, "|maxUint % maxIntUintPlus|", data); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got := strings.TrimSpace(buf.String()); got != "9223372036854775807" {
				t.Errorf("expected '9223372036854775807', got %q", got)
			}
		})

		t.Run("Negative dividend -5 % 2", func(t *testing.T) {
			data := map[string]any{
				"negFive": int64(-5),
				"two":     int64(2),
			}
			var buf bytes.Buffer
			if err := engine.RenderString(&buf, "|negFive % two|", data); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got := strings.TrimSpace(buf.String()); got != "-1" {
				t.Errorf("expected '-1', got %q", got)
			}
		})

		t.Run("Negative divisor 5 % -2", func(t *testing.T) {
			data := map[string]any{
				"five":   int64(5),
				"negTwo": int64(-2),
			}
			var buf bytes.Buffer
			if err := engine.RenderString(&buf, "|five % negTwo|", data); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got := strings.TrimSpace(buf.String()); got != "1" {
				t.Errorf("expected '1', got %q", got)
			}
		})

		t.Run("Modulo by zero returns error", func(t *testing.T) {
			data := map[string]any{
				"five": int64(5),
				"zero": int64(0),
			}
			var buf bytes.Buffer
			err := engine.RenderString(&buf, "prefix|five % zero|suffix", data)
			if err == nil {
				t.Fatalf("expected division by zero error, got nil")
			}
			if buf.Len() > 0 {
				t.Errorf("expected 0 bytes committed, got %q", buf.String())
			}
		})
	})

	t.Run("5. Defect 4: Restore fractional division", func(t *testing.T) {
		tests := []struct {
			name     string
			template string
			data     map[string]any
			expected string
		}{
			{
				name:     "5 / 2 literal",
				template: "|5 / 2|",
				data:     nil,
				expected: "2.5",
			},
			{
				name:     "1 / 2 literal",
				template: "|1 / 2|",
				data:     nil,
				expected: "0.5",
			},
			{
				name:     "-5 / 2 literal",
				template: "|-5 / 2|",
				data:     nil,
				expected: "-2.5",
			},
			{
				name:     "4 / 2 literal",
				template: "|4 / 2|",
				data:     nil,
				expected: "2",
			},
			{
				name:     "uint64(5) / uint64(2) data map",
				template: "|left / right|",
				data: map[string]any{
					"left":  uint64(5),
					"right": uint64(2),
				},
				expected: "2.5",
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				var buf bytes.Buffer
				if err := engine.RenderString(&buf, tt.template, tt.data); err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if got := strings.TrimSpace(buf.String()); got != tt.expected {
					t.Errorf("expected %q, got %q", tt.expected, got)
				}
			})
		}

		t.Run("Division by zero returns error and 0 bytes", func(t *testing.T) {
			var buf bytes.Buffer
			err := engine.RenderString(&buf, "prefix|5 / 0|suffix", nil)
			if err == nil {
				t.Fatalf("expected division by zero error, got nil")
			}
			if buf.Len() > 0 {
				t.Errorf("expected 0 bytes committed, got %q", buf.String())
			}
		})
	})

	t.Run("6. End-to-end pipelines", func(t *testing.T) {
		templates := map[string]string{
			"math/edge": `|if posInf > maxUint|INF_OK|/if|-|maxUint % maxUint|-|5 / 2|`,
		}
		e2eEngine := pte.NewEngine("", pte.WithInMemoryTemplates(templates))

		data := map[string]any{
			"posInf":  math.Inf(1),
			"maxUint": uint64(math.MaxUint64),
		}

		t.Run("Named template", func(t *testing.T) {
			var buf bytes.Buffer
			if err := e2eEngine.Render(&buf, "math/edge", data); err != nil {
				t.Fatal(err)
			}
			if got := strings.TrimSpace(buf.String()); got != "INF_OK-0-2.5" {
				t.Errorf("expected 'INF_OK-0-2.5', got %q", got)
			}
		})

		t.Run("Fragment", func(t *testing.T) {
			tmpl := `|fragment res|
|if posInf > maxUint|INF_OK|/if|-|maxUint % maxUint|-|5 / 2|
|/fragment|`
			var buf bytes.Buffer
			if err := engine.RenderFragment(&buf, tmpl, "res", data); err != nil {
				t.Fatal(err)
			}
			if got := strings.TrimSpace(buf.String()); got != "INF_OK-0-2.5" {
				t.Errorf("expected 'INF_OK-0-2.5', got %q", got)
			}
		})

		t.Run("RenderStream", func(t *testing.T) {
			reader := e2eEngine.RenderStream("math/edge", data)
			out, err := io.ReadAll(reader)
			if err != nil {
				t.Fatal(err)
			}
			if got := strings.TrimSpace(string(out)); got != "INF_OK-0-2.5" {
				t.Errorf("expected 'INF_OK-0-2.5', got %q", got)
			}
		})

		t.Run("HTTP Server", func(t *testing.T) {
			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var buf bytes.Buffer
				if err := e2eEngine.Render(&buf, "math/edge", data); err != nil {
					http.Error(w, err.Error(), 500)
					return
				}
				w.Write(buf.Bytes())
			})
			server := httptest.NewServer(handler)
			defer server.Close()

			resp, err := http.Get(server.URL)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			body, _ := io.ReadAll(resp.Body)
			if got := strings.TrimSpace(string(body)); got != "INF_OK-0-2.5" {
				t.Errorf("expected 'INF_OK-0-2.5', got %q", got)
			}
		})
	})

	t.Run("7. Concurrency test", func(t *testing.T) {
		data := map[string]any{
			"posInf":  math.Inf(1),
			"maxUint": uint64(math.MaxUint64),
			"nan":     math.NaN(),
		}
		tmplValid := "|if posInf > maxUint|OK|/if|-|maxUint % maxUint|-|5 / 2|"
		tmplErr := "|posInf + 1|"

		var wg sync.WaitGroup
		errChan := make(chan error, 200)

		for i := 0; i < 50; i++ {
			wg.Add(2)
			go func() {
				defer wg.Done()
				var buf bytes.Buffer
				if err := engine.RenderString(&buf, tmplValid, data); err != nil {
					errChan <- err
					return
				}
				if got := strings.TrimSpace(buf.String()); got != "OK-0-2.5" {
					errChan <- fmt.Errorf("concurrent valid expected 'OK-0-2.5', got %q", got)
				}
			}()
			go func() {
				defer wg.Done()
				var buf bytes.Buffer
				err := engine.RenderString(&buf, tmplErr, data)
				if err == nil {
					errChan <- fmt.Errorf("concurrent non-finite arith: expected error, got nil")
					return
				}
				if buf.Len() > 0 {
					errChan <- fmt.Errorf("concurrent non-finite arith: expected 0 bytes, got %q", buf.String())
				}
			}()
		}

		wg.Wait()
		close(errChan)

		for err := range errChan {
			t.Errorf("concurrency error: %v", err)
		}
	})
}
