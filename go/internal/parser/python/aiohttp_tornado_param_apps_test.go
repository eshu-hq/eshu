// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package python

import (
	"reflect"
	"testing"
)

func TestBuildPythonFrameworkSemanticsAioHTTPParamAppRoutes(t *testing.T) {
	root, source, closer := parsePythonForTest(t, `from aiohttp import web

async def index(request):
    return web.Response(text="ok")

async def poll(request):
    return web.json_response({})

async def vote(request):
    return web.json_response({})

def setup_routes(app):
    app.router.add_get('/', index)
    app.router.add_get('/poll/{question_id}', poll, name='poll')
    app.router.add_post('/poll/{question_id}/vote', vote)

async def clear_polls(request):
    return web.Response(status=204)

def setup_admin(app):
    app.router.add_route('DELETE', '/admin/clear', clear_polls)
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
	assertStringSlice(t, aiohttp, "route_methods", []string{"GET", "POST", "DELETE"})
	assertStringSlice(t, aiohttp, "route_paths", []string{"/", "/poll/{question_id}", "/poll/{question_id}/vote", "/admin/clear"})
	assertRouteEntries(t, aiohttp, []map[string]string{
		{"method": "GET", "path": "/", "handler": "index"},
		{"method": "GET", "path": "/poll/{question_id}", "handler": "poll"},
		{"method": "POST", "path": "/poll/{question_id}/vote", "handler": "vote"},
		{"method": "DELETE", "path": "/admin/clear", "handler": "clear_polls"},
	})
}

func TestBuildPythonFrameworkSemanticsAioHTTPParamAppRoutesWithAddRoutes(t *testing.T) {
	root, source, closer := parsePythonForTest(t, `from aiohttp import web

async def health(request):
    return web.Response(text="ok")

async def handler(request):
    return web.json_response({})

def setup_routes(app):
    app.add_routes([web.get('/health', health)])
    app.router.add_get('/api/items', handler)
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
		{"method": "GET", "path": "/health", "handler": "health"},
		{"method": "GET", "path": "/api/items", "handler": "handler"},
	})
}

func TestBuildPythonFrameworkSemanticsAioHTTPParamAppNegative_noRouterCall(t *testing.T) {
	root, source, closer := parsePythonForTest(t, `from aiohttp import web

async def handler(request):
    return web.Response(text="ok")

def helper(app):
    app.foo()
`)
	defer closer()

	got := buildPythonFrameworkSemantics(root, source)
	frameworks, _ := got["frameworks"].([]string)
	if len(frameworks) != 0 {
		t.Fatalf("frameworks = %#v, want empty for non-router param usage", frameworks)
	}
}

func TestBuildPythonFrameworkSemanticsAioHTTPParamAppNegative_nonRouterCall(t *testing.T) {
	root, source, closer := parsePythonForTest(t, `from aiohttp import web

async def handler(request):
    return web.Response(text="ok")

def setup_routes(app):
    app.foo('/fake', handler)
`)
	defer closer()

	got := buildPythonFrameworkSemantics(root, source)
	frameworks, _ := got["frameworks"].([]string)
	if len(frameworks) != 0 {
		t.Fatalf("frameworks = %#v, want empty for non-add_get/add_route param call", frameworks)
	}
}

func TestBuildPythonFrameworkSemanticsAioHTTPModuleLevelStillWorks(t *testing.T) {
	root, source, closer := parsePythonForTest(t, `from aiohttp import web

async def health(request):
    return web.Response(text="ok")

app = web.Application()
app.router.add_get('/health', health)
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
		{"method": "GET", "path": "/health", "handler": "health"},
	})
}

func TestBuildPythonFrameworkSemanticsAioHTTPParamAppLocalVariableHandlerOmitted(t *testing.T) {
	root, source, closer := parsePythonForTest(t, `from aiohttp import web

def setup_routes(app):
    handler = make_handler()
    app.router.add_get('/x', handler)
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
	entries, _ := aiohttp["route_entries"].([]map[string]string)
	if len(entries) != 1 {
		t.Fatalf("route_entries len = %d, want 1", len(entries))
	}
	if entries[0]["method"] != "GET" || entries[0]["path"] != "/x" {
		t.Fatalf("route = %v, want method=GET path=/x (no handler)", entries[0])
	}
	if _, hasHandler := entries[0]["handler"]; hasHandler {
		t.Fatalf("route has handler=%q, want no handler field (local variable)", entries[0]["handler"])
	}
}

func TestBuildPythonFrameworkSemanticsAioHTTPParamAppImportedHandlerEmitted(t *testing.T) {
	root, source, closer := parsePythonForTest(t, `from aiohttp import web
from views import index

def setup_routes(app):
    app.router.add_get('/x', index)
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
		{"method": "GET", "path": "/x", "handler": "index"},
	})
}

func TestBuildPythonFrameworkSemanticsAioHTTPParamAppModuleLevelFuncHandlerEmitted(t *testing.T) {
	root, source, closer := parsePythonForTest(t, `from aiohttp import web

async def my_handler(request):
    return web.Response(text="ok")

def setup_routes(app):
    app.router.add_get('/x', my_handler)
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
		{"method": "GET", "path": "/x", "handler": "my_handler"},
	})
}
