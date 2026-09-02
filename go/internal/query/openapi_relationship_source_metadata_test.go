// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"encoding/json"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querytestutil"
)

func TestOpenAPIRelationshipDocumentsSourceMetadata(t *testing.T) {
	var spec map[string]interface{}
	if err := json.Unmarshal([]byte(OpenAPISpec()), &spec); err != nil {
		t.Fatalf("json.Unmarshal(OpenAPISpec()) error = %v, want nil", err)
	}

	relationship := querytestutil.MustMapField(t, querytestutil.MustMapField(t, querytestutil.MustMapField(t, spec, "components"), "schemas"), "Relationship")
	properties := querytestutil.MustMapField(t, relationship, "properties")
	for _, field := range []string{
		"source_repo_id",
		"source_repo_name",
		"source_file_path",
		"source_language",
		"source_type",
		"source_start_line",
		"source_end_line",
		"target_repo_id",
		"target_repo_name",
		"target_file_path",
		"target_language",
		"target_type",
		"target_start_line",
		"target_end_line",
		"confidence_basis",
		"resolution_source",
		"evidence_type",
		"evidence_kinds",
	} {
		if _, ok := properties[field]; !ok {
			t.Fatalf("Relationship schema missing %s", field)
		}
	}
}

func TestOpenAPIRelationshipEvidenceDocumentsConfidenceBasis(t *testing.T) {
	var spec map[string]interface{}
	if err := json.Unmarshal([]byte(OpenAPISpec()), &spec); err != nil {
		t.Fatalf("json.Unmarshal(OpenAPISpec()) error = %v, want nil", err)
	}

	paths := querytestutil.MustMapField(t, spec, "paths")
	route := querytestutil.MustMapField(t, paths, "/api/v0/evidence/relationships/{resolved_id}")
	get := querytestutil.MustMapField(t, route, "get")
	responses := querytestutil.MustMapField(t, get, "responses")
	okResponse := querytestutil.MustMapField(t, responses, "200")
	content := querytestutil.MustMapField(t, querytestutil.MustMapField(t, okResponse, "content"), "application/json")
	properties := querytestutil.MustMapField(t, querytestutil.MustMapField(t, content, "schema"), "properties")
	if _, ok := properties["confidence_basis"]; !ok {
		t.Fatal("relationship evidence schema missing confidence_basis")
	}
}
