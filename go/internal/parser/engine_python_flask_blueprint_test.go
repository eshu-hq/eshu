// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package parser

import (
	"path/filepath"
	"testing"
)

func TestDefaultEngineParsePathPythonFlaskBlueprintRouteEntries(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "flask_blueprint.py")
	writeTestFile(
		t,
		filePath,
		`from flask import Blueprint
from otherlib import NotAFlaskThing

blueprint = Blueprint('articles', __name__)

@blueprint.route('/api/articles', methods=['GET'])
def get_articles():
    return []

@blueprint.route('/api/articles/<slug>', methods=['PUT'])
def update_article(slug):
    return {}

other = NotAFlaskThing()

@other.route('/api/admin')
def admin():
    return "ok"
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
	assertNestedStringSliceEqual(t, got, "flask", "route_methods", []string{"GET", "PUT"})
	assertNestedStringSliceEqual(t, got, "flask", "route_paths", []string{"/api/articles", "/api/articles/<slug>"})
	assertNestedRouteEntriesEqual(t, got, "flask", []map[string]string{
		{"method": "GET", "path": "/api/articles", "handler": "get_articles"},
		{"method": "PUT", "path": "/api/articles/<slug>", "handler": "update_article"},
	})
	assertNestedStringSliceEqual(t, got, "flask", "server_symbols", []string{"blueprint"})
}
