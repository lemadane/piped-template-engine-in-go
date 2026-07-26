package pte

import (
	"bytes"
	"io"
	"strings"
	"testing"
	"time"
)

func TestEscapingAndOutputModes(t *testing.T) {
	engine := NewEngine("")

	tests := []struct {
		name     string
		template string
		data     map[string]any
		expected string
	}{
		{
			name:     "Escapes HTML by default",
			template: "|comment|",
			data:     map[string]any{"comment": "<strong>Hello</strong>"},
			expected: "&lt;strong&gt;Hello&lt;/strong&gt;",
		},
		{
			name:     "Renders trusted HTML without escaping",
			template: "|html body|",
			data:     map[string]any{"body": "<strong>Hello</strong>"},
			expected: "<strong>Hello</strong>",
		},
		{
			name:     "Escapes HTML attributes",
			template: "<input value=\"|attr name|\">",
			data:     map[string]any{"name": "Juan \"Boss\" `dev`"},
			expected: "<input value=\"Juan &quot;Boss&quot; &#096;dev&#096;\">",
		},
		{
			name:     "Encodes URL values",
			template: "/search?q=|url query|",
			data:     map[string]any{"query": "coffee & sugar"},
			expected: "/search?q=coffee+%26+sugar",
		},
		{
			name:     "Renders JSON safely",
			template: "|json product|",
			data: map[string]any{"product": map[string]any{
				"name":        "Rice",
				"description": "<strong>Premium</strong>",
			}},
			expected: `{"description":"\u003cstrong\u003ePremium\u003c/strong\u003e","name":"Rice"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			if err := engine.Render(&buf, tt.template, tt.data); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if buf.String() != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, buf.String())
			}
		})
	}
}

func TestExpressions(t *testing.T) {
	engine := NewEngine("")

	type Profile struct {
		DisplayName string
	}
	type User struct {
		Profile *Profile
		Active  bool
	}

	tests := []struct {
		name     string
		template string
		data     map[string]any
		expected string
	}{
		{
			name:     "Optional chaining and null coalescing - missing field",
			template: "|user?.Profile?.DisplayName ?? 'Guest'|",
			data:     map[string]any{},
			expected: "Guest",
		},
		{
			name:     "Optional chaining and null coalescing - value present",
			template: "|user?.Profile?.DisplayName ?? 'Guest'|",
			data: map[string]any{
				"user": &User{Profile: &Profile{DisplayName: "Lemuel"}},
			},
			expected: "Lemuel",
		},
		{
			name:     "Ternary operator true",
			template: "|user.Active ? 'Active' : 'Inactive'|",
			data: map[string]any{
				"user": &User{Active: true},
			},
			expected: "Active",
		},
		{
			name:     "Ternary operator false",
			template: "|user.Active ? 'Active' : 'Inactive'|",
			data: map[string]any{
				"user": &User{Active: false},
			},
			expected: "Inactive",
		},
		{
			name: "Truthy/Falsy logical evaluation in if-block",
			template: `|if products|Has products|else|No products|/if|`,
			data:     map[string]any{"products": []any{}},
			expected: "No products",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			if err := engine.Render(&buf, tt.template, tt.data); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if buf.String() != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, buf.String())
			}
		})
	}
}

func TestConditionals(t *testing.T) {
	engine := NewEngine("")

	template := `
|if user.role == 'admin'|
   Admin
|else-if user.role == 'manager'|
   Manager
|else|
   User
|/if|`

	tests := []struct {
		role     string
		expected string
	}{
		{"admin", "Admin"},
		{"manager", "Manager"},
		{"guest", "User"},
	}

	for _, tt := range tests {
		t.Run(tt.role, func(t *testing.T) {
			var buf bytes.Buffer
			data := map[string]any{"user": map[string]any{"role": tt.role}}
			if err := engine.Render(&buf, template, data); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			res := strings.TrimSpace(buf.String())
			if res != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, res)
			}
		})
	}
}

func TestLoopsAndEach(t *testing.T) {
	engine := NewEngine("")

	t.Run("loops over list with metadata", func(t *testing.T) {
		template := `|each product in products||each.count|. |product.name||separator|, |/separator||/each|`
		data := map[string]any{
			"products": []map[string]any{
				{"name": "Rice"},
				{"name": "Coffee"},
			},
		}
		var buf bytes.Buffer
		if err := engine.Render(&buf, template, data); err != nil {
			t.Fatal(err)
		}
		expected := "1. Rice, 2. Coffee"
		if buf.String() != expected {
			t.Errorf("expected %q, got %q", expected, buf.String())
		}
	})

	t.Run("loops over empty list renders else block", func(t *testing.T) {
		template := `|each product in products||product.name||else|No products|/each|`
		data := map[string]any{"products": []any{}}
		var buf bytes.Buffer
		if err := engine.Render(&buf, template, data); err != nil {
			t.Fatal(err)
		}
		expected := "No products"
		if buf.String() != expected {
			t.Errorf("expected %q, got %q", expected, buf.String())
		}
	})

	t.Run("loops over map keys and values", func(t *testing.T) {
		template := `|each k, v in myMap||k|:|v| |/each|`
		data := map[string]any{
			"myMap": map[string]any{
				"A": 1,
				"B": 2,
			},
		}
		var buf bytes.Buffer
		if err := engine.Render(&buf, template, data); err != nil {
			t.Fatal(err)
		}
		res := buf.String()
		// Map keys in Go are randomized, so test both combinations
		if !strings.Contains(res, "A:1") || !strings.Contains(res, "B:2") {
			t.Errorf("expected map loop output to contain A:1 and B:2, got %q", res)
		}
	})
}

func TestSwitchFallthrough(t *testing.T) {
	engine := NewEngine("")

	t.Run("switch with fallthrough and automatic break", func(t *testing.T) {
		template := `
|switch role|
   |case 'admin'|
      Admin
      |fallthrough|
   |case 'manager'|
      Reports
   |default|
      User
|/switch|`

		var buf bytes.Buffer
		if err := engine.Render(&buf, template, map[string]any{"role": "admin"}); err != nil {
			t.Fatal(err)
		}
		res := strings.Join(strings.Fields(buf.String()), " ")
		if !strings.Contains(res, "Admin") || !strings.Contains(res, "Reports") || strings.Contains(res, "User") {
			t.Errorf("expected Admin and Reports with no User, got %q", res)
		}
	})
}

func TestFilters(t *testing.T) {
	engine := NewEngine("")

	t.Run("text filters", func(t *testing.T) {
		template := `|name, upper| |messyText, trim, capitalize| |val, slug|`
		data := map[string]any{
			"name":      "lemuel",
			"messyText": "   hello pte   ",
			"val":       "Rice & Coffee! _slugified_",
		}
		var buf bytes.Buffer
		if err := engine.Render(&buf, template, data); err != nil {
			t.Fatal(err)
		}
		expected := "LEMUEL Hello pte rice-coffee-slugified"
		if buf.String() != expected {
			t.Errorf("expected %q, got %q", expected, buf.String())
		}
	})

	t.Run("numeric formats", func(t *testing.T) {
		template := `|price, currency '₱'| |weight, number '#,##0.##'|`
		data := map[string]any{
			"price":  12345.67,
			"weight": 1200.5,
		}
		var buf bytes.Buffer
		if err := engine.Render(&buf, template, data); err != nil {
			t.Fatal(err)
		}
		expected := "₱12,345.67 1,200.5"
		if buf.String() != expected {
			t.Errorf("expected %q, got %q", expected, buf.String())
		}
	})

	t.Run("date layouts", func(t *testing.T) {
		dt := time.Date(2026, 7, 7, 10, 15, 30, 0, time.UTC)
		template := `|createdAt, date 'yyyy-MM-dd'| |createdAt, time 'HH:mm:ss'|`
		data := map[string]any{"createdAt": dt}
		var buf bytes.Buffer
		if err := engine.Render(&buf, template, data); err != nil {
			t.Fatal(err)
		}
		expected := "2026-07-07 10:15:30"
		if buf.String() != expected {
			t.Errorf("expected %q, got %q", expected, buf.String())
		}
	})
}

func TestLayoutsAndSlots(t *testing.T) {
	templates := map[string]string{
		"layouts/main": `<html>
<head><title>|yield title|</title></head>
<body>|yield content|</body>
</html>`,
		"pages/products": `|layout layouts/main|
|section title|
   Products
|/section|
|section content|
   <h1>Product List</h1>
|/section|`,
		"components/card": `<section>
   <h3>|slot title|</h3>
   <div>|slot body|</div>
</section>`,
	}

	engine := NewEngine("", WithInMemoryTemplates(templates))

	t.Run("renders layout and yields", func(t *testing.T) {
		var buf bytes.Buffer
		if err := engine.Render(&buf, "pages/products", nil); err != nil {
			t.Fatal(err)
		}
		res := strings.Join(strings.Fields(buf.String()), " ")
		if !strings.Contains(res, "<title> Products </title>") || !strings.Contains(res, "<h1>Product List</h1>") {
			t.Errorf("layout yield render failed, got %q", res)
		}
	})

	t.Run("renders component slots", func(t *testing.T) {
		template := `
|component components/card|
    |slot title|
        Product List
    |/slot|
    |slot body|
        <p>Products here</p>
    |/slot|
|/component|`

		var buf bytes.Buffer
		if err := engine.Render(&buf, template, nil); err != nil {
			t.Fatal(err)
		}
		res := strings.Join(strings.Fields(buf.String()), " ")
		if !strings.Contains(res, "<h3> Product List </h3>") || !strings.Contains(res, "<p>Products here</p>") {
			t.Errorf("component slot render failed, got %q", res)
		}
	})
}

func TestMacros(t *testing.T) {
	engine := NewEngine("")

	template := `
|macro myAlert(message, type)|
   <div class="alert alert-|type|">|message|</div>
|/macro|
|call myAlert('Operation completed', 'success')|`

	var buf bytes.Buffer
	if err := engine.Render(&buf, template, nil); err != nil {
		t.Fatal(err)
	}
	res := strings.TrimSpace(buf.String())
	expected := `<div class="alert alert-success">Operation completed</div>`
	if res != expected {
		t.Errorf("expected %q, got %q", expected, res)
	}
}

func TestAttemptRecover(t *testing.T) {
	engine := NewEngine("")

	template := `
|attempt|
   Hello |nonExistentField.subField|!
|recover as errMsg|
   Error occurred: |errMsg|
|/attempt|`

	var buf bytes.Buffer
	if err := engine.Render(&buf, template, nil); err != nil {
		t.Fatal(err)
	}
	res := strings.TrimSpace(buf.String())
	if !strings.Contains(res, "Error occurred") {
		t.Errorf("recover block did not render error, got %q", res)
	}
}

func TestStreamingRender(t *testing.T) {
	engine := NewEngine("")
	template := "Hello, |name|!"
	data := map[string]any{"name": "Golang"}

	r := engine.RenderStringStream(template, data)
	resBytes, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("failed to read from stream: %v", err)
	}
	res := string(resBytes)
	expected := "Hello, Golang!"
	if res != expected {
		t.Errorf("expected %q, got %q", expected, res)
	}
}
