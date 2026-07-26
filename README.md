# Piped Templating Engine for Go (PTEGo)

PTEGo is an extremely fast, lightweight, and compile-cached template engine written in pure Go (no third-party dependencies). It compiles templates into an Abstract Syntax Tree (AST) once and caches them, allowing for rapid HTML generation.

Featuring a goroutine-based asynchronous streaming renderer (`RenderStream`), PTEGo is highly optimized for modern Go MVC web projects, real-time server-sent events, and HTMX-driven interactive pages.

---

## Quick Start & Installation

Initialize PTEGo in your Go module:

```bash
go get pte # Or copy the package directly to your internal/vendor folder
```

---

## Web Framework Integrations

PTEGo integrates seamlessly with all popular Go web frameworks and standard multiplexers.

### 1. Standard `net/http` (with Asynchronous Streaming)
PTEGo can write compiled HTML bytes concurrently to network socket writers without buffering entire pages, optimizing Memory usage and Time to First Byte (TTFB).

```go
package main

import (
	"io"
	"net/http"
	"pte"
)

func main() {
	engine := pte.NewEngine("./templates")

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		
		data := map[string]any{
			"title": "Dashboard",
			"user":  map[string]any{"name": "Alice"},
		}

		// Compile and stream output concurrently via standard io.Pipe
		reader := engine.RenderStream("pages/home", data)
		_, _ = io.Copy(w, reader)
	})

	http.ListenAndServe(":8080", nil)
}
```

### 2. Fiber Integration
```go
package main

import (
	"github.com/gofiber/fiber/v2"
	"pte"
)

func main() {
	app := fiber.New()
	engine := pte.NewEngine("./templates")

	app.Get("/", func(c *fiber.Ctx) error {
		c.Set("Content-Type", "text/html; charset=utf-8")
		data := map[string]any{"message": "Hello from Fiber!"}
		
		// Write directly to Fiber context's response stream
		return engine.Render(c.Response().BodyWriter(), "pages/index", data)
	})

	app.Listen(":8080")
}
```

### 3. Gin Integration
```go
package main

import (
	"net/http"
	"github.com/gin-gonic/gin"
	"pte"
)

func main() {
	r := gin.Default()
	engine := pte.NewEngine("./templates")

	r.GET("/", func(c *gin.Context) {
		c.Header("Content-Type", "text/html; charset=utf-8")
		data := map[string]any{"message": "Hello from Gin!"}
		
		err := engine.Render(c.Writer, "pages/index", data)
		if err != nil {
			c.String(http.StatusInternalServerError, err.Error())
		}
	})

	r.Run(":8080")
}
```

### 4. Echo Integration
```go
package main

import (
	"github.com/labstack/echo/v4"
	"pte"
)

func main() {
	e := echo.New()
	engine := pte.NewEngine("./templates")

	e.GET("/", func(c echo.Context) error {
		c.Response().Header().Set(echo.HeaderContentType, echo.MIMETextHTMLCharsetUTF8)
		data := map[string]any{"message": "Hello from Echo!"}
		return engine.Render(c.Response().Writer, "pages/index", data)
	})

	e.Logger.Fatal(e.Start(":8080"))
}
```

---

## SvelteKit-Style File-Based Routing

PTEGo contains an built-in file-based router. You organize your pages in folders, and the engine automatically builds URL paths, matches routes, and extracts path parameters.

### 1. Directory Structure
Create a routes directory (e.g. `pte-routes`):
```text
pte-routes/
├── +page.pte                      // Maps to "/"
├── about/
│   └── +page.pte                  // Maps to "/about"
└── products/
    └── [id]/
        └── +page.pte              // Maps to "/products/:id" (dynamic parameter)
```

### 2. HTTP Server Routing Setup
```go
package main

import (
	"net/http"
	"pte"
)

func main() {
	engine := pte.NewEngine("")
	router, _ := pte.NewFileRouter(engine, "./pte-routes")

	// Register data loaders to inject database maps into specific route contexts
	router.RegisterDataLoader("/products/[id]", func(r *http.Request, params map[string]string) (map[string]any, error) {
		id := params["id"]
		return map[string]any{
			"product": map[string]any{"id": id, "name": "Coffee Maker", "price": 89.99},
		}, nil
	})

	// Optional Authentication Check hook mapping to |page auth=true roles='...'| metadata
	router.AuthCheck = func(r *http.Request, requiredRoles []string) (bool, int, string) {
		// Enforce auth logic
		return true, 0, ""
	}

	// ServeHTTP matches paths, loads data, validates auth, and renders pages
	http.ListenAndServe(":8080", router)
}
```

---

## Feature Reference (Prioritized by Frequency of Use)

Below is the complete list of PTEGo features, ordered from the **most-frequently-used** core template syntax to **advanced/niche** structures.

### 1. Variables, Escaping, and Output Modes (Essential)
Direct output injection with automatic escaping depending on context.
- **Default HTML Escaping** (prevents XSS): `|name|`
- **Raw/Trusted HTML**: `|html blogBody|`
- **HTML Attribute Escaping**: `<input value="|attr user.name|">`
- **URL Parameter Encoding**: `<a href="/search?q=|url query|">Search</a>`
- **JSON Encoding**: `const config = |json product|;`

### 2. Optional Chaining & Null Coalescing (Highly Used)
Avoid rendering crashes or nil-pointer checks when variables or sub-properties are missing:
```html
<span>Welcome, |user?.Profile?.DisplayName ?? 'Guest'|!</span>
```

### 3. Conditionals (Highly Used)
Conditional checks support standard value comparisons:
```html
|if user.role == 'admin'|
    <p>Welcome Admin!</p>
|else-if user.role == 'manager'|
    <p>Welcome Manager!</p>
|else|
    <p>Welcome User!</p>
|/if|
```

### 4. Iteration and Loops (Highly Used)
Loop through arrays, slices, or map structures.
- **Slice Loop with Iteration Metadata**:
```html
<ul>
    |each item in items|
        <li>|each.count| of |each.total|: |item.name||separator|, |/separator|</li>
    |else|
        <li>No items available.</li>
    |/each|
</ul>
```
*Note: The local `each` scope provides `index` (0-based), `count` (1-based), `first` (bool), `last` (bool), and `total` (int).*
*Note: The optional `|separator|` block renders delimiters only between items.*

- **Map Loop**:
```html
|each key, val in myMap|
    <p>|key|: |val|</p>
|/each|
```

### 5. Layout Inheritance & Yield Sections (Highly Used)
Wrap pages inside master templates to reuse headers, sidebars, and scripts.

**Layout (`templates/layouts/main.pte`):**
```html
<html>
    <head><title>|yield title|</title></head>
    <body>|yield content|</body>
</html>
```

**Child Template:**
```html
|layout layouts/main|
|section title|About Us|/section|
|section content|
    <h1>Who We Are</h1>
|/section|
```

### 6. File Includes & Scope Binding (Commonly Used)
Include partial file fragments directly. You can pass scoped sub-models using the `with` statement:
```html
|include partials/header|
|include partials/navbar with navItems|
```

### 7. Formatter Pipe Filters (Commonly Used)
Modify output variables directly using formatting chains:
- **Case Transformations**: `|name, lower, capitalize|`
- **URL Slugification**: `|title, slug|` (e.g. `"Hot Chocolate!"` becomes `"hot-chocolate"`)
- **Default fallback**: `|description, default 'No description'|`
- **Currency formatting**: `|price, currency '₱'|` (e.g. `123.4` -> `₱123.40`)
- **Number formatting**: `|weight, number '#,##0.##'|`
- **Date/Time formatting**: `|createdAt, datetime 'yyyy-MM-dd HH:mm:ss'|`

### 8. Conditional Attribute Shorthand & Whitespace Cleanup (Commonly Used)
Allows compact attribute bindings and automatically cleans up extra trailing spacing if the condition evaluates to `false`.

```html
<!-- Renders class="form-input checked" if completed, else class="form-input" with no trailing spacing -->
<input class="form-input" |attr checked if completed|>

<!-- Supports key-value attributes -->
<div |attr class=errorClass if hasError|>
```

### 9. Page Options and Routing Metadata (Commonly Used)
Declare route options directly inside the template. The `FileRouter` resolves and enforces these parameters:
```html
|page title = "Settings Panel"|
|page cache = "public, max-age=3600"|
|page auth = true|
|page roles = ["ADMIN"]|
```

### 10. Request Page Context (Commonly Used in Routing)
Router pages can access the built-in `page` context containing the request states:
```html
<p>Method: |page.Method|</p>
<p>Path: |page.RequestURI|</p>
<p>User-Agent: |page.Headers.User-Agent|</p>
<p>Session Cookie: |page.Cookies.session_id|</p>
```

### 11. Reusable Components & Custom Slots (Occasionally Used)
Define custom encapsulated components.

**Component (`templates/components/card.pte`):**
```html
<div class="card">
    <h3>|slot header|</h3>
    <div>|slot body|</div>
</div>
```

**Caller Template:**
```html
|component components/card|
    |slot header|Featured Item|/slot|
    |slot body|<p>Available in store.</p>|/slot|
|/component|
```

### 12. HTMX Inline Template Fragments (Occasionally Used)
Return lightweight, targeted HTML payload snippets for specific HTMX updates instead of the full layout:
```html
<div>
    <h1>Tasks Dashboard</h1>
    |fragment list-zone|
        <ul id="task-list">
            |each t in tasks|<li>|t|</li>|/each|
        </ul>
    |/fragment|
</div>
```
```go
// Renders only the <ul> block, skipping the surrounding header tags!
engine.RenderFragment(w, "pages/tasks", "list-zone", data)
```

### 13. Template Comments (Occasionally Used)
Developer notes are completely stripped out at compile time:
- **Single-Line**: `|# Comment text |`
- **Multi-Line**:
```html
|#
   Block comment.
   Spans multiple lines.
#|
```
- **Old Style**: `|-- Retro comments are also supported --|`

### 14. Error Boundaries: Attempt / Recover (Rarely Used)
Isolate sections from rendering errors and prevent page crashes:
```html
|attempt|
    <p>Details: |user.profile.details.description|</p>
|recover as err|
    <p class="alert-error">Failed loading details: |err|</p>
|/attempt|
```

### 15. Block-Level HTML Minification (Rarely Used)
Compress specific chunks of layout text inside templates:
```html
|minify|
    <div class="container">
        <span>Compressed Text</span>
    </div>
|/minify|
```

### 16. Macros (Rarely Used)
Define local template functions inside the page:
```html
|macro badge(text, color)|
    <span class="badge badge-|color|">|text|</span>
|/macro|

|call badge('New', 'blue')|
```

### 17. Strongly Typed Model Declarations (Rarely Used)
Provide type checking support for IDE plugins:
```html
|model com.example.model.TaskPageModel|
```

---

## Engine Configurations

Set global behaviors during engine instantiation:

```go
engine := pte.NewEngine(
    "./templates",
    pte.WithSuffix(".pte"),
    pte.WithMinify(true),   // Globally minifies all rendered output
    pte.WithPrettify(true), // Globally indents HTML output
)
```
