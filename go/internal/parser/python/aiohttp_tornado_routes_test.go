// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package python

import (
	"reflect"
	"testing"
)

func TestBuildPythonFrameworkSemanticsAioHTTPExactRoutes(t *testing.T) {
	root, source, closer := parsePythonForTest(t, `from aiohttp import web

routes = web.RouteTableDef()

async def health(request):
    return web.Response(text="ok")

@routes.get("/widgets")
async def list_widgets(request):
    return web.json_response([])

@routes.route("POST", "/widgets")
async def create_widget(request):
    return web.json_response({})

app = web.Application()
app.router.add_get("/health", health)
app.router.add_route("DELETE", "/widgets/{id}", delete_widget)
app.add_routes([web.patch("/widgets/{id}", update_widget)])

async def delete_widget(request):
    return web.Response(status=204)

async def update_widget(request):
    return web.json_response({})
`)
	defer closer()

	got := buildPythonFrameworkSemantics(root, source)
	frameworks, _ := got["frameworks"].([]string)
	if !reflect.DeepEqual(frameworks, []string{"aiohttp"}) {
		t.Fatalf("frameworks = %#v, want [aiohttp]", frameworks)
	}
	aiohttp, _ := got["aiohttp"].(map[string]any)
	if aiohttp == nil {
		t.Fatalf("aiohttp semantics missing: %#v", got)
	}
	assertStringSlice(t, aiohttp, "route_methods", []string{"GET", "POST", "DELETE", "PATCH"})
	assertStringSlice(t, aiohttp, "route_paths", []string{"/widgets", "/health", "/widgets/{id}"})
	assertRouteEntries(t, aiohttp, []map[string]string{
		{"method": "GET", "path": "/widgets", "handler": "list_widgets"},
		{"method": "POST", "path": "/widgets", "handler": "create_widget"},
		{"method": "GET", "path": "/health", "handler": "health"},
		{"method": "DELETE", "path": "/widgets/{id}", "handler": "delete_widget"},
		{"method": "PATCH", "path": "/widgets/{id}", "handler": "update_widget"},
	})
}

func TestBuildPythonFrameworkSemanticsAioHTTPDynamicRoutesStayUnclaimed(t *testing.T) {
	root, source, closer := parsePythonForTest(t, `from aiohttp import web

routes = web.RouteTableDef()
PATH = "/widgets"
method = "POST"
handler = dynamic_handler

@routes.get(PATH)
async def list_widgets(request):
    return web.json_response([])

@routes.route(method, "/widgets")
async def create_widget(request):
    return web.json_response({})

app = web.Application()
app.router.add_get("/health", handler)
app.router.add_route("DELETE", PATH, delete_widget)
app.add_routes(build_routes())
`)
	defer closer()

	got := buildPythonFrameworkSemantics(root, source)
	frameworks, _ := got["frameworks"].([]string)
	if len(frameworks) != 0 {
		t.Fatalf("frameworks = %#v, want empty for dynamic aiohttp routes", frameworks)
	}
}

func TestBuildPythonFrameworkSemanticsAioHTTPUnrelatedApplicationStayUnclaimed(t *testing.T) {
	root, source, closer := parsePythonForTest(t, `from aiohttp import web
from other.framework import Application

async def handler(request):
    return web.Response(text="ok")

app = Application()
app.router.add_get("/health", handler)
`)
	defer closer()

	got := buildPythonFrameworkSemantics(root, source)
	frameworks, _ := got["frameworks"].([]string)
	if len(frameworks) != 0 {
		t.Fatalf("frameworks = %#v, want empty for unrelated Application", frameworks)
	}
}

func TestBuildPythonFrameworkSemanticsAioHTTPAliasedWebRoutes(t *testing.T) {
	root, source, closer := parsePythonForTest(t, `from aiohttp import web as aiohttp_web

routes = aiohttp_web.RouteTableDef()

@routes.get("/aliased")
async def handler(request):
    return aiohttp_web.Response(text="ok")
`)
	defer closer()

	got := buildPythonFrameworkSemantics(root, source)
	frameworks, _ := got["frameworks"].([]string)
	if !reflect.DeepEqual(frameworks, []string{"aiohttp"}) {
		t.Fatalf("frameworks = %#v, want [aiohttp]", frameworks)
	}
	aiohttp, _ := got["aiohttp"].(map[string]any)
	if aiohttp == nil {
		t.Fatalf("aiohttp semantics missing: %#v", got)
	}
	assertRouteEntries(t, aiohttp, []map[string]string{
		{"method": "GET", "path": "/aliased", "handler": "handler"},
	})
}

func TestBuildPythonFrameworkSemanticsAioHTTPShadowedWebStayUnclaimed(t *testing.T) {
	root, source, closer := parsePythonForTest(t, `from aiohttp import web as aiohttp_web
from other.framework import web

async def handler(request):
    return aiohttp_web.Response(text="ok")

app = web.Application()
app.router.add_get("/shadowed", handler)
`)
	defer closer()

	got := buildPythonFrameworkSemantics(root, source)
	frameworks, _ := got["frameworks"].([]string)
	if len(frameworks) != 0 {
		t.Fatalf("frameworks = %#v, want empty for shadowed aiohttp web import", frameworks)
	}
}

func TestBuildPythonFrameworkSemanticsAioHTTPLocalAppRoutesStayUnclaimed(t *testing.T) {
	root, source, closer := parsePythonForTest(t, `from aiohttp import web

async def handler(request):
    return web.Response(text="ok")

def build_app():
    app = web.Application()
    return app

def install_routes(app):
    app.router.add_get("/shadowed", handler)
`)
	defer closer()

	got := buildPythonFrameworkSemantics(root, source)
	frameworks, _ := got["frameworks"].([]string)
	if len(frameworks) != 0 {
		t.Fatalf("frameworks = %#v, want empty for local/shadowed aiohttp app routes", frameworks)
	}
}

func TestBuildPythonFrameworkSemanticsTornadoExactURLSpecs(t *testing.T) {
	root, source, closer := parsePythonForTest(t, `import tornado.web
from tornado.web import URLSpec, url

class HealthHandler(tornado.web.RequestHandler):
    def get(self):
        self.write("ok")

class WidgetHandler(tornado.web.RequestHandler):
    def get(self, widget_id):
        self.write(widget_id)

    def post(self):
        self.write("created")

application = tornado.web.Application([
    (r"/health", HealthHandler),
    URLSpec(r"/widgets/(?P<widget_id>[^/]+)", WidgetHandler),
    url(r"/widgets", WidgetHandler, name="widgets"),
])
`)
	defer closer()

	got := buildPythonFrameworkSemantics(root, source)
	frameworks, _ := got["frameworks"].([]string)
	if !reflect.DeepEqual(frameworks, []string{"tornado"}) {
		t.Fatalf("frameworks = %#v, want [tornado]", frameworks)
	}
	tornado, _ := got["tornado"].(map[string]any)
	if tornado == nil {
		t.Fatalf("tornado semantics missing: %#v", got)
	}
	assertStringSlice(t, tornado, "route_methods", []string{"GET", "POST"})
	assertStringSlice(t, tornado, "route_paths", []string{"/health", "/widgets/(?P<widget_id>[^/]+)", "/widgets"})
	assertRouteEntries(t, tornado, []map[string]string{
		{"method": "GET", "path": "/health", "handler": "HealthHandler.get"},
		{"method": "GET", "path": "/widgets/(?P<widget_id>[^/]+)", "handler": "WidgetHandler.get"},
		{"method": "POST", "path": "/widgets/(?P<widget_id>[^/]+)", "handler": "WidgetHandler.post"},
		{"method": "GET", "path": "/widgets", "handler": "WidgetHandler.get"},
		{"method": "POST", "path": "/widgets", "handler": "WidgetHandler.post"},
	})
}

func TestBuildPythonFrameworkSemanticsTornadoAliasedApplication(t *testing.T) {
	root, source, closer := parsePythonForTest(t, `import tornado.web
from tornado.web import Application as TornadoApplication

class HealthHandler(tornado.web.RequestHandler):
    def get(self):
        self.write("ok")

application = TornadoApplication([
    (r"/health", HealthHandler),
])
`)
	defer closer()

	got := buildPythonFrameworkSemantics(root, source)
	tornado, _ := got["tornado"].(map[string]any)
	if tornado == nil {
		t.Fatalf("tornado semantics missing: %#v", got)
	}
	assertRouteEntries(t, tornado, []map[string]string{
		{"method": "GET", "path": "/health", "handler": "HealthHandler.get"},
	})
}

func TestBuildPythonFrameworkSemanticsTornadoShadowedApplicationStayUnclaimed(t *testing.T) {
	root, source, closer := parsePythonForTest(t, `import tornado.web
from tornado.web import Application as TornadoApplication
from other.framework import Application

class HealthHandler(tornado.web.RequestHandler):
    def get(self):
        self.write("ok")

application = Application([
    (r"/health", HealthHandler),
])
`)
	defer closer()

	got := buildPythonFrameworkSemantics(root, source)
	frameworks, _ := got["frameworks"].([]string)
	if len(frameworks) != 0 {
		t.Fatalf("frameworks = %#v, want empty for shadowed Tornado Application import", frameworks)
	}
}

func TestBuildPythonFrameworkSemanticsTornadoAnchoredRegexPath(t *testing.T) {
	root, source, closer := parsePythonForTest(t, `import tornado.web

class HealthHandler(tornado.web.RequestHandler):
    def get(self):
        self.write("ok")

application = tornado.web.Application([
    (r"^/health$", HealthHandler),
])
`)
	defer closer()

	got := buildPythonFrameworkSemantics(root, source)
	tornado, _ := got["tornado"].(map[string]any)
	if tornado == nil {
		t.Fatalf("tornado semantics missing: %#v", got)
	}
	assertStringSlice(t, tornado, "route_paths", []string{"^/health$"})
	assertRouteEntries(t, tornado, []map[string]string{
		{"method": "GET", "path": "^/health$", "handler": "HealthHandler.get"},
	})
}

func TestBuildPythonFrameworkSemanticsTornadoFactoryRoutesStayUnclaimed(t *testing.T) {
	root, source, closer := parsePythonForTest(t, `import tornado.web

class HealthHandler(tornado.web.RequestHandler):
    def get(self):
        self.write("ok")

def make_application():
    return tornado.web.Application([
        (r"/health", HealthHandler),
    ])
`)
	defer closer()

	got := buildPythonFrameworkSemantics(root, source)
	frameworks, _ := got["frameworks"].([]string)
	if len(frameworks) != 0 {
		t.Fatalf("frameworks = %#v, want empty for Tornado app factory routes", frameworks)
	}
}

func TestBuildPythonFrameworkSemanticsTornadoDynamicRoutesStayUnclaimed(t *testing.T) {
	root, source, closer := parsePythonForTest(t, `import tornado.web

ROUTE = r"/health"

class HealthHandler(tornado.web.RequestHandler):
    def get(self):
        self.write("ok")

application = tornado.web.Application([
    (ROUTE, HealthHandler),
    (r"/imported", handlers.ImportedHandler),
    get_runtime_route(),
])
`)
	defer closer()

	got := buildPythonFrameworkSemantics(root, source)
	frameworks, _ := got["frameworks"].([]string)
	if len(frameworks) != 0 {
		t.Fatalf("frameworks = %#v, want empty for dynamic/imported Tornado routes", frameworks)
	}
}

func TestBuildPythonFrameworkSemanticsTornadoUnrelatedApplicationStayUnclaimed(t *testing.T) {
	root, source, closer := parsePythonForTest(t, `import tornado.web
from other.framework import Application

class HealthHandler(tornado.web.RequestHandler):
    def get(self):
        self.write("ok")

application = Application([
    (r"/health", HealthHandler),
])
`)
	defer closer()

	got := buildPythonFrameworkSemantics(root, source)
	frameworks, _ := got["frameworks"].([]string)
	if len(frameworks) != 0 {
		t.Fatalf("frameworks = %#v, want empty for unrelated Application", frameworks)
	}
}
