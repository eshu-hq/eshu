package query

import (
	"encoding/json"
	"testing"
)

func TestOpenAPISpecAdminPathsMatchMountedContract(t *testing.T) {
	t.Parallel()

	var spec map[string]any
	if err := json.Unmarshal([]byte(OpenAPISpec()), &spec); err != nil {
		t.Fatalf("json.Unmarshal(OpenAPISpec()) error = %v, want nil", err)
	}

	paths := mustMapField(t, spec, "paths")
	expectedPaths := []string{
		"/api/v0/admin/refinalize",
		"/api/v0/admin/reindex",
		"/api/v0/admin/shared-projection/tuning-report",
		"/api/v0/admin/work-items/query",
		"/api/v0/admin/decisions/query",
		"/api/v0/admin/dead-letter",
		"/api/v0/admin/skip",
		"/api/v0/admin/replay",
		"/api/v0/admin/backfill",
		"/api/v0/admin/replay-events/query",
	}

	for _, path := range expectedPaths {
		if _, ok := paths[path]; !ok {
			t.Fatalf("OpenAPI paths missing %s", path)
		}
	}
}
