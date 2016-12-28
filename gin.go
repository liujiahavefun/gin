// Copyright 2014 Manu Martinez-Almeida.  All rights reserved.
// Use of this source code is governed by a MIT style
// license that can be found in the LICENSE file.

package gin

import (
	"html/template"
	"net"
	"net/http"
	"os"
	"sync"
	"github.com/gin-gonic/gin/render"
)

// Version is Framework's version
const Version = "v1.0rc2"

var default404Body = []byte("404 page not found")
var default405Body = []byte("405 method not allowed")

type HandlerFunc func(*Context)
type HandlersChain []HandlerFunc

// Last returns the last handler in the chain. ie. the last handler is the main own.
func (c HandlersChain) Last() HandlerFunc {
	length := len(c)
	if length > 0 {
		return c[length-1]
	}
	return nil
}

type (
	RoutesInfo []RouteInfo
	RouteInfo  struct {
		Method  string
		Path    string
		Handler string
	}

	// Engine is the framework's instance, it contains the muxer, middleware and configuration settings.
	// Create an instance of Engine, by using New() or Default()
	Engine struct {
		RouterGroup
		HTMLRender  render.HTMLRender
		allNoRoute  HandlersChain
		allNoMethod HandlersChain
		noRoute     HandlersChain
		noMethod    HandlersChain
		pool        sync.Pool
		trees       methodTrees

		// liujia: 就是如果访问的是"/xxx/"，但是实际路由只配了"/xxx"，这种情况就差了一个TrailingSlash
		// 这种情况下，对于GET，返回301（301 永久重定向,告诉客户端以后应从新地址访问）
		// 对于其他，返回307（对于POST请求，表示请求还没有被处理，客户端应该向Location里的URI重新发起POST请求）
		// Enables automatic redirection if the current route can't be matched but a
		// handler for the path with (without) the trailing slash exists.
		// For example if /foo/ is requested but a route only exists for /foo, the
		// client is redirected to /foo with http status code 301 for GET requests
		// and 307 for all other request methods.
		RedirectTrailingSlash bool

		// liujia: 就是如果访问的是"/XXX"，但是实际路由只配了"/xxx"，这种情况还是301 307重定向
		// 并且对"../" or "//" 这种，直接去掉，防止任何安全问题
		// If enabled, the router tries to fix the current request path, if no
		// handle is registered for it.
		// First superfluous path elements like ../ or // are removed.
		// Afterwards the router does a case-insensitive lookup of the cleaned path.
		// If a handle can be found for this route, the router makes a redirection
		// to the corrected path with status code 301 for GET requests and 307 for
		// all other request methods.
		// For example /FOO and /..//Foo could be redirected to /foo.
		// RedirectTrailingSlash is independent of this option.
		RedirectFixedPath bool

		// liujia: method not allowed，就是比如用POST请求去访问静态文件。
		// 如果这个配置是true，发现这种情况返回405 'Method Not Allowed'
		// 否则返回404
		// If enabled, the router checks if another method is allowed for the
		// current route, if the current request can not be routed.
		// If this is the case, the request is answered with 'Method Not Allowed'
		// and HTTP status code 405.
		// If no other Method is allowed, the request is delegated to the NotFound
		// handler.
		HandleMethodNotAllowed bool

		// liujia: 如果设置为true，对于ngnix反向代理这种情况，会尝试从X-Real-Ip X-Forwarded-For找真实client IP
		// 否则就是从c.Request.RemoteAddr解析拿，当然ngnix反向代理这种情况通常拿的不对
		ForwardedByClientIP    bool
	}
)

//liujia: 编译手法，保证Engine实现了IRouter接口
var _ IRouter = &Engine{}

// New returns a new blank Engine instance without any middleware attached.
// By default the configuration is:
// - RedirectTrailingSlash:  true
// - RedirectFixedPath:      false
// - HandleMethodNotAllowed: false
// - ForwardedByClientIP:    true
func New() *Engine {
	debugPrintWARNINGNew()
	engine := &Engine{
		RouterGroup: RouterGroup{
			Handlers: nil,
			basePath: "/",
			root:     true,
		},
		RedirectTrailingSlash:  true,
		RedirectFixedPath:      false,
		HandleMethodNotAllowed: false,
		ForwardedByClientIP:    true,
		trees:                  make(methodTrees, 0, 9), 
		//liujia: 有type methodTrees []methodTree，所以type后的类型如果是[]或者map，也可以make出来
	}
	engine.RouterGroup.engine = engine
	engine.pool.New = func() interface{} {
		return engine.allocateContext()
	}
	return engine
}

// Default returns an Engine instance with the Logger and Recovery middleware already attached.
func Default() *Engine {
	engine := New()
	engine.Use(Logger(), Recovery())
	return engine
}

// liujia: 对每一个请求都会分配这么一个Context，应该是最重要的一个结构，参考context.go实现
func (engine *Engine) allocateContext() *Context {
	return &Context{engine: engine}
}

// liujia: 下面这些都涉及http.template，不熟悉
func (engine *Engine) LoadHTMLGlob(pattern string) {
	if IsDebugging() {
		debugPrintLoadTemplate(template.Must(template.ParseGlob(pattern)))
		engine.HTMLRender = render.HTMLDebug{Glob: pattern}
	} else {
		templ := template.Must(template.ParseGlob(pattern))
		engine.SetHTMLTemplate(templ)
	}
}

func (engine *Engine) LoadHTMLFiles(files ...string) {
	if IsDebugging() {
		engine.HTMLRender = render.HTMLDebug{Files: files}
	} else {
		templ := template.Must(template.ParseFiles(files...))
		engine.SetHTMLTemplate(templ)
	}
}

func (engine *Engine) SetHTMLTemplate(templ *template.Template) {
	if len(engine.trees) > 0 {
		debugPrintWARNINGSetHTMLTemplate()
	}
	engine.HTMLRender = render.HTMLProduction{Template: templ}
}

// liujia: NoRoute() NoMethod() 表示添加用于处理404和405的喊
// NoRoute adds handlers for NoRoute. It return a 404 code by default.
func (engine *Engine) NoRoute(handlers ...HandlerFunc) {
	engine.noRoute = handlers
	engine.rebuild404Handlers()
}

// NoMethod sets the handlers called when... TODO
func (engine *Engine) NoMethod(handlers ...HandlerFunc) {
	engine.noMethod = handlers
	engine.rebuild405Handlers()
}

// liujia: 这里添加中间件，中间件middlewares对所有请求，无论api 404还是静态文件，都会被执行
// Use attachs a global middleware to the router. ie. the middleware attached though Use() will be
// included in the handlers chain for every single request. Even 404, 405, static files...
// For example, this is the right place for a logger or error management middleware.
func (engine *Engine) Use(middleware ...HandlerFunc) IRoutes {
	engine.RouterGroup.Use(middleware...)
	engine.rebuild404Handlers()
	engine.rebuild405Handlers()
	return engine
}

func (engine *Engine) rebuild404Handlers() {
	engine.allNoRoute = engine.combineHandlers(engine.noRoute)
}

func (engine *Engine) rebuild405Handlers() {
	engine.allNoMethod = engine.combineHandlers(engine.noMethod)
}

// liujia: 添加一个路由，node定义在tree.go中，这个方法可以重点看看，当然要看tree那块实现
// 感觉是一种method，例如GET，engine.trees中有一个节点对应，然后在这个tree上添加path和handler
func (engine *Engine) addRoute(method, path string, handlers HandlersChain) {
	assert1(path[0] == '/', "path must begin with '/'")
	assert1(len(method) > 0, "HTTP method can not be empty")
	assert1(len(handlers) > 0, "there must be at least one handler")

	debugPrintRoute(method, path, handlers)
	root := engine.trees.get(method)
	if root == nil {
		root = new(node)
		engine.trees = append(engine.trees, methodTree{method: method, root: root})
	}
	root.addRoute(path, handlers)
}

// Routes returns a slice of registered routes, including some useful information, such as:
// the http method, path and the handler name.
func (engine *Engine) Routes() (routes RoutesInfo) {
	for _, tree := range engine.trees {
		routes = iterate("", tree.method, routes, tree.root)
	}
	return routes
}

func iterate(path, method string, routes RoutesInfo, root *node) RoutesInfo {
	path += root.path
	if len(root.handlers) > 0 {
		routes = append(routes, RouteInfo{
			Method:  method,
			Path:    path,
			Handler: nameOfFunction(root.handlers.Last()),
		})
	}
	for _, child := range root.children {
		routes = iterate(path, method, routes, child)
	}
	return routes
}

// liujia: 启动http服务器，addr应该传空，传空默认监听":8080"，就是本地8080端口，否则传“127.0.0.1:8099”这种地址
// Run attaches the router to a http.Server and starts listening and serving HTTP requests.
// It is a shortcut for http.ListenAndServe(addr, router)
// Note: this method will block the calling goroutine indefinitely unless an error happens.
func (engine *Engine) Run(addr ...string) (err error) {
	defer func() { debugPrintError(err) }()

	address := resolveAddress(addr)
	debugPrint("Listening and serving HTTP on %s\n", address)
	err = http.ListenAndServe(address, engine)
	return
}

// RunTLS attaches the router to a http.Server and starts listening and serving HTTPS (secure) requests.
// It is a shortcut for http.ListenAndServeTLS(addr, certFile, keyFile, router)
// Note: this method will block the calling goroutine indefinitely unless an error happens.
func (engine *Engine) RunTLS(addr string, certFile string, keyFile string) (err error) {
	debugPrint("Listening and serving HTTPS on %s\n", addr)
	defer func() { debugPrintError(err) }()

	err = http.ListenAndServeTLS(addr, certFile, keyFile, engine)
	return
}

// RunUnix attaches the router to a http.Server and starts listening and serving HTTP requests
// through the specified unix socket (ie. a file).
// Note: this method will block the calling goroutine indefinitely unless an error happens.
func (engine *Engine) RunUnix(file string) (err error) {
	debugPrint("Listening and serving HTTP on unix:/%s", file)
	defer func() { debugPrintError(err) }()

	os.Remove(file)
	listener, err := net.Listen("unix", file)
	if err != nil {
		return
	}
	defer listener.Close()
	err = http.Serve(listener, engine)
	return
}

// liujia: 实现了http.Handler接口，即默认net.http的接口。即每次来一个新的HTTP请求，都现在走这个方法
// 做了如下事 1）分配Context(应该是对象池) 2）bind了w和req 3）处理  4）释放(交还)对象池Context对象
// Conforms to the http.Handler interface.
func (engine *Engine) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	c := engine.pool.Get().(*Context)
	c.writermem.reset(w)
	c.Request = req
	c.reset()

	engine.handleHTTPRequest(c)

	engine.pool.Put(c)
}

func (engine *Engine) handleHTTPRequest(context *Context) {
	httpMethod := context.Request.Method
	path := context.Request.URL.Path

	// Find root of the tree for the given HTTP method
	t := engine.trees

	//liujia: 这个遍历数组方法不错....但为毛不改成map？
	for i, tl := 0, len(t); i < tl; i++ {
		if t[i].method == httpMethod {
			root := t[i].root
			// Find route in tree
			handlers, params, tsr := root.getValue(path, context.Params)
			if handlers != nil {
				context.handlers = handlers
				context.Params = params
				context.Next()
				context.writermem.WriteHeaderNow()
				return

			} else if httpMethod != "CONNECT" && path != "/" {
				if tsr && engine.RedirectTrailingSlash {
					redirectTrailingSlash(context)
					return
				}
				if engine.RedirectFixedPath && redirectFixedPath(context, root, engine.RedirectFixedPath) {
					return
				}
			}
			break
		}
	}

	// TODO: unit test
	if engine.HandleMethodNotAllowed {
		for _, tree := range engine.trees {
			if tree.method != httpMethod {
				if handlers, _, _ := tree.root.getValue(path, nil); handlers != nil {
					context.handlers = engine.allNoMethod
					serveError(context, 405, default405Body)
					return
				}
			}
		}
	}
	context.handlers = engine.allNoRoute
	serveError(context, 404, default404Body)
}

var mimePlain = []string{MIMEPlain}

func serveError(c *Context, code int, defaultMessage []byte) {
	c.writermem.status = code
	c.Next()
	if !c.writermem.Written() {
		if c.writermem.Status() == code {
			c.writermem.Header()["Content-Type"] = mimePlain
			c.Writer.Write(defaultMessage)
		} else {
			c.writermem.WriteHeaderNow()
		}
	}
}

func redirectTrailingSlash(c *Context) {
	req := c.Request
	path := req.URL.Path
	code := 301 // Permanent redirect, request with GET method
	if req.Method != "GET" {
		code = 307
	}

	if len(path) > 1 && path[len(path)-1] == '/' {
		req.URL.Path = path[:len(path)-1]
	} else {
		req.URL.Path = path + "/"
	}
	debugPrint("redirecting request %d: %s --> %s", code, path, req.URL.String())
	http.Redirect(c.Writer, req, req.URL.String(), code)
	c.writermem.WriteHeaderNow()
}

func redirectFixedPath(c *Context, root *node, trailingSlash bool) bool {
	req := c.Request
	path := req.URL.Path

	fixedPath, found := root.findCaseInsensitivePath(
		cleanPath(path),
		trailingSlash,
	)
	if found {
		code := 301 // Permanent redirect, request with GET method
		if req.Method != "GET" {
			code = 307
		}
		req.URL.Path = string(fixedPath)
		debugPrint("redirecting request %d: %s --> %s", code, path, req.URL.String())
		http.Redirect(c.Writer, req, req.URL.String(), code)
		c.writermem.WriteHeaderNow()
		return true
	}
	return false
}
