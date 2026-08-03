package pte_test

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	pte "github.com/lemadane/piped-template-engine-in-go"
)

func TestSemanticJSCSSDirectivesEndToEnd(t *testing.T) {
	engine := pte.NewEngine("")

	t.Run("1. Inline JS Directive |js expr|", func(t *testing.T) {
		tmpl := `|js "console.log('hello');"|`
		var buf bytes.Buffer
		if err := engine.RenderString(&buf, tmpl, nil); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		expected := "<script>console.log('hello');</script>"
		if got := buf.String(); got != expected {
			t.Errorf("expected %q, got %q", expected, got)
		}
	})

	t.Run("2. Block JS Directive |js|...|/js|", func(t *testing.T) {
		tmpl := `|js|const x = |val|;|/js|`
		var buf bytes.Buffer
		data := map[string]any{"val": 42}
		if err := engine.RenderString(&buf, tmpl, data); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		expected := "<script>const x = 42;</script>"
		if got := buf.String(); got != expected {
			t.Errorf("expected %q, got %q", expected, got)
		}
	})

	t.Run("3. Inline CSS Directive |css expr|", func(t *testing.T) {
		tmpl := `|css "body { color: red; }"|`
		var buf bytes.Buffer
		if err := engine.RenderString(&buf, tmpl, nil); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		expected := "<style>body { color: red; }</style>"
		if got := buf.String(); got != expected {
			t.Errorf("expected %q, got %q", expected, got)
		}
	})

	t.Run("4. Block CSS Directive |css|...|/css|", func(t *testing.T) {
		tmpl := `|css|.btn { background: |bg|; }|/css|`
		var buf bytes.Buffer
		data := map[string]any{"bg": "#007bff"}
		if err := engine.RenderString(&buf, tmpl, data); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		expected := "<style>.btn { background: #007bff; }</style>"
		if got := buf.String(); got != expected {
			t.Errorf("expected %q, got %q", expected, got)
		}
	})

	t.Run("5. Directive Errors", func(t *testing.T) {
		t.Run("Unclosed |js| block error", func(t *testing.T) {
			tmpl := `|js|const a = 1;`
			var buf bytes.Buffer
			err := engine.RenderString(&buf, tmpl, nil)
			if err == nil || !strings.Contains(err.Error(), "missing closing |/js| tag") {
				t.Fatalf("expected missing closing |/js| error, got %v", err)
			}
		})

		t.Run("Unclosed |css| block error", func(t *testing.T) {
			tmpl := `|css|body { color: red; }`
			var buf bytes.Buffer
			err := engine.RenderString(&buf, tmpl, nil)
			if err == nil || !strings.Contains(err.Error(), "missing closing |/css| tag") {
				t.Fatalf("expected missing closing |/css| error, got %v", err)
			}
		})

		t.Run("Misplaced |/js| directive error", func(t *testing.T) {
			tmpl := `hello |/js| world`
			var buf bytes.Buffer
			err := engine.RenderString(&buf, tmpl, nil)
			if err == nil || !strings.Contains(err.Error(), "misplaced loop or block directive |/js|") {
				t.Fatalf("expected misplaced |/js| directive error, got %v", err)
			}
		})

		t.Run("Misplaced |/css| directive error", func(t *testing.T) {
			tmpl := `hello |/css| world`
			var buf bytes.Buffer
			err := engine.RenderString(&buf, tmpl, nil)
			if err == nil || !strings.Contains(err.Error(), "misplaced loop or block directive |/css|") {
				t.Fatalf("expected misplaced |/css| directive error, got %v", err)
			}
		})
	})

	t.Run("6. Full Pipelines & Concurrency", func(t *testing.T) {
		templates := map[string]string{
			"page": `|js|const user = "|name|";|/js||css|.user { color: |theme|; }|/css|`,
		}
		appEngine := pte.NewEngine("", pte.WithInMemoryTemplates(templates))
		data := map[string]any{
			"name":  "Alice",
			"theme": "darkblue",
		}

		expected := `<script>const user = "Alice";</script><style>.user { color: darkblue; }</style>`

		t.Run("Named template render", func(t *testing.T) {
			var buf bytes.Buffer
			if err := appEngine.Render(&buf, "page", data); err != nil {
				t.Fatal(err)
			}
			if got := buf.String(); got != expected {
				t.Errorf("expected %q, got %q", expected, got)
			}
		})

		t.Run("RenderStream pipeline", func(t *testing.T) {
			reader := appEngine.RenderStream("page", data)
			out, err := io.ReadAll(reader)
			if err != nil {
				t.Fatal(err)
			}
			if got := string(out); got != expected {
				t.Errorf("expected %q, got %q", expected, got)
			}
		})

		t.Run("HTTP Server response pipeline", func(t *testing.T) {
			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var buf bytes.Buffer
				if err := appEngine.Render(&buf, "page", data); err != nil {
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
			if got := string(body); got != expected {
				t.Errorf("expected %q, got %q", expected, got)
			}
		})

		t.Run("Concurrency race detector check", func(t *testing.T) {
			var wg sync.WaitGroup
			errChan := make(chan error, 50)

			for i := 0; i < 50; i++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					var buf bytes.Buffer
					if err := appEngine.Render(&buf, "page", data); err != nil {
						errChan <- err
						return
					}
					if got := buf.String(); got != expected {
						errChan <- fmt.Errorf("expected %q, got %q", expected, got)
					}
				}()
			}
			wg.Wait()
			close(errChan)

			for err := range errChan {
				t.Errorf("concurrent error: %v", err)
			}
		})
	})
}
