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
