// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"encoding/json"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querytestutil"
)

func TestOpenAPIRelationshipStoryRestrictsTargetlessOverrides(t *testing.T) {
	t.Parallel()

	var spec map[string]any
	if err := json.Unmarshal([]byte(OpenAPISpec()), &spec); err != nil {
		t.Fatalf("json.Unmarshal(OpenAPISpec()) error = %v", err)
	}

	paths := querytestutil.MustMapField(t, spec, "paths")
	relationshipStoryPath := querytestutil.MustMapField(t, paths, "/api/v0/code/relationships/story")
	relationshipStoryPost := querytestutil.MustMapField(t, relationshipStoryPath, "post")
	relationshipStoryBody := querytestutil.MustMapField(t, querytestutil.MustMapField(t, relationshipStoryPost, "requestBody"), "content")
	relationshipStoryJSON := querytestutil.MustMapField(t, relationshipStoryBody, "application/json")
	relationshipStoryRequestSchema := querytestutil.MustMapField(t, relationshipStoryJSON, "schema")
	anyOf, ok := relationshipStoryRequestSchema["anyOf"].([]any)
	if !ok || len(anyOf) != 3 {
		t.Fatalf("code/relationships/story anyOf = %#v, want three request branches", relationshipStoryRequestSchema["anyOf"])
	}
	overrideBranch := anyOf[2].(map[string]any)
	overrideProperties := querytestutil.MustMapField(t, overrideBranch, "properties")
	overrideQueryType := querytestutil.MustMapField(t, overrideProperties, "query_type")
	if !containsValue(overrideQueryType["enum"].([]any), "overrides") {
		t.Fatalf("targetless query_type+repo_id branch enum = %#v, want overrides only", overrideQueryType["enum"])
	}
}

func TestOpenAPIRelationshipStoryDocumentsMinConfidence(t *testing.T) {
	t.Parallel()

	var spec map[string]any
	if err := json.Unmarshal([]byte(OpenAPISpec()), &spec); err != nil {
		t.Fatalf("json.Unmarshal(OpenAPISpec()) error = %v", err)
	}

	paths := querytestutil.MustMapField(t, spec, "paths")
	relationshipStoryPath := querytestutil.MustMapField(t, paths, "/api/v0/code/relationships/story")
	relationshipStoryPost := querytestutil.MustMapField(t, relationshipStoryPath, "post")
	relationshipStoryBody := querytestutil.MustMapField(t, querytestutil.MustMapField(t, relationshipStoryPost, "requestBody"), "content")
	relationshipStoryJSON := querytestutil.MustMapField(t, relationshipStoryBody, "application/json")
	relationshipStoryProperties := querytestutil.MustMapField(t, querytestutil.MustMapField(t, relationshipStoryJSON, "schema"), "properties")
	minConfidenceSchema := querytestutil.MustMapField(t, relationshipStoryProperties, "min_confidence")
	if got, want := minConfidenceSchema["type"], "number"; got != want {
		t.Fatalf("min_confidence type = %#v, want %#v", got, want)
	}
	if got, want := minConfidenceSchema["minimum"], float64(0); got != want {
		t.Fatalf("min_confidence minimum = %#v, want %#v", got, want)
	}
	if got, want := minConfidenceSchema["maximum"], float64(1); got != want {
		t.Fatalf("min_confidence maximum = %#v, want %#v", got, want)
	}
}

func TestOpenAPIRelationshipSchemaDocumentsProvenanceBlock(t *testing.T) {
	t.Parallel()

	var spec map[string]any
	if err := json.Unmarshal([]byte(OpenAPISpec()), &spec); err != nil {
		t.Fatalf("json.Unmarshal(OpenAPISpec()) error = %v", err)
	}

	components := querytestutil.MustMapField(t, spec, "components")
	schemas := querytestutil.MustMapField(t, components, "schemas")
	relationship := querytestutil.MustMapField(t, schemas, "Relationship")
	properties := querytestutil.MustMapField(t, relationship, "properties")
	provenance := querytestutil.MustMapField(t, properties, "provenance")
	provenanceProperties := querytestutil.MustMapField(t, provenance, "properties")
	for _, field := range []string{
		"confidence",
		"confidence_state",
		"method",
		"source_family",
		"reason",
		"truth_state",
		"why_trail",
		"why_trail_truncated",
		"derived",
		"heuristic",
		"unsupported",
	} {
		if _, ok := provenanceProperties[field]; !ok {
			t.Fatalf("Relationship.provenance schema missing %s", field)
		}
	}
}
