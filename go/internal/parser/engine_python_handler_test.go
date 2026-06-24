// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package parser

import (
	"path/filepath"
	"testing"
)

// TestDefaultEngineParsePathPythonFastAPIBindsDefHandler proves that the
// function defined after a FastAPI decorator is captured as the handler, even
// when extra decorators are stacked between the route decorator and the def,
// while a route decorator with no following def stays unbound (#2788).
func TestDefaultEngineParsePathPythonFastAPIBindsDefHandler(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "fastapi_handlers.py")
	writeTestFile(
		t,
		filePath,
		`from fastapi import FastAPI

app = FastAPI()

@app.get("/health")
@auth_required
async def read_health():
    return {"ok": True}

@app.post("/orphan")
x = 1

@app.put("/update")
def update_item():
    return {"ok": True}
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

	assertFrameworksEqual(t, got, "fastapi")
	assertNestedRouteEntriesEqual(t, got, "fastapi", []map[string]string{
		{"method": "GET", "path": "/health", "handler": "read_health"},
		{"method": "POST", "path": "/orphan"},
		{"method": "PUT", "path": "/update", "handler": "update_item"},
	})
}

// TestDefaultEngineParsePathPythonFastAPIBindsHandlerAfterComment proves that
// comment-only lines between a route decorator and its def do not make the
// route appear unbound.
func TestDefaultEngineParsePathPythonFastAPIBindsHandlerAfterComment(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "fastapi_comment_handlers.py")
	writeTestFile(
		t,
		filePath,
		`from fastapi import FastAPI

app = FastAPI()

@app.get("/health")
# Readiness probe for the service.
async def read_health():
    return {"ok": True}
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

	assertFrameworksEqual(t, got, "fastapi")
	assertNestedRouteEntriesEqual(t, got, "fastapi", []map[string]string{
		{"method": "GET", "path": "/health", "handler": "read_health"},
	})
}

// TestDefaultEngineParsePathPythonFlaskBindsDefHandler proves that the function
// defined after a Flask route decorator is captured as the handler, shared
// across each method the route declares (#2788).
func TestDefaultEngineParsePathPythonFlaskBindsDefHandler(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "flask_handlers.py")
	writeTestFile(
		t,
		filePath,
		`from flask import Flask

app = Flask(__name__)

@app.route("/health")
# Flask allows comments between a route decorator and the handler def.
def health():
    return "ok"

@app.route("/proxy", methods=["GET", "POST"])
def proxy():
    return "proxied"
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

	assertFrameworksEqual(t, got, "flask")
	assertNestedRouteEntriesEqual(t, got, "flask", []map[string]string{
		{"method": "GET", "path": "/health", "handler": "health"},
		{"method": "GET", "path": "/proxy", "handler": "proxy"},
		{"method": "POST", "path": "/proxy", "handler": "proxy"},
	})
}
