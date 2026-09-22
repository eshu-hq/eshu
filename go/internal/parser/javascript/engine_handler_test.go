// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package javascript_test

import (
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser"
)

// TestDefaultEngineParsePathJavaScriptHapiBindsNamedHandlerOnly proves that a
// bare named Hapi handler is captured while an inline arrow handler stays
// unbound, mirroring the correlation-truth contract used for Express (#2788).
func TestDefaultEngineParsePathJavaScriptHapiBindsNamedHandlerOnly(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "server", "hapi_routes.js")
	writeTestFile(
		t,
		filePath,
		`const Hapi = require("@hapi/hapi");

module.exports = function registerRoutes(server) {
  server.route([
    { method: "GET", path: "/named", handler: namedHandler },
    { method: "POST", path: "/inline", handler: (req, h) => h.response("ok") },
  ]);
};
`,
	)

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("parser.DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, parser.Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	assertFrameworksEqual(t, got, "hapi")
	assertNestedRouteEntriesEqual(t, got, "hapi", []map[string]string{
		{"method": "GET", "path": "/named", "handler": "namedHandler"},
		{"method": "POST", "path": "/inline"},
	})
}
