package query

import (
	"encoding/json"
	"testing"
)

func TestOpenAPISpecIncludesServiceCatalogCorrelations(t *testing.T) {
	var spec map[string]any
	if err := json.Unmarshal([]byte(OpenAPISpec()), &spec); err != nil {
		t.Fatalf("json.Unmarshal(OpenAPISpec()) error = %v, want nil", err)
	}

	paths := mustMapField(t, spec, "paths")
	path := mustMapField(t, paths, "/api/v0/service-catalog/correlations")
	get := mustMapField(t, path, "get")
	if got, want := get["operationId"], "listServiceCatalogCorrelations"; got != want {
		t.Fatalf("operationId = %#v, want %#v", got, want)
	}
	responses := mustMapField(t, get, "responses")
	okResponse := mustMapField(t, responses, "200")
	content := mustMapField(t, mustMapField(t, okResponse, "content"), "application/json")
	schema := mustMapField(t, content, "schema")
	properties := mustMapField(t, schema, "properties")
	correlations := mustMapField(t, properties, "correlations")
	items := mustMapField(t, correlations, "items")
	itemProperties := mustMapField(t, items, "properties")
	if got, want := mustMapField(t, itemProperties, "provenance_only")["type"], "boolean"; got != want {
		t.Fatalf("provenance_only type = %#v, want %#v", got, want)
	}
	candidates := mustMapField(t, itemProperties, "candidate_repository_ids")
	if got, want := candidates["type"], "array"; got != want {
		t.Fatalf("candidate_repository_ids type = %#v, want %#v", got, want)
	}
	requiredAnchors := mustMapField(t, itemProperties, "required_anchor_keys")
	if got, want := requiredAnchors["type"], "array"; got != want {
		t.Fatalf("required_anchor_keys type = %#v, want %#v", got, want)
	}
	missing := mustMapField(t, properties, "missing_evidence")
	if got, want := missing["type"], "array"; got != want {
		t.Fatalf("missing_evidence type = %#v, want %#v", got, want)
	}
	evidenceSummary := mustMapField(t, properties, "evidence_summary")
	evidenceProperties := mustMapField(t, evidenceSummary, "properties")
	localDescriptors := mustMapField(t, evidenceProperties, "local_descriptors")
	localProperties := mustMapField(t, localDescriptors, "properties")
	if got, want := mustMapField(t, localProperties, "source_uris")["type"], "array"; got != want {
		t.Fatalf("local_descriptors.source_uris type = %#v, want %#v", got, want)
	}
	external := mustMapField(t, evidenceProperties, "external_catalog_confirmation")
	externalProperties := mustMapField(t, external, "properties")
	if got, want := mustMapField(t, externalProperties, "reason")["type"], "string"; got != want {
		t.Fatalf("external_catalog_confirmation.reason type = %#v, want %#v", got, want)
	}
}
