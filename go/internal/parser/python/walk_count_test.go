// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package python

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
	tree_sitter_python "github.com/tree-sitter/tree-sitter-python/bindings/go"
)

// TestWalkCountPythonReduction pins the exact number of shared.WalkNamed
// full-tree traversals Parse performs on a single file. Uses
// shared.SetWalkNamedHookForTest to count every call site — no manual
// annotation and no log-only comment. A future full-tree walk reintroduction
// (e.g. adding a per-framework walk back) will bump this count and fail.
//
// Not parallel: SetWalkNamedHookForTest installs a process-global hook.
func TestWalkCountPythonReduction(t *testing.T) {
	source := `from fastapi import FastAPI, APIRouter
from flask import Flask

app = FastAPI()
flask_app = Flask(__name__)
router = APIRouter(prefix="/api")

@router.get("/items")
def list_items():
    return []

@app.get("/health")
async def health():
    return {"ok": True}

@flask_app.route("/status")
def status():
    return "ok"
`

	dir := t.TempDir()
	path := filepath.Join(dir, "app.py")
	if err := os.WriteFile(path, []byte(source), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v, want nil", err)
	}

	parser := tree_sitter.NewParser()
	if err := parser.SetLanguage(tree_sitter.NewLanguage(tree_sitter_python.Language())); err != nil {
		t.Fatalf("SetLanguage(python) error = %v, want nil", err)
	}

	var walkNamedCalls int
	restore := shared.SetWalkNamedHookForTest(func() { walkNamedCalls++ })
	defer restore()

	payload, err := Parse(dir, path, false, shared.Options{}, parser)
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}

	fwSemantics, ok := payload["framework_semantics"].(map[string]any)
	if !ok {
		t.Fatal("payload[\"framework_semantics\"] not a map")
	}
	frameworks, ok := fwSemantics["frameworks"].([]string)
	if !ok || len(frameworks) == 0 {
		t.Fatal("expected at least one framework detected")
	}

	// The parse calls shared.WalkNamed for:
	//   - embeddedShellCommandPayloads (1 tree walk: walkNamed)
	//   - buildPythonPrimaryIndexes (1 tree walk: walkNamed)
	//   - the main declaration walk (1 tree walk: walkNamed)
	//   - pythonPublicAPIRootKinds (inside, calls walkNamed for module-level __all__)
	//     but its primary walk is shared with buildPythonPrimaryIndexes (already counted).
	//     The public-API __all__ walk is scoped per file.
	//   - gatherPythonFrameworkNodes (1 tree walk: walkNamed)
	//   - emitValueFlowBuckets may or may not call walkNamed depending on options
	//
	// Without EmitDataflow, the count is the main walk + primary-index walk +
	// gather walk + embedded-shell walk. Additional walks may come from
	// bounded subtree scans inside firstNamedDescendant and similar helpers.
	//
	// The critical invariant: this count does NOT include the ~20 per-framework
	// walks the old code added. If it ever goes up, a full-tree walk was
	// reintroduced.
	const wantWalkNamedCalls = 8
	if walkNamedCalls != wantWalkNamedCalls {
		t.Fatalf("shared.WalkNamed call count = %d, want %d (main walk + primary-index walk + gather walk + embedded-shell walk + bounded subtree scans; per-framework re-walks eliminated by gather-then-resolve #4922)", walkNamedCalls, wantWalkNamedCalls)
	}
}

// TestDjangoPathImportOnlyMatchesImportFrom asserts the Django path-import
// predicate matches ONLY import_from_statement shapes (from django.urls import
// path). An import_statement (import django.urls as path) must NOT produce
// false Django route entries. This guards the #4844 trap.
//
// Not parallel: SetWalkNamedHookForTest installs a process-global hook.
func TestDjangoPathImportOnlyMatchesImportFrom(t *testing.T) {
	t.Run("negative import_statement no false routes", func(t *testing.T) {
		source := `import django.urls as path

urlpatterns = [path("admin/", admin_view)]
`

		dir := t.TempDir()
		path := filepath.Join(dir, "urls.py")
		if err := os.WriteFile(path, []byte(source), 0o644); err != nil {
			t.Fatalf("WriteFile() error = %v", err)
		}

		parser := tree_sitter.NewParser()
		if err := parser.SetLanguage(tree_sitter.NewLanguage(tree_sitter_python.Language())); err != nil {
			t.Fatalf("SetLanguage(python) error = %v", err)
		}

		payload, err := Parse(dir, path, false, shared.Options{}, parser)
		if err != nil {
			t.Fatalf("Parse() error = %v", err)
		}

		fwSemantics, ok := payload["framework_semantics"].(map[string]any)
		if !ok {
			t.Fatal("framework_semantics not a map")
		}
		django, _ := fwSemantics["django"].(map[string]any)
		if django != nil {
			entries, _ := django["route_entries"].([]map[string]string)
			if len(entries) > 0 {
				t.Fatalf("expected 0 Django route_entries for import django.urls as path, got %d: %v", len(entries), entries)
			}
			t.Fatal("expected nil django semantics for import django.urls as path, but got a non-nil map")
		}
		// Also check with gathered path directly.
		sourceBytes := []byte(source)
		ptree := parser.Parse(sourceBytes, nil)
		if ptree == nil {
			t.Fatal("nil parse tree")
		}
		defer ptree.Close()
		old := detectPythonDjangoSemantics(ptree.RootNode(), sourceBytes)
		if old != nil {
			t.Fatalf("OLD detectPythonDjangoSemantics returned non-nil for import django.urls as path: %v", old)
		}
		gathered := gatherPythonFrameworkNodes(ptree.RootNode())
		newDjango := detectPythonDjangoSemanticsGathered(gathered, sourceBytes)
		if newDjango != nil {
			t.Fatalf("NEW detectPythonDjangoSemanticsGathered returned non-nil for import django.urls as path (BUG: g.imports includes import_statement): %v", newDjango)
		}
		t.Log("negative case PASS: import django.urls as path produces no Django routes in both OLD and NEW")
	})

	t.Run("positive import_from_statement produces routes", func(t *testing.T) {
		source := `from django.urls import path

def admin_view(request):
    return None

urlpatterns = [path("admin/", admin_view)]
`

		dir := t.TempDir()
		path := filepath.Join(dir, "urls.py")
		if err := os.WriteFile(path, []byte(source), 0o644); err != nil {
			t.Fatalf("WriteFile() error = %v", err)
		}

		parser := tree_sitter.NewParser()
		if err := parser.SetLanguage(tree_sitter.NewLanguage(tree_sitter_python.Language())); err != nil {
			t.Fatalf("SetLanguage(python) error = %v", err)
		}

		payload, err := Parse(dir, path, false, shared.Options{}, parser)
		if err != nil {
			t.Fatalf("Parse() error = %v", err)
		}

		fwSemantics, ok := payload["framework_semantics"].(map[string]any)
		if !ok {
			t.Fatal("framework_semantics not a map")
		}
		django, ok := fwSemantics["django"].(map[string]any)
		if !ok || django == nil {
			t.Fatal("expected django semantics for from django.urls import path, got nil")
		}
		entries, ok := django["route_entries"].([]map[string]string)
		if !ok || len(entries) == 0 {
			t.Fatalf("expected at least 1 Django route_entry, got %d", len(entries))
		}
		t.Logf("positive case PASS: from django.urls import path produces route_entries: %v", entries)
	})
}
