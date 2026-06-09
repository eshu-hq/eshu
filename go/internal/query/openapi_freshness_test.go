package query

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestOpenAPIIncludesFreshnessGenerationsRoute(t *testing.T) {
	t.Parallel()

	spec := OpenAPISpec()
	for _, want := range []string{
		`"/api/v0/freshness/generations"`,
		`"operationId": "listGenerationLifecycle"`,
		`"queue_status"`,
		`"latest_failure"`,
		`"current_active_generation_id"`,
		`"truncated"`,
	} {
		if !strings.Contains(spec, want) {
			t.Fatalf("OpenAPISpec() missing %q", want)
		}
	}
}

func TestOpenAPISpecIsValidJSONWithFreshnessRoute(t *testing.T) {
	t.Parallel()

	var parsed map[string]any
	if err := json.Unmarshal([]byte(OpenAPISpec()), &parsed); err != nil {
		t.Fatalf("OpenAPISpec() is not valid JSON: %v", err)
	}
	paths, ok := parsed["paths"].(map[string]any)
	if !ok {
		t.Fatal("OpenAPISpec() missing paths object")
	}
	if _, ok := paths["/api/v0/freshness/generations"]; !ok {
		t.Fatal("OpenAPISpec() paths missing /api/v0/freshness/generations")
	}
}
