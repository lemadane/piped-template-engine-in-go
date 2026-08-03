package pte

import (
	"bytes"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type PageDataLoader func(request *http.Request, params map[string]string) (map[string]any, error)

type AuthCheckHook func(request *http.Request, requiredRoles []string) (isAuthorized bool, statusCode int, message string)

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

func NewFileRouterFS(engine *Engine, filesystem fs.FS, rootDir string) (*FileRouter, error) {
	router := &FileRouter{
		engine:      engine,
		routesDir:   rootDir,
		dataLoaders: make(map[string]PageDataLoader),
	}

	var discoveredRoutes []Route
	err := fs.WalkDir(filesystem, rootDir, func(filePath string, dirEntry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		if dirEntry.IsDir() {
			return nil
		}

		if dirEntry.Name() == "+page.pte" {
			relativePath, relErr := filepath.Rel(rootDir, filePath)
			if relErr != nil {
				relativePath = filePath
			}

			dirPart := filepath.Dir(relativePath)
			var routePath string
			if dirPart == "." || dirPart == "" {
				routePath = "/"
			} else {
				routePath = "/" + filepath.ToSlash(dirPart)
			}

			templateData, readErr := fs.ReadFile(filesystem, filePath)
			if readErr != nil {
				return readErr
			}

			compiled, compileErr := engine.Compile(string(templateData))
			if compileErr != nil {
				return fmt.Errorf("failed to compile routing page %s: %w", filePath, compileErr)
			}

			discoveredRoutes = append(discoveredRoutes, Route{
				Path:         routePath,
				TemplatePath: filePath,
				Compiled:     compiled,
				Segments:     splitRoutePath(routePath),
			})
		}
		return nil
	})

	if err != nil {
		return nil, err
	}

	sort.Slice(discoveredRoutes, func(firstIndex, secondIndex int) bool {
		return isMoreSpecificRoute(discoveredRoutes[firstIndex], discoveredRoutes[secondIndex])
	})

	router.routes = discoveredRoutes
	return router, nil
}

func (router *FileRouter) RegisterDataLoader(routePath string, loader PageDataLoader) {
	router.dataLoaders[router.normalizePattern(routePath)] = loader
}

func (router *FileRouter) discoverRoutes() error {
	if _, err := os.Stat(router.routesDir); os.IsNotExist(err) {
		return nil // No routes directory, nothing to discover
	}

	var discoveredRoutes []Route
	err := filepath.WalkDir(router.routesDir, func(filePath string, dirEntry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		if dirEntry.IsDir() {
			return nil
		}

		if dirEntry.Name() == "+page.pte" {
			relativePath, relErr := filepath.Rel(router.routesDir, filePath)
			if relErr != nil {
				return relErr
			}

			// Convert relative path to URL path format
			dirPart := filepath.Dir(relativePath)
			var routePath string
			if dirPart == "." || dirPart == "" {
				routePath = "/"
			} else {
				routePath = "/" + filepath.ToSlash(dirPart)
			}

			// Compile template to parse AST and collect page metadata
			templateData, readErr := os.ReadFile(filePath)
			if readErr != nil {
				return readErr
			}

			compiled, compileErr := router.engine.Compile(string(templateData))
			if compileErr != nil {
				return fmt.Errorf("failed to compile routing page %s: %w", filePath, compileErr)
			}

			discoveredRoutes = append(discoveredRoutes, Route{
				Path:         routePath,
				TemplatePath: filePath,
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
	sort.Slice(discoveredRoutes, func(firstIndex, secondIndex int) bool {
		return isMoreSpecificRoute(discoveredRoutes[firstIndex], discoveredRoutes[secondIndex])
	})

	router.routes = discoveredRoutes
	return nil
}

func isMoreSpecificRoute(firstRoute, secondRoute Route) bool {
	firstSegments := firstRoute.Segments
	secondSegments := secondRoute.Segments

	segmentLimit := len(firstSegments)
	if len(secondSegments) < segmentLimit {
		segmentLimit = len(secondSegments)
	}

	for segmentIndex := 0; segmentIndex < segmentLimit; segmentIndex++ {
		isWildcardFirst := strings.HasPrefix(firstSegments[segmentIndex], "[") && strings.HasSuffix(firstSegments[segmentIndex], "]")
		isWildcardSecond := strings.HasPrefix(secondSegments[segmentIndex], "[") && strings.HasSuffix(secondSegments[segmentIndex], "]")

		if isWildcardFirst != isWildcardSecond {
			return !isWildcardFirst
		}
	}

	return len(firstSegments) > len(secondSegments)
}

func (router *FileRouter) Match(urlPath string) (*Route, map[string]string) {
	cleanedPath := filepath.Clean(urlPath)
	if cleanedPath == "" || cleanedPath == "." {
		cleanedPath = "/"
	}
	urlSegments := splitRoutePath(cleanedPath)

	for _, route := range router.routes {
		if len(route.Segments) != len(urlSegments) {
			continue
		}

		routeParams := make(map[string]string)
		isMatch := true
		for segmentIndex, segment := range route.Segments {
			if strings.HasPrefix(segment, "[") && strings.HasSuffix(segment, "]") {
				paramName := segment[1 : len(segment)-1]
				routeParams[paramName] = urlSegments[segmentIndex]
			} else if segment != urlSegments[segmentIndex] {
				isMatch = false
				break
			}
		}

		if isMatch {
			return &route, routeParams
		}
	}
	return nil, nil
}

func (router *FileRouter) ServeHTTP(responseWriter http.ResponseWriter, request *http.Request) {
	route, routeParams := router.Match(request.URL.Path)
	if route == nil {
		http.NotFound(responseWriter, request)
		return
	}

	metadata := route.Compiled.Metadata

	// Enforce metadata auth and roles checks
	if authVal, isFound := metadata["auth"]; isFound {
		requiredAuth := false
		if booleanVal, isBool := authVal.(bool); isBool {
			requiredAuth = booleanVal
		}
		if requiredAuth {
			var requiredRoles []string
			if rolesVal, isFoundRoles := metadata["roles"]; isFoundRoles {
				if roleList, isList := rolesVal.([]string); isList {
					requiredRoles = roleList
				} else if roleString, isString := rolesVal.(string); isString {
					requiredRoles = []string{roleString}
				}
			}

			if router.AuthCheck != nil {
				isAuthorized, statusCode, message := router.AuthCheck(request, requiredRoles)
				if !isAuthorized {
					if statusCode == 0 {
						statusCode = http.StatusUnauthorized
					}
					if message == "" {
						message = http.StatusText(statusCode)
					}
					http.Error(responseWriter, message, statusCode)
					return
				}
			} else {
				// No AuthCheck hook registered, deny by default as secure practice
				http.Error(responseWriter, "Unauthorized (No AuthCheck Hook Registered)", http.StatusUnauthorized)
				return
			}
		}
	}

	dataModel := make(map[string]any)
	for key, value := range routeParams {
		dataModel[key] = value
	}

	// Propagate query params
	for key, queryValues := range request.URL.Query() {
		if len(queryValues) > 0 {
			if len(queryValues) == 1 {
				dataModel[key] = queryValues[0]
			} else {
				dataModel[key] = queryValues
			}
		}
	}

	// Call Loader if registered
	if dataLoader, isFoundLoader := router.dataLoaders[route.Path]; isFoundLoader {
		loadedData, loaderErr := dataLoader(request, routeParams)
		if loaderErr != nil {
			http.Error(responseWriter, loaderErr.Error(), http.StatusInternalServerError)
			return
		}
		for key, value := range loadedData {
			dataModel[key] = value
		}
	}

	// Inject standard request PageContext
	if _, exists := dataModel["page"]; !exists {
		headerMap := make(map[string]string)
		for key, headerValues := range request.Header {
			if len(headerValues) > 0 {
				headerMap[key] = headerValues[0]
			}
		}

		cookieMap := make(map[string]string)
		for _, cookie := range request.Cookies() {
			cookieMap[cookie.Name] = cookie.Value
		}

		paramMap := make(map[string]any)
		for key, value := range routeParams {
			paramMap[key] = value
		}
		for key, queryValues := range request.URL.Query() {
			if len(queryValues) > 0 {
				if len(queryValues) == 1 {
					paramMap[key] = queryValues[0]
				} else {
					paramMap[key] = queryValues
				}
			}
		}

		isHTMX := request.Header.Get("HX-Request") == "true"
		dataModel["page"] = &PageContext{
			RequestURI:   request.URL.Path,
			QueryString:  request.URL.RawQuery,
			Method:       request.Method,
			Headers:      headerMap,
			Params:       paramMap,
			Cookies:      cookieMap,
			IsHTMX:       isHTMX,
			HXTarget:     request.Header.Get("HX-Target"),
			HXTrigger:    request.Header.Get("HX-Trigger"),
			HXCurrentURL: request.Header.Get("HX-Current-URL"),
		}
	}

	// Set title from metadata if not present in model
	if titleValue, isFoundTitle := metadata["title"]; isFoundTitle {
		if _, exists := dataModel["title"]; !exists {
			dataModel["title"] = titleValue
		}
	}

	// Apply custom headers from metadata
	if cacheValue, isFoundCache := metadata["cache"]; isFoundCache {
		responseWriter.Header().Set("Cache-Control", fmt.Sprintf("%v", cacheValue))
	}

	contentType := "text/html; charset=utf-8"
	if ctValue, isFoundContentType := metadata["contentType"]; isFoundContentType {
		contentType = fmt.Sprintf("%v", ctValue)
	}
	responseWriter.Header().Set("Content-Type", contentType)

	// Apply HTMX metadata response headers
	if hxTriggerVal, isFoundTrigger := metadata["hxTrigger"]; isFoundTrigger {
		responseWriter.Header().Set("HX-Trigger", fmt.Sprintf("%v", hxTriggerVal))
	}
	if hxRedirectVal, isFoundRedirect := metadata["hxRedirect"]; isFoundRedirect {
		responseWriter.Header().Set("HX-Redirect", fmt.Sprintf("%v", hxRedirectVal))
	}
	if hxPushUrlVal, isFoundPush := metadata["hxPushUrl"]; isFoundPush {
		responseWriter.Header().Set("HX-Push-Url", fmt.Sprintf("%v", hxPushUrlVal))
	}
	if hxRefreshVal, isFoundRefresh := metadata["hxRefresh"]; isFoundRefresh {
		if booleanRefresh, isBool := hxRefreshVal.(bool); isBool && booleanRefresh {
			responseWriter.Header().Set("HX-Refresh", "true")
		} else {
			responseWriter.Header().Set("HX-Refresh", fmt.Sprintf("%v", hxRefreshVal))
		}
	}

	// Render the template to a buffer before writing headers or body
	context := NewContext(dataModel)
	context.PushLocal("_engine", router.engine)

	var buffer bytes.Buffer
	renderErr := route.Compiled.RootNode.Render(context, &buffer)
	if renderErr != nil {
		http.Error(responseWriter, renderErr.Error(), http.StatusInternalServerError)
		return
	}

	renderedResult := buffer.String()
	if router.engine.minify {
		renderedResult = MinifyHTML(renderedResult)
	} else if router.engine.prettify {
		renderedResult = PrettifyHTML(renderedResult)
	}

	_, _ = io.WriteString(responseWriter, renderedResult)
}

func (router *FileRouter) normalizePattern(pattern string) string {
	pattern = strings.TrimSpace(pattern)
	pattern = strings.ReplaceAll(pattern, "\\", "/")
	if !strings.HasPrefix(pattern, "/") {
		pattern = "/" + pattern
	}
	if pattern != "/" {
		pattern = strings.TrimSuffix(pattern, "/")
	}
	return pattern
}

func splitRoutePath(path string) []string {
	path = strings.Trim(path, "/")
	if path == "" {
		return []string{}
	}
	return strings.Split(path, "/")
}

type PageContext struct {
	RequestURI   string
	QueryString  string
	Method       string
	Headers      map[string]string
	Params       map[string]any
	Cookies      map[string]string
	IsHTMX       bool
	HXTarget     string
	HXTrigger    string
	HXCurrentURL string
}
