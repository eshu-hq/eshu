// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package python_test

import (
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser"
	"github.com/eshu-hq/eshu/go/internal/parser/parsertest"
)

func TestDefaultEngineParsePathPythonAioHTTPTornadoExactRouteEntries(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "routes.py")
	writeTestFile(
		t,
		filePath,
		`from aiohttp import web
import tornado.web

routes = web.RouteTableDef()

@routes.get("/aiohttp/widgets")
async def list_widgets(request):
    return web.json_response([])

class HealthHandler(tornado.web.RequestHandler):
    def get(self):
        self.write("ok")

application = tornado.web.Application([
    (r"/tornado/health", HealthHandler),
])
`,
	)

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, parser.Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	parsertest.AssertFrameworksEqual(t, got, "aiohttp", "tornado")
	parsertest.AssertNestedRouteEntriesEqual(t, got, "aiohttp", []map[string]string{
		{"method": "GET", "path": "/aiohttp/widgets", "handler": "list_widgets"},
	})
	parsertest.AssertNestedRouteEntriesEqual(t, got, "tornado", []map[string]string{
		{"method": "GET", "path": "/tornado/health", "handler": "HealthHandler.get"},
	})
}

func TestDefaultEngineParsePathPythonAioHTTPParamAppRoutes(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "routes.py")
	writeTestFile(
		t,
		filePath,
		`from aiohttp import web

async def index(request):
    return web.Response(text="ok")

async def poll(request):
    return web.json_response({})

def setup_routes(app):
    app.router.add_get('/', index)
    app.router.add_get('/poll/{question_id}', poll, name='poll')
    app.router.add_post('/poll/{question_id}/vote', poll)
`,
	)

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, parser.Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	parsertest.AssertFrameworksEqual(t, got, "aiohttp")
	parsertest.AssertNestedRouteEntriesEqual(t, got, "aiohttp", []map[string]string{
		{"method": "GET", "path": "/", "handler": "index"},
		{"method": "GET", "path": "/poll/{question_id}", "handler": "poll"},
		{"method": "POST", "path": "/poll/{question_id}/vote", "handler": "poll"},
	})
}
