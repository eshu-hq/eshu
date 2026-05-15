package mcp

import "testing"

func TestResolveRouteMapsInspectCodeQualityToBoundedBody(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("inspect_code_quality", map[string]any{
		"check":         "argument_count",
		"repo_id":       "repo-payments",
		"language":      "go",
		"min_arguments": float64(5),
		"limit":         float64(25),
		"offset":        float64(50),
	})
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	if got, want := route.method, "POST"; got != want {
		t.Fatalf("route.method = %q, want %q", got, want)
	}
	if got, want := route.path, "/api/v0/code/quality/inspect"; got != want {
		t.Fatalf("route.path = %q, want %q", got, want)
	}
	body, ok := route.body.(map[string]any)
	if !ok {
		t.Fatalf("route.body type = %T, want map[string]any", route.body)
	}
	for key, want := range map[string]any{
		"check":         "argument_count",
		"repo_id":       "repo-payments",
		"language":      "go",
		"min_arguments": 5,
		"limit":         25,
		"offset":        50,
	} {
		if got := body[key]; got != want {
			t.Fatalf("body[%s] = %#v, want %#v", key, got, want)
		}
	}
}

func TestInspectCodeQualityToolMinComplexityDoesNotAdvertiseConflictingDefault(t *testing.T) {
	t.Parallel()

	tool := codeQualityInspectionTool()
	schema := tool.InputSchema.(map[string]any)
	properties := schema["properties"].(map[string]any)
	minComplexity := properties["min_complexity"].(map[string]any)

	if got, ok := minComplexity["default"]; ok {
		t.Fatalf("min_complexity default = %#v, want omitted for server-side check-specific defaults", got)
	}
	if _, ok := minComplexity["description"].(string); !ok {
		t.Fatal("min_complexity description missing, want server-side default guidance")
	}
}
