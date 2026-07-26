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

### 5. Chi Integration
```go
package main

import (
	"net/http"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"pte"
)

func main() {
	r := chi.NewRouter()
	r.Use(middleware.Logger)
	engine := pte.NewEngine("./templates")

	r.Get("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		data := map[string]any{"message": "Hello from Chi!"}
		_ = engine.Render(w, "pages/index", data)
	})

	http.ListenAndServe(":8080", r)
}
```

### 6. Beego Integration
```go
package main

import (
	beego "github.com/beego/beego/v2/server/web"
	"pte"
)

type MainController struct {
	beego.Controller
}

func (c *MainController) Get() {
	engine := pte.NewEngine("./templates")
	c.Ctx.Output.Header("Content-Type", "text/html; charset=utf-8")
	data := map[string]any{"message": "Hello from Beego!"}
	
	_ = engine.Render(c.Ctx.ResponseWriter, "pages/index", data)
}

func main() {
	beego.Router("/", &MainController{})
	beego.Run()
}
```

### 7. Buffalo Integration
```go
package main

import (
	"github.com/gobuffalo/buffalo"
	"pte"
)

func main() {
	app := buffalo.New(buffalo.Options{})
	engine := pte.NewEngine("./templates")

	app.GET("/", func(c buffalo.Context) error {
		c.Response().Header().Set("Content-Type", "text/html; charset=utf-8")
		data := map[string]any{"message": "Hello from Buffalo!"}
		
		return engine.Render(c.Response(), "pages/index", data)
	})

	app.Serve()
}
```

### 8. Iris Integration
```go
package main

import (
	"github.com/kataras/iris/v12"
	"pte"
)

func main() {
	app := iris.New()
	engine := pte.NewEngine("./templates")

	app.Get("/", func(ctx iris.Context) {
		ctx.ContentType("text/html; charset=utf-8")
		data := map[string]any{"message": "Hello from Iris!"}
		
		_ = engine.Render(ctx.ResponseWriter(), "pages/index", data)
	})

	app.Listen(":8080")
}
```

### 9. GoFrame Integration
```go
package main

import (
	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/net/ghttp"
	"pte"
)

func main() {
	s := g.Server()
	engine := pte.NewEngine("./templates")

	s.BindHandler("/", func(r *ghttp.Request) {
		r.Response.Header().Set("Content-Type", "text/html; charset=utf-8")
		data := map[string]any{"message": "Hello from GoFrame!"}
		
		_ = engine.Render(r.Response.Writer, "pages/index", data)
	})

	s.SetPort(8080)
	s.Run()
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
Logical branching with `|if|`, `|else if|`, `|else|`, and `|/if|`:
```html
|if user.role == 'admin'|
    <p>Welcome Admin!</p>
|else if user.role == 'manager'|
    <p>Welcome Manager!</p>
|else|
    <p>Welcome User!</p>
|/if|
```

Supports logical operators (`and`, `or`, `not`, `nand`, `nor`) and comparison operators (`==`, `!=`, `<`, `<=`, `>`, `>=`).

---

### 5. Iteration and Loops (Highly Used)
Loop through arrays, slices, or map structures with built-in iteration metadata, separator delimiters, and empty collection fallbacks.

#### A. Slice Loop & Iteration Metadata
Access iteration state properties (`index`, `count`, `first`, `last`, `total`) using the local `each` scope inside any loop:
```html
|each item in items|
    <div class="|each.first ? 'header-item' : ''|">
        Item |each.count| of |each.total| (Index: |each.index|): |item.name|
    </div>
|/each|
```
* **`each.index`**: 0-based index (`0, 1, 2, ...`)
* **`each.count`**: 1-based index (`1, 2, 3, ...`)
* **`each.first`**: boolean `true` on the first item
* **`each.last`**: boolean `true` on the final item
* **`each.total`**: total count of elements in the collection

#### B. Loop Separators (`|separator| ... |/separator|`)
Render delimiters (like commas, breadcrumb slashes, or HTML dividers) between loop iterations automatically, skipping the delimiter after the final item:
```html
<!-- Data: {"skills": ["HTML", "CSS", "JS"]} -->
<!-- Output: HTML / CSS / JS -->
|each skill in skills||skill||separator| / |/separator||/each|
```

#### C. Map Key-Value Loop
Iterate over Go map key-value pairs:
```html
|each key, val in myMap|
    <p>|key|: |val|</p>
|/each|
```

#### D. Empty Collection Fallback (`|else|`)
Render fallback HTML when a slice or map is empty or nil:
```html
<ul>
    |each item in items|
        <li>|item.name|</li>
    |else|
        <li>No items available.</li>
    |/each|
</ul>
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
Wrap pages inside master templates to reuse HTML headers, sidebars, footers, and scripts across multiple pages.

#### Layout Master File (`templates/layouts/main.pte`)
Define placeholder sections using `|yield sectionName|`:
```html
<!DOCTYPE html>
<html>
<head>
    <title>|yield title|</title>
</head>
<body>
    <header>
        <nav>Navbar Content</nav>
    </header>
    
    <main>
        |yield content|
    </main>

    <footer>
        <p>Footer Content</p>
    </footer>

    |yield scripts|
</body>
</html>
```

#### Child Page File (`templates/pages/dashboard.pte`)
Declare the layout master via `|layout relativePath|` and populate each section via `|section sectionName| ... |/section|`:
```html
|layout layouts/main|

|section title| Dashboard Page |/section|

|section content|
    <h1>Welcome User</h1>
    <p>This content will be injected into the master layout's |yield content| location.</p>
|/section|

|section scripts|
    <script src="/static/js/dashboard.js"></script>
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
Apply chainable formatting transformations directly to output expressions. Multiple filters can be chained together sequentially (evaluated left-to-right).

#### Filter Reference & Examples

| Filter Name | Usage Syntax | Example Input | Formatted Output |
| :--- | :--- | :--- | :--- |
| **`upper`** | `|name, upper|` | `"alice"` | `ALICE` |
| **`lower`** | `|name, lower|` | `"ALICE"` | `alice` |
| **`trim`** | `|text, trim|` | `" hello "` | `hello` |
| **`capitalize`** | `|name, capitalize|` | `"alice smith"` | `Alice Smith` |
| **`slug`** | `|title, slug|` | `"First Post!"` | `first-post` |
| **`length`** | `|items, length|` | `[1, 2, 3]` | `3` |
| **`default`** | `|bio, default 'N/A'|` | `""` or `nil` | `N/A` |
| **`currency`** | `|price, currency 'USD'|` | `15.5` | `$15.50` |
| **`number`** | `|amount, number '#,##0.00'|` | `1234.5` | `1,234.50` |
| **`date`** | `|createdAt, date 'yyyy-MM-dd'|` | `2026-07-26` | `2026-07-26` |
| **`time`** | `|createdAt, time 'HH:mm:ss'|` | `15:04:05` | `15:04:05` |
| **`datetime`** | `|createdAt, datetime 'yyyy-MM-dd HH:mm'|` | `2026-07-26 15:04` | `2026-07-26 15:04` |

#### Chaining Multiple Filters
Filters can be chained sequentially:
```html
<!-- Input: '  ALICE SMITH  ' -->
<!-- Output: 'Alice smith' -->
<p>User: |name, trim, lower, capitalize|</p>

<!-- Input: '  First Post!  ' -->
<!-- Output: 'first-post' -->
<p>Slug: |title, trim, slug|</p>
```

---

### 10. Conditional Attribute Shorthand & Whitespace Cleanup (Commonly Used)
Allows compact conditional attribute bindings. If a condition evaluates to `false`, PTEGo automatically cleans up surrounding whitespace, removing double spaces or trailing spaces before `>` / `/>`.

#### A. Boolean Attribute Shorthand
```html
<input class="form-input" |attr checked if completed|>
```
* **When `completed` is `true`**: Renders `<input class="form-input" checked>`
* **When `completed` is `false`**: Automatically strips the attribute and cleans up trailing whitespace, rendering exactly `<input class="form-input">` (no `<input class="form-input " >` artifact).

#### B. Dynamic Key-Value Attribute Binding
```html
<!-- Data: {"hasError": true, "btnClass": "alert-danger"} -->
<div |attr class=btnClass if hasError|>
```
* **When `hasError` is `true`**: Renders `<div class="alert-danger">`
* **When `hasError` is `false`**: Renders `<div>` (cleans up interior tag spacing)

---

### 11. Page Options and Routing Metadata (Commonly Used)
Declare page-level HTTP parameters and authorization requirements directly at the top of templates via `|page option = value|`. When served using `FileRouter`, these directives are automatically parsed, evaluated, and enforced before rendering.

#### Directive Reference & Template Example (`pte-routes/admin/settings/+page.pte`)
```html
|page title = "Admin Settings"|
|page cache = "public, max-age=3600"|
|page contentType = "text/html; charset=utf-8"|
|page auth = true|
|page roles = ["ADMIN", "MANAGER"]|

<div class="admin-panel">
    <h1>System Settings</h1>
</div>
```

#### How Metadata Directives Work
* **`|page title = "..."|`**: Automatically populates `title` in the template data model if not explicitly provided by a controller or data loader.
* **`|page cache = "..."|`**: Automatically sets the HTTP response `Cache-Control` header (e.g. `Cache-Control: public, max-age=3600`).
* **`|page contentType = "..."|`**: Sets the HTTP response `Content-Type` header.
* **`|page auth = true|`**: Gatekeeper flag. If `true`, `FileRouter` passes the request to `router.AuthCheck`. If `AuthCheck` returns `false` or is unregistered, the request is denied with an HTTP 401/403 status.
* **`|page roles = ["ADMIN"]|`**: Specifies required role requirements passed into the `router.AuthCheck(req, requiredRoles)` callback.

---

### 12. Request Page Context (Commonly Used in Routing)
When templates are served via `FileRouter`, PTEGo automatically constructs and injects a `PageContext` struct under the `page` variable. This allows templates to inspect the incoming HTTP request state without requiring manual controller boilerplate.

#### Property Reference & Template Example
```html
<div class="request-debug">
    <p>HTTP Method: |page.Method|</p>
    <p>Request Path: |page.RequestURI|</p>
    <p>Query String: |page.QueryString|</p>
    <p>Browser User-Agent: |page.Headers.User-Agent|</p>
    <p>Auth Cookie: |page.Cookies.auth_token|</p>
    <p>Dynamic Route ID: |page.Params.id|</p>
</div>
```

#### Supported `page` Context Fields

| Property Name | Data Type | Description |
| :--- | :--- | :--- |
| **`page.RequestURI`** | `string` | The requested URL path (e.g. `/products/101`). |
| **`page.QueryString`** | `string` | Raw URL query parameters string (e.g. `sort=asc&page=2`). |
| **`page.Method`** | `string` | HTTP verb (`GET`, `POST`, `PUT`, `DELETE`). |
| **`page.Headers`** | `map[string]string` | HTTP request header key-value map (`User-Agent`, `Accept`, etc.). |
| **`page.Cookies`** | `map[string]string` | Request cookie key-value map (`session_id`, `theme`, etc.). |
| **`page.Params`** | `map[string]any` | Merged map of dynamic path parameters (`[id]`) and query parameters. |

---

### 13. Reusable Components & Named Slots (Occasionally Used)
Create reusable UI widget components (e.g. modals, dialogs, cards, data tables) that define named slot sockets (`|slot slotName|`). Callers pass HTML markup into those slots via `|slot slotName| ... |/slot|`.

#### Component Template (`templates/components/modal.pte`)
Define named slot target locations:
```html
<div class="modal-dialog">
    <div class="modal-header">
        <h2>|slot title|</h2>
    </div>
    <div class="modal-body">
        |slot body|
    </div>
    <div class="modal-footer">
        |slot actions|
    </div>
</div>
```

#### Page Template Invocation
Invoke the component via `|component relativePath|` and pass named slot blocks:
```html
|component components/modal|
    |slot title| Confirm Action |/slot|
    
    |slot body|
        <p>Are you sure you want to delete this item? This action cannot be undone.</p>
    |/slot|
    
    |slot actions|
        <button class="btn btn-secondary">Cancel</button>
        <button class="btn btn-danger">Delete</button>
    |/slot|
|/component|
```

---

### 14. HTMX Integration & First-Class Tooling

PTEGo includes first-class HTMX tooling designed to simplify Server-Driven UI patterns.

#### A. Single & Multi-Fragment Out-of-Band (OOB) Rendering
Target specific sub-regions inside a single `.pte` file. Use `RenderFragments` to stream multiple Out-of-Band (OOB) DOM swaps in a single HTTP response:

```html
<div class="dashboard">
    <!-- Fragment 1: Toast Notification -->
    |fragment toast|
        <div id="toast" hx-swap-oob="true" class="alert alert-success">Item Saved!</div>
    |/fragment|

    <!-- Fragment 2: Cart Badge -->
    |fragment cart-count|
        <span id="cart-badge" hx-swap-oob="true">|cartCount|</span>
    |/fragment|
</div>
```

```go
// Controller streams BOTH fragments in one HTTP response for HTMX OOB updates
err := engine.RenderFragments(w, "pages/dashboard", []string{"toast", "cart-count"}, data)
```

#### B. HTMX Request Detection (`page.IsHTMX`)
Templates and page loaders can inspect HTMX headers directly via the request `page` context:

```html
|if page.IsHTMX|
    <!-- Render lightweight fragment without outer layout shell -->
    <div id="main-content">Target: |page.HXTarget|</div>
|else|
    |layout layouts/main|
|/if|
```

#### C. HTMX Response Header Directives (`hxTrigger`, `hxRedirect`, `hxPushUrl`, `hxRefresh`)
Set client-side HTMX response headers directly inside page metadata declarations:

```html
|page hxTrigger = "cartUpdated"|
|page hxRedirect = "/checkout"|
|page hxPushUrl = "/products/featured"|
|page hxRefresh = true|
```
When served via `FileRouter`, PTEGo automatically converts these directives into their corresponding `HX-Trigger`, `HX-Redirect`, `HX-Push-Url`, and `HX-Refresh` HTTP response headers.

---

### 15. Template Comments (Occasionally Used)
Write developer notes inside templates that are stripped out during lexing/compilation and never rendered in the browser.

#### A. Single-Line Comment (`|# ... |`)
```html
<h1>Product Catalog</h1>
|# This developer note will be stripped out at compile time |
<p>Catalog description...</p>
```

#### B. Multi-Line Block Comment (`|# ... #|`)
```html
|#
   Author: Dev Team
   Date: 2026-07-26
   Description: Temporary promotion banner block
#|
<div class="promo-banner">
    <p>Sale ends soon!</p>
</div>
```

#### C. Legacy Pipe Comment (`|-- ... --|`)
```html
|-- Retro comment syntax is also supported --|
```

---

### 16. Circular Include Detection (Built-in Safety)
PTEGo tracks the active rendering stack dynamically. If a template attempts to include itself or form a circular loop with another partial, execution is halted immediately to prevent stack overflows.

#### Example Circular Loop
* **`templates/partials/navbar.pte`**:
  ```html
  <div>Navbar</div>
  |include partials/sidebar|
  ```
* **`templates/partials/sidebar.pte`**:
  ```html
  <div>Sidebar</div>
  |include partials/navbar|
  ```

#### Error Output
When rendered, PTEGo returns a clean, descriptive error:
```text
circular include detected: partials/navbar -> partials/sidebar -> partials/navbar
```

---

### 17. Recoverable Rendering: Attempt / Recover Error Boundaries (Rarely Used)
Isolate template sub-regions from rendering errors (such as missing nested fields or missing partial imports) without crashing the rest of the page render.

#### Template Example (`templates/pages/profile.pte`)
```html
<div class="profile-card">
    <h2>User Profile</h2>

    <!-- Attempt to render nested sub-properties -->
    |attempt|
        <p>Bio: |user.Profile.SubDetails.Bio|</p>
    |recover as errMessage|
        <!-- Render fallback UI when attempt fails -->
        <div class="alert alert-warning">
            <p>Could not load user sub-details: |errMessage|</p>
        </div>
    |/attempt|

    <p>Account Status: Active</p>
</div>
```

#### How Attempt / Recover Works
1. **Isolated Execution**: PTEGo attempts to render the inner nodes of the `|attempt|` block.
2. **Error Interception**: If an error or evaluation panic occurs inside the block, PTEGo intercepts the error, discards the partial output buffer of `|attempt|`, and transfers execution to `|recover as varName|`.
3. **Bound Error Context**: The error message is bound to `varName` (e.g. `errMessage`), making it available to display friendly fallback alert messages while allowing the rest of the page to render successfully.

---

### 18. HTML Minification & Prettifying (Rarely Used)
Compress output payload sizes by stripping HTML comments and collapsing multi-line whitespaces, or format HTML with clean indents during local debugging.

#### A. Block-Level Minification (`|minify| ... |/minify|`)
Compress specific sections of layout text inside templates:
```html
|minify|
    <div class="row">
        <span>  Compressed Text  </span>
    </div>
|/minify|
<!-- Output: <div class="row"><span>Compressed Text</span></div> -->
```

#### B. Engine-Global Minification (`WithMinify`)
Globally minify all compiled templates automatically at the engine level:
```go
engine := pte.NewEngine(
    "./templates",
    pte.WithMinify(true), // Automatically collapses comments & whitespaces across all pages
)
```

#### C. Engine-Global Prettifying (`WithPrettify`)
Format and indent HTML output cleanly for local development and debugging:
```go
engine := pte.NewEngine(
    "./templates",
    pte.WithPrettify(true), // Indents HTML structure cleanly
)
```

---

### 19. Macros & Macro Calls (Rarely Used)
Define reusable functional template subroutines inside the page that accept positional parameters.

#### Macro Declaration & Invocation Example
```html
<!-- Macro Definition -->
|macro badge(text, color)|
    <span class="badge badge-|color|">|text|</span>
|/macro|

<!-- Macro Calls -->
<p>Status: |call badge('Active', 'success')|</p>
<p>Priority: |call badge('High', 'danger')|</p>
```

---

### 20. Form Field & Control Helpers (Rarely Used)
PTEGo includes built-in form control helpers for dynamic input binding and error highlighting.

#### A. Field Binding (`|field model.property|`)
Generates form input attributes (`name`, `id`, `value`) automatically based on the model property:
```html
<input |field user.email|>
<!-- Renders: name="email" id="email" value="alice@example.com" class="form-control" -->
```

#### B. Read-Only Display Control (`|display model.property|`)
Renders read-only view container markup for model attributes:
```html
|display user.bio|
<!-- Renders: <span class="field-display">User bio text...</span> -->
```

#### C. Input Editor Control (`|editor model.property|`)
Renders an editable textarea or input element bound to the property:
```html
|editor user.bio|
<!-- Renders: <textarea name="bio" id="bio" class="form-editor">User bio text...</textarea> -->
```

---

### 21. Strongly Typed Model Declarations (Rarely Used)
Explicitly declare your page model's struct or package type at the top of templates. This declaration is parsed into a `ModelNode` AST metadata node and is used by IDE extension plugins for autocomplete, static analysis, and type verification.

#### Go Struct Definition
```go
type TaskPageModel struct {
    PageTitle string
    DueDate   string
}
```

#### Template File (`templates/pages/task.pte`)
Declare the model contract at the top of the file:
```html
|model models.TaskPageModel|

<h1>|model.PageTitle|</h1>
<p>Due Date: |model.DueDate|</p>

---

### 23. Progressive Web App (PWA) Meta & Service Worker Tag (Rarely Used)
Abstract mobile viewports, theme colors, iOS app capability, icons, web app manifest links, and automatic service worker registration into a single inline tag:

#### Template Example (`templates/layouts/main.pte`)
```html
<head>
    <title>|title ?? 'My App'|</title>
    |pwa name='TaskMaster' theme='#4f46e5' icon='/icon-192.png' manifest='/manifest.json' sw='/sw.js'|
</head>
```

#### Generated HTML Output
```html
<meta name="viewport" content="width=device-width, initial-scale=1, viewport-fit=cover">
<meta name="theme-color" content="#4f46e5">
<meta name="mobile-web-app-capable" content="yes">
<meta name="apple-mobile-web-app-capable" content="yes">
<meta name="apple-mobile-web-app-status-bar-style" content="default">
<meta name="apple-mobile-web-app-title" content="TaskMaster">
<meta name="application-name" content="TaskMaster">
<link rel="manifest" href="/manifest.json">
<link rel="apple-touch-icon" href="/icon-192.png">
<link rel="icon" href="/icon-192.png">
<script>if('serviceWorker' in navigator){window.addEventListener('load',function(){navigator.serviceWorker.register('/sw.js');});}</script>
```

#### Supported PWA Tag Attributes
* **`name`** / **`title`**: Application name for iOS & Android home screen launchers.
* **`theme`**: Theme color hex code (default `#000000`).
* **`manifest`**: Path to web app manifest JSON (default `/manifest.json`).
* **`icon`**: Path to touch icon and favicon.
* **`sw`**: Path to Service Worker JS file to automatically register.
* **`statusColor`**: iOS status bar style (`default`, `black-translucent`).

---

### 24. HTMX Head & Element Attribute Tags (Rarely Used)
PTEGo abstracts HTMX library scripts, extension plugins, indicator CSS rules, and HTMX element action attributes into single inline tags.

#### A. HTMX Head Setup Tag (`|htmx ...|`)
```html
<head>
    <!-- Loads HTMX library, json-enc & sse extensions, and default loading indicator CSS -->
    |htmx src='/js/htmx.min.js' ext='json-enc,sse' indicator=true|
</head>
```

#### Generated Head HTML Output
```html
<script src="/js/htmx.min.js"></script>
<script src="https://unpkg.com/htmx.org@1.9.10/dist/ext/json-enc.js"></script>
<script src="https://unpkg.com/htmx.org@1.9.10/dist/ext/sse.js"></script>
<style>.htmx-indicator{display:none;}.htmx-request .htmx-indicator,.htmx-request.htmx-indicator{display:inline-block;}</style>
```

#### B. HTMX Element Action Shorthand Tag (`|htmx-get ...|`, `|htmx-post ...|`, `|htmx-put ...|`, `|htmx-delete ...|`, `|htmx-patch ...|`)
Write HTMX attribute bindings using `|htmx-get ...|`, `|htmx-post ...|`, `|htmx-put ...|`, `|htmx-delete ...|`, `|htmx-patch ...|`:
```html
<button |htmx-get '/api/tasks' target='#task-list' swap='outerHTML' indicator='#spinner'|>
    Refresh Tasks
</button>
```

#### Generated Button HTML Output
```html
<button hx-get="/api/tasks" hx-target="#task-list" hx-swap="outerHTML" hx-indicator="#spinner">
    Refresh Tasks
</button>
```

#### C. Universal HTMX Attribute Mapping Reference

| PTEGo Directive Syntax | Generated HTML Output Attribute |
| :--- | :--- |
| **`|htmx-get '/api/tasks'|`** | `hx-get="/api/tasks"` |
| **`|htmx-post '/api/tasks'|`** | `hx-post="/api/tasks"` |
| **`|htmx-put '/api/tasks'|`** | `hx-put="/api/tasks"` |
| **`|htmx-delete '/api/tasks'|`** | `hx-delete="/api/tasks"` |
| **`|htmx-patch '/api/tasks'|`** | `hx-patch="/api/tasks"` |
| *(inner param)* **`target='#list'`** | `hx-target="#list"` |
| *(inner param)* **`swap='outerHTML'`** | `hx-swap="outerHTML"` |
| *(inner param)* **`indicator='#spinner'`** | `hx-indicator="#spinner"` |
| *(inner param)* **`trigger='click'`** | `hx-trigger="click"` |

---

### 25. Alpine.js Integration & Reactive State Tags (Rarely Used)
Abstract Alpine.js core script tags, plugin CDN references, `x-cloak` CSS rules, and reactive component state declarations into single inline tags.

#### A. Alpine.js Head Setup Tag (`|alpine ...|`, `|reactive ...|`, `|alpinejs ...|`)
PTEGo supports `|alpine ...|`, `|reactive ...|`, and `|alpinejs ...|` interchangeably:
```html
<head>
    <!-- Loads Alpine.js core, collapse & focus plugins, and x-cloak CSS rules -->
    |reactive plugins='collapse,focus' cloak=true|
</head>
```

#### Generated Head HTML Output
```html
<script defer src="https://cdn.jsdelivr.net/npm/@alpinejs/collapse@3.x.x/dist/cdn.min.js"></script>
<script defer src="https://cdn.jsdelivr.net/npm/@alpinejs/focus@3.x.x/dist/cdn.min.js"></script>
<script defer src="https://cdn.jsdelivr.net/npm/alpinejs@3.x.x/dist/cdn.min.js"></script>
<style>[x-cloak]{display:none !important;}</style>
```

#### B. Alpine.js Reactive Component State Tag (`|alpine-data ...|`)
Use `|alpine-data ...|` to declare `x-data` reactive state:
```html
<div |alpine-data open=false count=0 tab='home'|>
    <button @click="open = !open">Toggle Menu</button>
    <div |alpine-show 'open'| |alpine-cloak|>
        <p>Active Tab: <span x-text="tab"></span></p>
    </div>
</div>
```

#### Generated Container HTML Output
```html
<div x-data="{ count: 0, open: false, tab: 'home' }">
    <button @click="open = !open">Toggle Menu</button>
    <div x-show="open" x-cloak>
        <p>Active Tab: <span x-text="tab"></span></p>
    </div>
</div>
```

#### C. Universal Alpine Element Attribute Mapping Reference

| PTEGo Directive Syntax | Generated HTML Output Attribute |
| :--- | :--- |
| **`|alpine-data open=false count=0|`** | `x-data="{ count: 0, open: false }"` |
| **`|alpine-show 'isOpen'|`** | `x-show="isOpen"` |
| **`|alpine-cloak|`** | `x-cloak` |
| **`|alpine-text 'message'|`** | `x-text="message"` |
| **`|alpine-html 'rawHtml'|`** | `x-html="rawHtml"` |
| **`|alpine-model 'userQuery'|`** | `x-model="userQuery"` |

---

### 26. Combined HTMX + Alpine.js Template Example
Using HTMX and Alpine.js together seamlessly in PTEGo:

```html
<head>
    |htmx|
    |alpine cloak=true|
</head>
<body>
    <div |alpine-data isOpen=false|>
        <button @click="isOpen = true">Open Dialog</button>

        <div |alpine-show 'isOpen'| |alpine-cloak|>
            <button |htmx-get '/api/tasks' target='#task-list' swap='outerHTML'|>
                Fetch Tasks
            </button>
        </div>
    </div>
</body>
```

---

## Single-Binary Embedded Deployments (`embed.FS`)

PTEGo natively supports Go's `embed.FS` filesystems, allowing you to bundle all `.pte` templates and SvelteKit-style route trees directly into a single compiled binary without external folder dependencies.

### 1. Embedded Templates with `WithFS`
```go
package main

import (
	"embed"
	"pte"
)

//go:embed templates/*
var templateFS embed.FS

func main() {
	// Compiles and reads templates directly from binary memory
	engine := pte.NewEngine("templates", pte.WithFS(templateFS))
	_ = engine
}
```

### 2. Embedded SvelteKit File Router with `NewFileRouterFS`
```go
package main

import (
	"embed"
	"net/http"
	"pte"
)

//go:embed templates/* pte-routes/*
var appFS embed.FS

func main() {
	engine := pte.NewEngine("templates", pte.WithFS(appFS))
	
	// Scans and mounts file routes directly from the embedded binary filesystem
	router, err := pte.NewFileRouterFS(engine, appFS, "pte-routes")
	if err != nil {
		panic(err)
	}

	http.ListenAndServe(":8080", router)
}
```

---

## Engine Configurations & Virtual Templates

Set global engine behaviors or pass in-memory template maps:

```go
engine := pte.NewEngine(
	"./templates",
	pte.WithSuffix(".pte"),
	pte.WithFS(appFS),                           // Embedded fs.FS templates
	pte.WithInMemoryTemplates(virtualTemplates), // Virtual memory map templates
	pte.WithMinify(true),                        // Globally minify output
	pte.WithPrettify(false),                     // Disable prettify formatting
)
```
