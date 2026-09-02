// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querytestutil"
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

	paths := querytestutil.MustMapField(t, spec, "paths")
	path := querytestutil.MustMapField(t, paths, "/api/v0/entities/{entity_id}/context")
	get := querytestutil.MustMapField(t, path, "get")
	responses := querytestutil.MustMapField(t, get, "responses")
	okResponse := querytestutil.MustMapField(t, responses, "200")
	content := querytestutil.MustMapField(t, okResponse, "content")
	jsonContent := querytestutil.MustMapField(t, content, "application/json")
	schema := querytestutil.MustMapField(t, jsonContent, "schema")
	properties := querytestutil.MustMapField(t, schema, "properties")
	reason := querytestutil.MustMapField(t, properties, "relationships_truncation_reason")

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
