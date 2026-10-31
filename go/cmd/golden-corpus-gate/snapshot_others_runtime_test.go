// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"encoding/json"
	"reflect"
	"testing"
)

// othersRuntimeLane binds one Others declaration's complete source tuple to
// the MCP and HTTP shape keys that pin it (issue #9709, follow-up to #9660).
type othersRuntimeLane struct {
	assertSuffix string
	runtime      string
	sourceKind   string
	relativePath string
	rawSpec      string
	specKind     string
	lineNumber   float64
	rnID         string
}

// othersRuntimeLanes covers the 8 new Others declarations, each asserted on
// both read surfaces for 16 shape keys total (issue #9710).
var othersRuntimeLanes = []othersRuntimeLane{
	{assertSuffix: "elixir_mix", runtime: "elixir", sourceKind: "mix_exs_elixir", relativePath: "runtime_elixir/mix.exs", rawSpec: "~> 1.17", specKind: "range", lineNumber: 8, rnID: "rn-elixir-mix-declaration"},
	{assertSuffix: "swift_tools_version", runtime: "swift", sourceKind: "swift_tools_version", relativePath: "runtime_swift/Package.swift", rawSpec: "5.9", specKind: "partial", lineNumber: 1, rnID: "rn-swift-tools-version-declaration"},
	{assertSuffix: "dart_pubspec", runtime: "dart", sourceKind: "dart_pubspec_environment_sdk", relativePath: "runtime_dart/pubspec.yaml", rawSpec: ">=3.0.0 <4.0.0", specKind: "range", lineNumber: 3, rnID: "rn-dart-pubspec-declaration"},
	{assertSuffix: "dart_fvmrc", runtime: "dart", sourceKind: "dart_fvmrc_flutter", relativePath: "runtime_dart/.fvmrc", rawSpec: "3.22.0", specKind: "exact", lineNumber: 1, rnID: "rn-dart-fvmrc-declaration"},
	{assertSuffix: "perl_cpanfile", runtime: "perl", sourceKind: "cpanfile_perl", relativePath: "runtime_perl/cpanfile", rawSpec: "5.38.0", specKind: "exact", lineNumber: 1, rnID: "rn-perl-cpanfile-declaration"},
	{assertSuffix: "perl_version_pin", runtime: "perl", sourceKind: "perl_version", relativePath: "runtime_perl/.perl-version", rawSpec: "5.38.2", specKind: "exact", lineNumber: 1, rnID: "rn-perl-version-declaration"},
	{assertSuffix: "haskell_stack", runtime: "haskell", sourceKind: "stack_yaml_resolver", relativePath: "runtime_haskell_stack/stack.yaml", rawSpec: "lts-22.43", specKind: "alias", lineNumber: 3, rnID: "rn-haskell-stack-declaration"},
	{assertSuffix: "haskell_cabal", runtime: "haskell", sourceKind: "cabal_tested_with", relativePath: "runtime_haskell_cabal/demo.cabal", rawSpec: "GHC==9.4.8", specKind: "alias", lineNumber: 1, rnID: "rn-haskell-cabal-declaration"},
}

// TestGoldenSnapshotPinsOthersRuntimeOnBothReadSurfaces proves each of the 16
// new Others query-shape assertions (#9709) binds the complete source tuple:
// a valid body passes EvaluateQueryShape while a missing or wrong-tuple body
// fails. The required_node_properties check in the live gate covers only
// runtime/source_kind, so without this mirror a wrong raw_spec, line_number,
// or relative_path in a full tuple is locally false-green (issue #9710).
//
// The 16 shape keys landed on main with #9709 (merge 96487a0bd0); each lane
// subtest asserts against the committed snapshot, so a deleted shape key
// fails instead of skipping.
func TestGoldenSnapshotPinsOthersRuntimeOnBothReadSurfaces(t *testing.T) {
	t.Parallel()
	snap, err := LoadSnapshot(goldenSnapshotPath())
	if err != nil {
		t.Fatal(err)
	}
	for _, lane := range othersRuntimeLanes {
		lane := lane
		wantArgs := map[string]any{"runtime": lane.runtime, "limit": float64(50)}
		wantRow := map[string]any{
			"runtime": lane.runtime, "source_kind": lane.sourceKind,
			"relative_path": lane.relativePath, "raw_spec": lane.rawSpec,
			"spec_kind": lane.specKind, "line_number": lane.lineNumber, "order_index": float64(0),
		}
		shapes := map[string]QueryShape{
			"mcp":  snap.QueryShapes.MCP["list_runtime_version_declarations?assert="+lane.assertSuffix],
			"http": snap.QueryShapes.HTTP["POST /api/v0/runtime-declarations/query?assert="+lane.assertSuffix],
		}
		for surface, shape := range shapes {
			surface, shape := surface, shape
			t.Run(lane.assertSuffix+"/"+surface, func(t *testing.T) {
				t.Parallel()
				args := shape.Arguments
				if surface == "http" {
					args = shape.RequestBody
				}
				if !reflect.DeepEqual(args, wantArgs) || shape.MinimumResults != 1 || shape.ResultsField != "declarations" {
					t.Fatalf("%s selector/count = args:%v min:%d field:%q", lane.assertSuffix, args, shape.MinimumResults, shape.ResultsField)
				}
				if got := shape.RequiredJSONObjectMatches["declarations[]"]; len(got) != 1 || !reflect.DeepEqual(got[0], wantRow) {
					t.Fatalf("%s same-row assertion = %v, want %v", lane.assertSuffix, got, wantRow)
				}
				if finding := EvaluateQueryShape(surface+":"+lane.assertSuffix, shape, othersRuntimeResponse(t, []map[string]any{laneRowCopy(wantRow)})); !finding.OK {
					t.Fatalf("valid %s row failed: %s", lane.assertSuffix, finding.Detail)
				}
				if finding := EvaluateQueryShape(surface+":"+lane.assertSuffix+":missing", shape, othersRuntimeResponse(t, nil)); finding.OK {
					t.Fatalf("missing %s row passed", lane.assertSuffix)
				}
				wrong := laneRowCopy(wantRow)
				wrong["raw_spec"] = "WRONG (mirror-test mutation)"
				if finding := EvaluateQueryShape(surface+":"+lane.assertSuffix+":wrong-tuple", shape, othersRuntimeResponse(t, []map[string]any{wrong})); finding.OK {
					t.Fatalf("wrong-tuple %s row passed", lane.assertSuffix)
				}
			})
		}
	}

	// The 8 rn-* required-node rows landed with the same merge; require all 8
	// with their runtime/source_kind pins.
	byID := map[string]RequiredNode{}
	for _, node := range snap.Graph.RequiredNodes {
		byID[node.ID] = node
	}
	for _, lane := range othersRuntimeLanes {
		node, ok := byID[lane.rnID]
		if !ok {
			t.Errorf("required_nodes lacks %s", lane.rnID)
			continue
		}
		if node.Label != "RuntimeDeclaration" || node.MinimumCount < 1 {
			t.Errorf("%s label/count = %q/%d, want RuntimeDeclaration/>=1", lane.rnID, node.Label, node.MinimumCount)
		}
		if !reflect.DeepEqual(node.AllowedNodePropertyValues["runtime"], []string{lane.runtime}) ||
			!reflect.DeepEqual(node.AllowedNodePropertyValues["source_kind"], []string{lane.sourceKind}) {
			t.Errorf("%s property pins = %v, want runtime=%q source_kind=%q",
				lane.rnID, node.AllowedNodePropertyValues, lane.runtime, lane.sourceKind)
		}
	}
}

// laneRowCopy duplicates a snapshot row map so a mutation never aliases the
// committed want row.
func laneRowCopy(row map[string]any) map[string]any {
	out := make(map[string]any, len(row))
	for k, v := range row {
		out[k] = v
	}
	return out
}

// othersRuntimeResponse builds a canonical runtime-declarations response body
// carrying exactly the given rows.
func othersRuntimeResponse(t *testing.T, rows []map[string]any) []byte {
	t.Helper()
	if rows == nil {
		rows = []map[string]any{}
	}
	body, err := json.Marshal(map[string]any{
		"declarations": rows, "count": len(rows), "limit": 50, "truncated": false,
		"filters": map[string]any{}, "excluded_from_comparison": map[string]any{}, "partial_reasons": []any{},
	})
	if err != nil {
		t.Fatal(err)
	}
	return body
}
