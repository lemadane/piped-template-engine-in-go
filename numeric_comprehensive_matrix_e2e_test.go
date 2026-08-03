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

func TestIssue3ComprehensiveNumericMatrixEndToEnd(t *testing.T) {
	engine := pte.NewEngine("")

	t.Run("1. Large integer division precision (> 2^53)", func(t *testing.T) {
		tests := []struct {
			name     string
			template string
			data     map[string]any
			expected string
		}{
			{
				name:     "uint64(9007199254740993) / uint64(1)",
				template: "|largeUint / oneUint|",
				data: map[string]any{
					"largeUint": uint64(9007199254740993),
					"oneUint":   uint64(1),
				},
				expected: "9007199254740993",
			},
			{
				name:     "uint64(9007199254740994) / uint64(2)",
				template: "|evenUint / twoUint|",
				data: map[string]any{
					"evenUint": uint64(9007199254740994),
					"twoUint":  uint64(2),
				},
				expected: "4503599627370497",
			},
			{
				name:     "MaxInt64 / 1",
				template: "|maxInt / oneInt|",
				data: map[string]any{
					"maxInt": int64(math.MaxInt64),
					"oneInt": int64(1),
				},
				expected: fmt.Sprintf("%d", int64(math.MaxInt64)),
			},
			{
				name:     "MaxUint64 / 1",
				template: "|maxUint / oneUint|",
				data: map[string]any{
					"maxUint": uint64(math.MaxUint64),
					"oneUint": uint64(1),
				},
				expected: fmt.Sprintf("%d", uint64(math.MaxUint64)),
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
	})

	t.Run("2. Fractional integer division", func(t *testing.T) {
		tests := []struct {
			name     string
			template string
			data     map[string]any
			expected string
		}{
			{
				name:     "5 / 2",
				template: "|5 / 2|",
				data:     nil,
				expected: "2.5",
			},
			{
				name:     "1 / 2",
				template: "|1 / 2|",
				data:     nil,
				expected: "0.5",
			},
			{
				name:     "-5 / 2",
				template: "|-5 / 2|",
				data:     nil,
				expected: "-2.5",
			},
			{
				name:     "4 / 2",
				template: "|4 / 2|",
				data:     nil,
				expected: "2",
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
	})

	t.Run("3. String 'NaN' and 'Inf' text preservation", func(t *testing.T) {
		data := map[string]any{
			"strNaN": "NaN",
			"strInf": "Inf",
		}

		t.Run("String 'NaN' == String 'NaN' is true", func(t *testing.T) {
			tmpl := "|if strNaN == 'NaN'|correct|else|wrong|/if|"
			var buf bytes.Buffer
			if err := engine.RenderString(&buf, tmpl, data); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got := strings.TrimSpace(buf.String()); got != "correct" {
				t.Errorf("expected 'correct', got %q", got)
			}
		})

		t.Run("String 'Inf' == String 'Inf' is true", func(t *testing.T) {
			tmpl := "|if strInf == 'Inf'|correct|else|wrong|/if|"
			var buf bytes.Buffer
			if err := engine.RenderString(&buf, tmpl, data); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got := strings.TrimSpace(buf.String()); got != "correct" {
				t.Errorf("expected 'correct', got %q", got)
			}
		})

		t.Run("String 'NaN' switch matches 'NaN' case", func(t *testing.T) {
			tmpl := `|switch strNaN|
|case 'NaN'|matched
|default|wrong
|/switch|`
			var buf bytes.Buffer
			if err := engine.RenderString(&buf, tmpl, data); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got := strings.TrimSpace(buf.String()); got != "matched" {
				t.Errorf("expected 'matched', got %q", got)
			}
		})
	})

	t.Run("4. Typed Float NaN & Inf Semantics", func(t *testing.T) {
		data := map[string]any{
			"floatNaN": math.NaN(),
			"floatInf": math.Inf(1),
			"maxUint":  uint64(math.MaxUint64),
		}

		t.Run("Float NaN == Float NaN is false", func(t *testing.T) {
			tmpl := "|if floatNaN == floatNaN|wrong|else|correct|/if|"
			var buf bytes.Buffer
			if err := engine.RenderString(&buf, tmpl, data); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got := strings.TrimSpace(buf.String()); got != "correct" {
				t.Errorf("expected 'correct', got %q", got)
			}
		})

		t.Run("Float Inf > MaxUint64 is true", func(t *testing.T) {
			tmpl := "|if floatInf > maxUint|correct|else|wrong|/if|"
			var buf bytes.Buffer
			if err := engine.RenderString(&buf, tmpl, data); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got := strings.TrimSpace(buf.String()); got != "correct" {
				t.Errorf("expected 'correct', got %q", got)
			}
		})

		t.Run("Float NaN switch does not match case", func(t *testing.T) {
			tmpl := `|switch floatNaN|
|case floatNaN|wrong
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

	t.Run("5. Non-Finite Arithmetic Guard", func(t *testing.T) {
		data := map[string]any{
			"floatInf": math.Inf(1),
			"floatNaN": math.NaN(),
		}

		t.Run("floatInf + 1 returns error", func(t *testing.T) {
			var buf bytes.Buffer
			err := engine.RenderString(&buf, "prefix|floatInf + 1|suffix", data)
			if err == nil {
				t.Fatalf("expected error for non-finite float arithmetic, got nil")
			}
			if buf.Len() > 0 {
				t.Errorf("expected 0 bytes committed on error, got %q", buf.String())
			}
		})

		t.Run("floatNaN + 1 returns error", func(t *testing.T) {
			var buf bytes.Buffer
			err := engine.RenderString(&buf, "prefix|floatNaN + 1|suffix", data)
			if err == nil {
				t.Fatalf("expected error for non-finite float arithmetic, got nil")
			}
			if buf.Len() > 0 {
				t.Errorf("expected 0 bytes committed on error, got %q", buf.String())
			}
		})
	})

	t.Run("6. Full 64-Bit Domain Boundary Invariants", func(t *testing.T) {
		data := map[string]any{
			"maxUint": uint64(math.MaxUint64),
			"maxInt":  int64(math.MaxInt64),
			"one":     int64(1),
			"oneUint": uint64(1),
		}

		t.Run("MaxUint64 % MaxUint64", func(t *testing.T) {
			var buf bytes.Buffer
			if err := engine.RenderString(&buf, "|maxUint % maxUint|", data); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got := strings.TrimSpace(buf.String()); got != "0" {
				t.Errorf("expected '0', got %q", got)
			}
		})

		t.Run("MaxInt64 + 1 overflow error", func(t *testing.T) {
			var buf bytes.Buffer
			err := engine.RenderString(&buf, "|maxInt + one|", data)
			if err == nil {
				t.Fatalf("expected overflow error, got nil")
			}
		})
	})

	t.Run("7. End-to-End Pipelines & Concurrency", func(t *testing.T) {
		templates := map[string]string{
			"math/matrix": `|largeUint / oneUint|-|if strNaN == 'NaN'|OK|/if|-|5 / 2|`,
		}
		matrixEngine := pte.NewEngine("", pte.WithInMemoryTemplates(templates))
		data := map[string]any{
			"largeUint": uint64(9007199254740993),
			"oneUint":   uint64(1),
			"strNaN":    "NaN",
		}

		t.Run("Named template render", func(t *testing.T) {
			var buf bytes.Buffer
			if err := matrixEngine.Render(&buf, "math/matrix", data); err != nil {
				t.Fatal(err)
			}
			if got := strings.TrimSpace(buf.String()); got != "9007199254740993-OK-2.5" {
				t.Errorf("expected '9007199254740993-OK-2.5', got %q", got)
			}
		})

		t.Run("Fragment render", func(t *testing.T) {
			tmpl := `|fragment res|
|largeUint / oneUint|-|if strNaN == 'NaN'|OK|/if|-|5 / 2|
|/fragment|`
			var buf bytes.Buffer
			if err := engine.RenderFragment(&buf, tmpl, "res", data); err != nil {
				t.Fatal(err)
			}
			if got := strings.TrimSpace(buf.String()); got != "9007199254740993-OK-2.5" {
				t.Errorf("expected '9007199254740993-OK-2.5', got %q", got)
			}
		})

		t.Run("RenderStream pipeline", func(t *testing.T) {
			reader := matrixEngine.RenderStream("math/matrix", data)
			out, err := io.ReadAll(reader)
			if err != nil {
				t.Fatal(err)
			}
			if got := strings.TrimSpace(string(out)); got != "9007199254740993-OK-2.5" {
				t.Errorf("expected '9007199254740993-OK-2.5', got %q", got)
			}
		})

		t.Run("HTTP Server response pipeline", func(t *testing.T) {
			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var buf bytes.Buffer
				if err := matrixEngine.Render(&buf, "math/matrix", data); err != nil {
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
			if got := strings.TrimSpace(string(body)); got != "9007199254740993-OK-2.5" {
				t.Errorf("expected '90071993-OK-2.5', got %q", got)
			}
		})

		t.Run("Concurrency race detector check", func(t *testing.T) {
			var wg sync.WaitGroup
			errChan := make(chan error, 100)

			for i := 0; i < 50; i++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					var buf bytes.Buffer
					if err := matrixEngine.Render(&buf, "math/matrix", data); err != nil {
						errChan <- err
						return
					}
					if got := strings.TrimSpace(buf.String()); got != "9007199254740993-OK-2.5" {
						errChan <- fmt.Errorf("expected '9007199254740993-OK-2.5', got %q", got)
					}
				}()
			}
			wg.Wait()
			close(errChan)

			for err := range errChan {
				t.Errorf("concurrent matrix error: %v", err)
			}
		})
	})
}
