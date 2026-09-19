// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codequalitytools

import "testing"

func TestInspectCodeQualityToolMinComplexityDoesNotAdvertiseConflictingDefault(t *testing.T) {
	t.Parallel()

	tool := codeQualityInspectionTool()
	schema := tool.InputSchema.(map[string]any)
	properties := schema["properties"].(map[string]any)
	minComplexity := properties["min_complexity"].(map[string]any)

	if got, ok := minComplexity["default"]; ok {
		t.Fatalf("min_complexity default = %#v, want omitted for server-side check-specific defaults", got)
	}
	if _, ok := minComplexity["description"].(string); !ok {
		t.Fatal("min_complexity description missing, want server-side default guidance")
	}
}
