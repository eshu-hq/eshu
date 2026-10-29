// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"encoding/json"
	"fmt"
	"reflect"
	"testing"
)

func TestGoldenSnapshotPinsJVMRuntimeOnBothReadSurfaces(t *testing.T) {
	t.Parallel()
	snap, err := LoadSnapshot(goldenSnapshotPath())
	if err != nil {
		t.Fatal(err)
	}
	want := []map[string]any{
		{"runtime": "java", "source_kind": "pom_maven_compiler_release", "relative_path": "runtime_jvm/pom.xml", "raw_spec": "21", "spec_kind": "partial", "line_number": float64(4), "order_index": float64(0)},
		{"runtime": "java", "source_kind": "gradle_java_toolchain", "relative_path": "runtime_jvm/build.gradle", "raw_spec": "21", "spec_kind": "partial", "line_number": float64(1), "order_index": float64(0)},
		{"runtime": "java", "source_kind": "gradle_kotlin_jvm_toolchain", "relative_path": "runtime_jvm/build.gradle.kts", "raw_spec": "21", "spec_kind": "partial", "line_number": float64(1), "order_index": float64(0)},
		{"runtime": "scala", "source_kind": "sbt_scala_version", "relative_path": "runtime_jvm/build.sbt", "raw_spec": "3.3.1", "spec_kind": "exact", "line_number": float64(1), "order_index": float64(0)},
		{"runtime": "sbt", "source_kind": "sbt_build_properties_version", "relative_path": "runtime_jvm/project/build.properties", "raw_spec": "1.9.9", "spec_kind": "exact", "line_number": float64(1), "order_index": float64(0)},
	}
	shapes := map[string]QueryShape{
		"mcp":  snap.QueryShapes.MCP["list_runtime_version_declarations?assert=jvm_sources"],
		"http": snap.QueryShapes.HTTP["POST /api/v0/runtime-declarations/query?assert=jvm_sources"],
	}
	for name, shape := range shapes {
		t.Run(name, func(t *testing.T) {
			args := shape.Arguments
			if name == "http" {
				args = shape.RequestBody
			}
			if !reflect.DeepEqual(args, map[string]any{"limit": float64(100)}) || shape.MinimumResults < len(want) || shape.ResultsField != "declarations" {
				t.Fatalf("JVM selector/count = args:%v min:%d field:%q", args, shape.MinimumResults, shape.ResultsField)
			}
			if got := shape.RequiredJSONObjectMatches["declarations[]"]; !reflect.DeepEqual(got, want) {
				t.Fatalf("JVM same-row assertions = %#v, want %#v", got, want)
			}
			response := func(rows []map[string]any) []byte {
				t.Helper()
				body, marshalErr := json.Marshal(map[string]any{"declarations": rows, "count": len(rows), "limit": 100, "truncated": false, "filters": map[string]any{}, "excluded_from_comparison": map[string]any{}, "partial_reasons": []any{}})
				if marshalErr != nil {
					t.Fatal(marshalErr)
				}
				return body
			}
			if finding := EvaluateQueryShape(name+":jvm", shape, response(want)); !finding.OK {
				t.Fatalf("valid JVM rows failed: %s", finding.Detail)
			}
			for i := range want {
				missing := append([]map[string]any{}, want[:i]...)
				missing = append(missing, want[i+1:]...)
				if finding := EvaluateQueryShape(fmt.Sprintf("%s:missing:%d", name, i), shape, response(missing)); finding.OK {
					t.Fatalf("missing JVM source %q passed", want[i]["source_kind"])
				}
			}
		})
	}
	found := false
	for _, node := range snap.Graph.RequiredNodes {
		if node.ID == "rn-jvm-runtime-declarations" && node.Label == "RuntimeDeclaration" && node.MinimumCount >= 5 && reflect.DeepEqual(node.AllowedNodePropertyValues["runtime"], []string{"java", "scala", "sbt"}) {
			found = true
		}
	}
	if !found {
		t.Fatal("required_nodes lacks JVM RuntimeDeclaration nodes")
	}
}
