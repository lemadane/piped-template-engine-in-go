package pte

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
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
	if !strings.Contains(res, `navigator.serviceWorker.register("/sw.js")`) {
		t.Errorf("expected service worker registration script, got %q", res)
	}

	// Test attribute aliases: theme-color, status-color, service-worker, title, touch-icon
	aliasTemplate := `|pwa title='AliasApp' theme-color='#ff0000' touch-icon='/touch.png' service-worker='/sw-custom.js' status-color='black-translucent'|`
	buf.Reset()
	if err := engine.Render(&buf, aliasTemplate, nil); err != nil {
		t.Fatalf("unexpected error rendering pwa node with aliases: %v", err)
	}

	resAlias := buf.String()
	if !strings.Contains(resAlias, `meta name="theme-color" content="#ff0000"`) {
		t.Errorf("expected theme-color alias tag, got %q", resAlias)
	}
	if !strings.Contains(resAlias, `apple-mobile-web-app-title" content="AliasApp"`) {
		t.Errorf("expected title alias tag, got %q", resAlias)
	}
	if !strings.Contains(resAlias, `apple-mobile-web-app-status-bar-style" content="black-translucent"`) {
		t.Errorf("expected status-color alias tag, got %q", resAlias)
	}
	if !strings.Contains(resAlias, `link rel="apple-touch-icon" href="/touch.png"`) {
		t.Errorf("expected touch-icon alias tag, got %q", resAlias)
	}
	if !strings.Contains(resAlias, `navigator.serviceWorker.register("/sw-custom.js")`) {
		t.Errorf("expected service-worker alias script, got %q", resAlias)
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

func TestAlpineSetup(testingInstance *testing.T) {
	templateEngine := NewEngine("")

	setupTestCases := []struct {
		caseName         string
		templateString   string
		expectedOutput   string
		errorSubstring   string
		checkScriptOrder bool
	}{
		{
			caseName:       "Default setup with alias alpine",
			templateString: `|alpine|`,
			expectedOutput: `<script defer src="https://cdn.jsdelivr.net/npm/alpinejs@3.14.8/dist/cdn.min.js"></script>`,
		},
		{
			caseName:       "Default setup with alias alpinejs",
			templateString: `|alpinejs|`,
			expectedOutput: `<script defer src="https://cdn.jsdelivr.net/npm/alpinejs@3.14.8/dist/cdn.min.js"></script>`,
		},
		{
			caseName:       "Default setup with alias reactive",
			templateString: `|reactive|`,
			expectedOutput: `<script defer src="https://cdn.jsdelivr.net/npm/alpinejs@3.14.8/dist/cdn.min.js"></script>`,
		},
		{
			caseName:       "Explicit pinned version",
			templateString: `|alpine version='3.12.0'|`,
			expectedOutput: `<script defer src="https://cdn.jsdelivr.net/npm/alpinejs@3.12.0/dist/cdn.min.js"></script>`,
		},
		{
			caseName:       "CSP build option",
			templateString: `|alpine build='csp' version='3.14.8'|`,
			expectedOutput: `<script defer src="https://cdn.jsdelivr.net/npm/@alpinejs/csp@3.14.8/dist/cdn.min.js"></script>`,
		},
		{
			caseName:       "Custom source URL",
			templateString: `|alpine src='https://example.com/custom-alpine.js'|`,
			expectedOutput: `<script defer src="https://example.com/custom-alpine.js"></script>`,
		},
		{
			caseName:         "Plugins loaded before core",
			templateString:   `|alpine plugins='collapse,focus'|`,
			checkScriptOrder: true,
		},
		{
			caseName:       "Cloak enabled",
			templateString: `|alpine cloak=true|`,
			expectedOutput: `<style>[x-cloak]{display:none !important;}</style>`,
		},
		{
			caseName:       "Cloak disabled",
			templateString: `|alpine cloak=false|`,
		},
		{
			caseName:       "Invalid cloak value",
			templateString: `|alpine cloak=maybe|`,
			errorSubstring: `invalid Alpine option "cloak": expected true, false, 1, or 0, received "maybe"`,
		},
		{
			caseName:       "Unknown setup option",
			templateString: `|alpine unknown=value|`,
			errorSubstring: `invalid Alpine setup option "unknown"`,
		},
		{
			caseName:       "Duplicate setup option",
			templateString: `|alpine cloak=true cloak=false|`,
			errorSubstring: `duplicate property "cloak"`,
		},
		{
			caseName:       "Invalid version string",
			templateString: `|alpine version='3.14<script>'|`,
			errorSubstring: `contains illegal characters`,
		},
		{
			caseName:       "Invalid source URL",
			templateString: `|alpine src='javascript:alert(1)'|`,
			errorSubstring: `must use http, https, or relative path`,
		},
		{
			caseName:       "Unterminated quoted source",
			templateString: `|alpine src='https://example.com/alpine.js|`,
			errorSubstring: `unterminated quote`,
		},
		{
			caseName:       "Unknown plugin name",
			templateString: `|alpine plugins='collaps'|`,
			errorSubstring: `unknown Alpine plugin "collaps"`,
		},
		{
			caseName:       "Duplicate plugin name",
			templateString: `|alpine plugins='collapse,collapse'|`,
			errorSubstring: `duplicate Alpine plugin "collapse"`,
		},
	}

	for _, currentTestCase := range setupTestCases {
		testingInstance.Run(currentTestCase.caseName, func(subTestingInstance *testing.T) {
			outputBuffer := &bytes.Buffer{}
			renderErr := templateEngine.Render(outputBuffer, currentTestCase.templateString, nil)

			if currentTestCase.errorSubstring != "" {
				if renderErr == nil {
					subTestingInstance.Fatalf("expected error containing %q, got nil", currentTestCase.errorSubstring)
				}
				if !strings.Contains(renderErr.Error(), currentTestCase.errorSubstring) {
					subTestingInstance.Errorf("expected error containing %q, got %v", currentTestCase.errorSubstring, renderErr)
				}
				return
			}

			if renderErr != nil {
				subTestingInstance.Fatalf("unexpected rendering error: %v", renderErr)
			}

			renderedOutput := outputBuffer.String()
			if currentTestCase.expectedOutput != "" && !strings.Contains(renderedOutput, currentTestCase.expectedOutput) {
				subTestingInstance.Errorf("expected output to contain %q, got %q", currentTestCase.expectedOutput, renderedOutput)
			}

			if currentTestCase.checkScriptOrder {
				collapseIndex := strings.Index(renderedOutput, "@alpinejs/collapse")
				focusIndex := strings.Index(renderedOutput, "@alpinejs/focus")
				coreIndex := strings.Index(renderedOutput, "alpinejs@3.14.8")

				if collapseIndex == -1 || focusIndex == -1 || coreIndex == -1 {
					subTestingInstance.Fatalf("missing expected plugin or core scripts in %q", renderedOutput)
				}
				if !(collapseIndex < coreIndex && focusIndex < coreIndex) {
					subTestingInstance.Errorf("expected plugin script tags before core script tag in %q", renderedOutput)
				}
			}
		})
	}
}

func TestAlpineState(testingInstance *testing.T) {
	templateEngine := NewEngine("")

	stateTestCases := []struct {
		caseName       string
		templateString string
		expectedValues map[string]any
		errorSubstring string
	}{
		{
			caseName:       "Normal string",
			templateString: `<div |alpine-data message='Hello World'|></div>`,
			expectedValues: map[string]any{"message": "Hello World"},
		},
		{
			caseName:       "String containing apostrophe",
			templateString: `<div |alpine-data message="It's ready"|></div>`,
			expectedValues: map[string]any{"message": "It's ready"},
		},
		{
			caseName:       "String containing double quotes",
			templateString: `<div |alpine-data message='Say "Hello"'|></div>`,
			expectedValues: map[string]any{"message": `Say "Hello"`},
		},
		{
			caseName:       "String containing both quote types",
			templateString: `<div |alpine-data message='It\'s "ready"'|></div>`,
			expectedValues: map[string]any{"message": `It's "ready"`},
		},
		{
			caseName:       "Backslashes and newlines",
			templateString: `<div |alpine-data path='C:\\Windows\\System32'|></div>`,
			expectedValues: map[string]any{"path": `C:\Windows\System32`},
		},
		{
			caseName:       "Unicode characters",
			templateString: `<div |alpine-data user='Ada 👩‍💻' greeting='你好'|></div>`,
			expectedValues: map[string]any{"user": "Ada 👩‍💻", "greeting": "你好"},
		},
		{
			caseName:       "HTML-significant characters",
			templateString: `<div |alpine-data markup='<div>Content & More</div>'|></div>`,
			expectedValues: map[string]any{"markup": "<div>Content & More</div>"},
		},
		{
			caseName:       "Boolean values",
			templateString: `<div |alpine-data active=true disabled=false|></div>`,
			expectedValues: map[string]any{"active": true, "disabled": false},
		},
		{
			caseName:       "Integer values",
			templateString: `<div |alpine-data count=25 step=-5|></div>`,
			expectedValues: map[string]any{"count": float64(25), "step": float64(-5)},
		},
		{
			caseName:       "Decimal values",
			templateString: `<div |alpine-data price=19.95 score=95.5|></div>`,
			expectedValues: map[string]any{"price": 19.95, "score": 95.5},
		},
		{
			caseName:       "Null value",
			templateString: `<div |alpine-data optional=null|></div>`,
			expectedValues: map[string]any{"optional": nil},
		},
		{
			caseName:       "JSON Array",
			templateString: `<div |alpine-data items='["Rice","Coffee"]'|></div>`,
			expectedValues: map[string]any{"items": []any{"Rice", "Coffee"}},
		},
		{
			caseName:       "JSON Object",
			templateString: `<div |alpine-data profile='{"name":"Lemuel","active":true}'|></div>`,
			expectedValues: map[string]any{"profile": map[string]any{"name": "Lemuel", "active": true}},
		},
		{
			caseName:       "Property name with hyphen",
			templateString: `<div |alpine-data user-name='Ada'|></div>`,
			expectedValues: map[string]any{"user-name": "Ada"},
		},
		{
			caseName:       "Invalid number 1.2.3",
			templateString: `<div |alpine-data count=1.2.3|></div>`,
			errorSubstring: `invalid Alpine state value for "count": "1.2.3" is not a valid number`,
		},
		{
			caseName:       "Invalid number --",
			templateString: `<div |alpine-data count=--|></div>`,
			errorSubstring: `invalid Alpine state value for "count": "--" is not a valid number`,
		},
		{
			caseName:       "Invalid number NaN",
			templateString: `<div |alpine-data count=NaN|></div>`,
			errorSubstring: `invalid Alpine state value for "count": "NaN" is not a valid number`,
		},
		{
			caseName:       "Malformed JSON array",
			templateString: `<div |alpine-data items='[1,'|></div>`,
			errorSubstring: `invalid Alpine state value for "items": malformed JSON array`,
		},
		{
			caseName:       "Malformed JSON object",
			templateString: `<div |alpine-data profile='{name:'|></div>`,
			errorSubstring: `invalid Alpine state value for "profile": malformed JSON object`,
		},
		{
			caseName:       "Duplicate properties",
			templateString: `<div |alpine-data count=1 count=2|></div>`,
			errorSubstring: `duplicate property "count"`,
		},
		{
			caseName:       "Unterminated quote",
			templateString: `<div |alpine-data message='unterminated|></div>`,
			errorSubstring: `unterminated quote`,
		},
		{
			caseName:       "Missing property name",
			templateString: `<div |alpine-data =value|></div>`,
			errorSubstring: `missing property name`,
		},
		{
			caseName:       "Missing value",
			templateString: `<div |alpine-data count=|></div>`,
			errorSubstring: `missing value`,
		},
	}

	for _, currentTestCase := range stateTestCases {
		testingInstance.Run(currentTestCase.caseName, func(subTestingInstance *testing.T) {
			outputBuffer := &bytes.Buffer{}
			renderErr := templateEngine.Render(outputBuffer, currentTestCase.templateString, nil)

			if currentTestCase.errorSubstring != "" {
				if renderErr == nil {
					subTestingInstance.Fatalf("expected error containing %q, got nil", currentTestCase.errorSubstring)
				}
				if !strings.Contains(renderErr.Error(), currentTestCase.errorSubstring) {
					subTestingInstance.Errorf("expected error containing %q, got %v", currentTestCase.errorSubstring, renderErr)
				}
				return
			}

			if renderErr != nil {
				subTestingInstance.Fatalf("unexpected rendering error: %v", renderErr)
			}

			renderedOutput := outputBuffer.String()
			attributeStartIndex := strings.Index(renderedOutput, `x-data="`)
			if attributeStartIndex == -1 {
				subTestingInstance.Fatalf("x-data attribute missing in %q", renderedOutput)
			}

			quoteStartIndex := attributeStartIndex + len(`x-data="`)
			quoteEndIndex := strings.Index(renderedOutput[quoteStartIndex:], `"`)
			if quoteEndIndex == -1 {
				subTestingInstance.Fatalf("unclosed x-data attribute quote in %q", renderedOutput)
			}

			rawAttributeContent := renderedOutput[quoteStartIndex : quoteStartIndex+quoteEndIndex]
			unescapedJSON := strings.ReplaceAll(rawAttributeContent, "&quot;", `"`)

			var decodedStateMap map[string]any
			if unmarshalErr := json.Unmarshal([]byte(unescapedJSON), &decodedStateMap); unmarshalErr != nil {
				subTestingInstance.Fatalf("decoded x-data is not valid JSON %q: %v", unescapedJSON, unmarshalErr)
			}

			for expectedKey, expectedVal := range currentTestCase.expectedValues {
				actualVal, keyExists := decodedStateMap[expectedKey]
				if !keyExists {
					subTestingInstance.Errorf("expected key %q missing from decoded x-data %q", expectedKey, unescapedJSON)
					continue
				}
				if !reflect.DeepEqual(actualVal, expectedVal) {
					subTestingInstance.Errorf("key %q: expected %#v (type %T), got %#v (type %T)", expectedKey, expectedVal, expectedVal, actualVal, actualVal)
				}
			}
		})
	}
}

func TestAlpineDirectives(testingInstance *testing.T) {
	templateEngine := NewEngine("")

	directiveTestCases := []struct {
		caseName       string
		templateString string
		expectedOutput string
		errorSubstring string
	}{
		{
			caseName:       "alpine-show valid",
			templateString: `<div |alpine-show 'isOpen'|></div>`,
			expectedOutput: `x-show="isOpen"`,
		},
		{
			caseName:       "alpine-show missing expression",
			templateString: `<div |alpine-show|></div>`,
			errorSubstring: `Alpine directive "alpine-show" requires an expression`,
		},
		{
			caseName:       "alpine-cloak valid",
			templateString: `<div |alpine-cloak|></div>`,
			expectedOutput: `x-cloak`,
		},
		{
			caseName:       "alpine-cloak with value error",
			templateString: `<div |alpine-cloak 'unexpected'|></div>`,
			errorSubstring: `Alpine directive "alpine-cloak" does not accept a value`,
		},
		{
			caseName:       "alpine-text valid",
			templateString: `<span |alpine-text 'message'|></span>`,
			expectedOutput: `x-text="message"`,
		},
		{
			caseName:       "alpine-html valid",
			templateString: `<div |alpine-html 'trustedMarkup'|></div>`,
			expectedOutput: `x-html="trustedMarkup"`,
		},
		{
			caseName:       "alpine-model valid",
			templateString: `<input |alpine-model 'userQuery'|>`,
			expectedOutput: `x-model="userQuery"`,
		},
		{
			caseName:       "alpine-model missing expression",
			templateString: `<input |alpine-model|>`,
			errorSubstring: `Alpine directive "alpine-model" requires an expression`,
		},
		{
			caseName:       "Misspelled directive alpine-shwo",
			templateString: `<div |alpine-shwo 'open'|></div>`,
			errorSubstring: `unknown Alpine directive "alpine-shwo"; did you mean "alpine-show"?`,
		},
		{
			caseName:       "Valid modifier alpine-show.important",
			templateString: `<div |alpine-show.important 'open'|></div>`,
			expectedOutput: `x-show.important="open"`,
		},
		{
			caseName:       "Valid modifier alpine-model.debounce.500ms",
			templateString: `<input |alpine-model.debounce.500ms 'query'|>`,
			expectedOutput: `x-model.debounce.500ms="query"`,
		},
		{
			caseName:       "Combined HTMX and Alpine",
			templateString: `<button |alpine-show 'open'| |htmx-get '/api/tasks'|>Click</button>`,
			expectedOutput: `x-show="open" hx-get="/api/tasks"`,
		},
		{
			caseName:       "Pure @click alongside abstracted directive",
			templateString: `<button @click="open = !open" |alpine-show 'open'|>Toggle</button>`,
			expectedOutput: `<button @click="open = !open" x-show="open">Toggle</button>`,
		},
	}

	for _, currentTestCase := range directiveTestCases {
		testingInstance.Run(currentTestCase.caseName, func(subTestingInstance *testing.T) {
			outputBuffer := &bytes.Buffer{}
			renderErr := templateEngine.Render(outputBuffer, currentTestCase.templateString, nil)

			if currentTestCase.errorSubstring != "" {
				if renderErr == nil {
					subTestingInstance.Fatalf("expected error containing %q, got nil", currentTestCase.errorSubstring)
				}
				if !strings.Contains(renderErr.Error(), currentTestCase.errorSubstring) {
					subTestingInstance.Errorf("expected error containing %q, got %v", currentTestCase.errorSubstring, renderErr)
				}
				return
			}

			if renderErr != nil {
				subTestingInstance.Fatalf("unexpected rendering error: %v", renderErr)
			}

			renderedOutput := outputBuffer.String()
			if currentTestCase.expectedOutput != "" && !strings.Contains(renderedOutput, currentTestCase.expectedOutput) {
				subTestingInstance.Errorf("expected output to contain %q, got %q", currentTestCase.expectedOutput, renderedOutput)
			}
		})
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

func TestIssue1Regression(t *testing.T) {
	t.Run("Issue 1: Template-root directory traversal", func(t *testing.T) {
		tempDir := t.TempDir()
		root := filepath.Join(tempDir, "templates")
		privRoot := filepath.Join(tempDir, "templates-private")

		if err := os.MkdirAll(root, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(privRoot, 0755); err != nil {
			t.Fatal(err)
		}

		if err := os.WriteFile(filepath.Join(privRoot, "secret.pte"), []byte("SECRET_DATA"), 0644); err != nil {
			t.Fatal(err)
		}

		engine := NewEngine(root)
		var buf bytes.Buffer
		err := engine.Render(&buf, "../templates-private/secret", nil)
		if err == nil {
			t.Fatalf("expected directory traversal error, got output %q", buf.String())
		}
		if !strings.Contains(err.Error(), "must not escape template root") {
			t.Errorf("expected escape template root error, got %v", err)
		}
	})
}

func TestSymlinkAndPathTraversalRegression(t *testing.T) {
	supportsSymlinks := func(t *testing.T, tempDir string) bool {
		oldPath := filepath.Join(tempDir, "target")
		newPath := filepath.Join(tempDir, "symlink")
		_ = os.WriteFile(oldPath, []byte("test"), 0644)
		err := os.Symlink(oldPath, newPath)
		return err == nil
	}

	t.Run("1. Direct parent traversal", func(t *testing.T) {
		tempDir := t.TempDir()
		root := filepath.Join(tempDir, "templates")
		priv := filepath.Join(tempDir, "private")
		_ = os.MkdirAll(root, 0755)
		_ = os.MkdirAll(priv, 0755)
		_ = os.WriteFile(filepath.Join(priv, "secret.pte"), []byte("SECRET"), 0644)

		engine := NewEngine(root)
		var buf bytes.Buffer
		err := engine.Render(&buf, "../private/secret", nil)
		if err == nil {
			t.Fatalf("expected error for direct parent traversal, got nil")
		}
		if !strings.Contains(err.Error(), "must not escape template root") {
			t.Errorf("expected escape template root error, got: %v", err)
		}
	})

	t.Run("2. Prefix-sibling traversal", func(t *testing.T) {
		tempDir := t.TempDir()
		root := filepath.Join(tempDir, "templates")
		privRoot := filepath.Join(tempDir, "templates-private")
		_ = os.MkdirAll(root, 0755)
		_ = os.MkdirAll(privRoot, 0755)
		_ = os.WriteFile(filepath.Join(privRoot, "secret.pte"), []byte("SECRET"), 0644)

		engine := NewEngine(root)
		var buf bytes.Buffer
		err := engine.Render(&buf, "../templates-private/secret", nil)
		if err == nil {
			t.Fatalf("expected error for prefix-sibling traversal, got nil")
		}
		if !strings.Contains(err.Error(), "must not escape template root") {
			t.Errorf("expected escape template root error, got: %v", err)
		}
	})

	t.Run("3. Symlinked directory escape", func(t *testing.T) {
		tempDir := t.TempDir()
		if !supportsSymlinks(t, tempDir) {
			t.Skip("symlinks not supported in environment")
		}

		root := filepath.Join(tempDir, "templates")
		priv := filepath.Join(tempDir, "private")
		_ = os.MkdirAll(root, 0755)
		_ = os.MkdirAll(priv, 0755)
		_ = os.WriteFile(filepath.Join(priv, "secret.pte"), []byte("SECRET_DATA"), 0644)

		symDir := filepath.Join(root, "leak")
		if err := os.Symlink(priv, symDir); err != nil {
			t.Skipf("symlink creation failed: %v", err)
		}

		engine := NewEngine(root)
		var buf bytes.Buffer
		err := engine.Render(&buf, "leak/secret", nil)
		if err == nil {
			t.Fatalf("expected error rendering symlinked directory escape, got content %q", buf.String())
		}
		if !strings.Contains(err.Error(), "must not escape template root") {
			t.Errorf("expected escape template root error, got: %v", err)
		}
		if strings.Contains(buf.String(), "SECRET_DATA") {
			t.Errorf("rendered sensitive external content despite escape attempt!")
		}
	})

	t.Run("4. Symlinked file escape", func(t *testing.T) {
		tempDir := t.TempDir()
		if !supportsSymlinks(t, tempDir) {
			t.Skip("symlinks not supported in environment")
		}

		root := filepath.Join(tempDir, "templates")
		priv := filepath.Join(tempDir, "private")
		_ = os.MkdirAll(root, 0755)
		_ = os.MkdirAll(priv, 0755)
		targetFile := filepath.Join(priv, "secret.pte")
		_ = os.WriteFile(targetFile, []byte("SECRET_FILE"), 0644)

		symFile := filepath.Join(root, "secret.pte")
		if err := os.Symlink(targetFile, symFile); err != nil {
			t.Skipf("symlink creation failed: %v", err)
		}

		engine := NewEngine(root)
		var buf bytes.Buffer
		err := engine.Render(&buf, "secret", nil)
		if err == nil {
			t.Fatalf("expected error for symlinked file escape, got output %q", buf.String())
		}
		if !strings.Contains(err.Error(), "must not escape template root") {
			t.Errorf("expected escape template root error, got: %v", err)
		}
	})

	t.Run("5. Valid nested template", func(t *testing.T) {
		tempDir := t.TempDir()
		root := filepath.Join(tempDir, "templates")
		pagesDir := filepath.Join(root, "pages")
		_ = os.MkdirAll(pagesDir, 0755)
		_ = os.WriteFile(filepath.Join(pagesDir, "dashboard.pte"), []byte("<h1>Dashboard</h1>"), 0644)

		engine := NewEngine(root)
		var buf bytes.Buffer
		err := engine.Render(&buf, "pages/dashboard", nil)
		if err != nil {
			t.Fatalf("expected successful render, got error: %v", err)
		}
		if got := strings.TrimSpace(buf.String()); got != "<h1>Dashboard</h1>" {
			t.Errorf("expected '<h1>Dashboard</h1>', got %q", got)
		}
	})

	t.Run("6. Configured root is itself a symlink", func(t *testing.T) {
		tempDir := t.TempDir()
		if !supportsSymlinks(t, tempDir) {
			t.Skip("symlinks not supported in environment")
		}

		realRoot := filepath.Join(tempDir, "real-templates")
		privDir := filepath.Join(tempDir, "private-dir")
		_ = os.MkdirAll(realRoot, 0755)
		_ = os.MkdirAll(privDir, 0755)

		_ = os.WriteFile(filepath.Join(realRoot, "home.pte"), []byte("HOME"), 0644)
		_ = os.WriteFile(filepath.Join(privDir, "secret.pte"), []byte("SECRET"), 0644)

		symRoot := filepath.Join(tempDir, "sym-templates")
		if err := os.Symlink(realRoot, symRoot); err != nil {
			t.Skipf("symlink creation failed: %v", err)
		}

		_ = os.Symlink(privDir, filepath.Join(realRoot, "leak"))

		engine := NewEngine(symRoot)

		var bufOk bytes.Buffer
		if err := engine.Render(&bufOk, "home", nil); err != nil {
			t.Errorf("expected render inside symlink root to succeed, got: %v", err)
		} else if got := bufOk.String(); got != "HOME" {
			t.Errorf("expected 'HOME', got %q", got)
		}

		var bufEscape bytes.Buffer
		err := engine.Render(&bufEscape, "leak/secret", nil)
		if err == nil {
			t.Fatalf("expected escape through symlink inside symlink root to fail")
		}
		if !strings.Contains(err.Error(), "must not escape template root") {
			t.Errorf("expected escape template root error, got: %v", err)
		}
	})

	t.Run("7. Missing template error distinction", func(t *testing.T) {
		tempDir := t.TempDir()
		root := filepath.Join(tempDir, "templates")
		_ = os.MkdirAll(root, 0755)

		engine := NewEngine(root)
		var buf bytes.Buffer
		err := engine.Render(&buf, "pages/missing", nil)
		if err == nil {
			t.Fatalf("expected error for missing template, got nil")
		}
		if strings.Contains(err.Error(), "must not escape template root") {
			t.Errorf("missing template incorrectly described as escaping root: %v", err)
		}
		if !strings.Contains(err.Error(), "failed to load template") && !os.IsNotExist(err) {
			t.Errorf("expected file missing error, got: %v", err)
		}
	})

	t.Run("8. Includes and layouts escape prevention", func(t *testing.T) {
		tempDir := t.TempDir()
		if !supportsSymlinks(t, tempDir) {
			t.Skip("symlinks not supported in environment")
		}

		root := filepath.Join(tempDir, "templates")
		priv := filepath.Join(tempDir, "private")
		_ = os.MkdirAll(root, 0755)
		_ = os.MkdirAll(priv, 0755)

		_ = os.WriteFile(filepath.Join(priv, "secret.pte"), []byte("SECRET"), 0644)
		_ = os.Symlink(priv, filepath.Join(root, "leak"))

		_ = os.WriteFile(filepath.Join(root, "inc.pte"), []byte("|include leak/secret|"), 0644)
		_ = os.WriteFile(filepath.Join(root, "lay.pte"), []byte("|layout leak/secret|"), 0644)

		engine := NewEngine(root)

		var bufInc bytes.Buffer
		errInc := engine.Render(&bufInc, "inc", nil)
		if errInc == nil || !strings.Contains(errInc.Error(), "must not escape template root") {
			t.Errorf("expected include escape to be rejected, got: %v", errInc)
		}

		var bufLay bytes.Buffer
		errLay := engine.Render(&bufLay, "lay", nil)
		if errLay == nil || !strings.Contains(errLay.Error(), "must not escape template root") {
			t.Errorf("expected layout escape to be rejected, got: %v", errLay)
		}
	})

	t.Run("9. Embedded filesystem validity and escape rejection", func(t *testing.T) {
		memFS := fstest.MapFS{
			"templates/valid.pte": &fstest.MapFile{
				Data: []byte("VALID_EMBEDDED"),
			},
		}

		engine := NewEngine("templates", WithFS(memFS))

		var bufOk bytes.Buffer
		if err := engine.Render(&bufOk, "valid", nil); err != nil {
			t.Errorf("expected valid embedded render to succeed, got: %v", err)
		} else if got := bufOk.String(); got != "VALID_EMBEDDED" {
			t.Errorf("expected 'VALID_EMBEDDED', got %q", got)
		}

		var bufEsc bytes.Buffer
		err := engine.Render(&bufEsc, "../private/secret", nil)
		if err == nil || !strings.Contains(err.Error(), "must not escape template root") {
			t.Errorf("expected embedded path traversal escape to be rejected, got: %v", err)
		}
	})
}

func TestConfirmedIssuesRegressionSuite(t *testing.T) {
	t.Run("Issue 2: Conditional attributes race condition", func(t *testing.T) {
		engine := NewEngine("")
		tmpl := `<button |attr disabled if disabled|>X</button>`

		var wg sync.WaitGroup
		for i := 0; i < 20; i++ {
			wg.Add(1)
			go func(idx int) {
				defer wg.Done()
				var buf bytes.Buffer
				dis := idx%2 == 0
				if err := engine.Render(&buf, tmpl, map[string]any{"disabled": dis}); err != nil {
					t.Errorf("render error: %v", err)
				}
			}(i)
		}
		wg.Wait()
	})

	t.Run("Issue 3: Field and Editor attribute injection escaping", func(t *testing.T) {
		engine := NewEngine("")
		data := map[string]any{
			"user": map[string]any{
				"email": `x" autofocus onfocus="alert(1)`,
			},
		}

		var bufField bytes.Buffer
		if err := engine.RenderString(&bufField, `|field user.email|`, data); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(bufField.String(), `x" autofocus`) {
			t.Errorf("unquoted attribute injection in field: %s", bufField.String())
		}
		if !strings.Contains(bufField.String(), `&quot;`) {
			t.Errorf("expected escaped quotes in field output: %s", bufField.String())
		}

		var bufEdit bytes.Buffer
		if err := engine.RenderString(&bufEdit, `|editor user.email|`, data); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(bufEdit.String(), `x" autofocus`) {
			t.Errorf("unquoted attribute injection in editor: %s", bufEdit.String())
		}
		if !strings.Contains(bufEdit.String(), `&quot;`) {
			t.Errorf("expected escaped quotes in editor output: %s", bufEdit.String())
		}
	})

	t.Run("Issue 4: Attempt panic recovery", func(t *testing.T) {
		engine := NewEngine("")
		tmpl := `|attempt||model.explode||recover|Recovered|/attempt|`

		panicModel := map[string]any{
			"model": panicModelObj{},
		}

		var buf bytes.Buffer
		if err := engine.RenderString(&buf, tmpl, panicModel); err != nil {
			t.Fatalf("unexpected rendering error: %v", err)
		}
		if got := strings.TrimSpace(buf.String()); got != "Recovered" {
			t.Errorf("expected 'Recovered', got %q", got)
		}
	})

	t.Run("Issue 6: Non-string map keys", func(t *testing.T) {
		engine := NewEngine("")
		intMap := map[int]string{1: "one", 2: "two"}
		data := map[string]any{"m": intMap}

		var buf bytes.Buffer
		if err := engine.RenderString(&buf, `|m.1|`, data); err != nil {
			t.Fatalf("unexpected error rendering int map key: %v", err)
		}
		if got := strings.TrimSpace(buf.String()); got != "one" {
			t.Errorf("expected 'one', got %q", got)
		}
	})

	t.Run("Issue 7: Unexported struct field safety", func(t *testing.T) {
		engine := NewEngine("")
		unexp := structWithUnexported{unexported: "secret", Exported: "public"}
		data := map[string]any{"u": unexp}

		var buf bytes.Buffer
		err := engine.RenderString(&buf, `|u.unexported|`, data)
		if err == nil {
			t.Errorf("expected property not found error for unexported field, got value %q", buf.String())
		}
	})

	t.Run("Issue 8: WithFS embedded template root lookup", func(t *testing.T) {
		memFS := fstest.MapFS{
			"templates/home.pte": &fstest.MapFile{
				Data: []byte("<h1>Embedded Home</h1>"),
			},
		}

		engine := NewEngine("templates", WithFS(memFS))
		var buf bytes.Buffer
		if err := engine.Render(&buf, "home", nil); err != nil {
			t.Fatalf("failed to render embedded template with root prefix: %v", err)
		}
		if got := strings.TrimSpace(buf.String()); got != "<h1>Embedded Home</h1>" {
			t.Errorf("expected '<h1>Embedded Home</h1>', got %q", got)
		}
	})

	t.Run("Issue 9: RenderString plain text", func(t *testing.T) {
		engine := NewEngine("")
		var buf bytes.Buffer
		if err := engine.RenderString(&buf, "Hello world", nil); err != nil {
			t.Fatalf("RenderString error: %v", err)
		}
		if got := buf.String(); got != "Hello world" {
			t.Errorf("expected 'Hello world', got %q", got)
		}
	})

	t.Run("Issue 10: Fragment API formatting options", func(t *testing.T) {
		engine := NewEngine("", WithInMemoryTemplates(map[string]string{
			"frag": `|fragment item|  <div>   Space   </div>  |/fragment|`,
		}), WithMinify(true))

		var buf bytes.Buffer
		if err := engine.RenderFragment(&buf, "frag", "item", nil); err != nil {
			t.Fatalf("RenderFragment error: %v", err)
		}
		if got := buf.String(); got != "<div> Space </div>" {
			t.Errorf("expected minified fragment '<div> Space </div>', got %q", got)
		}
	})

	t.Run("Additional concern: MinifyHTML raw text whitespace preservation", func(t *testing.T) {
		html := `<pre>  line1  \n  line2  </pre><textarea>  val  </textarea><script> let x = " a  b "; </script>`
		min := MinifyHTML(html)

		if !strings.Contains(min, "<pre>  line1  \\n  line2  </pre>") {
			t.Errorf("MinifyHTML altered <pre> content: %s", min)
		}
		if !strings.Contains(min, "<textarea>  val  </textarea>") {
			t.Errorf("MinifyHTML altered <textarea> content: %s", min)
		}
		if !strings.Contains(min, `<script> let x = " a  b "; </script>`) {
			t.Errorf("MinifyHTML altered <script> content: %s", min)
		}
	})
}

type panicModelObj struct{}

func (panicModelObj) Explode() string {
	panic("model method panic")
}

type structWithUnexported struct {
	unexported string
	Exported   string
}

// Extensive Regression Test Suites for Issues #6 through #10

type NamedString string
type NamedInt int
type NamedUint uint64

func TestIssue6MapKeyTypeSuite(t *testing.T) {
	engine := NewEngine("")

	t.Run("Named string key type reproduction", func(t *testing.T) {
		data := map[string]any{
			"values": map[NamedString]string{"answer": "42"},
		}
		var buf bytes.Buffer
		if err := engine.RenderString(&buf, "|values.answer|", data); err != nil {
			t.Fatalf("unexpected error rendering named string map key: %v", err)
		}
		if got := strings.TrimSpace(buf.String()); got != "42" {
			t.Errorf("expected '42', got %q", got)
		}
	})

	t.Run("Table of map key types", func(t *testing.T) {
		tests := []struct {
			name     string
			template string
			data     map[string]any
			want     string
		}{
			{
				name:     "map[string]string existing",
				template: "|m.hello|",
				data:     map[string]any{"m": map[string]string{"hello": "world"}},
				want:     "world",
			},
			{
				name:     "map[int]string existing",
				template: "|m.100|",
				data:     map[string]any{"m": map[int]string{100: "century"}},
				want:     "century",
			},
			{
				name:     "map[NamedInt]string existing",
				template: "|m.42|",
				data:     map[string]any{"m": map[NamedInt]string{42: "meaning"}},
				want:     "meaning",
			},
			{
				name:     "map[uint]string existing",
				template: "|m.7|",
				data:     map[string]any{"m": map[uint]string{7: "lucky"}},
				want:     "lucky",
			},
			{
				name:     "map[uintptr]string existing",
				template: "|m.123|",
				data:     map[string]any{"m": map[uintptr]string{123: "ptr"}},
				want:     "ptr",
			},
			{
				name:     "map[NamedUint]string existing",
				template: "|m.99|",
				data:     map[string]any{"m": map[NamedUint]string{99: "balloons"}},
				want:     "balloons",
			},
			{
				name:     "map[bool]string unsupported key falls back safely",
				template: "|m.true|",
				data:     map[string]any{"m": map[bool]string{true: "yes"}},
				want:     "",
			},
			{
				name:     "map[struct]string unsupported key falls back safely",
				template: "|m.key|",
				data:     map[string]any{"m": map[struct{ ID int }]string{{ID: 1}: "structKey"}},
				want:     "",
			},
			{
				name:     "missing integer key",
				template: "|m.999|",
				data:     map[string]any{"m": map[int]string{1: "one"}},
				want:     "",
			},
			{
				name:     "invalid numeric key string",
				template: "|m.abc|",
				data:     map[string]any{"m": map[int]string{1: "one"}},
				want:     "",
			},
			{
				name:     "numeric overflow for int8 key",
				template: "|m.99999999999999999999999|",
				data:     map[string]any{"m": map[int8]string{1: "one"}},
				want:     "",
			},
			{
				name:     "optional chaining on missing map key",
				template: "|m?.missing|",
				data:     map[string]any{"m": map[NamedString]string{"k": "v"}},
				want:     "",
			},
			{
				name:     "pointer to map",
				template: "|ptr.answer|",
				data:     map[string]any{"ptr": &map[NamedString]string{"answer": "42"}},
				want:     "42",
			},
			{
				name:     "map behind interface",
				template: "|inter.answer|",
				data:     map[string]any{"inter": any(map[NamedString]string{"answer": "42"})},
				want:     "42",
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				var buf bytes.Buffer
				err := engine.RenderString(&buf, tt.template, tt.data)
				if err != nil && tt.want != "" {
					t.Fatalf("unexpected render error: %v", err)
				}
				if got := strings.TrimSpace(buf.String()); got != tt.want {
					t.Errorf("expected %q, got %q", tt.want, got)
				}
			})
		}
	})

	t.Run("Concurrent map key property access", func(t *testing.T) {
		data := map[string]any{
			"values": map[NamedString]string{"key": "val"},
		}
		var wg sync.WaitGroup
		for i := 0; i < 20; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				var buf bytes.Buffer
				if err := engine.RenderString(&buf, "|values.key|", data); err != nil {
					t.Errorf("concurrent render error: %v", err)
				} else if got := strings.TrimSpace(buf.String()); got != "val" {
					t.Errorf("expected 'val', got %q", got)
				}
			}()
		}
		wg.Wait()
	})
}

type UserExported struct {
	Name string
}

type UserUnexported struct {
	Name       string
	secretCode string
}

type UserWithGetter struct {
	secretCode string
}

func (u UserWithGetter) GetSecretCode() string {
	return u.secretCode
}

func (u UserWithGetter) FetchStatus() (string, error) {
	return "Active", nil
}

func (u UserWithGetter) FailingGetter() (string, error) {
	return "", errors.New("getter failed")
}

type EmbeddedPublic struct {
	ID int
}

type embeddedPrivate struct {
	Secret string
}

type OuterStruct struct {
	EmbeddedPublic
	embeddedPrivate
}

func TestIssue7StructFieldVisibilitySuite(t *testing.T) {
	engine := NewEngine("")

	t.Run("Exported field", func(t *testing.T) {
		user := UserExported{Name: "Lemuel"}
		var buf bytes.Buffer
		if err := engine.RenderString(&buf, "|user.Name|", map[string]any{"user": user}); err != nil {
			t.Fatal(err)
		}
		if got := strings.TrimSpace(buf.String()); got != "Lemuel" {
			t.Errorf("expected 'Lemuel', got %q", got)
		}
	})

	t.Run("Unexported field returns controlled error", func(t *testing.T) {
		user := UserUnexported{Name: "Lemuel", secretCode: "pass123"}
		var buf bytes.Buffer
		err := engine.RenderString(&buf, "|user.secretCode|", map[string]any{"user": user})
		if err == nil {
			t.Fatalf("expected error accessing unexported field, got output %q", buf.String())
		}
		if strings.Contains(err.Error(), "pass123") {
			t.Errorf("error message must not leak private field contents: %v", err)
		}
	})

	t.Run("Optional chaining on unexported field renders empty", func(t *testing.T) {
		user := UserUnexported{Name: "Lemuel", secretCode: "pass123"}
		var buf bytes.Buffer
		if err := engine.RenderString(&buf, "|user?.secretCode|", map[string]any{"user": user}); err != nil {
			t.Fatalf("unexpected error with optional chaining: %v", err)
		}
		if got := buf.String(); got != "" {
			t.Errorf("expected empty string, got %q", got)
		}
	})

	t.Run("Struct pointer and nil struct pointer", func(t *testing.T) {
		user := &UserExported{Name: "Alice"}
		var buf1 bytes.Buffer
		if err := engine.RenderString(&buf1, "|user.Name|", map[string]any{"user": user}); err != nil {
			t.Fatal(err)
		}
		if got := strings.TrimSpace(buf1.String()); got != "Alice" {
			t.Errorf("expected 'Alice', got %q", got)
		}

		var nilUser *UserExported
		var buf2 bytes.Buffer
		if err := engine.RenderString(&buf2, "|user?.Name|", map[string]any{"user": nilUser}); err != nil {
			t.Fatal(err)
		}
		if got := buf2.String(); got != "" {
			t.Errorf("expected empty output for nil struct pointer optional chaining, got %q", got)
		}
	})

	t.Run("Embedded exported vs unexported struct", func(t *testing.T) {
		outer := OuterStruct{
			EmbeddedPublic:  EmbeddedPublic{ID: 42},
			embeddedPrivate: embeddedPrivate{Secret: "topsecret"},
		}

		var buf1 bytes.Buffer
		if err := engine.RenderString(&buf1, "|outer.ID|", map[string]any{"outer": outer}); err != nil {
			t.Fatal(err)
		}
		if got := strings.TrimSpace(buf1.String()); got != "42" {
			t.Errorf("expected '42', got %q", got)
		}

		var buf2 bytes.Buffer
		err := engine.RenderString(&buf2, "|outer.embeddedPrivate|", map[string]any{"outer": outer})
		if err == nil {
			t.Errorf("expected error accessing unexported embedded struct field, got %q", buf2.String())
		}
	})

	t.Run("Unexported field with exported getter", func(t *testing.T) {
		user := UserWithGetter{secretCode: "code99"}
		var buf bytes.Buffer
		if err := engine.RenderString(&buf, "|user.GetSecretCode|", map[string]any{"user": user}); err != nil {
			t.Fatal(err)
		}
		if got := strings.TrimSpace(buf.String()); got != "code99" {
			t.Errorf("expected 'code99', got %q", got)
		}
	})

	t.Run("Getter returning (value, error)", func(t *testing.T) {
		user := UserWithGetter{}
		var buf1 bytes.Buffer
		if err := engine.RenderString(&buf1, "|user.FetchStatus|", map[string]any{"user": user}); err != nil {
			t.Fatal(err)
		}
		if got := strings.TrimSpace(buf1.String()); got != "Active" {
			t.Errorf("expected 'Active', got %q", got)
		}

		var buf2 bytes.Buffer
		err := engine.RenderString(&buf2, "|user.FailingGetter|", map[string]any{"user": user})
		if err == nil || !strings.Contains(err.Error(), "getter failed") {
			t.Errorf("expected 'getter failed' error, got %v", err)
		}
	})

	t.Run("Attempt recovery around inaccessible property", func(t *testing.T) {
		user := UserUnexported{secretCode: "hidden"}
		tmpl := `|attempt||user.secretCode||recover as err|Recovered: |err||/attempt|`
		var buf bytes.Buffer
		if err := engine.RenderString(&buf, tmpl, map[string]any{"user": user}); err != nil {
			t.Fatalf("unexpected rendering error in attempt block: %v", err)
		}
		if !strings.Contains(buf.String(), "Recovered:") {
			t.Errorf("expected attempt block to recover from property error, got %q", buf.String())
		}
	})

	t.Run("Concurrent struct field visibility check", func(t *testing.T) {
		user := UserExported{Name: "Concurrent"}
		var wg sync.WaitGroup
		for i := 0; i < 20; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				var buf bytes.Buffer
				if err := engine.RenderString(&buf, "|user.Name|", map[string]any{"user": user}); err != nil {
					t.Errorf("concurrent struct access error: %v", err)
				}
			}()
		}
		wg.Wait()
	})
}

func TestIssue8WithFSEmbeddedTemplateSuite(t *testing.T) {
	memFS := fstest.MapFS{
		"templates/home.pte": &fstest.MapFile{
			Data: []byte("<h1>Embedded Home</h1>"),
		},
		"templates/pages/dashboard.pte": &fstest.MapFile{
			Data: []byte("<div>Dashboard |title|</div>"),
		},
		"templates/layouts/base.pte": &fstest.MapFile{
			Data: []byte("<main>|yield content|</main>"),
		},
		"templates/components/card.pte": &fstest.MapFile{
			Data: []byte("<card>|slot body|</card>"),
		},
		"templates/inc.pte": &fstest.MapFile{
			Data: []byte("<span>Included</span>"),
		},
		"templates/frag.pte": &fstest.MapFile{
			Data: []byte("|fragment item|<div>FragItem</div>|/fragment|"),
		},
		"templates/custom.html": &fstest.MapFile{
			Data: []byte("<p>Custom Suffix</p>"),
		},
	}

	t.Run("Root-level and nested embedded templates", func(t *testing.T) {
		engine := NewEngine("templates", WithFS(memFS))
		var buf1 bytes.Buffer
		if err := engine.Render(&buf1, "home", nil); err != nil {
			t.Fatalf("failed to render home: %v", err)
		}
		if got := strings.TrimSpace(buf1.String()); got != "<h1>Embedded Home</h1>" {
			t.Errorf("expected '<h1>Embedded Home</h1>', got %q", got)
		}

		var buf2 bytes.Buffer
		if err := engine.Render(&buf2, "pages/dashboard", map[string]any{"title": "Stats"}); err != nil {
			t.Fatalf("failed to render nested page: %v", err)
		}
		if got := strings.TrimSpace(buf2.String()); got != "<div>Dashboard Stats</div>" {
			t.Errorf("expected '<div>Dashboard Stats</div>', got %q", got)
		}
	})

	t.Run("Custom suffix WithSuffix", func(t *testing.T) {
		engine := NewEngine("templates", WithFS(memFS), WithSuffix(".html"))
		var buf bytes.Buffer
		if err := engine.Render(&buf, "custom", nil); err != nil {
			t.Fatalf("failed to render custom suffix: %v", err)
		}
		if got := strings.TrimSpace(buf.String()); got != "<p>Custom Suffix</p>" {
			t.Errorf("expected '<p>Custom Suffix</p>', got %q", got)
		}
	})

	t.Run("Embedded included, layout, component, and fragment", func(t *testing.T) {
		engine := NewEngine("templates", WithFS(memFS))

		// Include
		var bufInc bytes.Buffer
		if err := engine.RenderString(&bufInc, "|include inc|", nil); err != nil {
			t.Fatalf("failed to render embedded include: %v", err)
		}
		if got := strings.TrimSpace(bufInc.String()); got != "<span>Included</span>" {
			t.Errorf("expected '<span>Included</span>', got %q", got)
		}

		// Fragment
		var bufFrag bytes.Buffer
		if err := engine.RenderFragment(&bufFrag, "frag", "item", nil); err != nil {
			t.Fatalf("failed to render embedded fragment: %v", err)
		}
		if got := strings.TrimSpace(bufFrag.String()); got != "<div>FragItem</div>" {
			t.Errorf("expected '<div>FragItem</div>', got %q", got)
		}
	})

	t.Run("fs.Sub usage", func(t *testing.T) {
		subFS, err := fs.Sub(memFS, "templates")
		if err != nil {
			t.Fatalf("fs.Sub failed: %v", err)
		}
		engine := NewEngine(".", WithFS(subFS))
		var buf bytes.Buffer
		if err := engine.Render(&buf, "home", nil); err != nil {
			t.Fatalf("failed to render fs.Sub template: %v", err)
		}
		if got := strings.TrimSpace(buf.String()); got != "<h1>Embedded Home</h1>" {
			t.Errorf("expected '<h1>Embedded Home</h1>', got %q", got)
		}
	})

	t.Run("Embedded path traversal rejection", func(t *testing.T) {
		engine := NewEngine("templates", WithFS(memFS))
		traversals := []string{"../secret", "/etc/passwd", "..\\secret"}
		for _, target := range traversals {
			var buf bytes.Buffer
			err := engine.Render(&buf, target, nil)
			if err == nil || !strings.Contains(err.Error(), "must not escape template root") {
				t.Errorf("expected escape rejection for %q, got: %v", target, err)
			}
		}
	})

	t.Run("Precedence: In-memory > WithFS > Disk", func(t *testing.T) {
		engine := NewEngine("templates", WithFS(memFS), WithInMemoryTemplates(map[string]string{
			"home": "<h1>In-Memory Home</h1>",
		}))
		var buf bytes.Buffer
		if err := engine.Render(&buf, "home", nil); err != nil {
			t.Fatal(err)
		}
		if got := strings.TrimSpace(buf.String()); got != "<h1>In-Memory Home</h1>" {
			t.Errorf("in-memory template must override FS template, got %q", got)
		}
	})

	t.Run("Concurrent embedded template rendering", func(t *testing.T) {
		engine := NewEngine("templates", WithFS(memFS))
		var wg sync.WaitGroup
		for i := 0; i < 20; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				var buf bytes.Buffer
				if err := engine.Render(&buf, "home", nil); err != nil {
					t.Errorf("concurrent embedded render error: %v", err)
				}
			}()
		}
		wg.Wait()
	})
}

func TestIssue9RenderStringLiteralTextSuite(t *testing.T) {
	engine := NewEngine("nonexistent-root")

	t.Run("Literal text sources render without looking up disk template name", func(t *testing.T) {
		tests := []struct {
			name   string
			source string
			values map[string]any
			want   string
		}{
			{
				name:   "empty source",
				source: "",
				want:   "",
			},
			{
				name:   "plain words",
				source: "Hello world",
				want:   "Hello world",
			},
			{
				name:   "plain text containing spaces",
				source: "Plain text",
				want:   "Plain text",
			},
			{
				name:   "numbers",
				source: "12345",
				want:   "12345",
			},
			{
				name:   "path-like string",
				source: "/products",
				want:   "/products",
			},
			{
				name:   "HTML literal",
				source: "<h1>Title</h1>",
				want:   "<h1>Title</h1>",
			},
			{
				name:   "expression evaluation",
				source: "Hello |name|",
				values: map[string]any{"name": "Lemuel"},
				want:   "Hello Lemuel",
			},
			{
				name:   "conditional",
				source: "|if active|Active|else|Inactive|/if|",
				values: map[string]any{"active": true},
				want:   "Active",
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				var buf bytes.Buffer
				if err := engine.RenderString(&buf, tt.source, tt.values); err != nil {
					t.Fatalf("unexpected RenderString error: %v", err)
				}
				if got := buf.String(); got != tt.want {
					t.Errorf("expected %q, got %q", tt.want, got)
				}
			})
		}
	})

	t.Run("RenderStringStream literal text", func(t *testing.T) {
		reader := engine.RenderStringStream("Stream |val|", map[string]any{"val": "OK"})
		data, err := io.ReadAll(reader)
		if err != nil {
			t.Fatalf("stream read error: %v", err)
		}
		if got := string(data); got != "Stream OK" {
			t.Errorf("expected 'Stream OK', got %q", got)
		}
	})

	t.Run("Contrast: Render treats path-like string as template name vs RenderString renders literally", func(t *testing.T) {
		var bufRender bytes.Buffer
		errRender := engine.Render(&bufRender, "pages/home", nil)
		if errRender == nil {
			t.Errorf("Render('pages/home') should attempt to load template file and return error for nonexistent root, got output %q", bufRender.String())
		}

		var bufString bytes.Buffer
		errString := engine.RenderString(&bufString, "pages/home", nil)
		if errString != nil {
			t.Fatalf("RenderString('pages/home') must render literal string without error, got: %v", errString)
		}
		if got := bufString.String(); got != "pages/home" {
			t.Errorf("expected literal 'pages/home', got %q", got)
		}
	})
}

func TestIssue10FragmentFormattingAndAtomicOutputSuite(t *testing.T) {
	templates := map[string]string{
		"frag_page": `
|fragment head|  <head>   <title>PTE</title>   </head>  |/fragment|
|fragment body|  <body>   <h1>Header</h1>   </body>  |/fragment|
|fragment failing|  <div> |unknownVar.subProp| </div>  |/fragment|
`,
	}

	t.Run("RenderFragment default, minify, and prettify formatting", func(t *testing.T) {
		rawEng := NewEngine("", WithInMemoryTemplates(templates))
		var bufRaw bytes.Buffer
		if err := rawEng.RenderFragment(&bufRaw, "frag_page", "head", nil); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(bufRaw.String(), "<head>   <title>PTE</title>   </head>") {
			t.Errorf("expected raw unformatted output, got %q", bufRaw.String())
		}

		minEng := NewEngine("", WithInMemoryTemplates(templates), WithMinify(true))
		var bufMin bytes.Buffer
		if err := minEng.RenderFragment(&bufMin, "frag_page", "head", nil); err != nil {
			t.Fatal(err)
		}
		if got := bufMin.String(); got != "<head><title>PTE</title></head>" {
			t.Errorf("expected minified output '<head><title>PTE</title></head>', got %q", got)
		}

		prettyEng := NewEngine("", WithInMemoryTemplates(templates), WithPrettify(true))
		var bufPretty bytes.Buffer
		if err := prettyEng.RenderFragment(&bufPretty, "frag_page", "head", nil); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(bufPretty.String(), "<head>") || !strings.Contains(bufPretty.String(), "<title>PTE") {
			t.Errorf("expected prettified output, got %q", bufPretty.String())
		}
	})

	t.Run("RenderFragments combined formatting once", func(t *testing.T) {
		minEng := NewEngine("", WithInMemoryTemplates(templates), WithMinify(true))
		var buf bytes.Buffer
		if err := minEng.RenderFragments(&buf, "frag_page", []string{"head", "body"}, nil); err != nil {
			t.Fatal(err)
		}
		if got := buf.String(); got != "<head><title>PTE</title></head><body><h1>Header</h1></body>" {
			t.Errorf("expected combined minified fragments, got %q", got)
		}
	})

	t.Run("RenderFragments atomic output on failure", func(t *testing.T) {
		rawEng := NewEngine("", WithInMemoryTemplates(templates))
		var buf bytes.Buffer
		err := rawEng.RenderFragments(&buf, "frag_page", []string{"head", "failing"}, nil)
		if err == nil {
			t.Fatal("expected error when rendering failing fragment")
		}
		if buf.Len() != 0 {
			t.Errorf("caller writer must be 0 bytes on error (atomic output), got %d bytes: %q", buf.Len(), buf.String())
		}
	})

	t.Run("Fragment streaming APIs", func(t *testing.T) {
		minEng := NewEngine("", WithInMemoryTemplates(templates), WithMinify(true))

		r1 := minEng.RenderFragmentStream("frag_page", "head", nil)
		d1, err := io.ReadAll(r1)
		if err != nil {
			t.Fatal(err)
		}
		if got := string(d1); got != "<head><title>PTE</title></head>" {
			t.Errorf("expected minified fragment stream output, got %q", got)
		}

		r2 := minEng.RenderFragmentsStream("frag_page", []string{"head", "body"}, nil)
		d2, err := io.ReadAll(r2)
		if err != nil {
			t.Fatal(err)
		}
		if got := string(d2); got != "<head><title>PTE</title></head><body><h1>Header</h1></body>" {
			t.Errorf("expected minified fragments stream output, got %q", got)
		}
	})

	t.Run("Concurrent fragment rendering", func(t *testing.T) {
		minEng := NewEngine("", WithInMemoryTemplates(templates), WithMinify(true))
		var wg sync.WaitGroup
		for i := 0; i < 20; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				var buf bytes.Buffer
				if err := minEng.RenderFragment(&buf, "frag_page", "head", nil); err != nil {
					t.Errorf("concurrent fragment render error: %v", err)
				}
			}()
		}
		wg.Wait()
	})
}

// Regression Test Suites for Remaining Issues #1 through #8

func TestRemainingIssue1ModuloByZero(t *testing.T) {
	engine := NewEngine("")

	t.Run("Modulo by zero returns error", func(t *testing.T) {
		var buf bytes.Buffer
		err := engine.RenderString(&buf, "|0%0|", nil)
		if err == nil {
			t.Fatal("expected error for 0%0, got nil")
		}
		if !strings.Contains(err.Error(), "division by zero") {
			t.Errorf("expected division by zero error, got %v", err)
		}
	})

	t.Run("Modulo with fractional divisor returns error", func(t *testing.T) {
		var buf bytes.Buffer
		err := engine.RenderString(&buf, "|10%0.5|", nil)
		if err == nil {
			t.Fatal("expected error for 10%0.5, got nil")
		}
	})

	t.Run("Modulo with fractional dividend returns error", func(t *testing.T) {
		var buf bytes.Buffer
		err := engine.RenderString(&buf, "|10.5%2|", nil)
		if err == nil {
			t.Fatal("expected error for 10.5%2, got nil")
		}
	})

	t.Run("Valid modulo returns exact integer", func(t *testing.T) {
		var buf bytes.Buffer
		if err := engine.RenderString(&buf, "|10%3|", nil); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got := strings.TrimSpace(buf.String()); got != "1" {
			t.Errorf("expected '1', got %q", got)
		}
	})
}

func TestRemainingIssue2LiteralPipesAndRawBlocks(t *testing.T) {
	engine := NewEngine("")

	t.Run("Escaped single and double pipes", func(t *testing.T) {
		var buf bytes.Buffer
		tmpl := `<div x-text="primary \|\| fallback"></div>`
		if err := engine.RenderString(&buf, tmpl, nil); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		expected := `<div x-text="primary || fallback"></div>`
		if got := buf.String(); got != expected {
			t.Errorf("expected %q, got %q", expected, got)
		}
	})

	t.Run("Raw block emits unparsed content", func(t *testing.T) {
		tmpl := `|raw|
<div x-text="primary || fallback"></div>
<script>
    const flags = left | right;
</script>
|/raw|`
		var buf bytes.Buffer
		if err := engine.RenderString(&buf, tmpl, nil); err != nil {
			t.Fatalf("unexpected error rendering raw block: %v", err)
		}
		expected := "\n<div x-text=\"primary || fallback\"></div>\n<script>\n    const flags = left | right;\n</script>\n"
		if got := buf.String(); got != expected {
			t.Errorf("expected %q, got %q", expected, got)
		}
	})

	t.Run("Adjacent directives syntax preserved", func(t *testing.T) {
		tmpl := `|if active||name||/if|`
		var buf bytes.Buffer
		data := map[string]any{"active": true, "name": "Alice"}
		if err := engine.RenderString(&buf, tmpl, data); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got := buf.String(); got != "Alice" {
			t.Errorf("expected 'Alice', got %q", got)
		}
	})

	t.Run("Unclosed raw block error", func(t *testing.T) {
		tmpl := `|raw| unclosed raw text`
		var buf bytes.Buffer
		err := engine.RenderString(&buf, tmpl, nil)
		if err == nil || !strings.Contains(err.Error(), "missing closing |/raw| tag") {
			t.Fatalf("expected missing closing |/raw| error, got %v", err)
		}
	})

	t.Run("Nested raw block error", func(t *testing.T) {
		tmpl := `|raw| outer |raw| inner |/raw| |/raw|`
		var buf bytes.Buffer
		err := engine.RenderString(&buf, tmpl, nil)
		if err == nil || !strings.Contains(err.Error(), "nested raw block") {
			t.Fatalf("expected nested raw block error, got %v", err)
		}
	})
}

func TestRemainingIssue3LargeIntegerPrecision(t *testing.T) {
	engine := NewEngine("")

	t.Run("64-bit integer exact comparison", func(t *testing.T) {
		data := map[string]any{
			"actual": uint64(9007199254740993),
			"lower":  uint64(9007199254740992),
		}
		var buf bytes.Buffer
		if err := engine.RenderString(&buf, "|if actual == lower|wrong|else|correct|/if|", data); err != nil {
			t.Fatal(err)
		}
		if got := buf.String(); got != "correct" {
			t.Errorf("large integers 9007199254740993 and 9007199254740992 must compare unequal, got %q", got)
		}
	})

	t.Run("Large integer range bounds", func(t *testing.T) {
		data := map[string]any{
			"start": int64(9007199254740993),
			"end":   int64(9007199254740993),
		}
		var buf bytes.Buffer
		if err := engine.RenderString(&buf, "|for i from start to end||i||/for|", data); err != nil {
			t.Fatal(err)
		}
		if got := buf.String(); got != "9007199254740993" {
			t.Errorf("expected '9007199254740993', got %q", got)
		}
	})

	t.Run("Switch case large integer matching", func(t *testing.T) {
		data := map[string]any{
			"val": uint64(9007199254740993),
		}
		tmpl := "|switch val|\n|case 9007199254740993|\nmatch\n|default|\nno\n|/switch|"
		var buf bytes.Buffer
		if err := engine.RenderString(&buf, tmpl, data); err != nil {
			t.Fatal(err)
		}
		if got := strings.TrimSpace(buf.String()); got != "match" {
			t.Errorf("expected 'match', got %q", got)
		}
	})
}

func TestRemainingIssue4ForRangeFractionalAndOverflow(t *testing.T) {
	engine := NewEngine("")

	t.Run("Fractional range start rejected", func(t *testing.T) {
		var buf bytes.Buffer
		err := engine.RenderString(&buf, "|for i from 1.9 to 3||i||/for|", nil)
		if err == nil {
			t.Fatal("expected error for fractional range start 1.9")
		}
	})

	t.Run("Fractional range end rejected", func(t *testing.T) {
		var buf bytes.Buffer
		err := engine.RenderString(&buf, "|for i from 1 to 3.9||i||/for|", nil)
		if err == nil {
			t.Fatal("expected error for fractional range end 3.9")
		}
	})

	t.Run("Fractional step rejected", func(t *testing.T) {
		var buf bytes.Buffer
		err := engine.RenderString(&buf, "|for i from 1 to 5 step 1.5||i||/for|", nil)
		if err == nil {
			t.Fatal("expected error for fractional step 1.5")
		}
	})

	t.Run("Exact float 3.0 accepted", func(t *testing.T) {
		var buf bytes.Buffer
		if err := engine.RenderString(&buf, "|for i from 1.0 to 3.0||i||/for|", nil); err != nil {
			t.Fatalf("unexpected error for exact floats 1.0 to 3.0: %v", err)
		}
		if got := buf.String(); got != "123" {
			t.Errorf("expected '123', got %q", got)
		}
	})

	t.Run("Ascending boundary MaxInt64 loop terminates", func(t *testing.T) {
		data := map[string]any{
			"start": int64(math.MaxInt64 - 1),
			"end":   int64(math.MaxInt64),
		}
		var buf bytes.Buffer
		if err := engine.RenderString(&buf, "|for i from start to end||i||separator|, |/separator||/for|", data); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		expected := fmt.Sprintf("%d, %d", math.MaxInt64-1, math.MaxInt64)
		if got := buf.String(); got != expected {
			t.Errorf("expected %q, got %q", expected, got)
		}
	})

	t.Run("Descending boundary MinInt64 loop terminates", func(t *testing.T) {
		data := map[string]any{
			"start": int64(math.MinInt64 + 1),
			"end":   int64(math.MinInt64),
		}
		var buf bytes.Buffer
		if err := engine.RenderString(&buf, "|for i from start to end||i||separator|, |/separator||/for|", data); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		expected := fmt.Sprintf("%d, %d", math.MinInt64+1, math.MinInt64)
		if got := buf.String(); got != expected {
			t.Errorf("expected %q, got %q", expected, got)
		}
	})
}

func TestRemainingIssue5And6MinifierRawElementsAndPlaceholders(t *testing.T) {
	t.Run("Script containing HTML comment string preserved", func(t *testing.T) {
		html := "<script>\nconst marker = \"<!--must remain-->\";\n</script>"
		min := MinifyHTML(html)
		if !strings.Contains(min, "<!--must remain-->") {
			t.Errorf("MinifyHTML removed comment-like string inside script: %s", min)
		}
	})

	t.Run("Placeholder-like literal string preserved without collision", func(t *testing.T) {
		html := "___PTE_PRESERVED_0___ <pre>  keep spaces  </pre>"
		min := MinifyHTML(html)
		if !strings.Contains(min, "___PTE_PRESERVED_0___") {
			t.Errorf("MinifyHTML corrupted placeholder-like text: %s", min)
		}
		if !strings.Contains(min, "<pre>  keep spaces  </pre>") {
			t.Errorf("MinifyHTML corrupted pre text: %s", min)
		}
	})

	t.Run("MinifyHTML idempotency", func(t *testing.T) {
		html := `
<div>
    <h1> Title </h1>
    <script> let x = " <!-- comment --> "; </script>
    <pre>  formatted  </pre>
</div>`
		min1 := MinifyHTML(html)
		min2 := MinifyHTML(min1)
		if min1 != min2 {
			t.Errorf("MinifyHTML is not idempotent!\nMin1: %q\nMin2: %q", min1, min2)
		}
	})
}

func TestRemainingIssue7PWAServiceWorkerJSONEncoding(t *testing.T) {
	engine := NewEngine("")

	t.Run("Service worker path with query params JSON encoded", func(t *testing.T) {
		tmpl := `|pwa sw='/sw.js?cache=1&scope=app'|`
		var buf bytes.Buffer
		if err := engine.RenderString(&buf, tmpl, nil); err != nil {
			t.Fatal(err)
		}
		out := buf.String()
		if strings.Contains(out, "&amp;") {
			t.Errorf("PWA service worker URL inside script must not contain HTML entity &amp;: %s", out)
		}
		if !strings.Contains(out, `navigator.serviceWorker.register("/sw.js?cache=1\u0026scope=app")`) {
			t.Errorf("expected JSON-encoded service worker URL, got %s", out)
		}
	})

	t.Run("PWA sw='none' disables registration", func(t *testing.T) {
		tmpl := `|pwa sw='none'|`
		var buf bytes.Buffer
		if err := engine.RenderString(&buf, tmpl, nil); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(buf.String(), "serviceWorker.register") {
			t.Errorf("expected no serviceWorker.register call for sw='none', got %s", buf.String())
		}
	})
}

func TestRemainingIssue8FragmentMacroScoping(t *testing.T) {
	templates := map[string]string{
		"macro_page": `
|macro badge(text)|<b>|text|</b>|/macro|

|fragment result|
|call badge('OK')|
|/fragment|
`,
	}
	engine := NewEngine("", WithInMemoryTemplates(templates))

	t.Run("RenderFragment executes top-level macro declared before fragment", func(t *testing.T) {
		var buf bytes.Buffer
		if err := engine.RenderFragment(&buf, "macro_page", "result", nil); err != nil {
			t.Fatalf("RenderFragment error: %v", err)
		}
		if got := strings.TrimSpace(buf.String()); got != "<b>OK</b>" {
			t.Errorf("expected '<b>OK</b>', got %q", got)
		}
	})

	t.Run("RenderFragments executes top-level macro", func(t *testing.T) {
		var buf bytes.Buffer
		if err := engine.RenderFragments(&buf, "macro_page", []string{"result"}, nil); err != nil {
			t.Fatalf("RenderFragments error: %v", err)
		}
		if got := strings.TrimSpace(buf.String()); got != "<b>OK</b>" {
			t.Errorf("expected '<b>OK</b>', got %q", got)
		}
	})
}

func FuzzGeneralTemplateRendering(f *testing.F) {
	seeds := []string{
		"plain text",
		"|name|",
		"|0%0|",
		"|if active|yes|else|no|/if|",
		"|each item in items||item||/each|",
		"|switch role||case 'admin'|yes|default|no|/switch|",
		"|# comment |",
		`<div x-text="a \|\| b"></div>`,
	}

	for _, seed := range seeds {
		f.Add(seed)
	}

	engine := NewEngine("")

	f.Fuzz(func(t *testing.T, template string) {
		var output bytes.Buffer
		_ = engine.RenderString(&output, template, map[string]any{
			"name":   "PTE",
			"active": true,
			"role":   "admin",
			"items":  []int{1, 2, 3},
		})
	})
}
