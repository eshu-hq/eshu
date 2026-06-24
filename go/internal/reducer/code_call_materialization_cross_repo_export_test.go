// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/codeprovenance"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// goModFileEnvelope builds a parsed go.mod file fact for repositoryID whose
// module declaration path is modulePath, rooted at relativePath. The cross-repo
// export index uses this module declaration to anchor exported package import
// paths to their defining repository.
func goModFileEnvelope(repositoryID, relativePath, modulePath string) facts.Envelope {
	return facts.Envelope{
		FactKind: "file",
		Payload: map[string]any{
			"repo_id":       repositoryID,
			"relative_path": relativePath,
			"parsed_file_data": map[string]any{
				"path": relativePath,
				"lang": "gomod",
				"gomod_state": map[string]any{
					"state":       "parsed",
					"module_path": modulePath,
				},
				"variables": []any{
					map[string]any{
						"name":        modulePath,
						"value":       modulePath,
						"config_kind": "module_declaration",
						"section":     "module",
						"lang":        "gomod",
					},
				},
			},
		},
	}
}

// TestExtractCodeCallRowsResolvesCrossRepoExportedGoFunction proves a Go
// package-qualified call in repo B to a function EXPORTED by repo A resolves to
// repo A's function entity with the cross_repo_export_package method when the
// import path is exact and the export name is unique across the generation.
func TestExtractCodeCallRowsResolvesCrossRepoExportedGoFunction(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		{FactKind: "repository", Payload: map[string]any{"repo_id": "repo-a"}},
		{FactKind: "repository", Payload: map[string]any{"repo_id": "repo-b"}},
		goModFileEnvelope("repo-a", "go.mod", "github.com/org/repoa"),
		{FactKind: "file", Payload: map[string]any{
			"repo_id":       "repo-a",
			"relative_path": "pkg/process.go",
			"parsed_file_data": map[string]any{
				"path": "pkg/process.go",
				"functions": []any{
					map[string]any{"name": "Process", "line_number": 3, "end_line": 5, "uid": "content-entity:repoa-process"},
				},
			},
		}},
		{FactKind: "file", Payload: map[string]any{
			"repo_id":       "repo-b",
			"relative_path": "cmd/main.go",
			"parsed_file_data": map[string]any{
				"path": "cmd/main.go",
				"functions": []any{
					map[string]any{"name": "main", "line_number": 5, "end_line": 7, "uid": "content-entity:repob-main"},
				},
				"imports": []any{
					map[string]any{"name": "github.com/org/repoa/pkg", "lang": "go", "line_number": 3},
				},
				"function_calls": []any{
					map[string]any{
						"name":                     "Process",
						"full_name":                "pkg.Process",
						"receiver_identifier":      "pkg",
						"receiver_is_import_alias": true,
						"line_number":              6,
						"lang":                     "go",
					},
				},
			},
		}},
	}

	_, rows := ExtractCodeCallRows(envelopes)
	if got := resolutionMethodForCallee(t, rows, "content-entity:repoa-process"); got != codeprovenance.MethodCrossRepoExportPackage {
		t.Fatalf("resolution_method = %q, want %q (rows=%#v)", got, codeprovenance.MethodCrossRepoExportPackage, rows)
	}
	for _, row := range rows {
		if anyToString(row["callee_entity_id"]) == "content-entity:repoa-process" {
			if got, want := anyToString(row["caller_entity_id"]), "content-entity:repob-main"; got != want {
				t.Fatalf("caller_entity_id = %q, want %q", got, want)
			}
			if got, want := anyToString(row["callee_file"]), "pkg/process.go"; got != want {
				t.Fatalf("callee_file = %q, want %q", got, want)
			}
		}
	}
}

// TestExtractCodeCallRowsPrefersNativeGoSCIPSymbolForCrossRepoCall proves the
// native Go parser's stable SCIP-style symbols promote exact Go-to-Go
// cross-repo calls above the weaker package-export fallback.
func TestExtractCodeCallRowsPrefersNativeGoSCIPSymbolForCrossRepoCall(t *testing.T) {
	t.Parallel()

	scipSymbol := "scip-go gomod github.com/org/repoa/pkg Process()."
	envelopes := []facts.Envelope{
		{FactKind: "repository", Payload: map[string]any{"repo_id": "repo-a"}},
		{FactKind: "repository", Payload: map[string]any{"repo_id": "repo-b"}},
		goModFileEnvelope("repo-a", "go.mod", "github.com/org/repoa"),
		{FactKind: "file", Payload: map[string]any{
			"repo_id":       "repo-a",
			"relative_path": "pkg/process.go",
			"parsed_file_data": map[string]any{
				"path": "pkg/process.go",
				"functions": []any{
					map[string]any{
						"name":                "Process",
						"line_number":         3,
						"end_line":            5,
						"uid":                 "content-entity:repoa-process",
						"package_import_path": "github.com/org/repoa/pkg",
						"scip_symbol":         scipSymbol,
					},
				},
			},
		}},
		{FactKind: "file", Payload: map[string]any{
			"repo_id":       "repo-b",
			"relative_path": "cmd/main.go",
			"parsed_file_data": map[string]any{
				"path": "cmd/main.go",
				"functions": []any{
					map[string]any{"name": "main", "line_number": 5, "end_line": 7, "uid": "content-entity:repob-main"},
				},
				"imports": []any{
					map[string]any{"name": "github.com/org/repoa/pkg", "lang": "go", "line_number": 3},
				},
				"function_calls": []any{
					map[string]any{
						"name":                     "Process",
						"full_name":                "pkg.Process",
						"receiver_identifier":      "pkg",
						"receiver_is_import_alias": true,
						"stable_symbol_key":        scipSymbol,
						"line_number":              6,
						"lang":                     "go",
					},
				},
			},
		}},
	}

	_, rows := ExtractCodeCallRows(envelopes)
	if got := resolutionMethodForCallee(t, rows, "content-entity:repoa-process"); got != codeprovenance.MethodSCIP {
		t.Fatalf("resolution_method = %q, want %q (rows=%#v)", got, codeprovenance.MethodSCIP, rows)
	}
}

// TestExtractCodeCallRowsLeavesCrossRepoUnexportedGoFunctionUnresolved proves an
// unexported (lowercase) function is not part of the cross-repo export surface,
// so a call to it from another repo produces no cross-repo row.
func TestExtractCodeCallRowsLeavesCrossRepoUnexportedGoFunctionUnresolved(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		{FactKind: "repository", Payload: map[string]any{"repo_id": "repo-a"}},
		{FactKind: "repository", Payload: map[string]any{"repo_id": "repo-b"}},
		goModFileEnvelope("repo-a", "go.mod", "github.com/org/repoa"),
		{FactKind: "file", Payload: map[string]any{
			"repo_id":       "repo-a",
			"relative_path": "pkg/process.go",
			"parsed_file_data": map[string]any{
				"path": "pkg/process.go",
				"functions": []any{
					map[string]any{"name": "process", "line_number": 3, "end_line": 5, "uid": "content-entity:repoa-process-unexported"},
				},
			},
		}},
		{FactKind: "file", Payload: map[string]any{
			"repo_id":       "repo-b",
			"relative_path": "cmd/main.go",
			"parsed_file_data": map[string]any{
				"path": "cmd/main.go",
				"functions": []any{
					map[string]any{"name": "main", "line_number": 5, "end_line": 7, "uid": "content-entity:repob-main"},
				},
				"imports": []any{
					map[string]any{"name": "github.com/org/repoa/pkg", "lang": "go", "line_number": 3},
				},
				"function_calls": []any{
					map[string]any{
						"name":                     "process",
						"full_name":                "pkg.process",
						"receiver_identifier":      "pkg",
						"receiver_is_import_alias": true,
						"line_number":              6,
						"lang":                     "go",
					},
				},
			},
		}},
	}

	_, rows := ExtractCodeCallRows(envelopes)
	for _, row := range rows {
		if anyToString(row["callee_entity_id"]) == "content-entity:repoa-process-unexported" {
			t.Fatalf("unexported cross-repo function resolved (must stay unresolved); row=%#v", row)
		}
	}
}

// TestExtractCodeCallRowsLeavesAmbiguousCrossRepoExportUnresolved proves that
// when two repositories both export a function with the same name for the same
// import path, the call stays unresolved rather than picking arbitrarily.
func TestExtractCodeCallRowsLeavesAmbiguousCrossRepoExportUnresolved(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		{FactKind: "repository", Payload: map[string]any{"repo_id": "repo-a1"}},
		{FactKind: "repository", Payload: map[string]any{"repo_id": "repo-a2"}},
		{FactKind: "repository", Payload: map[string]any{"repo_id": "repo-b"}},
		goModFileEnvelope("repo-a1", "go.mod", "github.com/org/repoa"),
		goModFileEnvelope("repo-a2", "go.mod", "github.com/org/repoa"),
		{FactKind: "file", Payload: map[string]any{
			"repo_id":       "repo-a1",
			"relative_path": "pkg/process.go",
			"parsed_file_data": map[string]any{
				"path": "pkg/process.go",
				"functions": []any{
					map[string]any{"name": "Process", "line_number": 3, "end_line": 5, "uid": "content-entity:repoa1-process"},
				},
			},
		}},
		{FactKind: "file", Payload: map[string]any{
			"repo_id":       "repo-a2",
			"relative_path": "pkg/process.go",
			"parsed_file_data": map[string]any{
				"path": "pkg/process.go",
				"functions": []any{
					map[string]any{"name": "Process", "line_number": 3, "end_line": 5, "uid": "content-entity:repoa2-process"},
				},
			},
		}},
		{FactKind: "file", Payload: map[string]any{
			"repo_id":       "repo-b",
			"relative_path": "cmd/main.go",
			"parsed_file_data": map[string]any{
				"path": "cmd/main.go",
				"functions": []any{
					map[string]any{"name": "main", "line_number": 5, "end_line": 7, "uid": "content-entity:repob-main"},
				},
				"imports": []any{
					map[string]any{"name": "github.com/org/repoa/pkg", "lang": "go", "line_number": 3},
				},
				"function_calls": []any{
					map[string]any{
						"name":                     "Process",
						"full_name":                "pkg.Process",
						"receiver_identifier":      "pkg",
						"receiver_is_import_alias": true,
						"line_number":              6,
						"lang":                     "go",
					},
				},
			},
		}},
	}

	_, rows := ExtractCodeCallRows(envelopes)
	for _, row := range rows {
		callee := anyToString(row["callee_entity_id"])
		if callee == "content-entity:repoa1-process" || callee == "content-entity:repoa2-process" {
			t.Fatalf("ambiguous cross-repo export resolved (must stay unresolved); row=%#v", row)
		}
	}
}

// TestExtractCodeCallRowsPrefersSameRepoOverCrossRepoExport proves a same-repo
// definition still resolves through the existing same-repo branches, so the
// cross-repo export branch never shadows stronger local resolution.
func TestExtractCodeCallRowsPrefersSameRepoOverCrossRepoExport(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		{FactKind: "repository", Payload: map[string]any{"repo_id": "repo-a"}},
		{FactKind: "repository", Payload: map[string]any{"repo_id": "repo-b"}},
		// repo-a exports Process for github.com/org/repoa/pkg.
		goModFileEnvelope("repo-a", "go.mod", "github.com/org/repoa"),
		{FactKind: "file", Payload: map[string]any{
			"repo_id":       "repo-a",
			"relative_path": "pkg/process.go",
			"parsed_file_data": map[string]any{
				"path": "pkg/process.go",
				"functions": []any{
					map[string]any{"name": "Process", "line_number": 3, "end_line": 5, "uid": "content-entity:repoa-process"},
				},
			},
		}},
		// repo-b also defines Process locally in the same package and calls it
		// bare; the same-repo directory branch must win.
		{FactKind: "file", Payload: map[string]any{
			"repo_id":       "repo-b",
			"relative_path": "pkg/local.go",
			"parsed_file_data": map[string]any{
				"path": "pkg/local.go",
				"functions": []any{
					map[string]any{"name": "Process", "line_number": 10, "end_line": 12, "uid": "content-entity:repob-process"},
				},
			},
		}},
		{FactKind: "file", Payload: map[string]any{
			"repo_id":       "repo-b",
			"relative_path": "pkg/main.go",
			"parsed_file_data": map[string]any{
				"path": "pkg/main.go",
				"functions": []any{
					map[string]any{"name": "main", "line_number": 5, "end_line": 7, "uid": "content-entity:repob-main"},
				},
				"function_calls": []any{
					map[string]any{"name": "Process", "full_name": "Process", "line_number": 6, "lang": "go"},
				},
			},
		}},
	}

	_, rows := ExtractCodeCallRows(envelopes)
	method := resolutionMethodForCallee(t, rows, "content-entity:repob-process")
	if method == codeprovenance.MethodCrossRepoExportPackage {
		t.Fatalf("same-repo call resolved via cross-repo export branch; want a same-repo method, got %q", method)
	}
	for _, row := range rows {
		if anyToString(row["callee_entity_id"]) == "content-entity:repoa-process" {
			t.Fatalf("same-repo bare call leaked to cross-repo repo-a entity; row=%#v", row)
		}
	}
}

// TestExtractCodeCallRowsCrossRepoExportRequiresExactImportPath proves the join
// is exact: a caller importing a different package path than the one the
// exported function actually defines does not resolve, even when the function
// name matches. This is the false-positive guard for the accuracy contract.
func TestExtractCodeCallRowsCrossRepoExportRequiresExactImportPath(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		{FactKind: "repository", Payload: map[string]any{"repo_id": "repo-a"}},
		{FactKind: "repository", Payload: map[string]any{"repo_id": "repo-b"}},
		// repo-a exports Process for import path github.com/org/repoa/pkg.
		goModFileEnvelope("repo-a", "go.mod", "github.com/org/repoa"),
		{FactKind: "file", Payload: map[string]any{
			"repo_id":       "repo-a",
			"relative_path": "pkg/process.go",
			"parsed_file_data": map[string]any{
				"path": "pkg/process.go",
				"functions": []any{
					map[string]any{"name": "Process", "line_number": 3, "end_line": 5, "uid": "content-entity:repoa-process"},
				},
			},
		}},
		// repo-b imports a DIFFERENT module path (github.com/org/other/pkg) for
		// the same selector; the import path does not join, so no cross-repo row.
		{FactKind: "file", Payload: map[string]any{
			"repo_id":       "repo-b",
			"relative_path": "cmd/main.go",
			"parsed_file_data": map[string]any{
				"path": "cmd/main.go",
				"functions": []any{
					map[string]any{"name": "main", "line_number": 5, "end_line": 7, "uid": "content-entity:repob-main"},
				},
				"imports": []any{
					map[string]any{"name": "github.com/org/other/pkg", "lang": "go", "line_number": 3},
				},
				"function_calls": []any{
					map[string]any{
						"name":                     "Process",
						"full_name":                "pkg.Process",
						"receiver_identifier":      "pkg",
						"receiver_is_import_alias": true,
						"line_number":              6,
						"lang":                     "go",
					},
				},
			},
		}},
	}

	_, rows := ExtractCodeCallRows(envelopes)
	for _, row := range rows {
		if anyToString(row["callee_entity_id"]) == "content-entity:repoa-process" {
			t.Fatalf("cross-repo export resolved on a non-matching import path; row=%#v", row)
		}
	}
}

// TestExtractCodeCallRowsCrossRepoExportIsReprojectionStable proves the resolved
// cross-repo entity id is identical across two extraction passes over the same
// facts, because the durable function uid is generation-independent.
func TestExtractCodeCallRowsCrossRepoExportIsReprojectionStable(t *testing.T) {
	t.Parallel()

	build := func() []facts.Envelope {
		return []facts.Envelope{
			{FactKind: "repository", Payload: map[string]any{"repo_id": "repo-a"}},
			{FactKind: "repository", Payload: map[string]any{"repo_id": "repo-b"}},
			goModFileEnvelope("repo-a", "go.mod", "github.com/org/repoa"),
			{FactKind: "file", Payload: map[string]any{
				"repo_id":       "repo-a",
				"relative_path": "pkg/process.go",
				"parsed_file_data": map[string]any{
					"path": "pkg/process.go",
					"functions": []any{
						map[string]any{"name": "Process", "line_number": 3, "end_line": 5, "uid": "content-entity:repoa-process"},
					},
				},
			}},
			{FactKind: "file", Payload: map[string]any{
				"repo_id":       "repo-b",
				"relative_path": "cmd/main.go",
				"parsed_file_data": map[string]any{
					"path": "cmd/main.go",
					"functions": []any{
						map[string]any{"name": "main", "line_number": 5, "end_line": 7, "uid": "content-entity:repob-main"},
					},
					"imports": []any{
						map[string]any{"name": "github.com/org/repoa/pkg", "lang": "go", "line_number": 3},
					},
					"function_calls": []any{
						map[string]any{
							"name":                     "Process",
							"full_name":                "pkg.Process",
							"receiver_identifier":      "pkg",
							"receiver_is_import_alias": true,
							"line_number":              6,
							"lang":                     "go",
						},
					},
				},
			}},
		}
	}

	calleeFor := func(rows []map[string]any) string {
		for _, row := range rows {
			if anyToString(row["caller_entity_id"]) == "content-entity:repob-main" &&
				anyToString(row["resolution_method"]) == codeprovenance.MethodCrossRepoExportPackage {
				return anyToString(row["callee_entity_id"])
			}
		}
		return ""
	}

	_, firstRows := ExtractCodeCallRows(build())
	_, secondRows := ExtractCodeCallRows(build())
	first := calleeFor(firstRows)
	second := calleeFor(secondRows)
	if first == "" {
		t.Fatalf("first pass did not resolve a cross-repo export edge; rows=%#v", firstRows)
	}
	if first != second {
		t.Fatalf("cross-repo callee id drifted across reprojection: %q != %q", first, second)
	}
	if first != "content-entity:repoa-process" {
		t.Fatalf("resolved callee id = %q, want the durable repo-a uid", first)
	}
}
