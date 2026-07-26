package pte

type Context struct {
	values      map[string]any
	localValues map[string]any
}

func NewContext(values map[string]any) *Context {
	v := make(map[string]any)
	if values != nil {
		for k, val := range values {
			v[k] = val
		}
	}
	return &Context{
		values:      v,
		localValues: make(map[string]any),
	}
}

func (c *Context) Get(name string) any {
	if val, ok := c.localValues[name]; ok {
		return val
	}
	return c.values[name]
}

func (c *Context) With(name string, value any) *Context {
	nextValues := make(map[string]any, len(c.values)+1)
	for k, v := range c.values {
		nextValues[k] = v
	}
	nextValues[name] = value

	nextLocals := make(map[string]any, len(c.localValues))
	for k, v := range c.localValues {
		nextLocals[k] = v
	}

	return &Context{
		values:      nextValues,
		localValues: nextLocals,
	}
}

func (c *Context) WithAll(childValues map[string]any) *Context {
	nextValues := make(map[string]any, len(c.values)+len(childValues))
	for k, v := range c.values {
		nextValues[k] = v
	}
	for k, v := range childValues {
		nextValues[k] = v
	}

	nextLocals := make(map[string]any, len(c.localValues))
	for k, v := range c.localValues {
		nextLocals[k] = v
	}

	return &Context{
		values:      nextValues,
		localValues: nextLocals,
	}
}

func (c *Context) SubContext(childValues map[string]any) *Context {
	return c.WithAll(childValues)
}

func (c *Context) PushLocal(name string, value any) {
	c.localValues[name] = value
}
