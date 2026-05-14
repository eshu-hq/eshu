package query

import (
	"encoding/json"
	"testing"
)

func TestOpenAPIChangeSurfaceInvestigationDocumentsInputFamilies(t *testing.T) {
	t.Parallel()

	var spec map[string]any
	if err := json.Unmarshal([]byte(OpenAPISpec()), &spec); err != nil {
		t.Fatalf("json.Unmarshal(OpenAPISpec()) error = %v, want nil", err)
	}

	paths := mustMapField(t, spec, "paths")
	changeSurfacePath := mustMapField(t, paths, "/api/v0/impact/change-surface/investigate")
	post := mustMapField(t, changeSurfacePath, "post")
	requestBody := mustMapField(t, post, "requestBody")
	content := mustMapField(t, requestBody, "content")
	mediaType := mustMapField(t, content, "application/json")
	schema := mustMapField(t, mediaType, "schema")

	description, ok := schema["description"].(string)
	if !ok || description == "" {
		t.Fatalf("change-surface request description = %#v, want non-empty string", schema["description"])
	}
	anyOf, ok := schema["anyOf"].([]any)
	if !ok {
		t.Fatalf("change-surface request anyOf type = %T, want []any", schema["anyOf"])
	}
	if got, want := len(anyOf), 8; got != want {
		t.Fatalf("change-surface request anyOf count = %d, want %d", got, want)
	}
	if !openAPIAnyOfRequires(anyOf, "target") {
		t.Fatal("change-surface request anyOf missing target requirement")
	}
	if !openAPIAnyOfRequires(anyOf, "changed_paths", "repo_id") {
		t.Fatal("change-surface request anyOf missing changed_paths + repo_id requirement")
	}
	if !openAPIAnyOfRequires(anyOf, "query") {
		t.Fatal("change-surface request anyOf missing query requirement")
	}
}

func openAPIAnyOfRequires(anyOf []any, requiredFields ...string) bool {
	want := map[string]struct{}{}
	for _, field := range requiredFields {
		want[field] = struct{}{}
	}
	for _, candidate := range anyOf {
		candidateMap, ok := candidate.(map[string]any)
		if !ok {
			continue
		}
		required, ok := candidateMap["required"].([]any)
		if !ok || len(required) != len(want) {
			continue
		}
		matched := 0
		for _, field := range required {
			name, ok := field.(string)
			if !ok {
				continue
			}
			if _, ok := want[name]; ok {
				matched++
			}
		}
		if matched == len(want) {
			return true
		}
	}
	return false
}
