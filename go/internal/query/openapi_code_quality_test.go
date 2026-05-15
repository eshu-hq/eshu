package query

import (
	"encoding/json"
	"testing"
)

func TestOpenAPISpecIncludesCodeQualityInspection(t *testing.T) {
	t.Parallel()

	var spec map[string]any
	if err := json.Unmarshal([]byte(OpenAPISpec()), &spec); err != nil {
		t.Fatalf("OpenAPISpec() JSON error = %v, want nil", err)
	}
	paths := spec["paths"].(map[string]any)
	path, ok := paths["/api/v0/code/quality/inspect"].(map[string]any)
	if !ok {
		t.Fatal("OpenAPISpec() missing /api/v0/code/quality/inspect")
	}
	post := path["post"].(map[string]any)
	if got, want := post["operationId"], "inspectCodeQuality"; got != want {
		t.Fatalf("operationId = %#v, want %#v", got, want)
	}
}

func TestOpenAPICodeQualityMinComplexityDoesNotAdvertiseConflictingDefault(t *testing.T) {
	t.Parallel()

	var spec map[string]any
	if err := json.Unmarshal([]byte(OpenAPISpec()), &spec); err != nil {
		t.Fatalf("OpenAPISpec() JSON error = %v, want nil", err)
	}
	paths := spec["paths"].(map[string]any)
	path := paths["/api/v0/code/quality/inspect"].(map[string]any)
	post := path["post"].(map[string]any)
	requestBody := post["requestBody"].(map[string]any)
	content := requestBody["content"].(map[string]any)
	jsonContent := content["application/json"].(map[string]any)
	schema := jsonContent["schema"].(map[string]any)
	properties := schema["properties"].(map[string]any)
	minComplexity := properties["min_complexity"].(map[string]any)

	if got, ok := minComplexity["default"]; ok {
		t.Fatalf("min_complexity default = %#v, want omitted for server-side check-specific defaults", got)
	}
	if _, ok := minComplexity["description"].(string); !ok {
		t.Fatal("min_complexity description missing, want server-side default guidance")
	}
}
