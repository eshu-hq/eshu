package query

import (
	"encoding/json"
	"testing"
)

func TestOpenAPISpecIncludesAdmissionDecisions(t *testing.T) {
	t.Parallel()

	var spec map[string]any
	if err := json.Unmarshal([]byte(OpenAPISpec()), &spec); err != nil {
		t.Fatalf("json.Unmarshal(OpenAPISpec()) error = %v, want nil", err)
	}

	paths := mustMapField(t, spec, "paths")
	path := mustMapField(t, paths, "/api/v0/evidence/admission-decisions")
	get := mustMapField(t, path, "get")
	if got, want := get["operationId"], "listAdmissionDecisions"; got != want {
		t.Fatalf("operationId = %#v, want %#v", got, want)
	}
	parameters := get["parameters"].([]any)
	for _, name := range []string{"domain", "scope_id", "generation_id", "state", "anchor_kind", "anchor_id"} {
		if !openAPIParametersIncludeName(parameters, name) {
			t.Fatalf("parameters missing %q: %#v", name, parameters)
		}
	}
}

func openAPIParametersIncludeName(parameters []any, name string) bool {
	for _, parameter := range parameters {
		parameterMap, ok := parameter.(map[string]any)
		if ok && parameterMap["name"] == name {
			return true
		}
	}
	return false
}
