package query

import (
	"encoding/json"
	"testing"
)

func TestOpenAPISpecIncludesPackageRegistryCorrelations(t *testing.T) {
	var spec map[string]any
	if err := json.Unmarshal([]byte(OpenAPISpec()), &spec); err != nil {
		t.Fatalf("json.Unmarshal(OpenAPISpec()) error = %v, want nil", err)
	}

	paths := mustMapField(t, spec, "paths")
	path := mustMapField(t, paths, "/api/v0/package-registry/correlations")
	get := mustMapField(t, path, "get")
	if got, want := get["operationId"], "listPackageRegistryCorrelations"; got != want {
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
}
