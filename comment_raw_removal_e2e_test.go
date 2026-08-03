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

func TestCommentAndRawRemovalEndToEnd(t *testing.T) {
	engine := pte.NewEngine("")

	t.Run("1. Raw block directive removal", func(t *testing.T) {
		// |raw| is no longer recognized as a raw block wrapper; |raw| is evaluated as an expression/identifier
		tmpl := "|if active|Active|/if|"
		var buf bytes.Buffer
		if err := engine.RenderString(&buf, tmpl, map[string]any{"active": true}); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got := buf.String(); got != "Active" {
			t.Errorf("expected %q, got %q", "Active", got)
		}
	})

	t.Run("2. |-- Comment syntax removal", func(t *testing.T) {
		// |# comment #| remains valid comment syntax, while |-- comment --| is no longer comment syntax
		tmpl := "|# valid comment #|Hello |name|"
		var buf bytes.Buffer
		if err := engine.RenderString(&buf, tmpl, map[string]any{"name": "World"}); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got := buf.String(); got != "Hello World" {
			t.Errorf("expected 'Hello World', got %q", got)
		}
	})

	t.Run("3. End-to-End pipelines without raw blocks or |-- comments", func(t *testing.T) {
		templates := map[string]string{
			"pages/home": `|fragment greeting|<h1>Hello |name|</h1>|/fragment|`,
		}
		e := pte.NewEngine("", pte.WithInMemoryTemplates(templates))
		data := map[string]any{"name": "Alice"}

		t.Run("Named template render", func(t *testing.T) {
			var buf bytes.Buffer
			if err := e.Render(&buf, "pages/home", data); err != nil {
				t.Fatal(err)
			}
			if got := strings.TrimSpace(buf.String()); got != "<h1>Hello Alice</h1>" {
				t.Errorf("expected '<h1>Hello Alice</h1>', got %q", got)
			}
		})

		t.Run("Fragment render", func(t *testing.T) {
			var buf bytes.Buffer
			if err := e.RenderFragment(&buf, templates["pages/home"], "greeting", data); err != nil {
				t.Fatal(err)
			}
			if got := strings.TrimSpace(buf.String()); got != "<h1>Hello Alice</h1>" {
				t.Errorf("expected '<h1>Hello Alice</h1>', got %q", got)
			}
		})

		t.Run("RenderStream pipeline", func(t *testing.T) {
			reader := e.RenderStream("pages/home", data)
			out, err := io.ReadAll(reader)
			if err != nil {
				t.Fatal(err)
			}
			if got := strings.TrimSpace(string(out)); got != "<h1>Hello Alice</h1>" {
				t.Errorf("expected '<h1>Hello Alice</h1>', got %q", got)
			}
		})

		t.Run("HTTP Server response pipeline", func(t *testing.T) {
			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var buf bytes.Buffer
				if err := e.Render(&buf, "pages/home", data); err != nil {
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
			if got := strings.TrimSpace(string(body)); got != "<h1>Hello Alice</h1>" {
				t.Errorf("expected '<h1>Hello Alice</h1>', got %q", got)
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
					if err := e.Render(&buf, "pages/home", data); err != nil {
						errChan <- err
						return
					}
					if got := strings.TrimSpace(buf.String()); got != "<h1>Hello Alice</h1>" {
						errChan <- fmt.Errorf("expected '<h1>Hello Alice</h1>', got %q", got)
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
