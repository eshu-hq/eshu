// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package shared

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// resolveIndexWithPathEntry builds an index whose cross-file unique-name
// table resolves name in path to entityID served from file.
func resolveIndexWithPathEntry(path, name, entityID, file string) EntityIndex {
	return EntityIndex{
		uniqueNameByPath: map[string]map[string]string{
			path: {name: entityID},
		},
		entityFileByID: map[string]string{entityID: file},
	}
}

// TestResolveImportedCrossFileCalleeDirect proves a JavaScript import
// binding resolves to the entity declared at the matched target path.
func TestResolveImportedCrossFileCalleeDirect(t *testing.T) {
	t.Parallel()

	index := resolveIndexWithPathEntry("/repo/src/server.js", "serve", "entity-1", "/repo/src/server.js")
	fileData := map[string]any{
		"imports": []any{
			map[string]any{"name": "serve", "source": "./server"},
		},
	}
	call := map[string]any{"name": "serve", "full_name": "serve", "lang": "javascript"}
	entityID, calleeFile := ResolveImportedCrossFileCallee(
		index,
		map[string][]string{"serve": {"/repo/src/server.js"}},
		ReexportIndex{},
		"repo",
		"/repo/src/a.js", "src/a.js", fileData, call,
	)
	if entityID != "entity-1" || calleeFile != "/repo/src/server.js" {
		t.Fatalf("ResolveImportedCrossFileCallee = (%q, %q), want (entity-1, /repo/src/server.js)", entityID, calleeFile)
	}
}

// TestResolveImportedCrossFileCalleeNegative proves unbound calls and
// unmatched import sources resolve to empty without inventing an edge.
func TestResolveImportedCrossFileCalleeNegative(t *testing.T) {
	t.Parallel()

	fileData := map[string]any{
		"imports": []any{
			map[string]any{"name": "serve", "source": "./server"},
		},
	}
	call := map[string]any{"name": "serve", "full_name": "serve", "lang": "javascript"}
	if id, file := ResolveImportedCrossFileCallee(
		EntityIndex{}, nil, ReexportIndex{}, "repo",
		"/repo/src/a.js", "src/a.js", map[string]any{}, call,
	); id != "" || file != "" {
		t.Fatalf("unbound call resolved to (%q, %q), want empty", id, file)
	}
	empty := resolveIndexWithPathEntry("/repo/src/other.js", "other", "entity-9", "/repo/src/other.js")
	if id, file := ResolveImportedCrossFileCallee(
		empty,
		map[string][]string{"serve": {"/repo/src/unrelated.js"}},
		ReexportIndex{}, "repo",
		"/repo/src/a.js", "src/a.js", fileData, call,
	); id != "" || file != "" {
		t.Fatalf("unmatched source resolved to (%q, %q), want empty", id, file)
	}
}

// TestResolveImportedCrossFileCalleePython proves a Python module import
// resolves through the repository-symbol path when exactly one file claims
// the module.
func TestResolveImportedCrossFileCalleePython(t *testing.T) {
	t.Parallel()

	index := resolveIndexWithPathEntry("/repo/pkg/mod.py", "serve", "entity-py", "/repo/pkg/mod.py")
	fileData := map[string]any{
		"imports": []any{
			map[string]any{"name": "serve", "source": "pkg.mod"},
		},
	}
	call := map[string]any{"name": "serve", "full_name": "serve", "lang": "python"}
	entityID, calleeFile := ResolveImportedCrossFileCallee(
		index,
		map[string][]string{"serve": {"/repo/pkg/mod.py"}},
		ReexportIndex{}, "repo",
		"/repo/src/a.py", "src/a.py", fileData, call,
	)
	if entityID != "entity-py" || calleeFile != "/repo/pkg/mod.py" {
		t.Fatalf("Python resolve = (%q, %q), want (entity-py, /repo/pkg/mod.py)", entityID, calleeFile)
	}
}

// reexportBarrelEnvelope builds a file fact for a barrel that re-exports
// exportedName from source.
func reexportBarrelEnvelope(repoID, rawPath, relativePath, exportedName, originalName, source string) facts.Envelope {
	return facts.Envelope{
		FactKind: "file",
		Payload: map[string]any{
			"repo_id":       repoID,
			"relative_path": relativePath,
			"parsed_file_data": map[string]any{
				"path": rawPath,
				"imports": []any{
					map[string]any{
						"import_type":   "reexport",
						"name":          exportedName,
						"original_name": originalName,
						"source":        source,
					},
				},
			},
		},
	}
}

// TestBuildReexportIndexSkipsNonReexports proves only relative re-export
// entries enter the index.
func TestBuildReexportIndexSkipsNonReexports(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		reexportBarrelEnvelope("repo", "/repo/src/index.js", "src/index.js", "serve", "start", "./server"),
		{
			FactKind: "file",
			Payload: map[string]any{
				"repo_id":       "repo",
				"relative_path": "src/plain.js",
				"parsed_file_data": map[string]any{
					"path":    "/repo/src/plain.js",
					"imports": []any{map[string]any{"import_type": "import", "name": "x", "source": "./x"}},
				},
			},
		},
		{
			FactKind: "file",
			Payload: map[string]any{
				"repo_id":       "repo",
				"relative_path": "src/bare.js",
				"parsed_file_data": map[string]any{
					"path":    "/repo/src/bare.js",
					"imports": []any{map[string]any{"import_type": "reexport", "name": "y", "source": "pkg"}},
				},
			},
		},
		{FactKind: "repository", Payload: map[string]any{"repo_id": "repo"}},
	}
	index := BuildReexportIndex(envelopes)
	if got := index.entries("repo", "/repo/src/index.js"); len(got) != 1 {
		t.Fatalf("barrel entries = %#v, want 1 entry", got)
	}
	if got := index.entries("repo", "/repo/src/plain.js"); len(got) != 0 {
		t.Fatalf("plain import entries = %#v, want none", got)
	}
	if got := index.entries("repo", "/repo/src/bare.js"); len(got) != 0 {
		t.Fatalf("bare-source re-export entries = %#v, want none", got)
	}
	if got := index.entries("  ", "/repo/src/index.js"); got != nil {
		t.Fatalf("blank repo entries = %#v, want nil", got)
	}
}

// TestResolveReexportedCrossFileCallee proves the caller -> barrel ->
// original-module hop resolves and star re-exports pass the name through.
func TestResolveReexportedCrossFileCallee(t *testing.T) {
	t.Parallel()

	t.Run("named_reexport", func(t *testing.T) {
		t.Parallel()
		reexports := BuildReexportIndex([]facts.Envelope{
			reexportBarrelEnvelope("repo", "/repo/src/index.js", "src/index.js", "serve", "start", "./server"),
		})
		index := resolveIndexWithPathEntry("/repo/src/server.js", "start", "entity-s", "/repo/src/server.js")
		fileData := map[string]any{
			"imports": []any{
				map[string]any{"name": "serve", "source": "./index"},
			},
		}
		call := map[string]any{"name": "serve", "full_name": "serve", "lang": "javascript"}
		entityID, calleeFile := ResolveImportedCrossFileCallee(
			index, nil, reexports, "repo",
			"/repo/src/a.js", "src/a.js", fileData, call,
		)
		if entityID != "entity-s" || calleeFile != "/repo/src/server.js" {
			t.Fatalf("re-export resolve = (%q, %q), want (entity-s, /repo/src/server.js)", entityID, calleeFile)
		}
	})

	t.Run("star_reexport", func(t *testing.T) {
		t.Parallel()
		reexports := BuildReexportIndex([]facts.Envelope{
			reexportBarrelEnvelope("repo", "/repo/src/index.js", "src/index.js", "*", "", "./server"),
		})
		index := resolveIndexWithPathEntry("/repo/src/server.js", "serve", "entity-s", "/repo/src/server.js")
		fileData := map[string]any{
			"imports": []any{
				map[string]any{"name": "serve", "source": "./index"},
			},
		}
		call := map[string]any{"name": "serve", "full_name": "serve", "lang": "javascript"}
		entityID, _ := ResolveImportedCrossFileCallee(
			index, nil, reexports, "repo",
			"/repo/src/a.js", "src/a.js", fileData, call,
		)
		if entityID != "entity-s" {
			t.Fatalf("star re-export resolve = %q, want entity-s", entityID)
		}
	})

	t.Run("name_mismatch_stays_unresolved", func(t *testing.T) {
		t.Parallel()
		reexports := BuildReexportIndex([]facts.Envelope{
			reexportBarrelEnvelope("repo", "/repo/src/index.js", "src/index.js", "other", "start", "./server"),
		})
		index := resolveIndexWithPathEntry("/repo/src/server.js", "start", "entity-s", "/repo/src/server.js")
		fileData := map[string]any{
			"imports": []any{
				map[string]any{"name": "serve", "source": "./index"},
			},
		}
		call := map[string]any{"name": "serve", "full_name": "serve", "lang": "javascript"}
		if id, _ := ResolveImportedCrossFileCallee(
			index, nil, reexports, "repo",
			"/repo/src/a.js", "src/a.js", fileData, call,
		); id != "" {
			t.Fatalf("mismatched re-export resolved to %q, want empty", id)
		}
	})
}

// TestCodeCallPythonImportSourceCanContainPath proves root mismatch,
// outside-root, blank-source, and module/init/submodule containment.
func TestCodeCallPythonImportSourceCanContainPath(t *testing.T) {
	t.Parallel()

	if codeCallPythonImportSourceCanContainPath("/other/a.py", "src/a.py", "pkg.mod", "/repo/pkg/mod.py") {
		t.Fatal("root mismatch reported containment, want false")
	}
	if codeCallPythonImportSourceCanContainPath("/repo/src/a.py", "src/a.py", "pkg.mod", "/other/pkg/mod.py") {
		t.Fatal("outside-root target reported containment, want false")
	}
	if codeCallPythonImportSourceCanContainPath("/repo/src/a.py", "src/a.py", "pkg.mod", "  ") {
		t.Fatal("blank target reported containment, want false")
	}
	if codeCallPythonImportSourceCanContainPath("/repo/src/a.py", "src/a.py", "...", "/repo/pkg/mod.py") {
		t.Fatal("dots-only source reported containment, want false")
	}
	if !codeCallPythonImportSourceCanContainPath("/repo/src/a.py", "src/a.py", "pkg.mod", "/repo/pkg/mod.py") {
		t.Fatal("module target missed containment, want true")
	}
	if !codeCallPythonImportSourceCanContainPath("/repo/a.py", "a.py", "pkg", "/repo/pkg/__init__.py") {
		t.Fatal("package-init target missed containment, want true")
	}
	if !codeCallPythonImportSourceCanContainPath("/repo/a.py", "a.py", "pkg", "/repo/pkg/sub/x.py") {
		t.Fatal("submodule target missed containment, want true")
	}
	if codeCallPythonImportSourceCanContainPath("/repo/a.py", "a.py", "pkg", "/repo/other/x.py") {
		t.Fatal("foreign-module target reported containment, want false")
	}
}

// TestResolvePythonImportedSourceCandidateTarget proves relative-import
// candidates resolve through the unique-name table.
func TestResolvePythonImportedSourceCandidateTarget(t *testing.T) {
	t.Parallel()

	index := resolveIndexWithPathEntry("/repo/pkg/helper.py", "helper_func", "entity-h", "/repo/pkg/helper.py")
	fileData := map[string]any{
		"imports": []any{
			map[string]any{"name": "helper_func", "source": "./helper"},
		},
	}
	call := map[string]any{"name": "helper_func", "full_name": "helper_func", "lang": "python"}
	entityID, calleeFile := ResolveImportedCrossFileCallee(
		index, map[string][]string{}, ReexportIndex{}, "repo",
		"/repo/pkg/main.py", "pkg/main.py", fileData, call,
	)
	if entityID != "entity-h" || calleeFile != "/repo/pkg/helper.py" {
		t.Fatalf("relative resolve = (%q, %q), want (entity-h, /repo/pkg/helper.py)", entityID, calleeFile)
	}
	if id, _ := ResolveImportedCrossFileCallee(
		EntityIndex{}, map[string][]string{}, ReexportIndex{}, "repo",
		"/repo/pkg/main.py", "pkg/main.py", fileData, call,
	); id != "" {
		t.Fatalf("unindexed relative resolve = %q, want empty", id)
	}
}

// TestReexportEntryOriginalNameFor pins the exported/star/bank name mapping.
func TestReexportEntryOriginalNameFor(t *testing.T) {
	t.Parallel()

	star := codeCallReexportEntry{exportedName: "*", source: "./server"}
	if name, ok := star.originalNameFor("serve"); !ok || name != "serve" {
		t.Fatalf("star originalNameFor = (%q, %v), want (serve, true)", name, ok)
	}
	named := codeCallReexportEntry{exportedName: "serve", originalName: "start"}
	if name, ok := named.originalNameFor("serve"); !ok || name != "start" {
		t.Fatalf("named originalNameFor = (%q, %v), want (start, true)", name, ok)
	}
	if _, ok := named.originalNameFor("other"); ok {
		t.Fatal("mismatched originalNameFor reported ok, want false")
	}
	if _, ok := named.originalNameFor("  "); ok {
		t.Fatal("blank originalNameFor reported ok, want false")
	}
}
