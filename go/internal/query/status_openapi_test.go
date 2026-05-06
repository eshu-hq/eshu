package query

import (
	"encoding/json"
	"testing"
)

func TestOpenAPISpecStatusPathsMatchCurrentContract(t *testing.T) {
	t.Parallel()

	var spec map[string]any
	if err := json.Unmarshal([]byte(OpenAPISpec()), &spec); err != nil {
		t.Fatalf("json.Unmarshal(OpenAPISpec()) error = %v, want nil", err)
	}

	paths := mustMapField(t, spec, "paths")
	if _, ok := paths["/api/v0/index-status"]; !ok {
		t.Fatal("OpenAPI paths missing /api/v0/index-status")
	}
	if _, ok := paths["/api/v0/ingesters"]; !ok {
		t.Fatal("OpenAPI paths missing /api/v0/ingesters")
	}
	if _, ok := paths["/api/v0/ingesters/{ingester}"]; !ok {
		t.Fatal("OpenAPI paths missing /api/v0/ingesters/{ingester}")
	}
	if _, ok := paths["/api/v0/index-runs/{run_id}"]; ok {
		t.Fatal("OpenAPI paths unexpectedly advertise /api/v0/index-runs/{run_id}")
	}
	if _, ok := paths["/api/v0/index-runs/{run_id}/coverage"]; ok {
		t.Fatal("OpenAPI paths unexpectedly advertise /api/v0/index-runs/{run_id}/coverage")
	}
}
