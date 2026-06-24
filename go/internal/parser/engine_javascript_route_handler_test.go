// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package parser

import (
	"path/filepath"
	"testing"
)

// TestDefaultEngineParsePathJavaScriptExpressCapturesNamedHandler proves the
// parser carries the Express route handler symbol (issue #2721) only when the
// binding is exact. A bare named callback resolves to its symbol; an inline
// callback or a middleware chain is ambiguous and must carry no handler so the
// reducer never fabricates a Function-[:HANDLES_ROUTE]->Endpoint binding.
func TestDefaultEngineParsePathJavaScriptExpressCapturesNamedHandler(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "server", "routes.js")
	writeTestFile(
		t,
		filePath,
		`const express = require("express");
const app = express();

app.get("/widgets", listWidgets);
app.get("/widgets/inline", (req, res) => res.end());
app.post("/widgets/guarded", requireAuth, createWidget);
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

	assertNestedRouteEntriesEqual(t, got, "express", []map[string]string{
		{"method": "GET", "path": "/widgets", "handler": "listWidgets"},
		{"method": "GET", "path": "/widgets/inline"},
		{"method": "POST", "path": "/widgets/guarded"},
	})
}

// TestDefaultEngineParsePathJavaScriptExpressDuplicateRouteStaysUnbound proves a
// route registered more than once (here an inline and a named callback for the
// same method+path) is ambiguous, so neither entry carries a handler symbol
// rather than leak the named symbol onto the inline registration (#2721).
func TestDefaultEngineParsePathJavaScriptExpressDuplicateRouteStaysUnbound(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "server", "routes.js")
	writeTestFile(
		t,
		filePath,
		`const express = require("express");
const app = express();

app.get("/widgets", (req, res) => res.end());
app.get("/widgets", listWidgets);
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

	assertNestedRouteEntriesEqual(t, got, "express", []map[string]string{
		{"method": "GET", "path": "/widgets"},
		{"method": "GET", "path": "/widgets"},
	})
}
