package pte

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFileRouter(t *testing.T) {
	tempDir := t.TempDir()

	// Write test templates for routes
	routes := map[string]string{
		"+page.pte":                     "|page title='Home'|\n<h1>Hello, |name ?? 'Guest'|!</h1>",
		"products/+page.pte":            "|page title='Products'|\n<h1>Products List</h1>",
		"products/[id]/+page.pte":       "|page title='Product Detail'|\n<h1>Product |id|</h1>",
		"products/featured/+page.pte":   "|page title='Featured Products'|\n<h1>Featured</h1>",
		"admin/+page.pte":               "|page auth=true|\n|page roles='admin'|\n<h1>Admin Page</h1>",
	}

	for relPath, content := range routes {
		fullPath := filepath.Join(tempDir, relPath)
		if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(fullPath, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}

	engine := NewEngine("")
	router, err := NewFileRouter(engine, tempDir)
	if err != nil {
		t.Fatalf("failed to create FileRouter: %v", err)
	}

	t.Run("Precedence and Path Matching", func(t *testing.T) {
		// Test static route at root
		route, params := router.Match("/")
		if route == nil {
			t.Fatal("expected to match root route")
		}
		if route.Path != "/" {
			t.Errorf("expected route '/' but got %q", route.Path)
		}

		// Test static subroute
		route, params = router.Match("/products")
		if route == nil {
			t.Fatal("expected to match '/products'")
		}
		if route.Path != "/products" {
			t.Errorf("expected route '/products' but got %q", route.Path)
		}

		// Test static specificity matching before wildcards
		route, params = router.Match("/products/featured")
		if route == nil {
			t.Fatal("expected to match '/products/featured'")
		}
		if route.Path != "/products/featured" {
			t.Errorf("expected route '/products/featured' but got %q", route.Path)
		}

		// Test dynamic route matching
		route, params = router.Match("/products/12345")
		if route == nil {
			t.Fatal("expected to match dynamic route")
		}
		if route.Path != "/products/[id]" {
			t.Errorf("expected route '/products/[id]' but got %q", route.Path)
		}
		if params["id"] != "12345" {
			t.Errorf("expected param 'id' to be '12345' but got %q", params["id"])
		}
	})

	t.Run("HTTP Serve & Loaders", func(t *testing.T) {
		router.RegisterDataLoader("/", func(r *http.Request, params map[string]string) (map[string]any, error) {
			return map[string]any{"name": "Alice"}, nil
		})

		req := httptest.NewRequest("GET", "/", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		resp := w.Result()
		if resp.StatusCode != http.StatusOK {
			t.Errorf("expected 200 OK but got %d", resp.StatusCode)
		}

		body := strings.TrimSpace(w.Body.String())
		expected := "<h1>Hello, Alice!</h1>"
		if body != expected {
			t.Errorf("expected body %q but got %q", expected, body)
		}

		title := resp.Header.Get("Content-Type")
		if !strings.Contains(title, "text/html") {
			t.Errorf("expected content type to be html but got %q", title)
		}
	})

	t.Run("HTTP Authorization Deny/Allow", func(t *testing.T) {
		// Test unauthorized request denied by default
		req := httptest.NewRequest("GET", "/admin", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Result().StatusCode != http.StatusUnauthorized {
			t.Errorf("expected 401 Unauthorized by default, got %d", w.Result().StatusCode)
		}

		// Attach an auth check hook
		router.AuthCheck = func(r *http.Request, requiredRoles []string) (bool, int, string) {
			if len(requiredRoles) > 0 && requiredRoles[0] == "admin" {
				token := r.Header.Get("Authorization")
				if token == "Bearer secret-admin" {
					return true, 0, ""
				}
				return false, http.StatusForbidden, "Forbidden"
			}
			return false, http.StatusUnauthorized, "Unauthorized"
		}

		// Try with bad token
		req = httptest.NewRequest("GET", "/admin", nil)
		req.Header.Set("Authorization", "Bearer bad")
		w = httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Result().StatusCode != http.StatusForbidden {
			t.Errorf("expected 403 Forbidden with bad token, got %d", w.Result().StatusCode)
		}

		// Try with correct token
		req = httptest.NewRequest("GET", "/admin", nil)
		req.Header.Set("Authorization", "Bearer secret-admin")
		w = httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Result().StatusCode != http.StatusOK {
			t.Errorf("expected 200 OK with correct token, got %d", w.Result().StatusCode)
		}
	})
}

// Ensure Context passes the router context correctly to subagents or handlers
func TestRouterNormalize(t *testing.T) {
	router := &FileRouter{}
	tests := []struct {
		in  string
		out string
	}{
		{"products", "/products"},
		{"/products/", "/products"},
		{"\\products\\", "/products"},
		{"/", "/"},
	}

	for _, tt := range tests {
		res := router.normalizePattern(tt.in)
		if res != tt.out {
			t.Errorf("expected %q -> %q, got %q", tt.in, tt.out, res)
		}
	}
}
