// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/codeprovenance"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// goSourceFileEnvelope builds a parsed Go source file fact carrying one
// exported function for the cross-repo export-layout tests.
func goSourceFileEnvelope(repositoryID, relativePath, funcName, uid string) facts.Envelope {
	return facts.Envelope{
		FactKind: "file",
		Payload: map[string]any{
			"repo_id":       repositoryID,
			"relative_path": relativePath,
			"parsed_file_data": map[string]any{
				"path": relativePath,
				"lang": "go",
				"functions": []any{
					map[string]any{"name": funcName, "line_number": 3, "end_line": 5, "uid": uid},
				},
			},
		},
	}
}

// goCallerEnvelope builds a parsed Go caller file fact that imports importPath
// and makes one package-qualified call to qualifier.callName.
func goCallerEnvelope(repositoryID, relativePath, importPath, qualifier, callName string) facts.Envelope {
	return facts.Envelope{
		FactKind: "file",
		Payload: map[string]any{
			"repo_id":       repositoryID,
			"relative_path": relativePath,
			"parsed_file_data": map[string]any{
				"path": relativePath,
				"lang": "go",
				"functions": []any{
					map[string]any{"name": "main", "line_number": 5, "end_line": 7, "uid": "content-entity:caller-main"},
				},
				"imports": []any{
					map[string]any{"name": importPath, "lang": "go", "line_number": 3},
				},
				"function_calls": []any{
					map[string]any{
						"name":                     callName,
						"full_name":                qualifier + "." + callName,
						"receiver_identifier":      qualifier,
						"receiver_is_import_alias": true,
						"line_number":              6,
						"lang":                     "go",
					},
				},
			},
		},
	}
}

// TestExtractCodeCallRowsCrossRepoExportHonorsNestedModule proves a file under a
// nested go.mod resolves via the deepest containing module root, so its import
// path uses the nested module path rather than the repo-root module.
func TestExtractCodeCallRowsCrossRepoExportHonorsNestedModule(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		{FactKind: "repository", Payload: map[string]any{"repo_id": "repo-a"}},
		{FactKind: "repository", Payload: map[string]any{"repo_id": "repo-b"}},
		goModFileEnvelope("repo-a", "go.mod", "github.com/org/repoa"),
		goModFileEnvelope("repo-a", "services/api/go.mod", "github.com/org/api"),
		// Defined under the nested module: import path must be the nested module
		// path + dir relative to the nested root, not the repo-root module.
		goSourceFileEnvelope("repo-a", "services/api/internal/h.go", "Handler", "content-entity:nested-handler"),
		goCallerEnvelope("repo-b", "cmd/main.go", "github.com/org/api/internal", "internal", "Handler"),
	}

	_, rows := ExtractCodeCallRows(envelopes)
	if got := resolutionMethodForCallee(t, rows, "content-entity:nested-handler"); got != codeprovenance.MethodCrossRepoExportPackage {
		t.Fatalf("resolution_method = %q, want %q (rows=%#v)", got, codeprovenance.MethodCrossRepoExportPackage, rows)
	}
}

// TestExtractCodeCallRowsCrossRepoExportHonorsRepoRootModule proves a file at the
// module root directory derives its import path as the bare module path (empty
// relative directory), so a caller importing the module path resolves it.
func TestExtractCodeCallRowsCrossRepoExportHonorsRepoRootModule(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		{FactKind: "repository", Payload: map[string]any{"repo_id": "repo-a"}},
		{FactKind: "repository", Payload: map[string]any{"repo_id": "repo-b"}},
		goModFileEnvelope("repo-a", "go.mod", "github.com/org/repoa"),
		// Exported function in a file at the repo/module root → import path is the
		// bare module path.
		goSourceFileEnvelope("repo-a", "lib.go", "Run", "content-entity:root-run"),
		goCallerEnvelope("repo-b", "cmd/main.go", "github.com/org/repoa", "repoa", "Run"),
	}

	_, rows := ExtractCodeCallRows(envelopes)
	if got := resolutionMethodForCallee(t, rows, "content-entity:root-run"); got != codeprovenance.MethodCrossRepoExportPackage {
		t.Fatalf("resolution_method = %q, want %q (rows=%#v)", got, codeprovenance.MethodCrossRepoExportPackage, rows)
	}
}
