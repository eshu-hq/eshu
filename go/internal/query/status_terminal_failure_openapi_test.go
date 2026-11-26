// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/status/collector"
)

// TestOpenAPITerminalFailureFieldsMatchStatusContract pins the published
// last_failure_class enum on every route that carries collector
// terminal-failure fields (collectors, collector readiness and its legacy
// alias, operations board) to the bounded class set the status package
// reports, plus unknown, and pins last_failure_at as omitted-when-absent
// (never null) on all four (#8803). A class added to the status set
// without the OpenAPI enum fails here. The alias counts because it is
// served by the same getCollectorReadiness handler (status.go:116-117).
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
	// The admin poisoned-links read carries its own last_failure_class
	// (admin.ChangedSincePoisonedLink LastFailureClass): a free-form class
	// string for a changed-since cursor, not the collector bounded set, so
	// it carries no enum. The census partitions on the enum: exactly the
	// four collector routes carry the bounded set (the legacy alias serves
	// the identical getCollectorReadiness payload).
	var bounded, unbounded []map[string]any
	for _, schema := range classes {
		if _, ok := schema["enum"]; ok {
			bounded = append(bounded, schema)
		} else {
			unbounded = append(unbounded, schema)
		}
	}
	if len(bounded) != 4 || len(timestamps) != 4 {
		t.Fatalf("found %d bounded last_failure_class and %d last_failure_at schemas, want 4 each (collectors, collector readiness and its legacy alias, operations board)",
			len(bounded), len(timestamps))
	}
	if len(unbounded) != 1 {
		t.Fatalf("found %d unbounded last_failure_class schemas, want 1 (admin poisoned-links)", len(unbounded))
	}
	if typ, _ := unbounded[0]["type"].(string); typ != "string" {
		t.Fatalf("admin poisoned-links last_failure_class type = %q, want string", typ)
	}
	if desc, _ := unbounded[0]["description"].(string); !strings.Contains(desc, "ChangedSincePoisonedLink") {
		t.Fatalf("admin poisoned-links last_failure_class description does not name its owner: %q", desc)
	}
	for _, schema := range bounded {
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

	// The acknowledged_failure sub-record (#8830) carries the same bounded
	// class on all four routes, so its enum is pinned to the same set,
	// and its timestamps are omitted-when-absent (never null) like
	// last_failure_at.
	var acknowledged []map[string]any
	collectNamedProperties(spec, "acknowledged_failure", &acknowledged)
	if len(acknowledged) != 4 {
		t.Fatalf("found %d acknowledged_failure schemas, want 4 (collectors, collector readiness and its legacy alias, operations board)", len(acknowledged))
	}
	for _, schema := range acknowledged {
		properties, _ := schema["properties"].(map[string]any)
		class, _ := properties["failure_class"].(map[string]any)
		raw, _ := class["enum"].([]any)
		got := make([]string, 0, len(raw))
		for _, value := range raw {
			text, _ := value.(string)
			got = append(got, text)
		}
		slices.Sort(got)
		if !slices.Equal(got, want) {
			t.Fatalf("acknowledged_failure.failure_class enum = %v, want %v", got, want)
		}
		for _, key := range []string{"failed_at", "acknowledged_at"} {
			if stamp, ok := properties[key].(map[string]any); ok {
				if _, hasNullable := stamp["nullable"]; hasNullable {
					t.Fatalf("acknowledged_failure.%s has a nullable marker, want omitted-when-absent", key)
				}
			}
		}
	}
}

// collectNamedProperties gathers every object-valued property named name.
func collectNamedProperties(node any, name string, out *[]map[string]any) {
	switch typed := node.(type) {
	case map[string]any:
		for key, value := range typed {
			if schema, ok := value.(map[string]any); ok && key == name {
				*out = append(*out, schema)
			}
			collectNamedProperties(value, name, out)
		}
	case []any:
		for _, value := range typed {
			collectNamedProperties(value, name, out)
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
