// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/codeprovenance"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// crossRepoIdentity captures the durable, generation-independent identity facts
// of a single resolved cross-repo code-call edge: which callee entity it points
// at, by which resolution method, and the provenance confidence that method
// derives. Issue #2717 requires all three to be byte-identical across two
// consecutive generations of the same facts.
type crossRepoIdentity struct {
	calleeEntityID   string
	resolutionMethod string
	confidence       float64
}

// crossRepoIdentityForCaller scans rows for the single edge whose caller is
// callerEntityID and returns its durable identity. It fails when no such edge
// resolved, so a silent non-resolution can never masquerade as stable identity.
func crossRepoIdentityForCaller(t *testing.T, rows []map[string]any, callerEntityID string) crossRepoIdentity {
	t.Helper()
	for _, row := range rows {
		if anyToString(row["caller_entity_id"]) != callerEntityID {
			continue
		}
		method := anyToString(row["resolution_method"])
		return crossRepoIdentity{
			calleeEntityID:   anyToString(row["callee_entity_id"]),
			resolutionMethod: method,
			confidence:       codeprovenance.Confidence(codeprovenance.Method(method)),
		}
	}
	t.Fatalf("no resolved code-call edge for caller %q (rows=%d)", callerEntityID, len(rows))
	return crossRepoIdentity{}
}

// stampGeneration mutates a freshly built fixture so it carries generationID at
// every layer the parser would re-stamp on a new generation: the envelope
// GenerationID/FactID, the payload generation_id, and each parsed function's
// fact_id/generation_id. The durable identity inputs (uid, scip_symbol,
// package_export_symbol, paths) are left untouched, mirroring how the ingester
// re-emits the same code with a fresh generation id.
func stampGeneration(envelopes []facts.Envelope, generationID string) []facts.Envelope {
	for i := range envelopes {
		envelopes[i].GenerationID = generationID
		envelopes[i].FactID = "fact:" + generationID + ":" + anyToString(envelopes[i].Payload["repo_id"])
		envelopes[i].Payload["generation_id"] = generationID
		fileData, ok := envelopes[i].Payload["parsed_file_data"].(map[string]any)
		if !ok {
			continue
		}
		fileData["generation_id"] = generationID
		for _, fn := range mapSlice(fileData["functions"]) {
			fn["generation_id"] = generationID
			fn["fact_id"] = "fact:" + generationID + ":" + anyToString(fn["uid"])
		}
	}
	return envelopes
}

// TestExtractCodeCallRowsCrossRepoSCIPSymbolIdentityStableAcrossGenerations is
// the reprojection-stability proof required by issue #2717 for the SCIP-symbol
// cross-repo path. It runs resolution twice over the same call (repo B calling a
// symbol exported by repo A) under two different generation ids and proves the
// resolved callee entity id, resolution method, and derived provenance
// confidence are byte-identical: identity is keyed on the durable function uid
// and the generation-independent SCIP symbol, never the generation id.
func TestExtractCodeCallRowsCrossRepoSCIPSymbolIdentityStableAcrossGenerations(t *testing.T) {
	t.Parallel()

	appPath := "/workspace/app/main.py"
	libPath := "/workspace/lib/client.py"
	scipSymbol := "scip-python python acme_lib/client.py Client#request()."
	build := func() []facts.Envelope {
		return []facts.Envelope{
			{FactKind: "file", Payload: map[string]any{
				"repo_id":       "repo-app",
				"relative_path": "main.py",
				"parsed_file_data": map[string]any{
					"path": appPath,
					"functions": []any{
						map[string]any{"name": "handler", "line_number": 1, "end_line": 5, "uid": "uid:app:handler"},
					},
					"function_calls_scip": []any{
						map[string]any{
							"caller_file":   appPath,
							"caller_line":   1,
							"caller_symbol": "scip-python python service/main.py handler().",
							"callee_symbol": scipSymbol,
							"ref_line":      3,
						},
					},
				},
			}},
			{FactKind: "file", Payload: map[string]any{
				"repo_id":       "repo-lib",
				"relative_path": "client.py",
				"parsed_file_data": map[string]any{
					"path": libPath,
					"functions": []any{
						map[string]any{
							"name":        "request",
							"line_number": 8,
							"end_line":    11,
							"uid":         "uid:lib:request",
							"scip_symbol": scipSymbol,
						},
					},
				},
			}},
		}
	}

	_, firstRows := ExtractCodeCallRows(stampGeneration(build(), "gen-1"))
	_, secondRows := ExtractCodeCallRows(stampGeneration(build(), "gen-2"))

	first := crossRepoIdentityForCaller(t, firstRows, "uid:app:handler")
	second := crossRepoIdentityForCaller(t, secondRows, "uid:app:handler")

	if first.resolutionMethod != string(codeprovenance.MethodSCIP) {
		t.Fatalf("resolution_method = %q, want %q", first.resolutionMethod, codeprovenance.MethodSCIP)
	}
	if first.calleeEntityID != "uid:lib:request" {
		t.Fatalf("callee_entity_id = %q, want the durable lib uid %q", first.calleeEntityID, "uid:lib:request")
	}
	if first != second {
		t.Fatalf("cross-repo SCIP identity churned across generations: gen-1 %#v != gen-2 %#v", first, second)
	}
}

// TestExtractCodeCallRowsNativeGoSCIPSymbolIdentityStableAcrossGenerations is
// the native-Go reprojection proof for issue #2846. It uses the same symbol
// shape emitted by the Go parser and proves a package-qualified Go-to-Go call
// resolves by SCIP provenance with stable identity across generations.
func TestExtractCodeCallRowsNativeGoSCIPSymbolIdentityStableAcrossGenerations(t *testing.T) {
	t.Parallel()

	goSymbol := "scip-go gomod github.com/org/repoa/pkg Process()."
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
						map[string]any{
							"name":                "Process",
							"line_number":         3,
							"end_line":            5,
							"uid":                 "content-entity:repoa-process",
							"package_import_path": "github.com/org/repoa/pkg",
							"scip_symbol":         goSymbol,
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
							"stable_symbol_key":        goSymbol,
							"line_number":              6,
							"lang":                     "go",
						},
					},
				},
			}},
		}
	}

	_, firstRows := ExtractCodeCallRows(stampGeneration(build(), "gen-1"))
	_, secondRows := ExtractCodeCallRows(stampGeneration(build(), "gen-2"))

	first := crossRepoIdentityForCaller(t, firstRows, "content-entity:repob-main")
	second := crossRepoIdentityForCaller(t, secondRows, "content-entity:repob-main")

	if first.resolutionMethod != string(codeprovenance.MethodSCIP) {
		t.Fatalf("resolution_method = %q, want %q", first.resolutionMethod, codeprovenance.MethodSCIP)
	}
	if first.calleeEntityID != "content-entity:repoa-process" {
		t.Fatalf("callee_entity_id = %q, want the durable repo-a uid %q", first.calleeEntityID, "content-entity:repoa-process")
	}
	if first != second {
		t.Fatalf("native Go cross-repo SCIP identity churned across generations: gen-1 %#v != gen-2 %#v", first, second)
	}
}

// TestExtractCodeCallRowsCrossRepoPackageExportIdentityStableAcrossGenerations
// is the reprojection-stability proof required by issue #2717 for the
// package-export cross-repo path. It runs resolution twice over the same call
// (repo B calling renderButton exported by repo UI) under two different
// generation ids and proves the resolved callee entity id, resolution method,
// and derived provenance confidence are byte-identical.
func TestExtractCodeCallRowsCrossRepoPackageExportIdentityStableAcrossGenerations(t *testing.T) {
	t.Parallel()

	appPath := "/workspace/app/src/page.ts"
	libPath := "/workspace/ui/src/index.ts"
	exportSymbol := "package:npm://registry.npmjs.org/@acme/ui#renderButton"
	build := func() []facts.Envelope {
		return []facts.Envelope{
			{FactKind: "file", Payload: map[string]any{
				"repo_id":       "repo-app",
				"relative_path": "src/page.ts",
				"parsed_file_data": map[string]any{
					"path": appPath,
					"functions": []any{
						map[string]any{"name": "page", "line_number": 1, "end_line": 4, "uid": "uid:app:page"},
					},
					"function_calls": []any{
						map[string]any{
							"name":                  "renderButton",
							"full_name":             "renderButton",
							"line_number":           2,
							"lang":                  "typescript",
							"package_export_symbol": exportSymbol,
						},
					},
				},
			}},
			{FactKind: "file", Payload: map[string]any{
				"repo_id":       "repo-ui",
				"relative_path": "src/index.ts",
				"parsed_file_data": map[string]any{
					"path": libPath,
					"functions": []any{
						map[string]any{
							"name":                  "renderButton",
							"line_number":           3,
							"end_line":              6,
							"uid":                   "uid:ui:renderButton",
							"package_export_symbol": exportSymbol,
						},
					},
				},
			}},
		}
	}

	_, firstRows := ExtractCodeCallRows(stampGeneration(build(), "gen-1"))
	_, secondRows := ExtractCodeCallRows(stampGeneration(build(), "gen-2"))

	first := crossRepoIdentityForCaller(t, firstRows, "uid:app:page")
	second := crossRepoIdentityForCaller(t, secondRows, "uid:app:page")

	if first.resolutionMethod != string(codeprovenance.MethodImportBinding) {
		t.Fatalf("resolution_method = %q, want %q", first.resolutionMethod, codeprovenance.MethodImportBinding)
	}
	if first.calleeEntityID != "uid:ui:renderButton" {
		t.Fatalf("callee_entity_id = %q, want the durable ui uid %q", first.calleeEntityID, "uid:ui:renderButton")
	}
	if first != second {
		t.Fatalf("cross-repo package-export identity churned across generations: gen-1 %#v != gen-2 %#v", first, second)
	}
}
