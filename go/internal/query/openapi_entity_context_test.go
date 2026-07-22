// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestOpenAPIEntityContextDocumentsIncompleteRelationshipReasons keeps the
// entity-context wire contract aligned with every reason the handler emits
// when relationship truth is incomplete.
func TestOpenAPIEntityContextDocumentsIncompleteRelationshipReasons(t *testing.T) {
	t.Parallel()

	var spec map[string]any
	if err := json.Unmarshal([]byte(OpenAPISpec()), &spec); err != nil {
		t.Fatalf("json.Unmarshal(OpenAPISpec()) error = %v, want nil", err)
	}

	paths := mustMapField(t, spec, "paths")
	path := mustMapField(t, paths, "/api/v0/entities/{entity_id}/context")
	get := mustMapField(t, path, "get")
	responses := mustMapField(t, get, "responses")
	okResponse := mustMapField(t, responses, "200")
	content := mustMapField(t, okResponse, "content")
	jsonContent := mustMapField(t, content, "application/json")
	schema := mustMapField(t, jsonContent, "schema")
	properties := mustMapField(t, schema, "properties")
	reason := mustMapField(t, properties, "relationships_truncation_reason")

	allowed := mustStringSliceField(t, reason, "enum")
	for _, want := range []string{
		k8sSelectCandidateScanTruncationReason,
		githubActionsSourceCacheTruncationReason,
	} {
		if !containsOpenAPIEnumString(allowed, want) {
			t.Fatalf("relationships_truncation_reason enum = %#v, want %q", allowed, want)
		}
	}

	description, _ := reason["description"].(string)
	if !strings.Contains(description, "GitHub Actions") {
		t.Fatalf("relationships_truncation_reason description = %q, want GitHub Actions disclosure", description)
	}
}
