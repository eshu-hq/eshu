// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"encoding/json"
	"slices"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/status/collector"
)

// TestOpenAPITerminalFailureFieldsMatchStatusContract pins the published
// last_failure_class enum on both collector status routes to the bounded
// class set the status package reports, plus unknown, and pins
// last_failure_at as omitted-when-absent (never null) on both (#8803). A
// class added to the status set without the OpenAPI enum fails here.
func TestOpenAPITerminalFailureFieldsMatchStatusContract(t *testing.T) {
	t.Parallel()
	var spec map[string]any
	if err := json.Unmarshal([]byte(OpenAPISpec()), &spec); err != nil {
		t.Fatalf("json.Unmarshal(OpenAPISpec()) error = %v, want nil", err)
	}
	want := append(collector.BoundedFailureClasses(), collector.UnknownFailureToken)
	slices.Sort(want)

	var classes, timestamps []map[string]any
	collectTerminalFailureProperties(spec, &classes, &timestamps)
	if len(classes) != 2 || len(timestamps) != 2 {
		t.Fatalf("found %d last_failure_class and %d last_failure_at schemas, want 2 each (collectors and collector readiness)",
			len(classes), len(timestamps))
	}
	for _, schema := range classes {
		raw, _ := schema["enum"].([]any)
		got := make([]string, 0, len(raw))
		for _, value := range raw {
			text, _ := value.(string)
			got = append(got, text)
		}
		slices.Sort(got)
		if !slices.Equal(got, want) {
			t.Fatalf("last_failure_class enum = %v, want %v", got, want)
		}
	}
	for _, schema := range timestamps {
		if _, ok := schema["nullable"]; ok {
			t.Fatalf("last_failure_at = %v, want omitted-when-absent with no nullable marker", schema)
		}
	}
}

// collectTerminalFailureProperties walks the spec and gathers every
// last_failure_class and last_failure_at property schema.
func collectTerminalFailureProperties(node any, classes, timestamps *[]map[string]any) {
	switch typed := node.(type) {
	case map[string]any:
		for key, value := range typed {
			if schema, ok := value.(map[string]any); ok {
				switch key {
				case "last_failure_class":
					*classes = append(*classes, schema)
				case "last_failure_at":
					*timestamps = append(*timestamps, schema)
				}
			}
			collectTerminalFailureProperties(value, classes, timestamps)
		}
	case []any:
		for _, value := range typed {
			collectTerminalFailureProperties(value, classes, timestamps)
		}
	}
}
