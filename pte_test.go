package pte

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
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
	Name       string
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

	t.Run("Literal pipes and double pipes", func(t *testing.T) {
		tests := []struct {
			name     string
			template string
			expected string
		}{
			{"Single escaped pipe", `\|`, "|"},
			{"Double escaped pipes", `\|\|`, "||"},
			{"Pipe surrounded by spaces", `A \| B`, "A | B"},
			{"JavaScript OR expression", `<div x-text="primary \|\| fallback"></div>`, `<div x-text="primary || fallback"></div>`},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				var buf bytes.Buffer
				if err := engine.RenderString(&buf, tt.template, nil); err != nil {
					t.Fatalf("unexpected error rendering %q: %v", tt.template, err)
				}
				if got := buf.String(); got != tt.expected {
					t.Errorf("template %q: expected %q, got %q", tt.template, tt.expected, got)
				}
			})
		}
	})

	t.Run("Backslash parity before pipe", func(t *testing.T) {
		// Test 1 through 6 backslashes before a pipe
		// Odd count = pipe is escaped, 1 backslash removed.
		// Even count = pipe is directive delimiter, 0 backslashes removed.
		data := map[string]any{"name": "PTE"}

		// 1 backslash: \| -> literal pipe '|'
		var buf bytes.Buffer
		if err := engine.RenderString(&buf, `\|`, data); err != nil {
			t.Fatal(err)
		}
		if got := buf.String(); got != "|" {
			t.Errorf("1 backslash: expected '|', got %q", got)
		}

		// 2 backslashes: \\|name| -> \\PTE (directive evaluated, 2 backslashes preserved)
		buf.Reset()
		if err := engine.RenderString(&buf, `\\|name|`, data); err != nil {
			t.Fatal(err)
		}
		if got := buf.String(); got != `\\PTE` {
			t.Errorf("2 backslashes: expected %q, got %q", `\\PTE`, got)
		}

		// 3 backslashes: \\\| -> \\| (escaped pipe, 2 backslashes preserved)
		buf.Reset()
		if err := engine.RenderString(&buf, `\\\|`, data); err != nil {
			t.Fatal(err)
		}
		if got := buf.String(); got != `\\|` {
			t.Errorf("3 backslashes: expected %q, got %q", `\\|`, got)
		}

		// 4 backslashes: \\\\|name| -> \\\\PTE (directive evaluated, 4 backslashes preserved)
		buf.Reset()
		if err := engine.RenderString(&buf, `\\\\|name|`, data); err != nil {
			t.Fatal(err)
		}
		if got := buf.String(); got != `\\\\PTE` {
			t.Errorf("4 backslashes: expected %q, got %q", `\\\\PTE`, got)
		}

		// 5 backslashes: \\\\\| -> \\\\| (escaped pipe, 4 backslashes preserved)
		buf.Reset()
		if err := engine.RenderString(&buf, `\\\\\|`, data); err != nil {
			t.Fatal(err)
		}
		if got := buf.String(); got != `\\\\|` {
			t.Errorf("5 backslashes: expected %q, got %q", `\\\\|`, got)
		}

		// 6 backslashes: \\\\\\|name| -> \\\\\\PTE (directive evaluated, 6 backslashes preserved)
		buf.Reset()
		if err := engine.RenderString(&buf, `\\\\\\|name|`, data); err != nil {
			t.Fatal(err)
		}
		if got := buf.String(); got != `\\\\\\PTE` {
			t.Errorf("6 backslashes: expected %q, got %q", `\\\\\\PTE`, got)
		}
	})

	t.Run("Ordinary backslashes remain untouched", func(t *testing.T) {
		tests := []struct {
			name     string
			template string
			expected string
		}{
			{"Windows path single backslashes", `C:\templates\page.pte`, `C:\templates\page.pte`},
			{"Windows path double backslashes", `C:\\templates\\page.pte`, `C:\\templates\\page.pte`},
			{"UNC path", `\\server\share\folder`, `\\server\share\folder`},
			{"Regular expression", `regular\expression`, `regular\expression`},
			{"Trailing backslash", `trailing\`, `trailing\`},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				var buf bytes.Buffer
				if err := engine.RenderString(&buf, tt.template, nil); err != nil {
					t.Fatalf("unexpected error rendering %q: %v", tt.template, err)
				}
				if got := buf.String(); got != tt.expected {
					t.Errorf("template %q: expected %q, got %q", tt.template, tt.expected, got)
				}
			})
		}
	})

	t.Run("JavaScript and AlpineJS escaping", func(t *testing.T) {
		jsTmpl := `<script>
    const result = primary \|\| fallback;
    const flags = left \| right;
    const path = "C:\\temp\\file";
</script>`
		var buf bytes.Buffer
		if err := engine.RenderString(&buf, jsTmpl, nil); err != nil {
			t.Fatal(err)
		}
		expectedJS := `<script>
    const result = primary || fallback;
    const flags = left | right;
    const path = "C:\\temp\\file";
</script>`
		if got := buf.String(); got != expectedJS {
			t.Errorf("JS expected %q, got %q", expectedJS, got)
		}

		alpineTmpl := `<div x-data="{ ready: false }"
     x-show="ready \|\| loading">
</div>`
		buf.Reset()
		if err := engine.RenderString(&buf, alpineTmpl, nil); err != nil {
			t.Fatal(err)
		}
		expectedAlpine := `<div x-data="{ ready: false }"
     x-show="ready || loading">
</div>`
		if got := buf.String(); got != expectedAlpine {
			t.Errorf("Alpine expected %q, got %q", expectedAlpine, got)
		}
	})

	t.Run("Raw block exact byte-for-byte rendering", func(t *testing.T) {
		rawTmpl := `|raw|C:\\templates\|literal|/raw|`
		var buf bytes.Buffer
		if err := engine.RenderString(&buf, rawTmpl, nil); err != nil {
			t.Fatal(err)
		}
		expectedRaw := `C:\\templates\|literal`
		if got := buf.String(); got != expectedRaw {
			t.Errorf("raw block expected %q, got %q", expectedRaw, got)
		}

		complexRaw := `|raw|
|if thisLooksLikePTE|
C:\\templates\|literal
<div x-text="a || b"></div>
|# this must remain literal |
|/raw|`
		buf.Reset()
		if err := engine.RenderString(&buf, complexRaw, nil); err != nil {
			t.Fatal(err)
		}
		expectedComplex := "\n|if thisLooksLikePTE|\nC:\\\\templates\\|literal\n<div x-text=\"a || b\"></div>\n|# this must remain literal |\n"
		if got := buf.String(); got != expectedComplex {
			t.Errorf("complex raw block expected %q, got %q", expectedComplex, got)
		}
	})

	t.Run("Empty raw block renders 0 bytes", func(t *testing.T) {
		var buf bytes.Buffer
		if err := engine.RenderString(&buf, "|raw||/raw|", nil); err != nil {
			t.Fatal(err)
		}
		if got := buf.String(); got != "" {
			t.Errorf("empty raw block expected '', got %q", got)
		}
	})

	t.Run("Multiple raw blocks in single template", func(t *testing.T) {
		tmpl := `|raw|first \| raw|/raw| middle |raw|second \| raw|/raw|`
		var buf bytes.Buffer
		if err := engine.RenderString(&buf, tmpl, nil); err != nil {
			t.Fatal(err)
		}
		expected := `first \| raw middle second \| raw`
		if got := buf.String(); got != expected {
			t.Errorf("multiple raw blocks expected %q, got %q", expected, got)
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

	t.Run("Stray /raw directive error", func(t *testing.T) {
		tmpl := `hello |/raw| world`
		var buf bytes.Buffer
		err := engine.RenderString(&buf, tmpl, nil)
		if err == nil || !strings.Contains(err.Error(), "misplaced |/raw| directive") {
			t.Fatalf("expected misplaced |/raw| directive error, got %v", err)
		}
	})

	t.Run("Concurrent raw block rendering", func(t *testing.T) {
		source := `|raw|
<script>
    const res = primary || fallback;
    const path = "C:\\temp\\file.txt";
</script>
|/raw|
|if active|Hello |name|!|/if|`
		compiled, err := engine.Compile(source)
		if err != nil {
			t.Fatalf("failed to compile template: %v", err)
		}

		var wg sync.WaitGroup
		for i := 0; i < 100; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				var buf bytes.Buffer
				context := NewContext(map[string]any{"active": true, "name": "Alice"})
				if err := compiled.RootNode.Render(context, &buf); err != nil {
					t.Errorf("concurrent render error: %v", err)
				}
				if !strings.Contains(buf.String(), `primary || fallback`) || !strings.Contains(buf.String(), "Hello Alice!") {
					t.Errorf("concurrent render output mismatch: %s", buf.String())
				}
			}()
		}
		wg.Wait()
	})
}

func TestIssue2EndToEndFullEnginePipelineRegression(t *testing.T) {
	tempDir := t.TempDir()

	// Create real template files on disk
	pageContent := `|page auth=false|
<!DOCTYPE html>
<html>
<head>
    <title>E2E Test</title>
</head>
<body>
    <div id="path-container">Path: C:\app\templates\page.pte</div>
    <div id="unc-container">UNC: \\server\share\folder</div>
    <div x-data="{ open: false }" x-show="open \|\| loading">JS Escaped</div>
    |raw|
    <script>
        const rawPath = "C:\\app\\templates\\page.pte";
        const isReady = primary || fallback;
        const comment = "<!-- must not be stripped -->";
    </script>
    |/raw|
    |if active||user.name||/if|
    |fragment status|
        <span>Status: |status|</span>
    |/fragment|
</body>
</html>`

	if err := os.WriteFile(filepath.Join(tempDir, "dashboard.pte"), []byte(pageContent), 0644); err != nil {
		t.Fatalf("failed to write test template: %v", err)
	}

	engine := NewEngine(tempDir, WithMinify(true))
	data := map[string]any{
		"active": true,
		"user":   map[string]any{"name": "Alice"},
		"status": "Online",
	}

	t.Run("1. Disk template render (Engine.Render)", func(t *testing.T) {
		var buf bytes.Buffer
		if err := engine.Render(&buf, "dashboard", data); err != nil {
			t.Fatalf("Engine.Render failed: %v", err)
		}

		out := buf.String()
		// Validate Windows path backslashes preserved
		if !strings.Contains(out, `C:\app\templates\page.pte`) {
			t.Errorf("Windows path lost backslashes in disk render: %s", out)
		}
		// Validate UNC path backslashes preserved
		if !strings.Contains(out, `\\server\share\folder`) {
			t.Errorf("UNC path lost backslashes in disk render: %s", out)
		}
		// Validate JS logical OR escaped pipe
		if !strings.Contains(out, `x-show="open || loading"`) {
			t.Errorf("Escaped double pipe failed in disk render: %s", out)
		}
		// Validate raw block byte-for-byte content
		expectedRawJS := `const rawPath = "C:\\app\\templates\\page.pte";
        const isReady = primary || fallback;
        const comment = "<!-- must not be stripped -->";`
		if !strings.Contains(out, expectedRawJS) {
			t.Errorf("Raw block content transformed in minified disk render: %s", out)
		}
		// Validate adjacent directives
		if !strings.Contains(out, `Alice`) {
			t.Errorf("Adjacent directive failed in disk render: %s", out)
		}
	})

	t.Run("2. Asynchronous streaming pipeline (Engine.RenderStream)", func(t *testing.T) {
		streamReader := engine.RenderStream("dashboard", data)
		streamBytes, err := io.ReadAll(streamReader)
		if err != nil {
			t.Fatalf("RenderStream read error: %v", err)
		}

		out := string(streamBytes)
		if !strings.Contains(out, `C:\app\templates\page.pte`) || !strings.Contains(out, `x-show="open || loading"`) {
			t.Errorf("RenderStream output corrupted: %s", out)
		}
	})

	t.Run("3. HTTP Web Server FileRouter pipeline", func(t *testing.T) {
		routesDir := filepath.Join(tempDir, "routes")
		if err := os.MkdirAll(routesDir, 0755); err != nil {
			t.Fatal(err)
		}

		routeContent := `|page auth=false|
<div>
    <h1>Route Test</h1>
    <p>Windows: C:\\routes\\page.pte</p>
    |raw|
    <script>
        const routeJS = a || b;
    </script>
    |/raw|
</div>`
		if err := os.WriteFile(filepath.Join(routesDir, "+page.pte"), []byte(routeContent), 0644); err != nil {
			t.Fatal(err)
		}

		routerEngine := NewEngine("")
		fileRouter, err := NewFileRouter(routerEngine, routesDir)
		if err != nil {
			t.Fatalf("NewFileRouter error: %v", err)
		}

		req := httptest.NewRequest("GET", "/", nil)
		rec := httptest.NewRecorder()
		fileRouter.ServeHTTP(rec, req)

		out := rec.Body.String()
		if !strings.Contains(out, `C:\\routes\\page.pte`) {
			t.Errorf("HTTP router output corrupted Windows path: %s", out)
		}
		if !strings.Contains(out, `const routeJS = a || b;`) {
			t.Errorf("HTTP router output corrupted raw block: %s", out)
		}
	})

	t.Run("4. Fragment rendering pipeline (Engine.RenderFragment)", func(t *testing.T) {
		var buf bytes.Buffer
		if err := engine.RenderFragment(&buf, "dashboard", "status", data); err != nil {
			t.Fatalf("RenderFragment error: %v", err)
		}
		if !strings.Contains(buf.String(), "Status: Online") {
			t.Errorf("RenderFragment output mismatch: %s", buf.String())
		}
	})
}

func TestIssues6To10EndToEndFullPipelineSuite(t *testing.T) {
	// Embedded filesystem fixture for Issue #8 (WithFS)
	appFS := fstest.MapFS{
		"templates/layouts/main.pte": &fstest.MapFile{
			Data: []byte(`<!DOCTYPE html><html><head><title>App</title></head><body>|yield content|</body></html>`),
		},
		"templates/pages/dashboard.pte": &fstest.MapFile{
			Data: []byte(`|layout layouts/main|
|section content|
<main>
    <h1>Dashboard</h1>
    <div id="named-key">Answer: |dataMap.answer|</div>
    <div id="int-key">Category: |intMap.100|</div>
    <div id="user-exported">User: |user.Name|</div>
    <div id="user-getter">Secret: |user.GetSecretCode|</div>
    <div id="opt-chain">Optional: |user?.secretCode|</div>
    |fragment user_fragment|
        <section class="user-card">
            <span>User Fragment: |user.Name|</span>
        </section>
    |/fragment|
    |fragment stats_fragment|
        <section class="stats-card">
            <span>Stats: |intMap.100|</span>
        </section>
    |/fragment|
</main>
|/section|`),
		},
		"templates/pages/failing.pte": &fstest.MapFile{
			Data: []byte(`|fragment failing_fragment|
    <div>|nonexistentVar.subProperty|</div>
|/fragment|`),
		},
	}

	engine := NewEngine("templates", WithFS(appFS), WithMinify(true))

	type Key string
	type CatID int

	data := map[string]any{
		"dataMap": map[Key]string{"answer": "42"},
		"intMap":  map[CatID]string{100: "Hardware"},
		"user": UserWithGetter{
			Name:       "Alice",
			secretCode: "topsecret",
		},
	}

	t.Run("1. Issue #8 (WithFS) + Issue #6 (Named Map Keys) + Issue #7 (Struct Getters/Opt Chain) + Issue #10 (Minified Output)", func(t *testing.T) {
		var buf bytes.Buffer
		if err := engine.Render(&buf, "pages/dashboard", data); err != nil {
			t.Fatalf("End-to-end full page render failed: %v", err)
		}

		out := buf.String()
		// Verify minification
		if strings.Contains(out, "\n") {
			t.Errorf("output should be minified into a single line, got: %s", out)
		}
		// Verify Issue #6 (Named Map Keys)
		if !strings.Contains(out, "Answer: 42") {
			t.Errorf("named map key failed in E2E render: %s", out)
		}
		if !strings.Contains(out, "Category: Hardware") {
			t.Errorf("named int map key failed in E2E render: %s", out)
		}
		// Verify Issue #7 (Struct Getters and Optional Chaining)
		if !strings.Contains(out, "Secret: topsecret") {
			t.Errorf("struct getter failed in E2E render: %s", out)
		}
		if !strings.Contains(out, "Optional: ") {
			t.Errorf("optional chaining failed in E2E render: %s", out)
		}
	})

	t.Run("2. Issue #9 (RenderString Plain Source Text)", func(t *testing.T) {
		var buf bytes.Buffer
		if err := engine.RenderString(&buf, "pages/dashboard", data); err != nil {
			t.Fatalf("RenderString failed: %v", err)
		}
		if got := buf.String(); got != "pages/dashboard" {
			t.Errorf("RenderString must render literal text 'pages/dashboard', got %q", got)
		}
	})

	t.Run("3. Issue #10 (Single & Multiple Fragments Rendered Once & Minified)", func(t *testing.T) {
		var bufSingle bytes.Buffer
		if err := engine.RenderFragment(&bufSingle, "pages/dashboard", "user_fragment", data); err != nil {
			t.Fatalf("RenderFragment failed: %v", err)
		}
		if got := bufSingle.String(); !strings.Contains(got, `<section class="user-card">`) {
			t.Errorf("RenderFragment output mismatch: %s", got)
		}

		var bufMulti bytes.Buffer
		if err := engine.RenderFragments(&bufMulti, "pages/dashboard", []string{"user_fragment", "stats_fragment"}, data); err != nil {
			t.Fatalf("RenderFragments failed: %v", err)
		}
		expectedMulti := `<section class="user-card"><span>User Fragment: Alice</span></section><section class="stats-card"><span>Stats: Hardware</span></section>`
		if got := bufMulti.String(); got != expectedMulti {
			t.Errorf("RenderFragments expected %q, got %q", expectedMulti, got)
		}
	})

	t.Run("4. Issue #10 (Atomic Error Safety - 0 bytes written on failure)", func(t *testing.T) {
		var bufAtomic bytes.Buffer
		err := engine.RenderFragments(&bufAtomic, "pages/failing", []string{"failing_fragment"}, data)
		if err == nil {
			t.Fatal("expected error for failing fragment")
		}
		if bufAtomic.Len() != 0 {
			t.Errorf("expected 0 bytes written to writer on error, got %d bytes (%q)", bufAtomic.Len(), bufAtomic.String())
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

func TestIssue4OverflowSafeRangeAndSeparatorSuite(t *testing.T) {
	engine := NewEngine("")

	t.Run("1. Reported overflow case: start=-10, end=MaxInt64, step=MaxInt64", func(t *testing.T) {
		data := map[string]any{
			"start": int64(-10),
			"end":   int64(math.MaxInt64),
			"step":  int64(math.MaxInt64),
		}
		var buf bytes.Buffer
		if err := engine.RenderString(&buf, "|for i from start to end step step||i||separator|,|/separator||/for|", data); err != nil {
			t.Fatalf("unexpected error rendering reported overflow case: %v", err)
		}
		expected := fmt.Sprintf("-10,%d", uint64(math.MaxInt64)-10)
		if got := buf.String(); got != expected {
			t.Errorf("expected %q, got %q", expected, got)
		}
	})

	t.Run("2. Ascending ranges near MaxInt64", func(t *testing.T) {
		data := map[string]any{
			"start": int64(math.MaxInt64 - 2),
			"end":   int64(math.MaxInt64),
		}
		var buf bytes.Buffer
		if err := engine.RenderString(&buf, "|for i from start to end||i||separator|,|/separator||/for|", data); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		expected := fmt.Sprintf("%d,%d,%d", math.MaxInt64-2, math.MaxInt64-1, math.MaxInt64)
		if got := buf.String(); got != expected {
			t.Errorf("expected %q, got %q", expected, got)
		}
	})

	t.Run("3. Descending ranges near MinInt64", func(t *testing.T) {
		data := map[string]any{
			"start": int64(math.MinInt64 + 2),
			"end":   int64(math.MinInt64),
		}
		var buf bytes.Buffer
		if err := engine.RenderString(&buf, "|for i from start to end||i||separator|,|/separator||/for|", data); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		expected := fmt.Sprintf("%d,%d,%d", math.MinInt64+2, math.MinInt64+1, math.MinInt64)
		if got := buf.String(); got != expected {
			t.Errorf("expected %q, got %q", expected, got)
		}
	})

	t.Run("4. Ascending range spanning negative to large positive", func(t *testing.T) {
		var buf bytes.Buffer
		if err := engine.RenderString(&buf, "|for i from -100 to 100 step 50||i||separator|, |/separator||/for|", nil); err != nil {
			t.Fatal(err)
		}
		if got := buf.String(); got != "-100, -50, 0, 50, 100" {
			t.Errorf("expected '-100, -50, 0, 50, 100', got %q", got)
		}
	})

	t.Run("5. Descending range spanning large positive to negative", func(t *testing.T) {
		var buf bytes.Buffer
		if err := engine.RenderString(&buf, "|for i from 100 to -100 step 50||i||separator|, |/separator||/for|", nil); err != nil {
			t.Fatal(err)
		}
		if got := buf.String(); got != "100, 50, 0, -50, -100" {
			t.Errorf("expected '100, 50, 0, -50, -100', got %q", got)
		}
	})

	t.Run("6. Step larger than remaining distance", func(t *testing.T) {
		var buf bytes.Buffer
		if err := engine.RenderString(&buf, "|for i from 1 to 5 step 10||i||separator|,|/separator||/for|", nil); err != nil {
			t.Fatal(err)
		}
		if got := buf.String(); got != "1" {
			t.Errorf("expected '1', got %q", got)
		}
	})

	t.Run("7. Endpoint reached exactly by step", func(t *testing.T) {
		var buf bytes.Buffer
		if err := engine.RenderString(&buf, "|for i from 0 to 10 step 5||i||separator|,|/separator||/for|", nil); err != nil {
			t.Fatal(err)
		}
		if got := buf.String(); got != "0,5,10" {
			t.Errorf("expected '0,5,10', got %q", got)
		}
	})

	t.Run("8. Endpoint not reached exactly by step", func(t *testing.T) {
		var buf bytes.Buffer
		if err := engine.RenderString(&buf, "|for i from 0 to 9 step 5||i||separator|,|/separator||/for|", nil); err != nil {
			t.Fatal(err)
		}
		if got := buf.String(); got != "0,5" {
			t.Errorf("expected '0,5', got %q", got)
		}
	})

	t.Run("9. Single-value range", func(t *testing.T) {
		var buf bytes.Buffer
		if err := engine.RenderString(&buf, "|for i from 5 to 5 step 1||i||separator|,|/separator||/for|", nil); err != nil {
			t.Fatal(err)
		}
		if got := buf.String(); got != "5" {
			t.Errorf("expected '5', got %q", got)
		}
	})

	t.Run("10. Zero, negative, and fractional steps rejected", func(t *testing.T) {
		var buf bytes.Buffer
		if err := engine.RenderString(&buf, "|for i from 1 to 5 step 0||i||/for|", nil); err == nil {
			t.Error("expected error for step 0")
		}

		buf.Reset()
		if err := engine.RenderString(&buf, "|for i from 1 to 5 step -1||i||/for|", nil); err == nil {
			t.Error("expected error for step -1")
		}

		buf.Reset()
		if err := engine.RenderString(&buf, "|for i from 1 to 5 step 1.5||i||/for|", nil); err == nil {
			t.Error("expected error for step 1.5")
		}
	})

	t.Run("11. continue in first, middle, and last iteration", func(t *testing.T) {
		// First iteration continued
		var buf1 bytes.Buffer
		if err := engine.RenderString(&buf1, "|for i from 1 to 3||if i == 1||continue||/if||i||separator|,|/separator||/for|", nil); err != nil {
			t.Fatal(err)
		}
		if got := buf1.String(); got != "2,3" {
			t.Errorf("first iteration continue: expected '2,3', got %q", got)
		}

		// Middle iteration continued
		var buf2 bytes.Buffer
		if err := engine.RenderString(&buf2, "|for i from 1 to 3||if i == 2||continue||/if||i||separator|,|/separator||/for|", nil); err != nil {
			t.Fatal(err)
		}
		if got := buf2.String(); got != "1,3" {
			t.Errorf("middle iteration continue: expected '1,3', got %q", got)
		}

		// Last iteration continued
		var buf3 bytes.Buffer
		if err := engine.RenderString(&buf3, "|for i from 1 to 3||if i == 3||continue||/if||i||separator|,|/separator||/for|", nil); err != nil {
			t.Fatal(err)
		}
		if got := buf3.String(); got != "1,2" {
			t.Errorf("last iteration continue: expected '1,2', got %q", got)
		}
	})

	t.Run("12. Multiple consecutive continued iterations", func(t *testing.T) {
		var buf bytes.Buffer
		if err := engine.RenderString(&buf, "|for i from 1 to 4||if i == 2 or i == 3||continue||/if||i||separator|,|/separator||/for|", nil); err != nil {
			t.Fatal(err)
		}
		if got := buf.String(); got != "1,4" {
			t.Errorf("consecutive continue: expected '1,4', got %q", got)
		}
	})

	t.Run("13. All iterations continued renders empty string", func(t *testing.T) {
		var buf bytes.Buffer
		if err := engine.RenderString(&buf, "|for i from 1 to 3||continue||i||separator|,|/separator||/for|", nil); err != nil {
			t.Fatal(err)
		}
		if got := buf.String(); got != "" {
			t.Errorf("all iterations continued: expected '', got %q", got)
		}
	})

	t.Run("14. break in first, middle, and last iteration", func(t *testing.T) {
		// First iteration break
		var buf1 bytes.Buffer
		if err := engine.RenderString(&buf1, "|for i from 1 to 3||if i == 1||break||/if||i||separator|,|/separator||/for|", nil); err != nil {
			t.Fatal(err)
		}
		if got := buf1.String(); got != "" {
			t.Errorf("first iteration break: expected '', got %q", got)
		}

		// Middle iteration break
		var buf2 bytes.Buffer
		if err := engine.RenderString(&buf2, "|for i from 1 to 3||if i == 2||break||/if||i||separator|,|/separator||/for|", nil); err != nil {
			t.Fatal(err)
		}
		if got := buf2.String(); got != "1" {
			t.Errorf("middle iteration break: expected '1', got %q", got)
		}
	})

	t.Run("15. Nested ranges where inner loop continues or breaks", func(t *testing.T) {
		tmpl := `|for i from 1 to 2||for j from 1 to 3||if j == 2||continue||/if||i|-|j||separator|;|/separator||/for||separator| |/separator||/for|`
		var buf bytes.Buffer
		if err := engine.RenderString(&buf, tmpl, nil); err != nil {
			t.Fatal(err)
		}
		expected := "1-1;1-3 2-1;2-3"
		if got := buf.String(); got != expected {
			t.Errorf("nested range continue: expected %q, got %q", expected, got)
		}
	})

	t.Run("16. Body rendering empty string still renders separators", func(t *testing.T) {
		tmpl := `|for i from 1 to 3||if i == 2||else|x|/if||separator|,|/separator||/for|`
		var buf bytes.Buffer
		if err := engine.RenderString(&buf, tmpl, nil); err != nil {
			t.Fatal(err)
		}
		expected := "x,,x"
		if got := buf.String(); got != expected {
			t.Errorf("empty body string iteration: expected %q, got %q", expected, got)
		}
	})
}

func TestIssue5HTMLMinifierCommentAndAttributeProtectionSuite(t *testing.T) {
	t.Run("1. Comment-like sequence in double-quoted attribute", func(t *testing.T) {
		html := `<div title="<!--keep-->">Content</div>`
		min := MinifyHTML(html)
		if !strings.Contains(min, `title="<!--keep-->"`) {
			t.Errorf("expected title=\"<!--keep-->\" preserved, got %q", min)
		}
	})

	t.Run("2. Comment-like sequence in single-quoted attribute", func(t *testing.T) {
		html := `<div title='<!--keep-->'>Content</div>`
		min := MinifyHTML(html)
		if !strings.Contains(min, `title='<!--keep-->'`) {
			t.Errorf("expected title='<!--keep-->' preserved, got %q", min)
		}
	})

	t.Run("3. > inside each quote style", func(t *testing.T) {
		htmlDouble := `<div data-test="a > b">Content</div>`
		minDouble := MinifyHTML(htmlDouble)
		if !strings.Contains(minDouble, `data-test="a > b"`) {
			t.Errorf("expected double-quoted > preserved, got %q", minDouble)
		}

		htmlSingle := `<div data-test='a > b'>Content</div>`
		minSingle := MinifyHTML(htmlSingle)
		if !strings.Contains(minSingle, `data-test='a > b'`) {
			t.Errorf("expected single-quoted > preserved, got %q", minSingle)
		}
	})

	t.Run("4. Multiple attributes containing comment markers and > characters", func(t *testing.T) {
		html := `<div title="<!--keep-->" data-expression="value > minimum">Content</div>`
		min := MinifyHTML(html)
		if !strings.Contains(min, `title="<!--keep-->"`) || !strings.Contains(min, `data-expression="value > minimum"`) {
			t.Errorf("expected multiple attributes preserved, got %q", min)
		}
	})

	t.Run("5. Whitespace and line breaks between attributes", func(t *testing.T) {
		html := "<div\n    title=\"<!--keep-->\"\n    data-expression=\"value > minimum\">\n    Content\n</div>"
		min := MinifyHTML(html)
		if !strings.Contains(min, `title="<!--keep-->"`) || !strings.Contains(min, `data-expression="value > minimum"`) {
			t.Errorf("expected attributes preserved across linebreaks, got %q", min)
		}
	})

	t.Run("6. Empty quoted attribute followed by comment-like attribute", func(t *testing.T) {
		html := `<div title="" data-comment="<!--keep-->"></div>`
		min := MinifyHTML(html)
		if !strings.Contains(min, `title="" data-comment="<!--keep-->"`) {
			t.Errorf("expected empty attribute and comment attribute preserved, got %q", min)
		}
	})

	t.Run("7. Real comments before and after an element", func(t *testing.T) {
		html := `<!-- before --><div>Before</div><div>After</div><!-- after -->`
		min := MinifyHTML(html)
		if min != "<div>Before</div><div>After</div>" {
			t.Errorf("expected real comments removed, got %q", min)
		}
	})

	t.Run("8. Comment-like text in element text after HTML escaping", func(t *testing.T) {
		html := `<div>&lt;!--keep--&gt;</div>`
		min := MinifyHTML(html)
		if !strings.Contains(min, `&lt;!--keep--&gt;`) {
			t.Errorf("expected HTML escaped text preserved, got %q", min)
		}
	})

	t.Run("9. Comment-like strings inside script", func(t *testing.T) {
		html := "<script>\nconst text = \"<!--not an HTML comment here-->\";\n</script>"
		min := MinifyHTML(html)
		if !strings.Contains(min, "<!--not an HTML comment here-->") {
			t.Errorf("expected script comment string preserved, got %q", min)
		}
	})

	t.Run("10. Comment-like strings inside style", func(t *testing.T) {
		html := "<style>\n.example::before {\n    content: \"<!--keep-->\";\n}\n</style>"
		min := MinifyHTML(html)
		if !strings.Contains(min, "<!--keep-->") {
			t.Errorf("expected style comment string preserved, got %q", min)
		}
	})

	t.Run("11. Comment-like content inside PTE raw block", func(t *testing.T) {
		engine := NewEngine("", WithMinify(true))
		var buf bytes.Buffer
		tmpl := `|raw|<div title="<!--keep-->">C:\\path</div>|/raw|`
		if err := engine.RenderString(&buf, tmpl, nil); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(buf.String(), `title="<!--keep-->"`) {
			t.Errorf("expected PTE raw block title attribute preserved, got %q", buf.String())
		}
	})

	t.Run("12. Adjacent real comments and quoted attribute values", func(t *testing.T) {
		html := `<!-- comment1 --><div title="<!--keep-->"></div><!-- comment2 -->`
		min := MinifyHTML(html)
		if !strings.Contains(min, `<div title="<!--keep-->"></div>`) || strings.Contains(min, `comment1`) || strings.Contains(min, `comment2`) {
			t.Errorf("expected real comments removed and title attribute preserved, got %q", min)
		}
	})

	t.Run("13. Uppercase or mixed-case SCRIPT and STYLE tags", func(t *testing.T) {
		html := `<SCRIPT>\nconst x = "<!--keep-->";\n</SCRIPT><STYLE>.a{content:"<!--keep-->";}</STYLE>`
		min := MinifyHTML(html)
		if !strings.Contains(min, "<!--keep-->") {
			t.Errorf("expected uppercase raw tag content preserved, got %q", min)
		}
	})

	t.Run("14. Attribute values containing PTE pipe characters and escaped pipes", func(t *testing.T) {
		html := `<div data-val="a || b \| c"></div>`
		min := MinifyHTML(html)
		if !strings.Contains(min, `data-val="a || b \| c"`) {
			t.Errorf("expected pipes in attribute preserved, got %q", min)
		}
	})

	t.Run("15. Malformed or unterminated comments, tags, and quoted attributes", func(t *testing.T) {
		malformedInputs := []string{
			`<div title="<!--keep-->`,
			`<!-- unterminated comment`,
			`<script>const x = "1";`,
			`<div title='<!--keep-->`,
			`<div data-val="a > b`,
		}
		for _, input := range malformedInputs {
			// Ensure deterministic execution without panic or hang
			_ = MinifyHTML(input)
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

func TestAll34FeaturesEndToEndMasterSuite(t *testing.T) {
	// Setup embedded filesystem fixture for Feature 3 & 24
	appFS := fstest.MapFS{
		"templates/layouts/main.pte": &fstest.MapFile{
			Data: []byte(`<!DOCTYPE html><html><head><title>|yield title|</title>|pwa name='Master' theme='#000' icon='/i.png' manifest='/m.json' sw='/sw.js'|</head><body>|yield content|</body></html>`),
		},
		"templates/components/card.pte": &fstest.MapFile{
			Data: []byte(`<div class="card"><h3>|slot title|</h3><p>|slot body|</p></div>`),
		},
		"templates/partials/nav.pte": &fstest.MapFile{
			Data: []byte(`<nav>Navigation: |subItem.title|</nav>`),
		},
		"templates/pages/master.pte": &fstest.MapFile{
			Data: []byte(`|layout layouts/main|
|section title|Master E2E|/section|
|section content|
    |model models.TaskModel|
    |# Single-line comment |
    |#
       Multi-line comment block
    #|
    |-- Legacy pipe comment --|
    <main>
        <h1>|title, trim, upper|</h1>
        <div id="esc">|rawInput|</div>
        <div id="html">|html trustedHTML|</div>
        <div id="attr"><input value="|attr attrInput|"></div>
        <div id="url"><a href="/search?q=|url queryInput|">Link</a></div>
        <div id="json"><script>const cfg = |json jsonInput|;</script></div>
        <div id="opt">User: |user?.Profile?.DisplayName ?? 'Guest'|</div>
        <div id="ternary">Status: |active ? 'Online' : 'Offline'|</div>

        |if role == 'admin'|
            <p>Admin Role</p>
        |else if role == 'manager'|
            <p>Manager Role</p>
        |else|
            <p>User Role</p>
        |/if|

        <ul id="loop">
        |each item in items|
            <li>Item |each.count| of |each.total| (Index: |each.index|): |item.name|</li>|separator|, |/separator|
        |else|
            <li>No items</li>
        |/each|
        </ul>

        |for i from 1 to 5 step 2|
            <span>Num: |i|</span>|separator|; |/separator|
        |/for|

        |switch status|
            |case 'pending'|
                <span>Pending</span>
            |case 'approved'|
                <span>Approved</span>
                |fallthrough|
            |case 'notified'|
                <span>Notified</span>
            |default|
                <span>Unknown</span>
        |/switch|

        |include partials/nav with subData|

        |macro badge(text, color)|
            <span class="badge badge-|color|">|text|</span>
        |/macro|
        <div id="macro-call">|call badge('Active', 'success')|</div>

        |component components/card|
            |slot title| Card Title |/slot|
            |slot body| Card Body |/slot|
        |/component|

        <input |field formUser.email|>
        |display formUser.bio|
        |editor formUser.bio|

        |raw|
        <script>
            const unparsed = "C:\\app\\path" || false;
        </script>
        |/raw|

        |attempt|
            <div>|user.MissingField.Value|</div>
        |recover as err|
            <div class="err">Caught: |err|</div>
        |/attempt|

        |fragment header_frag|
            <header>Header Fragment</header>
        |/fragment|
        |fragment footer_frag|
            <footer>Footer Fragment</footer>
        |/fragment|
    </main>
|/section|`),
		},
	}

	engine := NewEngine("templates", WithFS(appFS), WithMinify(true))

	type Key string
	data := map[string]any{
		"title":       "  all 34 features  ",
		"rawInput":    "<script>alert(1)</script>",
		"trustedHTML": "<strong>Trusted</strong>",
		"attrInput":   `Hello "World"`,
		"queryInput":  "coffee & tea",
		"jsonInput":   map[string]any{"id": 101},
		"active":      true,
		"role":        "admin",
		"items":       []map[string]string{{"name": "Alpha"}, {"name": "Beta"}},
		"status":      "approved",
		"subData":     map[string]any{"subItem": map[string]string{"title": "SubNav"}},
		"formUser":    map[string]any{"email": "test@example.com", "bio": "Bio text"},
		"mapData":     map[Key]string{"key": "val"},
		"user":        UserWithGetter{Name: "Alice", secretCode: "secret"},
	}

	t.Run("Features 1-34 End-to-End Master Pipeline Verification", func(t *testing.T) {
		var buf bytes.Buffer
		if err := engine.Render(&buf, "pages/master", data); err != nil {
			t.Fatalf("Master E2E render failed: %v", err)
		}

		out := buf.String()
		// 1. Minification
		if !strings.Contains(out, "<h1>ALL 34 FEATURES</h1><div id=\"esc\">") {
			t.Errorf("expected HTML minification around tags, got: %s", out)
		}
		// 2. Auto HTML Escaping
		if !strings.Contains(out, "&lt;script&gt;alert(1)&lt;/script&gt;") {
			t.Errorf("Auto HTML escaping failed: %s", out)
		}
		// 3. Raw HTML
		if !strings.Contains(out, "<strong>Trusted</strong>") {
			t.Errorf("Raw HTML failed: %s", out)
		}
		// 4. Attribute Escaping
		if !strings.Contains(out, `value="Hello &quot;World&quot;"`) {
			t.Errorf("Attribute escaping failed: %s", out)
		}
		// 5. URL Encoding
		if !strings.Contains(out, "coffee+%26+tea") {
			t.Errorf("URL encoding failed: %s", out)
		}
		// 6. JSON Encoding
		if !strings.Contains(out, `const cfg = {"id":101};`) {
			t.Errorf("JSON encoding failed: %s", out)
		}
		// 7. Filters (trim + upper)
		if !strings.Contains(out, "<h1>ALL 34 FEATURES</h1>") {
			t.Errorf("Filter chaining failed: %s", out)
		}
		// 8. Loops & Separators
		if !strings.Contains(out, "Item 1 of 2 (Index: 0): Alpha</li> , <li>Item 2 of 2 (Index: 1): Beta</li>") {
			t.Errorf("Loop & separator failed: %s", out)
		}
		// 9. Range for loop with step
		if !strings.Contains(out, "Num: 1</span> ; <span>Num: 3</span> ; <span>Num: 5</span>") {
			t.Errorf("Range for loop with step failed: %s", out)
		}
		// 10. Switch with fallthrough
		if !strings.Contains(out, "Approved</span><span>Notified") {
			t.Errorf("Switch with fallthrough failed: %s", out)
		}
		// 11. Includes with sub-model
		if !strings.Contains(out, "Navigation: SubNav") {
			t.Errorf("Include with sub-model failed: %s", out)
		}
		// 12. Macro call
		if !strings.Contains(out, `<span class="badge badge-success">Active</span>`) {
			t.Errorf("Macro call failed: %s", out)
		}
		// 13. Components & Slots
		if !strings.Contains(out, `<div class="card"><h3> Card Title </h3><p> Card Body </p></div>`) {
			t.Errorf("Component & slot failed: %s", out)
		}
		// 14. Form field binding
		if !strings.Contains(out, `name="email" id="email" value="test@example.com"`) {
			t.Errorf("Form field binding failed: %s", out)
		}
		// 15. Raw Block
		if !strings.Contains(out, `const unparsed = "C:\\app\\path" || false;`) {
			t.Errorf("Raw block failed: %s", out)
		}
		// 16. Attempt / Recover
		if !strings.Contains(out, `Caught: `) {
			t.Errorf("Attempt / Recover failed: %s", out)
		}
		// 17. PWA meta tag
		if !strings.Contains(out, `<meta name="application-name" content="Master">`) {
			t.Errorf("PWA meta tag failed: %s", out)
		}
	})

	t.Run("Stream & Fragment E2E Pipelines", func(t *testing.T) {
		// RenderStream
		reader := engine.RenderStream("pages/master", data)
		streamBytes, err := io.ReadAll(reader)
		if err != nil {
			t.Fatalf("RenderStream error: %v", err)
		}
		if !strings.Contains(string(streamBytes), "<h1>ALL 34 FEATURES</h1>") {
			t.Errorf("RenderStream pipeline failed")
		}

		// RenderFragments
		var fragBuf bytes.Buffer
		if err := engine.RenderFragments(&fragBuf, "pages/master", []string{"header_frag", "footer_frag"}, data); err != nil {
			t.Fatalf("RenderFragments error: %v", err)
		}
		if got := fragBuf.String(); got != "<header>Header Fragment</header><footer>Footer Fragment</footer>" {
			t.Errorf("RenderFragments pipeline output mismatch: %q", got)
		}
	})
}

func FuzzGeneralTemplateRendering(f *testing.F) {
	seeds := []string{
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

func FuzzLiteralPipeAndRawLexer(f *testing.F) {
	seeds := []string{
		`\|`,
		`\|\|`,
		`\\|`,
		`\\\|`,
		`|raw|a || b|/raw|`,
		`|raw|C:\\temp\|value|/raw|`,
		`|if true||name||/if|`,
		`|raw|unclosed`,
	}

	for _, seed := range seeds {
		f.Add(seed)
	}

	lexer := NewLexer()

	f.Fuzz(func(t *testing.T, template string) {
		tokens, err := lexer.Tokenize(template)
		if err != nil {
			return
		}

		for _, tok := range tokens {
			if tok.Type == TokenRaw {
				if strings.Contains(tok.Value, "|raw|") {
					t.Errorf("TokenRaw contained nested raw block: %s", tok.Value)
				}
			}
		}
	})
}

func rangeFitsTestLimit(start, end, step int64, limit uint64) bool {
	if step <= 0 || limit == 0 {
		return false
	}

	var distance uint64
	if start <= end {
		distance = uint64(end) - uint64(start)
	} else {
		distance = uint64(start) - uint64(end)
	}

	// Iteration count is distance/step + 1.
	// Avoid adding 1 because that could overflow.
	return distance/uint64(step) < limit
}

func referenceRangeIterator(start, end, step int64) ([]int64, bool) {
	if step <= 0 {
		return nil, false
	}

	var values []int64
	uStep := uint64(step)
	curr := start
	isAscending := start <= end

	for {
		values = append(values, curr)

		var dist uint64
		if isAscending {
			dist = uint64(end) - uint64(curr)
		} else {
			dist = uint64(curr) - uint64(end)
		}

		if uStep > dist {
			break
		}

		if isAscending {
			curr += int64(uStep)
		} else {
			curr -= int64(uStep)
		}
	}

	return values, true
}

func FuzzNumericRangeIterator(f *testing.F) {
	// Seed with boundary-biased inputs around MinInt64, -1, 0, 1, MaxInt64
	boundaryValues := []int64{
		math.MinInt64,
		math.MinInt64 + 1,
		-100,
		-10,
		-1,
		0,
		1,
		10,
		100,
		math.MaxInt64 - 1,
		math.MaxInt64,
	}

	for _, start := range boundaryValues {
		for _, end := range boundaryValues {
			for _, step := range []int64{-1, 0, 1, 5, math.MaxInt64} {
				f.Add(start, end, step)
			}
		}
	}

	engine := NewEngine("")

	f.Fuzz(func(t *testing.T, start, end, step int64) {
		data := map[string]any{
			"start": start,
			"end":   end,
			"step":  step,
		}

		// Invalid steps should still reach the engine and be tested.
		if step <= 0 {
			var buf bytes.Buffer
			err := engine.RenderString(
				&buf,
				"|for i from start to end step step||i||/for|",
				data,
			)

			if err == nil {
				t.Fatalf("expected error for invalid step %d", step)
			}
			return
		}

		const testIterationLimit uint64 = 100

		// Do not ask the renderer to produce billions of values.
		if !rangeFitsTestLimit(start, end, step, testIterationLimit) {
			t.Skip()
		}

		expectedValues, valid := referenceRangeIterator(start, end, step)
		if !valid {
			t.Fatal("positive step unexpectedly rejected")
		}

		var buf bytes.Buffer
		err := engine.RenderString(
			&buf,
			"|for i from start to end step step||i||separator|,|/separator||/for|",
			data,
		)
		if err != nil {
			t.Fatalf(
				"unexpected error for start=%d end=%d step=%d: %v",
				start,
				end,
				step,
				err,
			)
		}

		expectedStrings := make([]string, len(expectedValues))
		for i, value := range expectedValues {
			expectedStrings[i] = strconv.FormatInt(value, 10)
		}

		expected := strings.Join(expectedStrings, ",")
		if buf.String() != expected {
			t.Fatalf("expected %q, got %q", expected, buf.String())
		}
	})
}

func FuzzHTMLMinifierContextAware(f *testing.F) {
	seeds := []string{
		`<div title="<!--keep-->" data-message="A > B">Content</div>`,
		`<div title='<!--keep-->'>Content</div>`,
		`<script>const x = "<!--not comment-->";</script>`,
		`<style>.class::before { content: "<!--keep-->"; }</style>`,
		`<!-- real comment --><div>Hello</div><!-- comment 2 -->`,
		`<div title="" data-comment="<!--keep-->"></div>`,
		`<div data-val="a || b \| c"></div>`,
		`<div>&lt;!--keep--&gt;</div>`,
		`<div title="<!--keep-->`,
		`<!-- unterminated`,
		`<SCRIPT>let a = '<!-- unicode ❤️ -->';</SCRIPT>`,
		`<STYLE>body { content: '<!-- café -->'; }</STYLE>`,
		`|raw|<div title="<!--keep-->">PTE raw</div>|/raw|`,
		`<div attr=unquoted><!--comment--><span>text</span></div>`,
		`<div attr="quote > text" attr2='single > quote'><!-- --></div>`,
		`<a href="url?a=1&b=2" title="<!-- comment -->">Unicode: 你好</a>`,
	}

	for _, seed := range seeds {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, html string) {
		min := MinifyHTML(html)

		if strings.Contains(html, `<div title="<!--keep-->">`) {
			if !strings.Contains(min, `title="<!--keep-->"`) {
				t.Errorf("MinifyHTML corrupted title=\"<!--keep-->\": %s", min)
			}
		}
		if strings.Contains(html, `<div title='<!--keep-->'>`) {
			if !strings.Contains(min, `title='<!--keep-->'`) {
				t.Errorf("MinifyHTML corrupted title='<!--keep-->': %s", min)
			}
		}
	})
}

func TestIssue8FragmentMacroVisibility(t *testing.T) {
	engine := NewEngine("")

	t.Run("1. Macro before fragment", func(t *testing.T) {
		tmpl := `|macro badge(text)|<b>|text|</b>|/macro|
|fragment result||call badge('OK')||/fragment|`
		var buf bytes.Buffer
		err := engine.RenderFragment(&buf, tmpl, "result", nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got := buf.String(); got != "<b>OK</b>" {
			t.Errorf("expected %q, got %q", "<b>OK</b>", got)
		}
	})

	t.Run("2. Macro after fragment", func(t *testing.T) {
		tmpl := `|fragment result||call badge('OK')||/fragment|
|macro badge(text)|<b>|text|</b>|/macro|`
		var buf bytes.Buffer
		err := engine.RenderFragment(&buf, tmpl, "result", nil)
		if err == nil {
			t.Fatalf("expected error for macro declared after fragment, got nil")
		}
		if !strings.Contains(err.Error(), "undefined macro \"badge\"") {
			t.Errorf("expected undefined macro error, got %v", err)
		}
	})

	t.Run("3. Macro before call inside fragment", func(t *testing.T) {
		tmpl := `|fragment result|
|macro badge(text)|<b>|text|</b>|/macro|
|call badge('OK')|
|/fragment|`
		var buf bytes.Buffer
		err := engine.RenderFragment(&buf, tmpl, "result", nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got := strings.TrimSpace(buf.String()); got != "<b>OK</b>" {
			t.Errorf("expected %q, got %q", "<b>OK</b>", got)
		}
	})

	t.Run("4. Macro after call inside fragment", func(t *testing.T) {
		tmpl := `|fragment result|
|call badge('OK')|
|macro badge(text)|<b>|text|</b>|/macro|
|/fragment|`
		var buf bytes.Buffer
		err := engine.RenderFragment(&buf, tmpl, "result", nil)
		if err == nil {
			t.Fatalf("expected error for macro declared after call inside fragment, got nil")
		}
		if !strings.Contains(err.Error(), "undefined macro \"badge\"") {
			t.Errorf("expected undefined macro error, got %v", err)
		}
	})

	t.Run("5. Macro inside an unrelated conditional", func(t *testing.T) {
		tmpl := `|if enabled|
|macro badge(text)|<b>|text|</b>|/macro|
|/if|
|fragment result|
|call badge('OK')|
|/fragment|`

		for _, enabled := range []bool{true, false} {
			var buf bytes.Buffer
			err := engine.RenderFragment(&buf, tmpl, "result", map[string]any{"enabled": enabled})
			if err == nil {
				t.Fatalf("enabled=%v: expected error for macro inside conditional, got nil", enabled)
			}
			if !strings.Contains(err.Error(), "undefined macro \"badge\"") {
				t.Errorf("enabled=%v: expected undefined macro error, got %v", enabled, err)
			}
		}
	})

	t.Run("6. Macro inside another fragment", func(t *testing.T) {
		tmpl := `|fragment first|
|macro badge(text)|<b>|text|</b>|/macro|
|/fragment|
|fragment second|
|call badge('OK')|
|/fragment|`
		var buf bytes.Buffer
		err := engine.RenderFragment(&buf, tmpl, "second", nil)
		if err == nil {
			t.Fatalf("expected error for macro inside another fragment, got nil")
		}
		if !strings.Contains(err.Error(), "undefined macro \"badge\"") {
			t.Errorf("expected undefined macro error, got %v", err)
		}
	})

	t.Run("7. Shadowing before vs after fragment", func(t *testing.T) {
		tmpl := `|macro badge(text)|v1: <b>|text|</b>|/macro|
|macro badge(text)|v2: <b>|text|</b>|/macro|
|fragment result||call badge('OK')||/fragment|
|macro badge(text)|v3: <b>|text|</b>|/macro|`
		var buf bytes.Buffer
		err := engine.RenderFragment(&buf, tmpl, "result", nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got := buf.String(); got != "v2: <b>OK</b>" {
			t.Errorf("expected %q, got %q", "v2: <b>OK</b>", got)
		}
	})

	t.Run("8. Multiple fragments", func(t *testing.T) {
		tmpl := `|macro firstMacro()|first|/macro|
|fragment first|
|call firstMacro()|
|/fragment|
|macro secondMacro()|second|/macro|
|fragment second|
|call firstMacro()|-|call secondMacro()|
|/fragment|`

		var buf1 bytes.Buffer
		if err := engine.RenderFragment(&buf1, tmpl, "first", nil); err != nil {
			t.Fatalf("unexpected error rendering first: %v", err)
		}
		if got := strings.TrimSpace(buf1.String()); got != "first" {
			t.Errorf("first fragment: expected %q, got %q", "first", got)
		}

		var buf2 bytes.Buffer
		if err := engine.RenderFragment(&buf2, tmpl, "second", nil); err != nil {
			t.Fatalf("unexpected error rendering second: %v", err)
		}
		if got := strings.TrimSpace(buf2.String()); got != "first-second" {
			t.Errorf("second fragment: expected %q, got %q", "first-second", got)
		}

		var buf1Rev bytes.Buffer
		if err := engine.RenderFragment(&buf1Rev, tmpl, "first", nil); err != nil {
			t.Fatalf("unexpected error rendering first in reverse order: %v", err)
		}
		if got := strings.TrimSpace(buf1Rev.String()); got != "first" {
			t.Errorf("first fragment reverse: expected %q, got %q", "first", got)
		}
	})

	t.Run("9. RenderFragments scoping", func(t *testing.T) {
		tmpl := `|fragment first|
|call secondMacro()|
|/fragment|
|macro secondMacro()|second|/macro|
|fragment second|
|call secondMacro()|
|/fragment|`

		var buf bytes.Buffer
		err := engine.RenderFragments(&buf, tmpl, []string{"first", "second"}, nil)
		if err == nil {
			t.Fatalf("expected error when first fragment calls macro declared after it, got nil")
		}
		if !strings.Contains(err.Error(), "undefined macro \"secondMacro\"") {
			t.Errorf("expected undefined macro error, got %v", err)
		}
	})

	t.Run("10. Concurrent rendering", func(t *testing.T) {
		tmpl := `|macro badge(text)|<b>|text|</b>|/macro|
|fragment first||call badge('1')||/fragment|
|macro label(text)|<i>|text|</i>|/macro|
|fragment second||call badge('2')|-|call label('2')||/fragment|`

		var wg sync.WaitGroup
		errChan := make(chan error, 100)

		for i := 0; i < 50; i++ {
			wg.Add(2)
			go func() {
				defer wg.Done()
				var buf bytes.Buffer
				if err := engine.RenderFragment(&buf, tmpl, "first", nil); err != nil {
					errChan <- err
					return
				}
				if got := buf.String(); got != "<b>1</b>" {
					errChan <- fmt.Errorf("concurrent first: expected %q, got %q", "<b>1</b>", got)
				}
			}()
			go func() {
				defer wg.Done()
				var buf bytes.Buffer
				if err := engine.RenderFragment(&buf, tmpl, "second", nil); err != nil {
					errChan <- err
					return
				}
				if got := buf.String(); got != "<b>2</b>-<i>2</i>" {
					errChan <- fmt.Errorf("concurrent second: expected %q, got %q", "<b>2</b>-<i>2</i>", got)
				}
			}()
		}

		wg.Wait()
		close(errChan)

		for err := range errChan {
			t.Errorf("concurrency error: %v", err)
		}
	})

	t.Run("11. Partial-output protection", func(t *testing.T) {
		tmpl := `|fragment result|
Output text before error
|call undefinedMacro()|
|/fragment|`

		var buf bytes.Buffer
		err := engine.RenderFragment(&buf, tmpl, "result", nil)
		if err == nil {
			t.Fatalf("expected error, got nil")
		}
		if buf.Len() > 0 {
			t.Errorf("expected buffer to be empty on failure, got %q", buf.String())
		}
	})

	t.Run("End-to-end router integration test", func(t *testing.T) {
		tmpl := `|macro nav(title)|<nav>|title|</nav>|/macro|
|fragment menu||call nav('Home')||/fragment|
|macro footer(text)|<footer>|text|</footer>|/macro|
|fragment info||call nav('Home')|-|call footer('2026')||/fragment|`

		var bufMenu bytes.Buffer
		if err := engine.RenderFragment(&bufMenu, tmpl, "menu", nil); err != nil {
			t.Fatalf("unexpected error rendering menu: %v", err)
		}
		if got := bufMenu.String(); got != "<nav>Home</nav>" {
			t.Errorf("menu fragment: expected %q, got %q", "<nav>Home</nav>", got)
		}

		var bufInfo bytes.Buffer
		if err := engine.RenderFragment(&bufInfo, tmpl, "info", nil); err != nil {
			t.Fatalf("unexpected error rendering info: %v", err)
		}
		if got := bufInfo.String(); got != "<nav>Home</nav>-<footer>2026</footer>" {
			t.Errorf("info fragment: expected %q, got %q", "<nav>Home</nav>-<footer>2026</footer>", got)
		}
	})
}
