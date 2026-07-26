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

PTEGo integrates seamlessly with all popular Go web frameworks and standard HTTP multiplexers.

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

PTEGo contains a built-in file-based router. You organize your pages in folders, and the engine automatically builds URL paths, matches routes, and extracts path parameters.

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

## Complete Feature Reference (Prioritized by Frequency of Use)

Below is the complete list of all PTEGo features, ordered from the **most-frequently-used** core template syntax to **advanced/niche** structures.

---

### 1. Variables, Escaping, and Output Modes (Essential)

PTEGo provides specific output prefix modifiers to handle HTML escaping, raw HTML, attribute safety, URL parameters, and JSON output.

#### A. Default HTML Escaped Output (`|expr|`)
By default, all variable outputs are automatically escaped to prevent XSS attacks:
```html
<p>User: |username|</p>
<!-- Data: {"username": "<script>alert('xss')</script>"} -->
<!-- Output: <p>User: &lt;script&gt;alert(&#039;xss&#039;)&lt;/script&gt;</p> -->
```

#### B. Raw / Trusted HTML Output (`|html expr|`)
Render unescaped, raw HTML string contents (e.g. rich text editor output):
```html
<div class="article-body">
    |html article.bodyContent|
</div>
<!-- Data: {"article": {"bodyContent": "<strong>Rich HTML Content</strong>"}} -->
<!-- Output: <div class="article-body"><strong>Rich HTML Content</strong></div> -->
```

#### C. HTML Attribute Escaping (`|attr expr|`)
Escapes double quotes, single quotes, HTML tags, and backticks specifically for HTML attribute values:
```html
<input type="text" name="bio" value="|attr user.bio|">
<!-- Data: {"user": {"bio": "Hello \"World\" `dev`"}} -->
<!-- Output: <input type="text" name="bio" value="Hello &quot;World&quot; &#096;dev&#096;"> -->
```

#### D. URL Parameter Encoding (`|url expr|`)
URL-query encodes values for href parameters or API request links:
```html
<a href="/search?q=|url searchQuery|">Search</a>
<!-- Data: {"searchQuery": "coffee & tea"} -->
<!-- Output: <a href="/search?q=coffee+%26+tea">Search</a> -->
```

#### E. JSON Encoding (`|json expr|`)
Marshals Go structs, slices, or maps into valid, HTML-safe JSON output:
```html
<script>
    const currentProduct = |json productData|;
</script>
<!-- Data: {"productData": map[string]any{"id": 101, "name": "Coffee"}} -->
<!-- Output: <script>const currentProduct = {"id":101,"name":"Coffee"};</script> -->
```

---

### 2. Optional Chaining & Null Coalescing (Highly Used)
Safely traverse nested struct fields or maps without nil-pointer crashes:
```html
<span>Welcome, |user?.Profile?.DisplayName ?? 'Guest'|!</span>
```

---

### 3. Ternary Conditional Operator (Highly Used)
Inline binary choice expressions:
```html
<div class="|user.Active ? 'status-active' : 'status-inactive'|">
    |user.Name|
</div>
```

---

### 4. Conditionals (Highly Used)
Logical branching with `|if|`, `|else-if|` (or `|else if|`), `|else|`, and `|/if|`:
```html
|if user.role == 'admin'|
    <p>Welcome Admin!</p>
|else-if user.role == 'manager'|
    <p>Welcome Manager!</p>
|else|
    <p>Welcome User!</p>
|/if|
```

Supports logical operators (`and`, `or`, `not`, `nand`, `nor`) and comparison operators (`==`, `!=`, `<`, `<=`, `>`, `>=`).

---

### 5. Iteration and Loops (Highly Used)
Loop through arrays, slices, or map structures.

#### Slice Loop with Iteration Metadata
```html
<ul>
    |each item in items|
        <li>|each.count| of |each.total|: |item.name||separator|, |/separator|</li>
    |else|
        <li>No items available.</li>
    |/each|
</ul>
```
* **Loop Metadata**: The local `each` scope provides `index` (0-based), `count` (1-based), `first` (bool), `last` (bool), and `total` (int).
* **Separator Node**: The `|separator| ... |/separator|` block renders delimiters only between items (skipping the last item).
* **Empty Fallback**: The `|else|` block renders when the collection is empty or nil.

#### Map Key-Value Loop
```html
|each key, val in myMap|
    <p>|key|: |val|</p>
|/each|
```

---

### 6. Switch & Case Statements (Highly Used)
Branching structure with automatic breaking and explicit `|fallthrough|`:
```html
|switch status|
    |case 'pending'|
        <span class="warning">Pending approval</span>
    |case 'approved'|
        <span class="success">Approved</span>
        |fallthrough|
    |case 'notified'|
        <span class="info">User notified</span>
    |default|
        <span>Unknown status</span>
|/switch|
```

---

### 7. Layout Inheritance & Yield Sections (Highly Used)
Wrap pages inside master templates to reuse headers, sidebars, and scripts.

**Layout (`templates/layouts/main.pte`):**
```html
<html>
    <head><title>|yield title|</title></head>
    <body>|yield content|</body>
</html>
```

**Page Template:**
```html
|layout layouts/main|
|section title|About Us|/section|
|section content|
    <h1>Who We Are</h1>
|/section|
```

---

### 8. File Includes & Scope Binding (Commonly Used)
Include partial file fragments directly. You can pass scoped sub-models using the `with` statement:
```html
|include partials/header|
|include partials/navbar with navItems|
```

---

### 9. Formatter Pipe Filters (Commonly Used)
Modify output variables directly using formatting chains:
- **Case Transformations**: `|name, lower, capitalize|`
- **URL Slugification**: `|title, slug|` (e.g. `"Hot Chocolate!"` -> `"hot-chocolate"`)
- **Default Fallback**: `|description, default 'No description'|`
- **Currency Formatting**: `|price, currency '₱'|` (e.g. `123.4` -> `₱123.40`)
- **Number Formatting**: `|weight, number '#,##0.##'|`
- **Date/Time Formatting**: `|createdAt, datetime 'yyyy-MM-dd HH:mm:ss'|`

---

### 10. Conditional Attribute Shorthand & Whitespace Cleanup (Commonly Used)
Allows compact attribute bindings and automatically cleans up extra trailing spacing if the condition evaluates to `false`.

```html
<!-- Renders class="form-input checked" if completed, else class="form-input" with no trailing spacing -->
<input class="form-input" |attr checked if completed|>

<!-- Supports key-value attribute bindings conditionally -->
<div |attr class=btnClass if hasError|>
```

---

### 11. Page Options and Routing Metadata (Commonly Used)
Declare route options directly inside the template. The `FileRouter` resolves and enforces these parameters:
```html
|page title = "Settings Panel"|
|page cache = "public, max-age=3600"|
|page auth = true|
|page roles = ["ADMIN"]|
```

---

### 12. Request Page Context (Commonly Used in Routing)
Router pages can access the built-in `page` context containing request states:
```html
<p>Method: |page.Method|</p>
<p>Path: |page.RequestURI|</p>
<p>Query String: |page.QueryString|</p>
<p>User-Agent: |page.Headers.User-Agent|</p>
<p>Session Cookie: |page.Cookies.session_id|</p>
```

---

### 13. Reusable Components & Custom Slots (Occasionally Used)
Define custom encapsulated components with custom slot placeholders.

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

---

### 14. HTMX Inline Template Fragments (Occasionally Used)
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

---

### 15. Template Comments (Occasionally Used)
Developer notes are completely stripped out at compile time:
- **Single-Line Comment**: `|# This comment will not render |`
- **Multi-Line Comment**:
```html
|#
   This is a block comment.
   It spans multiple lines.
#|
```
- **Old Style Comment**: `|-- Retro comments are also supported --|`

---

### 16. Circular Include Detection (Built-in Safety)
PTEGo tracks active template imports at render time and raises an error if templates recursively import each other (e.g. `a -> b -> a`).

---

### 17. Error Boundaries: Attempt / Recover (Rarely Used)
Isolate sections from rendering crashes or nil-pointer errors:
```html
|attempt|
    <p>Details: |user.profile.details.description|</p>
|recover as err|
    <p class="alert-error">Failed loading details: |err|</p>
|/attempt|
```

---

### 18. Block-Level HTML Minification (Rarely Used)
Compress specific chunks of layout text inside templates:
```html
|minify|
    <div class="container">
        <span>Compressed Text</span>
    </div>
|/minify|
```

---

### 19. Macros & Macro Calls (Rarely Used)
Define functional template subroutines inside the page:
```html
|macro badge(text, color)|
    <span class="badge badge-|color|">|text|</span>
|/macro|

|call badge('New', 'blue')|
```

---

### 20. Form Field Helpers (Rarely Used)
Generate form input attributes and HTMX error indicator styles automatically:
```html
<input |field user.email|>
<!-- Renders: name="email" id="email" value="..." class="input (is-danger if error)" -->

|editor user.bio|
<!-- Renders generic text input helper -->
```

---

### 21. Strongly Typed Model Declarations (Rarely Used)
Provide type checking declarations for IDE plugins:
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
