// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package parser

import (
	"path/filepath"
	"testing"
)

// TestDefaultEngineParsePathJavaScriptKoaRouterRequireCallRouteEntries verifies
// that require('@koa/router')() — immediately-invoked require — is recognized as
// a Koa router base and its routes are collected (#4935).
func TestDefaultEngineParsePathJavaScriptKoaRouterRequireCallRouteEntries(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "server", "routes.js")
	writeTestFile(
		t,
		filePath,
		`const Koa = require('koa');
const router = require('@koa/router')();
const app = new Koa();
function list(ctx) {}
router.get('/', list);
app.use(router.routes());
`,
	)

	got := mustParsePath(t, repoRoot, filePath)

	assertFrameworksEqual(t, got, "koa")
	assertNestedRouteEntriesEqual(t, got, "koa", []map[string]string{
		{"method": "GET", "path": "/", "handler": "list"},
	})
}

// TestDefaultEngineParsePathJavaScriptKoaRouterRequireCallNoRouteForNonKoaFile
// verifies that require('express')() — a call expression invoking a non-Koa
// require — is NOT treated as a Koa router base, and its routes are never
// emitted as koa route_entries (#4935).
func TestDefaultEngineParsePathJavaScriptKoaRouterRequireCallNoRouteForNonKoaFile(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "src", "not-koa.js")
	writeTestFile(
		t,
		filePath,
		`const express = require('express')();
const app = express;
function home(req, res) {}
express.get('/y', home);
`,
	)

	got := mustParsePath(t, repoRoot, filePath)

	assertNoFrameworkOrNoRoutes(t, got, "koa")
}

// TestDefaultEngineParsePathJavaScriptKoaRouterRequireCallNegativeNoNonRouterRequire verifies
// that a .get() call on a variable NOT recognized as a Koa base (no @koa/router
// import) does NOT produce koa route_entries (#4935).
func TestDefaultEngineParsePathJavaScriptKoaRouterRequireCallNegativeNoNonRouterRequire(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "src", "no-koa.js")
	writeTestFile(
		t,
		filePath,
		`const something = require('something')();
something.get('/foo', handler);
function handler() {}
`,
	)

	got := mustParsePath(t, repoRoot, filePath)

	assertNoFrameworkOrNoRoutes(t, got, "koa")
}
