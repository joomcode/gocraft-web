package web

import (
	"reflect"
	"sort"
	"strings"
	"time"
)

type httpMethod string

const (
	httpMethodGet     = httpMethod("GET")
	httpMethodPost    = httpMethod("POST")
	httpMethodPut     = httpMethod("PUT")
	httpMethodDelete  = httpMethod("DELETE")
	httpMethodPatch   = httpMethod("PATCH")
	httpMethodHead    = httpMethod("HEAD")
	httpMethodOptions = httpMethod("OPTIONS")
)

var httpMethods = []httpMethod{httpMethodGet, httpMethodPost, httpMethodPut, httpMethodDelete, httpMethodPatch, httpMethodHead, httpMethodOptions}

type Measure int

const (
	MeasureBefore Measure = iota
	MeasureAfter
)

// Router implements net/http's Handler interface and is what you attach middleware, routes/handlers, and subrouters to.
type Router struct {
	// Hierarchy:
	parent           *Router // nil if root router.
	children         []*Router
	maxChildrenDepth int

	// For each request we'll create one of these objects
	contextType reflect.Type

	// Eg, "/" or "/admin". Any routes added to this router will be prefixed with this.
	pathPrefix string

	// Routeset contents:
	middleware []*middlewareHandler
	routes     []*route

	// The root pathnode is the same for a tree of Routers
	root map[httpMethod]*pathNode

	// This can can be set on any router. The target's ErrorHandler will be invoked if it exists
	errorHandler reflect.Value

	// This can only be set on the root handler, since by virtue of not finding a route, we don't have a target.
	// (That being said, in the future we could investigate namespace matches)
	notFoundHandler reflect.Value

	// This can only be set on the root handler, since by virtue of not finding a route, we don't have a target.
	measureCallback  reflect.Value
	measureThreshold time.Duration

	// This can only be set on the root handler, since by virtue of not finding a route, we don't have a target.
	optionsHandler reflect.Value
}

// NextMiddlewareFunc are functions passed into your middleware. To advance the middleware, call the function.
// You should usually pass the existing ResponseWriter and *Request into the next middlware, but you can
// chose to swap them if you want to modify values or capture things written to the ResponseWriter.
type NextMiddlewareFunc func(ResponseWriter, *Request)

// GenericMiddleware are middleware that doesn't have or need a context. General purpose middleware, such as
// static file serving, has this signature. If your middlware doesn't need a context, you can use this
// signature to get a small performance boost.
type GenericMiddleware func(ResponseWriter, *Request, NextMiddlewareFunc)

// GenericHandler are handlers that don't have or need a context. If your handler doesn't need a context,
// you can use this signature to get a small performance boost.
type GenericHandler func(ResponseWriter, *Request)

type route struct {
	Router  *Router
	Method  httpMethod
	Path    string
	Handler *actionHandler
}

type middlewareHandler struct {
	Generic           bool
	DynamicMiddleware reflect.Value
	GenericMiddleware GenericMiddleware
}

type actionHandler struct {
	Generic        bool
	DynamicHandler reflect.Value
	GenericHandler GenericHandler
}

var emptyInterfaceType = reflect.TypeOf((*interface{})(nil)).Elem()

// New returns a new router with context type ctx. ctx should be a struct instance,
// whose purpose is to communicate type information. On each request, an instance of this
// context type will be automatically allocated and sent to handlers.
func New(ctx interface{}) *Router {
	validateContext(ctx, nil)

	r := &Router{}
	r.contextType = reflect.TypeOf(ctx)
	r.pathPrefix = "/"
	r.maxChildrenDepth = 1
	r.root = make(map[httpMethod]*pathNode)
	for _, method := range httpMethods {
		r.root[method] = newPathNode()
	}
	return r
}

// NewWithPrefix returns a new router (see New) but each route will have an implicit prefix.
// For instance, with pathPrefix = "/api/v2", all routes under this router will begin with "/api/v2".
func NewWithPrefix(ctx interface{}, pathPrefix string) *Router {
	r := New(ctx)
	r.pathPrefix = pathPrefix

	return r
}

func (r *Router) PathPrefix() string {
	return r.pathPrefix
}

func (r *Router) RootRouter() *Router {
	result := r
	for result.parent != nil {
		result = result.parent
	}
	return result
}

func (r *Router) AllFullRoutes() []string {
	if r.parent != nil {
		panic("You can only call AllFullRoutes() on the root router.")
	}

	var allRoutes []string
	traverse(r, func(route *route) {
		allRoutes = append(allRoutes, route.Path)
	})

	sort.Strings(allRoutes)

	return compact(allRoutes)
}

func (r *Router) AllFullRoutesWithMethods() []string {
	if r.parent != nil {
		panic("You can only call AllFullRoutesWithMethods() on the root router.")
	}

	var allRoutesWithMethods []string
	traverse(r, func(route *route) {
		allRoutesWithMethods = append(allRoutesWithMethods, string(route.Method)+" "+route.Path)
	})

	// Sort by path, ignoring method
	sort.Slice(allRoutesWithMethods, func(i, j int) bool {
		return strings.SplitN(allRoutesWithMethods[i], " ", 2)[1] < strings.SplitN(allRoutesWithMethods[j], " ", 2)[1]
	})

	return compact(allRoutesWithMethods)
}

// Subrouter attaches a new subrouter to the specified router and returns it.
// You can use the same context or pass a new one. If you pass a new one, it must
// embed a pointer to the previous context in the first slot. You can also pass
// a pathPrefix that each route will have. If "" is passed, then no path prefix is applied.
func (r *Router) Subrouter(ctx interface{}, pathPrefix string) *Router {
	validateContext(ctx, r.contextType)

	// Create new router, link up hierarchy
	newRouter := &Router{parent: r}
	r.children = append(r.children, newRouter)

	// Increment maxChildrenDepth if this is the first child of the router
	if len(r.children) == 1 {
		curParent := r
		for curParent != nil {
			curParent.maxChildrenDepth = curParent.depth()
			curParent = curParent.parent
		}
	}

	newRouter.contextType = reflect.TypeOf(ctx)
	newRouter.pathPrefix = appendPath(r.pathPrefix, pathPrefix)
	newRouter.root = r.root

	return newRouter
}

// Middleware adds the specified middleware tot he router and returns the router.
func (r *Router) Middleware(fn interface{}) *Router {
	vfn := reflect.ValueOf(fn)
	validateMiddleware(vfn, r.contextType)
	if vfn.Type().NumIn() == 3 {
		r.middleware = append(r.middleware, &middlewareHandler{Generic: true, GenericMiddleware: fn.(func(ResponseWriter, *Request, NextMiddlewareFunc))})
	} else {
		r.middleware = append(r.middleware, &middlewareHandler{Generic: false, DynamicMiddleware: vfn})
	}

	return r
}

// Error sets the specified function as the error handler (when panics happen) and returns the router.
func (r *Router) Error(fn interface{}) *Router {
	vfn := reflect.ValueOf(fn)
	validateErrorHandler(vfn, r.contextType)
	r.errorHandler = vfn
	return r
}

// NotFound sets the specified function as the not-found handler (when no route matches) and returns the router.
// Note that only the root router can have a NotFound handler.
func (r *Router) NotFound(fn interface{}) *Router {
	if r.parent != nil {
		panic("You can only set a NotFoundHandler on the root router.")
	}
	vfn := reflect.ValueOf(fn)
	validateNotFoundHandler(vfn, r.contextType)
	r.notFoundHandler = vfn
	return r
}

// MeasureMiddlewares sets the specified callback function to report time spent in middleware before and after call of next().
// If threshold is not equal zero reports only measurements greater than specified threshold.
// Expected callback should be in form: func(ctx ContextType, middlewareName string, measure Measure, duration time.Duration)
// Note that only the root router can have a Measure callback.
func (r *Router) MeasureMiddlewares(threshold time.Duration, callback interface{}) *Router {
	if r.parent != nil {
		panic("You can only set a MeasureMiddlewares on the root router.")
	}
	cb := reflect.ValueOf(callback)
	validateMeasurementHandler(cb, r.contextType)
	r.measureCallback = cb
	r.measureThreshold = threshold
	return r
}

// OptionsHandler sets the specified function as the options handler and returns the router.
// Note that only the root router can have a OptionsHandler handler.
func (r *Router) OptionsHandler(fn interface{}) *Router {
	if r.parent != nil {
		panic("You can only set an OptionsHandler on the root router.")
	}
	vfn := reflect.ValueOf(fn)
	validateOptionsHandler(vfn, r.contextType)
	r.optionsHandler = vfn
	return r
}

// Get will add a route to the router that matches on GET requests and the specified path.
func (r *Router) Get(path string, fn interface{}) *Router {
	return r.addRoute(httpMethodGet, path, fn)
}

// Post will add a route to the router that matches on POST requests and the specified path.
func (r *Router) Post(path string, fn interface{}) *Router {
	return r.addRoute(httpMethodPost, path, fn)
}

// Put will add a route to the router that matches on PUT requests and the specified path.
func (r *Router) Put(path string, fn interface{}) *Router {
	return r.addRoute(httpMethodPut, path, fn)
}

// Delete will add a route to the router that matches on DELETE requests and the specified path.
func (r *Router) Delete(path string, fn interface{}) *Router {
	return r.addRoute(httpMethodDelete, path, fn)
}

// Patch will add a route to the router that matches on PATCH requests and the specified path.
func (r *Router) Patch(path string, fn interface{}) *Router {
	return r.addRoute(httpMethodPatch, path, fn)
}

// Head will add a route to the router that matches on HEAD requests and the specified path.
func (r *Router) Head(path string, fn interface{}) *Router {
	return r.addRoute(httpMethodHead, path, fn)
}

// Options will add a route to the router that matches on OPTIONS requests and the specified path.
func (r *Router) Options(path string, fn interface{}) *Router {
	return r.addRoute(httpMethodOptions, path, fn)
}

func (r *Router) addRoute(method httpMethod, path string, fn interface{}) *Router {
	vfn := reflect.ValueOf(fn)
	validateHandler(vfn, r.contextType)
	fullPath := appendPath(r.pathPrefix, path)
	route := &route{Method: method, Path: fullPath, Router: r}
	if vfn.Type().NumIn() == 2 {
		route.Handler = &actionHandler{Generic: true, GenericHandler: fn.(func(ResponseWriter, *Request))}
	} else {
		route.Handler = &actionHandler{Generic: false, DynamicHandler: vfn}
	}
	r.routes = append(r.routes, route)
	r.root[method].add(fullPath, route)
	return r
}

// Calculates the max child depth of the node. Leaves return 1. For Parent->Child, Parent is 2.
func (r *Router) depth() int {
	max := 0
	for _, child := range r.children {
		childDepth := child.depth()
		if childDepth > max {
			max = childDepth
		}
	}
	return max + 1
}

//
// Private methods:
//

// Panics unless validation is correct
func validateContext(ctx interface{}, parentCtxType reflect.Type) {
	ctxType := reflect.TypeOf(ctx)

	if ctxType.Kind() != reflect.Struct {
		panic("web: Context needs to be a struct type")
	}

	if parentCtxType != nil && parentCtxType != ctxType {
		if ctxType.NumField() == 0 {
			panic("web: Context needs to have first field be a pointer to parent context")
		}

		fldType := ctxType.Field(0).Type

		// Ensure fld is a pointer to parentCtxType
		if fldType != reflect.PtrTo(parentCtxType) {
			panic("web: Context needs to have first field be a pointer to parent context")
		}
	}
}

// Panics unless fn is a proper handler wrt ctxType
// eg, func(ctx *ctxType, writer, request)
func validateHandler(vfn reflect.Value, ctxType reflect.Type) {
	var req *Request
	var resp func() ResponseWriter
	if !isValidHandler(vfn, ctxType, reflect.TypeOf(resp).Out(0), reflect.TypeOf(req)) {
		panic(instructiveMessage(vfn, "a handler", "handler", "rw web.ResponseWriter, req *web.Request", ctxType))
	}
}

func validateErrorHandler(vfn reflect.Value, ctxType reflect.Type) {
	var req *Request
	var resp func() ResponseWriter
	if !isValidHandler(vfn, ctxType, reflect.TypeOf(resp).Out(0), reflect.TypeOf(req), emptyInterfaceType) {
		panic(instructiveMessage(vfn, "an error handler", "error handler", "rw web.ResponseWriter, req *web.Request, err interface{}", ctxType))
	}
}

func validateNotFoundHandler(vfn reflect.Value, ctxType reflect.Type) {
	var req *Request
	var resp func() ResponseWriter
	if !isValidHandler(vfn, ctxType, reflect.TypeOf(resp).Out(0), reflect.TypeOf(req)) {
		panic(instructiveMessage(vfn, "a 'not found' handler", "not found handler", "rw web.ResponseWriter, req *web.Request", ctxType))
	}
}

func validateMeasurementHandler(cb reflect.Value, ctxType reflect.Type) {
	if !isValidHandler(cb, ctxType,
		reflect.TypeOf(""),
		reflect.TypeOf(Measure(0)),
		reflect.TypeOf(time.Now()),
		reflect.TypeOf(time.Now()),
	) {
		panic(instructiveMessage(cb, "a middleware measurement callback", "middleware measurement callback", "name string, measure Measure, start, end time.Time", ctxType))
	}
}

func validateOptionsHandler(vfn reflect.Value, ctxType reflect.Type) {
	var req *Request
	var resp func() ResponseWriter
	var methods []string
	if !isValidHandler(vfn, ctxType, reflect.TypeOf(resp).Out(0), reflect.TypeOf(req), reflect.TypeOf(methods)) {
		panic(instructiveMessage(vfn, "an 'options' handler", "options handler", "rw web.ResponseWriter, req *web.Request, methods []string", ctxType))
	}
}

func validateMiddleware(vfn reflect.Value, ctxType reflect.Type) {
	var req *Request
	var resp func() ResponseWriter
	var n NextMiddlewareFunc
	if !isValidHandler(vfn, ctxType, reflect.TypeOf(resp).Out(0), reflect.TypeOf(req), reflect.TypeOf(n)) {
		panic(instructiveMessage(vfn, "middleware", "middleware", "rw web.ResponseWriter, req *web.Request, next web.NextMiddlewareFunc", ctxType))
	}
}

// Ensures vfn is a function, that optionally takes a *ctxType as the first argument, followed by the specified types. Handlers have no return value.
// Returns true if valid, false otherwise.
func isValidHandler(vfn reflect.Value, ctxType reflect.Type, types ...reflect.Type) bool {
	fnType := vfn.Type()

	if fnType.Kind() != reflect.Func {
		return false
	}

	typesStartIdx := 0
	typesLen := len(types)
	numIn := fnType.NumIn()
	numOut := fnType.NumOut()

	if numOut != 0 {
		return false
	}

	if numIn == typesLen {
		// No context
	} else if numIn == (typesLen + 1) {
		// context, types
		firstArgType := fnType.In(0)
		if ctxPtrType := reflect.PtrTo(ctxType); firstArgType != ctxPtrType && !ctxPtrType.AssignableTo(firstArgType) {
			return false
		}
		typesStartIdx = 1
	} else {
		return false
	}

	for _, typeArg := range types {
		if fnType.In(typesStartIdx) != typeArg {
			return false
		}
		typesStartIdx++
	}

	return true
}

// Since it's easy to pass the wrong method to a middleware/handler route, and since the user can't rely on static type checking since we use reflection,
// lets be super helpful about what they did and what they need to do.
// Arguments:
//  - vfn is the failed method
//  - addingType is for "You are adding {addingType} to a router...". Eg, "middleware" or "a handler" or "an error handler"
//  - yourType is for "Your {yourType} function can have...". Eg, "middleware" or "handler" or "error handler"
//  - args is like "rw web.ResponseWriter, req *web.Request, next web.NextMiddlewareFunc"
//    - NOTE: args can be calculated if you pass in each type. BUT, it doesn't have example argument name, so it has less copy/paste value.
func instructiveMessage(vfn reflect.Value, addingType string, yourType string, args string, ctxType reflect.Type) string {
	// Get context type without package.
	ctxString := ctxType.String()
	splitted := strings.Split(ctxString, ".")
	if len(splitted) <= 1 {
		ctxString = splitted[0]
	} else {
		ctxString = splitted[1]
	}

	str := "\n" + strings.Repeat("*", 120) + "\n"
	str += "* You are adding " + addingType + " to a router with context type '" + ctxString + "'\n"
	str += "*\n*\n"
	str += "* Your " + yourType + " function can have one of these signatures:\n"
	str += "*\n"
	str += "* // If you don't need context:\n"
	str += "* func YourFunctionName(" + args + ")\n"
	str += "*\n"
	str += "* // If you want your " + yourType + " to accept a context:\n"
	str += "* func (c *" + ctxString + ") YourFunctionName(" + args + ")  // or,\n"
	str += "* func YourFunctionName(c *" + ctxString + ", " + args + ") // or,\n"
	str += "* func YourFunctionName(c InterfaceImplementedByContext, " + args + ")\n"
	str += "*\n"
	str += "* Unfortunately, your function has this signature: " + vfn.Type().String() + "\n"
	str += "*\n"
	str += strings.Repeat("*", 120) + "\n"

	return str
}

// Both rootPath/childPath are like "/" and "/users"
// Assumption is that both are well-formed paths.
// Returns a path without a trailing "/" unless the overall path is just "/"
func appendPath(rootPath, childPath string) string {
	return strings.TrimRight(rootPath, "/") + "/" + strings.TrimLeft(childPath, "/")
}

// Traverse the router tree and call the process function on each route
func traverse(r *Router, process func(*route)) {
	for _, route := range r.routes {
		process(route)
	}
	for _, child := range r.children {
		traverse(child, process)
	}
}

// Remove duplicates from a sorted slice of strings
func compact(s []string) []string {
	unique := s[:0]
	for i, r := range s {
		if i == 0 || r != s[i-1] {
			unique = append(unique, r)
		}
	}
	return unique
}
