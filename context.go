package pte

type Context struct {
	values      map[string]any
	localValues map[string]any
}

func NewContext(values map[string]any) *Context {
	initialValues := make(map[string]any)
	if values != nil {
		for key, val := range values {
			initialValues[key] = val
		}
	}
	return &Context{
		values:      initialValues,
		localValues: make(map[string]any),
	}
}

func (context *Context) Get(name string) any {
	if val, exists := context.localValues[name]; exists {
		return val
	}
	return context.values[name]
}

func (context *Context) With(name string, value any) *Context {
	nextValues := make(map[string]any, len(context.values)+1)
	for key, val := range context.values {
		nextValues[key] = val
	}
	nextValues[name] = value

	nextLocals := make(map[string]any, len(context.localValues))
	for key, val := range context.localValues {
		nextLocals[key] = val
	}

	return &Context{
		values:      nextValues,
		localValues: nextLocals,
	}
}

func (context *Context) WithAll(childValues map[string]any) *Context {
	nextValues := make(map[string]any, len(context.values)+len(childValues))
	for key, val := range context.values {
		nextValues[key] = val
	}
	for key, val := range childValues {
		nextValues[key] = val
	}

	nextLocals := make(map[string]any, len(context.localValues))
	for key, val := range context.localValues {
		nextLocals[key] = val
	}

	return &Context{
		values:      nextValues,
		localValues: nextLocals,
	}
}

func (context *Context) SubContext(childValues map[string]any) *Context {
	return context.WithAll(childValues)
}

func (context *Context) PushLocal(name string, value any) {
	context.localValues[name] = value
}
