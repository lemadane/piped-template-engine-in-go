package pte

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type PageDataLoader func(r *http.Request, params map[string]string) (map[string]any, error)

type AuthCheckHook func(r *http.Request, requiredRoles []string) (ok bool, status int, message string)

type Route struct {
	Path         string
	TemplatePath string
	Compiled     *CompiledTemplate
	Segments     []string
}

type FileRouter struct {
	engine      *Engine
	routesDir   string
	routes      []Route
	dataLoaders map[string]PageDataLoader
	AuthCheck   AuthCheckHook
}

func NewFileRouter(engine *Engine, routesDir string) (*FileRouter, error) {
	absRoutesDir, err := filepath.Abs(filepath.Clean(routesDir))
	if err != nil {
		absRoutesDir = routesDir
	}

	router := &FileRouter{
		engine:      engine,
		routesDir:   absRoutesDir,
		dataLoaders: make(map[string]PageDataLoader),
	}

	if err := router.discoverRoutes(); err != nil {
		return nil, err
	}

	return router, nil
}

func (r *FileRouter) RegisterDataLoader(routePath string, loader PageDataLoader) {
	r.dataLoaders[r.normalizePattern(routePath)] = loader
}

func (r *FileRouter) discoverRoutes() error {
	if _, err := os.Stat(r.routesDir); os.IsNotExist(err) {
		return nil // No routes directory, nothing to discover
	}

	var routes []Route
	err := filepath.WalkDir(r.routesDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if d.IsDir() {
			return nil
		}

		if d.Name() == "+page.pte" {
			rel, err := filepath.Rel(r.routesDir, path)
			if err != nil {
				return err
			}

			// Convert relative path to URL path format
			dirPart := filepath.Dir(rel)
			var routePath string
			if dirPart == "." || dirPart == "" {
				routePath = "/"
			} else {
				routePath = "/" + filepath.ToSlash(dirPart)
			}

			// Compile template to parse AST and collect page metadata
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}

			compiled, err := r.engine.Compile(string(data))
			if err != nil {
				return fmt.Errorf("failed to compile routing page %s: %w", path, err)
			}

			routes = append(routes, Route{
				Path:         routePath,
				TemplatePath: path,
				Compiled:     compiled,
				Segments:     splitRoutePath(routePath),
			})
		}
		return nil
	})

	if err != nil {
		return err
	}

	// Sort routes based on segment specificity (static routes priority over wildcards)
	sort.Slice(routes, func(i, j int) bool {
		segsI := routes[i].Segments
		segsJ := routes[j].Segments

		limit := len(segsI)
		if len(segsJ) < limit {
			limit = len(segsJ)
		}

		for k := 0; k < limit; k++ {
			isWildI := strings.HasPrefix(segsI[k], "[") && strings.HasSuffix(segsI[k], "]")
			isWildJ := strings.HasPrefix(segsJ[k], "[") && strings.HasSuffix(segsJ[k], "]")

			if isWildI != isWildJ {
				// Static segments (isWildI = false) should come first in search order
				return !isWildI
			}
		}

		return len(segsI) > len(segsJ)
	})

	r.routes = routes
	return nil
}

func (r *FileRouter) Match(urlPath string) (*Route, map[string]string) {
	cleaned := filepath.Clean(urlPath)
	if cleaned == "" || cleaned == "." {
		cleaned = "/"
	}
	urlSegs := splitRoutePath(cleaned)

	for _, route := range r.routes {
		if len(route.Segments) != len(urlSegs) {
			continue
		}

		params := make(map[string]string)
		matched := true
		for i, seg := range route.Segments {
			if strings.HasPrefix(seg, "[") && strings.HasSuffix(seg, "]") {
				paramName := seg[1 : len(seg)-1]
				params[paramName] = urlSegs[i]
			} else if seg != urlSegs[i] {
				matched = false
				break
			}
		}

		if matched {
			return &route, params
		}
	}
	return nil, nil
}

func (r *FileRouter) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	route, params := r.Match(req.URL.Path)
	if route == nil {
		http.NotFound(w, req)
		return
	}

	metadata := route.Compiled.Metadata

	// Enforce metadata auth and roles checks
	if authVal, ok := metadata["auth"]; ok {
		requiredAuth := false
		if b, ok := authVal.(bool); ok {
			requiredAuth = b
		}
		if requiredAuth {
			var requiredRoles []string
			if rolesVal, ok := metadata["roles"]; ok {
				if rList, ok := rolesVal.([]string); ok {
					requiredRoles = rList
				} else if rStr, ok := rolesVal.(string); ok {
					requiredRoles = []string{rStr}
				}
			}

			if r.AuthCheck != nil {
				ok, status, msg := r.AuthCheck(req, requiredRoles)
				if !ok {
					if status == 0 {
						status = http.StatusUnauthorized
					}
					if msg == "" {
						msg = http.StatusText(status)
					}
					http.Error(w, msg, status)
					return
				}
			} else {
				// No AuthCheck hook registered, deny by default as secure practice
				http.Error(w, "Unauthorized (No AuthCheck Hook Registered)", http.StatusUnauthorized)
				return
			}
		}
	}

	model := make(map[string]any)
	for k, v := range params {
		model[k] = v
	}

	// Propagate query params
	for k, vals := range req.URL.Query() {
		if len(vals) > 0 {
			if len(vals) == 1 {
				model[k] = vals[0]
			} else {
				model[k] = vals
			}
		}
	}

	// Call Loader if registered
	if loader, ok := r.dataLoaders[route.Path]; ok {
		loadedData, err := loader(req, params)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		for k, v := range loadedData {
			model[k] = v
		}
	}

	// Set title from metadata if not present in model
	if title, ok := metadata["title"]; ok {
		if _, exists := model["title"]; !exists {
			model["title"] = title
		}
	}

	// Apply custom headers from metadata
	if cache, ok := metadata["cache"]; ok {
		w.Header().Set("Cache-Control", fmt.Sprintf("%v", cache))
	}

	contentType := "text/html; charset=utf-8"
	if ct, ok := metadata["contentType"]; ok {
		contentType = fmt.Sprintf("%v", ct)
	}
	w.Header().Set("Content-Type", contentType)

	// Render the template
	ctx := NewContext(model)
	ctx.PushLocal("_engine", r.engine)

	var renderErr error
	if r.engine.minify {
		var buf bytes.Buffer
		renderErr = route.Compiled.RootNode.Render(ctx, &buf)
		if renderErr == nil {
			_, _ = io.WriteString(w, MinifyHTML(buf.String()))
		}
	} else if r.engine.prettify {
		var buf bytes.Buffer
		renderErr = route.Compiled.RootNode.Render(ctx, &buf)
		if renderErr == nil {
			_, _ = io.WriteString(w, PrettifyHTML(buf.String()))
		}
	} else {
		renderErr = route.Compiled.RootNode.Render(ctx, w)
	}

	if renderErr != nil {
		// Only write error if headers have not been written
		http.Error(w, renderErr.Error(), http.StatusInternalServerError)
	}
}

func (r *FileRouter) normalizePattern(p string) string {
	p = strings.TrimSpace(p)
	p = strings.ReplaceAll(p, "\\", "/")
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	if p != "/" {
		p = strings.TrimSuffix(p, "/")
	}
	return p
}

func splitRoutePath(p string) []string {
	p = strings.Trim(p, "/")
	if p == "" {
		return []string{}
	}
	return strings.Split(p, "/")
}
