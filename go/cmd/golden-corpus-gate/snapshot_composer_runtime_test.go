// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"reflect"
	"testing"
)

// TestGoldenSnapshotPinsComposerPHPOnBothReadSurfaces proves the committed
// B-12 shapes require the same PHP declaration from each live query surface.
func TestGoldenSnapshotPinsComposerPHPOnBothReadSurfaces(t *testing.T) {
	t.Parallel()
	snap, err := LoadSnapshot(goldenSnapshotPath())
	if err != nil {
		t.Fatal(err)
	}
	wantArgs := map[string]any{"runtime": "php", "source_kind": "composer_json_require_php", "limit": float64(10)}
	wantRow := map[string]any{
		"runtime": "php", "source_kind": "composer_json_require_php",
		"relative_path": "composer.json", "raw_spec": ">=8.1",
		"spec_kind": "range", "line_number": float64(6), "order_index": float64(0),
	}
	shapes := map[string]QueryShape{
		"mcp":  snap.QueryShapes.MCP["list_runtime_version_declarations?assert=php_composer_require"],
		"http": snap.QueryShapes.HTTP["POST /api/v0/runtime-declarations/query?assert=php_composer_require"],
	}
	valid := []byte(`{"declarations":[{"runtime":"php","source_kind":"composer_json_require_php","relative_path":"composer.json","raw_spec":">=8.1","spec_kind":"range","line_number":6,"order_index":0}],"count":1,"limit":10,"truncated":false,"filters":{},"excluded_from_comparison":{},"partial_reasons":[]}`)
	missing := []byte(`{"declarations":[],"count":0,"limit":10,"truncated":false,"filters":{},"excluded_from_comparison":{},"partial_reasons":[]}`)
	wrongTuple := []byte(`{"declarations":[{"runtime":"php","source_kind":"composer_json_require_php","relative_path":"composer.json","raw_spec":">=8.1","spec_kind":"range","line_number":5,"order_index":0}],"count":1,"limit":10,"truncated":false,"filters":{},"excluded_from_comparison":{},"partial_reasons":[]}`)
	for name, shape := range shapes {
		t.Run(name, func(t *testing.T) {
			args := shape.Arguments
			if name == "http" {
				args = shape.RequestBody
			}
			if !reflect.DeepEqual(args, wantArgs) || shape.MinimumResults != 1 || shape.ResultsField != "declarations" {
				t.Fatalf("PHP selector/count = args:%v min:%d field:%q", args, shape.MinimumResults, shape.ResultsField)
			}
			if got := shape.RequiredJSONObjectMatches["declarations[]"]; len(got) != 1 || !reflect.DeepEqual(got[0], wantRow) {
				t.Fatalf("PHP same-row assertion = %v, want %v", got, wantRow)
			}
			if finding := EvaluateQueryShape(name+":php", shape, valid); !finding.OK {
				t.Fatalf("valid PHP row failed: %s", finding.Detail)
			}
			for caseName, body := range map[string][]byte{"missing": missing, "wrong tuple": wrongTuple} {
				if finding := EvaluateQueryShape(name+":"+caseName, shape, body); finding.OK {
					t.Fatalf("%s PHP row passed", caseName)
				}
			}
		})
	}

	found := false
	for _, node := range snap.Graph.RequiredNodes {
		if node.Label != "RuntimeDeclaration" || node.MinimumCount < 1 {
			continue
		}
		if got := node.AllowedNodePropertyValues["runtime"]; len(got) == 1 && got[0] == "php" {
			found = true
		}
	}
	if !found {
		t.Fatal("required_nodes lacks a PHP RuntimeDeclaration property assertion")
	}
}
