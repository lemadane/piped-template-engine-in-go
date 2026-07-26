# Piped Templating Engine for Go (PTEGo)

PTEGo is an extremely lightweight, high-performance, and feature-rich template engine written in pure Go (no third-party dependencies). It compiles templates into an Abstract Syntax Tree (AST) once and caches them, allowing for ultra-fast rendering.

PTEGo features a goroutine-based asynchronous streaming renderer (`RenderStream`), making it exceptionally suitable for modern Go MVC web projects, server-sent events, and HTMX-driven real-time interfaces.

---

## Features

1. **Pipe-Based Output syntax** (`|var|`) with HTML escaping by default.
2. **Flexible Escaping Modes**: `|html expr|` (raw/trusted), `|attr expr|` (attribute-escaped), `|url expr|` (URL query encoded), and `|json expr|` (JSON encoded).
3. **Optional Chaining & Null Coalescing** (`?.`, `??`): Safely traverse maps, structs, and pointers.
4. **Ternary Conditional Expressions** (`? :`).
5. **Conditionals** (`|if|`, `|else-if|`, `|else|`, `|/if|`).
6. **Loops** (`|each item in list|`, Map key-value iteration, loop metadata `each.index`, fallback `|else|` block, and `|separator|` nodes).
7. **Switch Statements** (`|switch|`, `|case|`, `|default|`) with automatic break and explicit `|fallthrough|`.
8. **Macros** (`|macro alert(msg, type)|` ... `|/macro|`) to define reusable template functions.
9. **File-Based Includes** (`|include partials/header|` and optional model scoping `with childModel`).
10. **Layout Inheritance** (`|layout layouts/main|`, sections and yields).
11. **Reusable Components & Slots** (`|component components/card|` with custom `|slot title|`).
12. **Fragment Rendering**: Render target subsets (`RenderFragment`) for optimal HTMX updates.
13. **Attempt/Recover Blocks** (`|attempt|` ... `|recover as err|`) for granular error isolation.
14. **Filters**: `upper`, `lower`, `trim`, `capitalize`, `slug`, `length`, `default`, `currency`, `number`, `date`, `time`, `datetime`.
15. **Minification & Prettification**: Automated space collapsing and code formatting.

---

## Installation

Add PTEGo to your Go module:

```bash
go get pte # If published, or copy the package directly to your vendor/internal folder
```

---

## MVC Web Project Integrations

PTEGo integrates seamlessly with standard `net/http` and all popular Go web frameworks.

### 1. Standard `net/http` (with Streaming)
Leverage `RenderStream` to compile and stream HTML chunks to the client concurrently:

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
			"title": "Home Page",
			"user":  map[string]any{"name": "Alice"},
		}

		// Streams template output directly over the network using io.Pipe
		reader := engine.RenderStream("pages/home", data)
		io.Copy(w, reader)
	})

	http.ListenAndServe(":8080", nil)
}
```

### 2. Gin Integration
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

	r.GET("/products", func(c *gin.Context) {
		c.Header("Content-Type", "text/html; charset=utf-8")
		data := map[string]any{
			"title":    "Store",
			"products": []any{"Coffee", "Tea"},
		}
		
		err := engine.Render(c.Writer, "pages/products", data)
		if err != nil {
			c.String(http.StatusInternalServerError, err.Error())
		}
	})

	r.Run(":8080")
}
```

### 3. Fiber Integration
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
		
		data := map[string]any{"name": "World"}
		
		// Render directly to Fiber's context response stream
		return engine.Render(c.Response().BodyWriter(), "pages/index", data)
	})

	app.Listen(":8080")
}
```

### 4. Echo Integration
```go
package main

import (
	"net/http"
	"github.com/labstack/echo/v4"
	"pte"
)

func main() {
	e := echo.New()
	engine := pte.NewEngine("./templates")

	e.GET("/", func(c echo.Context) error {
		c.Response().Header().Set(echo.HeaderContentType, echo.MIMETextHTMLCharsetUTF8)
		data := map[string]any{"message": "Hello Echo!"}
		return engine.Render(c.Response().Writer, "index", data)
	})

	e.Logger.Fatal(e.Start(":8080"))
}
```

---

## HTMX Integration (Fragment Rendering)

HTMX works by swapping HTML fragments into the DOM. With PTEGo, you can render just a single `|fragment|` block out of a full template.

**Template (`templates/pages/dashboard.pte`):**
```html
<div class="container">
    <h1>Dashboard</h1>
    
    |fragment metrics-card|
        <div id="metrics" class="card">
            <h3>Active Users</h3>
            <p>|activeUsersCount|</p>
        </div>
    |/fragment|
</div>
```

**Go Handler:**
```go
func handleMetricsUpdate(w http.ResponseWriter, r *http.Request) {
    data := map[string]any{"activeUsersCount": 1420}

    // Only compiles and renders the "metrics-card" fragment block
    engine.RenderFragment(w, "pages/dashboard", "metrics-card", data)
}
```

---

## Template Syntax & Feature Reference

### 1. Variables & Escaping
- **Default HTML Escaping**: `|name|`
- **Raw/Trusted HTML**: `|html blogBody|`
- **HTML Attributes**: `<input value="|attr user.name|">`
- **URL Encoding**: `<a href="/search?q=|url query|">Search</a>`
- **JSON Encoding**: `var data = |json product|;`

### 2. Optional Chaining & Null Coalescing
Avoid errors on missing nested variables:
```html
<span>Welcome, |user?.Profile?.DisplayName ?? 'Guest'|!</span>
```

### 3. Ternary Operator
```html
<div class="|user.Active ? 'status-active' : 'status-inactive'|">
    |user.Name|
</div>
```

### 4. Conditionals
```html
|if user.role == 'admin'|
    <p>Welcome Admin!</p>
|else-if user.role == 'manager'|
    <p>Welcome Manager!</p>
|else|
    <p>Welcome User!</p>
|/if|
```

### 5. Loops (Each)
Iterate over slices, arrays, or maps.
- **Slice Iteration**:
```html
<ul>
    |each item in items|
        <li>|each.count|. |item.name||separator|, |/separator|</li>
    |else|
        <li>No items available.</li>
    |/each|
</ul>
```
*Note: `each` metadata includes `index` (0-based), `count` (1-based), `first` (bool), `last` (bool), and `total` (int).*

- **Map Iteration**:
```html
|each key, val in myMap|
    <p>|key|: |val|</p>
|/each|
```

### 6. Switch & Case
Supports automatic break and explicit fallthrough:
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

### 7. Filters
Transform outputs using filters:
- **Case Formatting**: `|name, upper|`, `|name, lower|`, `|name, capitalize|`
- **URL Slugification**: `|title, slug|` (e.g. `"Rice & Beans!"` becomes `"rice-beans"`)
- **Strings**: `|name, trim|`
- **Collection Length**: `|items, length|`
- **Defaults**: `|description, default 'No description provided'|`
- **Currency Format**: `|price, currency '₱'|` (e.g. `12345.6` becomes `₱12,345.60`)
- **Number Formats**: `|weight, number '#,##0.##'|`
- **Temporal Formats**: `|createdAt, datetime 'yyyy-MM-dd HH:mm:ss'|`

### 8. Layouts, Sections, and Yields
Define layouts and fill sections from child pages.

**Layout (`templates/layouts/main.pte`):**
```html
<html>
    <head><title>|yield title|</title></head>
    <body>
        |yield body|
    </body>
</html>
```

**Child Template (`templates/pages/info.pte`):**
```html
|layout layouts/main|

|section title|About Us|/section|

|section body|
    <h1>Who We Are</h1>
    <p>PTEGo makes templating fun.</p>
|/section|
```

### 9. Components & Slots
Define reusable UI components.

**Component (`templates/components/alert.pte`):**
```html
<div class="alert">
    <h4>|slot title|</h4>
    <div>|slot body|</div>
</div>
```

**Caller Template:**
```html
|component components/alert|
    |slot title|Success!|/slot|
    |slot body|<p>Your changes have been saved.</p>|/slot|
|/component|
```

### 10. Macros
Declare reusable functional template blocks:
```html
|macro badge(text, color)|
    <span class="badge badge-|color|">|text|</span>
|/macro|

<!-- Call the macro -->
|call badge('New', 'blue')|
```

### 11. Attempt & Recover
Isolate sections from rendering crashes/nil-pointer errors:
```html
|attempt|
    <!-- If user is nil, accessing nested properties will error out -->
    <p>Profile: |user.Profile.Detail.Text|</p>
|recover as err|
    <p class="error-log">Failed to load profile details: |err|</p>
|/attempt|
```
