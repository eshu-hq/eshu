package query

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestOpenAPISpecNamesHexPackageRegistryEcosystemScope(t *testing.T) {
	t.Parallel()

	var spec map[string]any
	if err := json.Unmarshal([]byte(OpenAPISpec()), &spec); err != nil {
		t.Fatalf("json.Unmarshal(OpenAPISpec()) error = %v, want nil", err)
	}

	paths := mustMapField(t, spec, "paths")
	path := mustMapField(t, paths, "/api/v0/package-registry/packages")
	get := mustMapField(t, path, "get")
	parameters := get["parameters"].([]any)
	for _, parameter := range parameters {
		parameterMap := parameter.(map[string]any)
		if parameterMap["name"] != "ecosystem" {
			continue
		}
		description, _ := parameterMap["description"].(string)
		if !strings.Contains(description, "hex") {
			t.Fatalf("ecosystem description = %q, want Hex named among package-registry scopes", description)
		}
		return
	}
	t.Fatalf("package-registry packages parameters missing ecosystem: %#v", parameters)
}

func TestOpenAPISpecIncludesPackageRegistryCorrelations(t *testing.T) {
	t.Parallel()

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
