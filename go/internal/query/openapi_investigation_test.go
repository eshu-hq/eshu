package query

import (
	"encoding/json"
	"testing"
)

func TestOpenAPISpecServiceInvestigationExposesCoverageFields(t *testing.T) {
	t.Parallel()

	var spec map[string]any
	if err := json.Unmarshal([]byte(OpenAPISpec()), &spec); err != nil {
		t.Fatalf("json.Unmarshal(OpenAPISpec()) error = %v, want nil", err)
	}
	paths := mustMapField(t, spec, "paths")
	investigationPath := mustMapField(t, paths, "/api/v0/investigations/services/{service_name}")
	investigationGet := mustMapField(t, investigationPath, "get")
	responses := mustMapField(t, investigationGet, "responses")
	okResponse := mustMapField(t, responses, "200")
	content := mustMapField(t, mustMapField(t, okResponse, "content"), "application/json")
	properties := mustMapField(t, mustMapField(t, content, "schema"), "properties")

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
	paths := mustMapField(t, spec, "paths")
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

			get := mustMapField(t, mustMapField(t, paths, tc.path), "get")
			if got := get["operationId"]; got != tc.operationID {
				t.Fatalf("operationId = %#v, want %#v", got, tc.operationID)
			}
			parameters := get["parameters"].([]any)
			for _, name := range tc.parameters {
				if !openAPIParametersIncludeName(parameters, name) {
					t.Fatalf("parameters missing %q: %#v", name, parameters)
				}
			}
			responses := mustMapField(t, get, "responses")
			okResponse := mustMapField(t, responses, "200")
			schema := mustMapField(
				t,
				mustMapField(
					t,
					mustMapField(t, okResponse, "content"),
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
