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
			name:     "Truthy/Falsy logical evaluation in if-block",
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
|else if user.role == 'manager'|
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

func TestHashComments(t *testing.T) {
	engine := NewEngine("")
	template := "Hello|# this is a single line comment | World! |# \n this is a \n multi line block comment #| Hello!"
	var buf bytes.Buffer
	if err := engine.Render(&buf, template, nil); err != nil {
		t.Fatal(err)
	}
	expected := "Hello World!  Hello!"
	if buf.String() != expected {
		t.Errorf("expected %q, got %q", expected, buf.String())
	}
}

func TestCircularInclude(t *testing.T) {
	templates := map[string]string{
		"a": "|include b|",
		"b": "|include a|",
	}
	engine := NewEngine("", WithInMemoryTemplates(templates))
	var buf bytes.Buffer
	err := engine.Render(&buf, "a", nil)
	if err == nil {
		t.Fatal("expected circular include error but got nil")
	}
	if !strings.Contains(err.Error(), "circular include detected") {
		t.Errorf("expected circular include error, got %v", err)
	}
}

func TestConditionalAttributeShorthandAndCleanup(t *testing.T) {
	engine := NewEngine("")

	tests := []struct {
		name      string
		template  string
		completed bool
		expected  string
	}{
		{
			name:      "Attribute shorthand and cleanup - true case",
			template:  `<input class="form-input" |attr checked if completed|>`,
			completed: true,
			expected:  `<input class="form-input" checked>`,
		},
		{
			name:      "Attribute shorthand and cleanup - false case",
			template:  `<input class="form-input" |attr checked if completed|>`,
			completed: false,
			expected:  `<input class="form-input">`,
		},
		{
			name:      "Attribute shorthand and cleanup - false case with trailing tag space",
			template:  `<input class="form-input" |attr checked if completed| >`,
			completed: false,
			expected:  `<input class="form-input">`,
		},
		{
			name:      "Attribute shorthand with expression - true case",
			template:  `<div |attr class=cls if completed|>`,
			completed: true,
			expected:  `<div class="btn-success">`,
		},
		{
			name:      "Attribute shorthand with expression - false case",
			template:  `<div |attr class=cls if completed|>`,
			completed: false,
			expected:  `<div>`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			data := map[string]any{"completed": tt.completed, "cls": "btn-success"}
			if err := engine.Render(&buf, tt.template, data); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if buf.String() != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, buf.String())
			}
		})
	}
}

func TestRenderFragments(t *testing.T) {
	templates := map[string]string{
		"page": `
<div>
   |fragment toast|
      <div id="toast" hx-swap-oob="true">Saved!</div>
   |/fragment|

   |fragment cart|
      <span id="cart-count" hx-swap-oob="true">3</span>
   |/fragment|
</div>`,
	}

	engine := NewEngine("", WithInMemoryTemplates(templates))
	var buf bytes.Buffer
	err := engine.RenderFragments(&buf, "page", []string{"toast", "cart"}, nil)
	if err != nil {
		t.Fatalf("unexpected error rendering fragments: %v", err)
	}

	res := strings.TrimSpace(buf.String())
	if !strings.Contains(res, `id="toast"`) || !strings.Contains(res, `id="cart-count"`) {
		t.Errorf("expected both OOB fragments in output, got %q", res)
	}
}

func TestPWANode(t *testing.T) {
	engine := NewEngine("")
	template := `|pwa name='TaskMaster' theme='#4f46e5' icon='/icon-192.png' sw='/sw.js'|`

	var buf bytes.Buffer
	if err := engine.Render(&buf, template, nil); err != nil {
		t.Fatalf("unexpected error rendering pwa node: %v", err)
	}

	res := buf.String()
	if !strings.Contains(res, `meta name="theme-color" content="#4f46e5"`) {
		t.Errorf("expected theme color tag, got %q", res)
	}
	if !strings.Contains(res, `apple-mobile-web-app-title" content="TaskMaster"`) {
		t.Errorf("expected title tag, got %q", res)
	}
	if !strings.Contains(res, `link rel="apple-touch-icon" href="/icon-192.png"`) {
		t.Errorf("expected icon tag, got %q", res)
	}
	if !strings.Contains(res, `navigator.serviceWorker.register('/sw.js')`) {
		t.Errorf("expected service worker registration script, got %q", res)
	}
}

func TestHTMXTags(t *testing.T) {
	engine := NewEngine("")
	headTemplate := `|htmx src='/js/htmx.min.js' ext='json-enc' indicator=true|`

	var buf bytes.Buffer
	if err := engine.Render(&buf, headTemplate, nil); err != nil {
		t.Fatalf("unexpected error rendering htmx head node: %v", err)
	}

	res := buf.String()
	if !strings.Contains(res, `script src="/js/htmx.min.js"`) {
		t.Errorf("expected custom htmx src script tag, got %q", res)
	}
	if !strings.Contains(res, `dist/ext/json-enc.js`) {
		t.Errorf("expected extension script tag, got %q", res)
	}
	if !strings.Contains(res, `.htmx-indicator{display:none;}`) {
		t.Errorf("expected indicator css, got %q", res)
	}

	btnTemplate := `<button |htmx-get '/api/tasks' target='#task-list' swap='outerHTML'|>Refresh</button>`
	buf.Reset()
	if err := engine.Render(&buf, btnTemplate, nil); err != nil {
		t.Fatalf("unexpected error rendering htmx-get attr node: %v", err)
	}

	btnRes := buf.String()
	if !strings.Contains(btnRes, `hx-get="/api/tasks"`) || !strings.Contains(btnRes, `hx-target="#task-list"`) || !strings.Contains(btnRes, `hx-swap="outerHTML"`) {
		t.Errorf("expected hx attribute shorthand rendering, got %q", btnRes)
	}
}

func TestAlpineTags(t *testing.T) {
	engine := NewEngine("")
	headTemplate := `|reactive plugins='collapse,focus' cloak=true|`

	var buf bytes.Buffer
	if err := engine.Render(&buf, headTemplate, nil); err != nil {
		t.Fatalf("unexpected error rendering reactive head node: %v", err)
	}

	res := buf.String()
	if !strings.Contains(res, `cdn.jsdelivr.net/npm/@alpinejs/collapse`) {
		t.Errorf("expected collapse plugin script tag, got %q", res)
	}
	if !strings.Contains(res, `cdn.jsdelivr.net/npm/alpinejs`) {
		t.Errorf("expected alpine core script tag, got %q", res)
	}
	if !strings.Contains(res, `[x-cloak]{display:none !important;}`) {
		t.Errorf("expected x-cloak css rule, got %q", res)
	}

	// Verify alpinejs alias
	buf.Reset()
	if err := engine.Render(&buf, `|alpinejs|`, nil); err != nil {
		t.Fatalf("unexpected error rendering alpinejs alias: %v", err)
	}
	if !strings.Contains(buf.String(), `cdn.jsdelivr.net/npm/alpinejs`) {
		t.Errorf("expected alpine core script tag for alpinejs alias, got %q", buf.String())
	}

	stateTemplate := `<div |alpine-data open=false count=0 tab='home'|>`
	buf.Reset()
	if err := engine.Render(&buf, stateTemplate, nil); err != nil {
		t.Fatalf("unexpected error rendering state node: %v", err)
	}

	stateRes := buf.String()
	if !strings.Contains(stateRes, `x-data="{ count: 0, open: false, tab: 'home' }"`) {
		t.Errorf("expected x-data reactive state output, got %q", stateRes)
	}

	showTemplate := `<div |alpine-show 'open'| |alpine-cloak|>`
	buf.Reset()
	if err := engine.Render(&buf, showTemplate, nil); err != nil {
		t.Fatalf("unexpected error rendering alpine-show node: %v", err)
	}
	showRes := buf.String()
	if !strings.Contains(showRes, `x-show="open"`) || !strings.Contains(showRes, `x-cloak`) {
		t.Errorf("expected x-show and x-cloak rendering, got %q", showRes)
	}
}

func TestRangeForAndControlFlow(t *testing.T) {
	engine := NewEngine("")

	t.Run("ascending range default step", func(t *testing.T) {
		tmpl := `|for i from 1 to 5||i||separator|,|/separator||/for|`
		var buf bytes.Buffer
		if err := engine.Render(&buf, tmpl, nil); err != nil {
			t.Fatal(err)
		}
		if got := buf.String(); got != "1,2,3,4,5" {
			t.Errorf("expected '1,2,3,4,5', got %q", got)
		}
	})

	t.Run("ascending range custom step and reachable boundary", func(t *testing.T) {
		tmpl := `|for i from 1 to 5 step 2||i||separator|,|/separator||/for|`
		var buf bytes.Buffer
		if err := engine.Render(&buf, tmpl, nil); err != nil {
			t.Fatal(err)
		}
		if got := buf.String(); got != "1,3,5" {
			t.Errorf("expected '1,3,5', got %q", got)
		}
	})

	t.Run("ascending range unreachable boundary", func(t *testing.T) {
		tmpl := `|for i from 1 to 6 step 2||i||separator|,|/separator||/for|`
		var buf bytes.Buffer
		if err := engine.Render(&buf, tmpl, nil); err != nil {
			t.Fatal(err)
		}
		if got := buf.String(); got != "1,3,5" {
			t.Errorf("expected '1,3,5', got %q", got)
		}
	})

	t.Run("descending range custom step (10 to 1 step 2)", func(t *testing.T) {
		tmpl := `|for i from 10 to 1 step 2||i||separator|, |/separator||/for|`
		var buf bytes.Buffer
		if err := engine.Render(&buf, tmpl, nil); err != nil {
			t.Fatal(err)
		}
		if got := buf.String(); got != "10, 8, 6, 4, 2" {
			t.Errorf("expected '10, 8, 6, 4, 2', got %q", got)
		}
	})

	t.Run("descending range custom step (10 to 1 step 3)", func(t *testing.T) {
		tmpl := `|for i from 10 to 1 step 3||i||separator|, |/separator||/for|`
		var buf bytes.Buffer
		if err := engine.Render(&buf, tmpl, nil); err != nil {
			t.Fatal(err)
		}
		if got := buf.String(); got != "10, 7, 4, 1" {
			t.Errorf("expected '10, 7, 4, 1', got %q", got)
		}
	})

	t.Run("equal start and end", func(t *testing.T) {
		tmpl := `|for i from 5 to 5||i||/for|`
		var buf bytes.Buffer
		if err := engine.Render(&buf, tmpl, nil); err != nil {
			t.Fatal(err)
		}
		if got := buf.String(); got != "5" {
			t.Errorf("expected '5', got %q", got)
		}
	})

	t.Run("expression based bounds and step", func(t *testing.T) {
		tmpl := `|for i from start to items.size() - 1 step interval||i| |/for|`
		data := map[string]any{
			"start":    2,
			"items":    []int{10, 20, 30, 40, 50, 60, 70, 80},
			"interval": 2,
		}
		var buf bytes.Buffer
		if err := engine.Render(&buf, tmpl, data); err != nil {
			t.Fatal(err)
		}
		if got := strings.TrimSpace(buf.String()); got != "2 4 6" {
			t.Errorf("expected '2 4 6', got %q", got)
		}
	})

	t.Run("continue during first, middle, final iterations and content after continue", func(t *testing.T) {
		tmpl := `|for i from 1 to 5||if i == 1 or i == 3 or i == 5||continue|UNRENDERED|/if||i| |/for|`
		var buf bytes.Buffer
		if err := engine.Render(&buf, tmpl, nil); err != nil {
			t.Fatal(err)
		}
		if got := strings.TrimSpace(buf.String()); got != "2 4" {
			t.Errorf("expected '2 4', got %q", got)
		}
	})

	t.Run("break during first iteration and content after break", func(t *testing.T) {
		tmpl := `|for i from 1 to 5||if i == 1||break|UNRENDERED|/if||i| |/for|`
		var buf bytes.Buffer
		if err := engine.Render(&buf, tmpl, nil); err != nil {
			t.Fatal(err)
		}
		if got := buf.String(); got != "" {
			t.Errorf("expected empty string, got %q", got)
		}
	})

	t.Run("break during middle iteration (6)", func(t *testing.T) {
		tmpl := `|for i from 1 to 10||if i == 6||break||/if||i| |/for|`
		var buf bytes.Buffer
		if err := engine.Render(&buf, tmpl, nil); err != nil {
			t.Fatal(err)
		}
		if got := strings.TrimSpace(buf.String()); got != "1 2 3 4 5" {
			t.Errorf("expected '1 2 3 4 5', got %q", got)
		}
	})

	t.Run("break during final iteration", func(t *testing.T) {
		tmpl := `|for i from 1 to 3||if i == 3||break||/if||i| |/for|`
		var buf bytes.Buffer
		if err := engine.Render(&buf, tmpl, nil); err != nil {
			t.Fatal(err)
		}
		if got := strings.TrimSpace(buf.String()); got != "1 2" {
			t.Errorf("expected '1 2', got %q", got)
		}
	})

	t.Run("nested loop break and continue isolation", func(t *testing.T) {
		tmpl := `|for i from 1 to 3|Outer |i|: [|for j from 1 to 5||if j == 2||continue||/if||if j == 4||break||/if||j| |/for|] |/for|`
		var buf bytes.Buffer
		if err := engine.Render(&buf, tmpl, nil); err != nil {
			t.Fatal(err)
		}
		expected := "Outer 1: [1 3 ] Outer 2: [1 3 ] Outer 3: [1 3 ] "
		if got := buf.String(); got != expected {
			t.Errorf("expected %q, got %q", expected, got)
		}
	})

	t.Run("loop variable scoping and non-leakage", func(t *testing.T) {
		tmpl := `Outside before: '|i|' |for i from 1 to 3||i||else|Else: '|i|'|/for| Outside after: '|i|'`
		data := map[string]any{"i": "outer"}
		var buf bytes.Buffer
		if err := engine.Render(&buf, tmpl, data); err != nil {
			t.Fatal(err)
		}
		expected := "Outside before: 'outer' 123 Outside after: 'outer'"
		if got := buf.String(); got != expected {
			t.Errorf("expected %q, got %q", expected, got)
		}
	})

	t.Run("for else block - not rendered when iterations executed", func(t *testing.T) {
		tmpl := `|for i from 1 to 3||i||else|Empty|/for|`
		var buf bytes.Buffer
		if err := engine.Render(&buf, tmpl, nil); err != nil {
			t.Fatal(err)
		}
		if got := buf.String(); got != "123" {
			t.Errorf("expected '123', got %q", got)
		}
	})

	t.Run("for else block - not rendered when loop broke after iteration started", func(t *testing.T) {
		tmpl := `|for i from 1 to 5||if i == 1||break||/if||else|Empty|/for|`
		var buf bytes.Buffer
		if err := engine.Render(&buf, tmpl, nil); err != nil {
			t.Fatal(err)
		}
		if got := buf.String(); got != "" {
			t.Errorf("expected empty string, got %q", got)
		}
	})

	t.Run("for else block - not rendered when all iterations continued", func(t *testing.T) {
		tmpl := `|for i from 1 to 3||continue||else|Empty|/for|`
		var buf bytes.Buffer
		if err := engine.Render(&buf, tmpl, nil); err != nil {
			t.Fatal(err)
		}
		if got := buf.String(); got != "" {
			t.Errorf("expected empty string, got %q", got)
		}
	})

	t.Run("each with non-empty collection", func(t *testing.T) {
		tmpl := `|each item in items||item||else|No items|/each|`
		var buf bytes.Buffer
		if err := engine.Render(&buf, tmpl, map[string]any{"items": []int{10, 20}}); err != nil {
			t.Fatal(err)
		}
		if got := buf.String(); got != "1020" {
			t.Errorf("expected '1020', got %q", got)
		}
	})

	t.Run("each with empty collection renders else", func(t *testing.T) {
		tmpl := `|each item in items||item||else|No items|/each|`
		var buf bytes.Buffer
		if err := engine.Render(&buf, tmpl, map[string]any{"items": []int{}}); err != nil {
			t.Fatal(err)
		}
		if got := buf.String(); got != "No items" {
			t.Errorf("expected 'No items', got %q", got)
		}
	})

	t.Run("each with null collection renders else", func(t *testing.T) {
		tmpl := `|each item in items||item||else|No items|/each|`
		var buf bytes.Buffer
		if err := engine.Render(&buf, tmpl, map[string]any{"items": nil}); err != nil {
			t.Fatal(err)
		}
		if got := buf.String(); got != "No items" {
			t.Errorf("expected 'No items', got %q", got)
		}
	})

	t.Run("each with break after at least one iteration does not render else", func(t *testing.T) {
		tmpl := `|each item in items||if item == 2||break||/if||item||else|No items|/each|`
		var buf bytes.Buffer
		if err := engine.Render(&buf, tmpl, map[string]any{"items": []int{1, 2, 3}}); err != nil {
			t.Fatal(err)
		}
		if got := buf.String(); got != "1" {
			t.Errorf("expected '1', got %q", got)
		}
	})

	t.Run("each with continue for every item does not render else", func(t *testing.T) {
		tmpl := `|each item in items||continue||else|No items|/each|`
		var buf bytes.Buffer
		if err := engine.Render(&buf, tmpl, map[string]any{"items": []int{1, 2, 3}}); err != nil {
			t.Fatal(err)
		}
		if got := buf.String(); got != "" {
			t.Errorf("expected empty string, got %q", got)
		}
	})
}

func TestRangeForAndControlFlowErrors(t *testing.T) {
	engine := NewEngine("")

	tests := []struct {
		name        string
		template    string
		data        map[string]any
		errContains string
	}{
		{
			name:        "zero step error",
			template:    `|for i from 1 to 5 step 0||i||/for|`,
			errContains: "zero or negative step",
		},
		{
			name:        "negative step error",
			template:    `|for i from 1 to 5 step -1||i||/for|`,
			errContains: "zero or negative step",
		},
		{
			name:        "continue outside loop",
			template:    `|if true||continue||/if|`,
			errContains: "continue| outside a loop",
		},
		{
			name:        "break outside loop",
			template:    `|if true||break||/if|`,
			errContains: "break| outside a loop",
		},
		{
			name:        "else outside for or each",
			template:    `|else|`,
			errContains: "outside for",
		},
		{
			name:        "multiple else in for loop",
			template:    `|for i from 1 to 5||i||else|E1|else|E2|/for|`,
			errContains: "multiple |else| blocks",
		},
		{
			name:        "multiple else in each loop",
			template:    `|each item in items||item||else|E1|else|E2|/each|`,
			errContains: "multiple |else| blocks",
		},
		{
			name:        "missing loop variable",
			template:    `|for from 1 to 5||/for|`,
			errContains: "missing loop variable",
		},
		{
			name:        "missing from",
			template:    `|for i 1 to 5||/for|`,
			errContains: "missing from",
		},
		{
			name:        "missing to",
			template:    `|for i from 1 5||/for|`,
			errContains: "missing to",
		},
		{
			name:        "missing closing for",
			template:    `|for i from 1 to 5||i|`,
			errContains: "missing closing |/for|",
		},
		{
			name:        "invalid start expression",
			template:    `|for i from invalidVar to 5||i||/for|`,
			data:        map[string]any{},
			errContains: "invalid start expression",
		},
		{
			name:        "misplaced loop directive",
			template:    `|for i from 1 to 5||i||/each|`,
			errContains: "misplaced loop or block directive",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			err := engine.Render(&buf, tt.template, tt.data)
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tt.errContains)
			}
			if !strings.Contains(err.Error(), tt.errContains) {
				t.Errorf("expected error containing %q, got %v", tt.errContains, err)
			}
		})
	}
}

func TestSwitchSemanticsValid(t *testing.T) {
	engine := NewEngine("")

	tests := []struct {
		name     string
		template string
		data     map[string]any
		expected string
	}{
		{
			name: "1. First string case matches",
			template: `|switch role|
    |case 'admin'|Administrator
    |case 'manager'|Manager
    |default|User
|/switch|`,
			data:     map[string]any{"role": "admin"},
			expected: "Administrator",
		},
		{
			name: "2. Middle string case matches",
			template: `|switch role|
    |case 'admin'|Administrator
    |case 'manager'|Manager
    |case 'user'|Regular User
    |default|Guest
|/switch|`,
			data:     map[string]any{"role": "manager"},
			expected: "Manager",
		},
		{
			name: "3. Last string case matches",
			template: `|switch role|
    |case 'admin'|Administrator
    |case 'manager'|Manager
    |case 'user'|Regular User
|/switch|`,
			data:     map[string]any{"role": "user"},
			expected: "Regular User",
		},
		{
			name: "4. No case matches and default renders",
			template: `|switch role|
    |case 'admin'|Administrator
    |case 'manager'|Manager
    |default|Guest
|/switch|`,
			data:     map[string]any{"role": "unknown"},
			expected: "Guest",
		},
		{
			name: "5. No case matches and no default renders nothing",
			template: `|switch role|
    |case 'admin'|Administrator
    |case 'manager'|Manager
|/switch|`,
			data:     map[string]any{"role": "unknown"},
			expected: "",
		},
		{
			name: "6. Integer case comparison",
			template: `|switch status|
    |case 200|OK
    |case 404|Not Found
    |case 500|Error
|/switch|`,
			data:     map[string]any{"status": 200},
			expected: "OK",
		},
		{
			name: "7. Floating-point-compatible numeric comparison",
			template: `|switch score|
    |case 95.5|A+
    |case 80.0|B
|/switch|`,
			data:     map[string]any{"score": 95.5},
			expected: "A+",
		},
		{
			name: "8. Boolean case comparison",
			template: `|switch active|
    |case true|Active User
    |case false|Inactive User
|/switch|`,
			data:     map[string]any{"active": true},
			expected: "Active User",
		},
		{
			name: "9. Variable expression used as a case value",
			template: `|switch inputRole|
    |case targetRole|Matched Target
    |case 'other'|Other
|/switch|`,
			data:     map[string]any{"inputRole": "editor", "targetRole": "editor"},
			expected: "Matched Target",
		},
		{
			name: "10. Automatic break prevents later clauses from rendering",
			template: `|switch role|
    |case 'admin'|Administrator
    |case 'manager'|Manager
    |default|User
|/switch|`,
			data:     map[string]any{"role": "admin"},
			expected: "Administrator",
		},
		{
			name: "11. Single fallthrough",
			template: `|switch role|
    |case 'admin'|Administrator
        |fallthrough|
    |case 'manager'|Reports
    |default|Regular user
|/switch|`,
			data:     map[string]any{"role": "admin"},
			expected: "Administrator Reports",
		},
		{
			name: "12. Multiple chained fallthroughs",
			template: `|switch step|
    |case 1|One
        |fallthrough|
    |case 2|Two
        |fallthrough|
    |case 3|Three
|/switch|`,
			data:     map[string]any{"step": 1},
			expected: "One Two Three",
		},
		{
			name: "13. Fallthrough into default",
			template: `|switch role|
    |case 'admin'|Admin
        |fallthrough|
    |default|Default Handling
|/switch|`,
			data:     map[string]any{"role": "admin"},
			expected: "Admin Default Handling",
		},
		{
			name: "14. Default located before another case, if clause ordering permits it",
			template: `|switch role|
    |default|Default Option
    |case 'admin'|Admin Option
|/switch|`,
			data:     map[string]any{"role": "admin"},
			expected: "Admin Option",
		},
		{
			name: "15. Fallthrough from a non-final default into the next case",
			template: `|switch role|
    |default|Fallback
        |fallthrough|
    |case 'admin'|Admin
|/switch|`,
			data:     map[string]any{"role": "guest"},
			expected: "Fallback Admin",
		},
		{
			name: "16. Nested if/else inside a case",
			template: `|switch role|
    |case 'admin'|
        |if active|Active Admin|else|Inactive Admin|/if|
|/switch|`,
			data:     map[string]any{"role": "admin", "active": true},
			expected: "Active Admin",
		},
		{
			name: "17. Nested switch inside a case",
			template: `|switch outer|
    |case 'user'|User: |switch inner|
        |case 'a'|Type A
        |case 'b'|Type B
    |/switch|
|/switch|`,
			data:     map[string]any{"outer": "user", "inner": "b"},
			expected: "User: Type B",
		},
		{
			name: "18. Inner-switch fallthrough does not affect the outer switch",
			template: `|switch outer|
    |case 'first'|Outer1: |switch inner|
        |case 'a'|InnerA
            |fallthrough|
        |case 'b'|InnerB
    |/switch|
    |case 'second'|Outer2
|/switch|`,
			data:     map[string]any{"outer": "first", "inner": "a"},
			expected: "Outer1: InnerA InnerB",
		},
		{
			name: "19. Outer-switch fallthrough does not corrupt nested-switch state",
			template: `|switch outer|
    |case 'first'|Outer1: |switch inner|
        |case 'a'|InnerA
    |/switch|
        |fallthrough|
    |case 'second'|Outer2
|/switch|`,
			data:     map[string]any{"outer": "first", "inner": "a"},
			expected: "Outer1: InnerA Outer2",
		},
		{
			name: "20. HTML escaping inside rendered case bodies remains correct",
			template: `|switch role|
    |case 'user'|<span>|name|</span>
|/switch|`,
			data:     map[string]any{"role": "user", "name": "<script>alert('xss')</script>"},
			expected: "<span>&lt;script&gt;alert(&#039;xss&#039;)&lt;/script&gt;</span>",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			if err := engine.Render(&buf, tt.template, tt.data); err != nil {
				t.Fatalf("unexpected rendering error: %v", err)
			}
			got := strings.TrimSpace(buf.String())
			gotNorm := strings.Join(strings.Fields(got), " ")
			expNorm := strings.Join(strings.Fields(tt.expected), " ")
			if gotNorm != expNorm {
				t.Errorf("expected %q, got %q", expNorm, gotNorm)
			}
		})
	}
}

func TestSwitchSemanticsInvalid(t *testing.T) {
	engine := NewEngine("")

	tests := []struct {
		name        string
		template    string
		errContains string
	}{
		{
			name:        "1. Empty switch expression",
			template:    `|switch ||default|Unknown|/switch|`,
			errContains: "switch expression must not be empty",
		},
		{
			name:        "2. Empty case expression",
			template:    `|switch role||case |Invalid|/switch|`,
			errContains: "case expression must not be empty",
		},
		{
			name:        "3. Duplicate default clauses",
			template:    `|switch role||default|First|default|Second|/switch|`,
			errContains: "switch cannot contain more than one default clause",
		},
		{
			name:        "4. Fallthrough outside a switch",
			template:    `<p>Before</p>|fallthrough|<p>After</p>`,
			errContains: "fallthrough is only allowed as the final directive of a switch clause",
		},
		{
			name:        "5. Fallthrough before the first case",
			template:    `|switch role||fallthrough||case 'admin'|Administrator|/switch|`,
			errContains: "unexpected content before first switch clause",
		},
		{
			name:        "6. Fallthrough nested inside if",
			template:    `|switch role||case 'admin'||if active||fallthrough||/if||/switch|`,
			errContains: "fallthrough is only allowed as the final directive of a switch clause",
		},
		{
			name:        "7. Fallthrough nested inside a loop",
			template:    `|switch role||case 'admin'||for i from 1 to 3||fallthrough||/for||/switch|`,
			errContains: "fallthrough is only allowed as the final directive of a switch clause",
		},
		{
			name:        "8. Rendered content following fallthrough",
			template:    `|switch role||case 'admin'|Administrator|fallthrough|This content is unreachable.|case 'manager'|Manager|/switch|`,
			errContains: "fallthrough is only allowed as the final directive of a switch clause",
		},
		{
			name:        "9. A second fallthrough in the same clause",
			template:    `|switch role||case 'admin'|Administrator|fallthrough||fallthrough||case 'manager'|Manager|/switch|`,
			errContains: "fallthrough is only allowed as the final directive of a switch clause",
		},
		{
			name:        "10. Fallthrough in the final case",
			template:    `|switch role||case 'admin'|Administrator|fallthrough||/switch|`,
			errContains: "fallthrough cannot appear in the final switch clause",
		},
		{
			name:        "11. Fallthrough in the final default",
			template:    `|switch role||case 'admin'|Admin|default|Guest|fallthrough||/switch|`,
			errContains: "fallthrough cannot appear in the final switch clause",
		},
		{
			name:        "12. Unexpected content before the first clause",
			template:    `|switch role|This content must not disappear silently.|case 'admin'|Administrator|/switch|`,
			errContains: "unexpected content before first switch clause",
		},
		{
			name:        "13. Case outside a switch",
			template:    `|case 'admin'|Administrator`,
			errContains: "misplaced |case",
		},
		{
			name:        "14. Default outside a switch",
			template:    `|default|Default User`,
			errContains: "misplaced |default| directive",
		},
		{
			name:        "15. Missing |/switch|",
			template:    `|switch role||case 'admin'|Administrator`,
			errContains: "missing closing |/switch|",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			err := engine.Render(&buf, tt.template, nil)
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tt.errContains)
			}
			if !strings.Contains(err.Error(), tt.errContains) {
				t.Errorf("expected error containing %q, got %v", tt.errContains, err)
			}
		})
	}
}
