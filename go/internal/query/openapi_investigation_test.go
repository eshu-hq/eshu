// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"encoding/json"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querytestutil"
)

func TestOpenAPISpecServiceInvestigationExposesCoverageFields(t *testing.T) {
	t.Parallel()

	var spec map[string]any
	if err := json.Unmarshal([]byte(OpenAPISpec()), &spec); err != nil {
		t.Fatalf("json.Unmarshal(OpenAPISpec()) error = %v, want nil", err)
	}
	paths := querytestutil.MustMapField(t, spec, "paths")
	investigationPath := querytestutil.MustMapField(t, paths, "/api/v0/investigations/services/{service_name}")
	investigationGet := querytestutil.MustMapField(t, investigationPath, "get")
	responses := querytestutil.MustMapField(t, investigationGet, "responses")
	okResponse := querytestutil.MustMapField(t, responses, "200")
	content := querytestutil.MustMapField(t, querytestutil.MustMapField(t, okResponse, "content"), "application/json")
	properties := querytestutil.MustMapField(t, querytestutil.MustMapField(t, content, "schema"), "properties")

	for _, field := range []string{
		"repositories_considered",
		"repositories_with_evidence",
		"evidence_families_found",
		"coverage_summary",
		"investigation_findings",
		"recommended_next_calls",
	} {
		if _, ok := properties[field]; !ok {
			t.Fatalf("investigation response schema missing %s", field)
		}
	}
}

func TestOpenAPISpecIncludesInvestigationPacketRoutes(t *testing.T) {
	t.Parallel()

	var spec map[string]any
	if err := json.Unmarshal([]byte(OpenAPISpec()), &spec); err != nil {
		t.Fatalf("json.Unmarshal(OpenAPISpec()) error = %v, want nil", err)
	}
	paths := querytestutil.MustMapField(t, spec, "paths")
	tests := []struct {
		path        string
		operationID string
		parameters  []string
	}{
		{
			path:        "/api/v0/investigations/supply-chain/impact/packet",
			operationID: "getSupplyChainImpactPacket",
			parameters:  []string{"finding_id", "repository_id", "max_source_facts"},
		},
		{
			path:        "/api/v0/investigations/deployable-unit/packet",
			operationID: "getDeployableUnitPacket",
			parameters:  []string{"scope_id", "generation_id", "repository_id", "max_source_facts"},
		},
		{
			path:        "/api/v0/investigations/drift/packet",
			operationID: "getDriftPacket",
			parameters:  []string{"scope_id", "provider", "cloud_resource_uid", "max_source_facts"},
		},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.operationID, func(t *testing.T) {
			t.Parallel()

			get := querytestutil.MustMapField(t, querytestutil.MustMapField(t, paths, tc.path), "get")
			if got := get["operationId"]; got != tc.operationID {
				t.Fatalf("operationId = %#v, want %#v", got, tc.operationID)
			}
			parameters := get["parameters"].([]any)
			for _, name := range tc.parameters {
				if !openAPIParametersIncludeName(parameters, name) {
					t.Fatalf("parameters missing %q: %#v", name, parameters)
				}
			}
			responses := querytestutil.MustMapField(t, get, "responses")
			okResponse := querytestutil.MustMapField(t, responses, "200")
			schema := querytestutil.MustMapField(
				t,
				querytestutil.MustMapField(
					t,
					querytestutil.MustMapField(t, okResponse, "content"),
					"application/json",
				),
				"schema",
			)
			if got := schema["$ref"]; got != "#/components/schemas/InvestigationEvidencePacket" {
				t.Fatalf("200 schema ref = %#v, want InvestigationEvidencePacket", got)
			}
		})
	}
}
