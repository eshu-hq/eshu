// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"encoding/json"
	"fmt"
	"reflect"
	"testing"
)

// TestGoldenSnapshotPinsPythonRuntimeOnBothReadSurfaces binds all seven
// synthetic source rows through both deployed query transports.
func TestGoldenSnapshotPinsPythonRuntimeOnBothReadSurfaces(t *testing.T) {
	t.Parallel()
	snap, err := LoadSnapshot(goldenSnapshotPath())
	if err != nil {
		t.Fatal(err)
	}
	want := []map[string]any{
		{"runtime": "python", "source_kind": "pyproject_requires_python", "relative_path": "runtime_sources/pyproject.toml", "raw_spec": ">=3.10", "spec_kind": "range", "line_number": float64(2), "order_index": float64(0)},
		{"runtime": "python", "source_kind": "poetry_python", "relative_path": "runtime_sources/pyproject.toml", "raw_spec": "^3.11", "spec_kind": "range", "line_number": float64(5), "order_index": float64(0)},
		{"runtime": "python", "source_kind": "pipfile_python_version", "relative_path": "runtime_sources/Pipfile", "raw_spec": "3.12", "spec_kind": "partial", "line_number": float64(2), "order_index": float64(0)},
		{"runtime": "python", "source_kind": "pipfile_python_full_version", "relative_path": "runtime_sources/Pipfile", "raw_spec": "3.12.1", "spec_kind": "exact", "line_number": float64(3), "order_index": float64(0)},
		{"runtime": "python", "source_kind": "setup_cfg_python_requires", "relative_path": "runtime_sources/setup.cfg", "raw_spec": ">=3.9", "spec_kind": "range", "line_number": float64(2), "order_index": float64(0)},
		{"runtime": "python", "source_kind": "setup_py_python_requires", "relative_path": "runtime_sources/setup.py", "raw_spec": ">=3.8", "spec_kind": "range", "line_number": float64(2), "order_index": float64(0)},
		{"runtime": "python", "source_kind": "runtime_txt", "relative_path": "runtime_sources/runtime.txt", "raw_spec": "python-3.11.2", "spec_kind": "exact", "line_number": float64(1), "order_index": float64(0)},
	}
	shapes := map[string]QueryShape{
		"mcp":  snap.QueryShapes.MCP["list_runtime_version_declarations?assert=python_sources"],
		"http": snap.QueryShapes.HTTP["POST /api/v0/runtime-declarations/query?assert=python_sources"],
	}
	for name, shape := range shapes {
		t.Run(name, func(t *testing.T) {
			args := shape.Arguments
			if name == "http" {
				args = shape.RequestBody
			}
			if !reflect.DeepEqual(args, map[string]any{"runtime": "python", "limit": float64(50)}) || shape.MinimumResults < 7 || shape.ResultsField != "declarations" {
				t.Fatalf("Python selector/count = args:%v min:%d field:%q", args, shape.MinimumResults, shape.ResultsField)
			}
			if got := shape.RequiredJSONObjectMatches["declarations[]"]; !reflect.DeepEqual(got, want) {
				t.Fatalf("Python same-row assertions = %#v, want %#v", got, want)
			}
			valid := pythonRuntimeResponse(t, want)
			if finding := EvaluateQueryShape(name+":python", shape, valid); !finding.OK {
				t.Fatalf("valid Python rows failed: %s", finding.Detail)
			}
			for i := range want {
				missing := append([]map[string]any{}, want[:i]...)
				missing = append(missing, want[i+1:]...)
				if finding := EvaluateQueryShape(fmt.Sprintf("%s:missing:%d", name, i), shape, pythonRuntimeResponse(t, missing)); finding.OK {
					t.Fatalf("missing Python source %q passed", want[i]["source_kind"])
				}
			}
		})
	}
	found := false
	for _, node := range snap.Graph.RequiredNodes {
		if node.ID == "rn-python-runtime-txt" && node.Label == "RuntimeDeclaration" && node.MinimumCount >= 1 && reflect.DeepEqual(node.AllowedNodePropertyValues["runtime"], []string{"python"}) && reflect.DeepEqual(node.AllowedNodePropertyValues["source_kind"], []string{"runtime_txt"}) {
			found = true
		}
	}
	if !found {
		t.Fatal("required_nodes lacks a Python RuntimeDeclaration node")
	}
}

func pythonRuntimeResponse(t *testing.T, rows []map[string]any) []byte {
	t.Helper()
	body, err := json.Marshal(map[string]any{"declarations": rows, "count": len(rows), "limit": 50, "truncated": false, "filters": map[string]any{}, "excluded_from_comparison": map[string]any{}, "partial_reasons": []any{}})
	if err != nil {
		t.Fatal(err)
	}
	return body
}
