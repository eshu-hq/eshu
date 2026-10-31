// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package openapi

import (
	"encoding/json"
	"testing"
)

// TestSuppressionStateEnumsListAdvisoryRetracted proves every OpenAPI enum that
// lists the reducer suppression states also lists advisory_retracted (#9695),
// for the filter parameters and for the finding's suppression block alike.
func TestSuppressionStateEnumsListAdvisoryRetracted(t *testing.T) {
	t.Parallel()

	var spec any
	if err := json.Unmarshal([]byte(Spec()), &spec); err != nil {
		t.Fatalf("unmarshal spec: %v", err)
	}
	var found, missing int
	var walk func(any)
	walk = func(node any) {
		switch value := node.(type) {
		case map[string]any:
			if values, ok := value["enum"].([]any); ok && enumHas(values, "scope_mismatch") {
				found++
				if !enumHas(values, "advisory_retracted") {
					missing++
				}
			}
			for _, child := range value {
				walk(child)
			}
		case []any:
			for _, child := range value {
				walk(child)
			}
		}
	}
	walk(spec)
	if found == 0 {
		t.Fatal("no suppression-state enum found in the OpenAPI spec; the test is vacuous")
	}
	if missing != 0 {
		t.Fatalf("%d of %d suppression-state enums lack advisory_retracted", missing, found)
	}
}

func enumHas(values []any, want string) bool {
	for _, value := range values {
		if text, ok := value.(string); ok && text == want {
			return true
		}
	}
	return false
}
