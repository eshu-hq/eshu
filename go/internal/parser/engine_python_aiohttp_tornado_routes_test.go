// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package parser

import (
	"path/filepath"
	"testing"
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

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	assertFrameworksEqual(t, got, "aiohttp", "tornado")
	assertNestedRouteEntriesEqual(t, got, "aiohttp", []map[string]string{
		{"method": "GET", "path": "/aiohttp/widgets", "handler": "list_widgets"},
	})
	assertNestedRouteEntriesEqual(t, got, "tornado", []map[string]string{
		{"method": "GET", "path": "/tornado/health", "handler": "HealthHandler.get"},
	})
}
