package mcp

import "testing"

func TestInvestigateHardcodedSecretsToolAdvertisesBoundedContract(t *testing.T) {
	t.Parallel()

	var tool *ToolDefinition
	for _, candidate := range codebaseTools() {
		if candidate.Name == "investigate_hardcoded_secrets" {
			tool = &candidate
			break
		}
	}
	if tool == nil {
		t.Fatal("investigate_hardcoded_secrets tool is not registered")
	}
	schema, ok := tool.InputSchema.(map[string]any)
	if !ok {
		t.Fatalf("InputSchema type = %T, want map", tool.InputSchema)
	}
	properties, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("properties type = %T, want map", schema["properties"])
	}
	for _, field := range []string{"repo_id", "language", "finding_kinds", "include_suppressed", "limit", "offset"} {
		if _, ok := properties[field]; !ok {
			t.Fatalf("investigate_hardcoded_secrets schema missing %q", field)
		}
	}
	limit := properties["limit"].(map[string]any)
	if got, want := limit["maximum"], 200; got != want {
		t.Fatalf("limit.maximum = %#v, want %#v", got, want)
	}
	offset := properties["offset"].(map[string]any)
	if got, want := offset["maximum"], 10000; got != want {
		t.Fatalf("offset.maximum = %#v, want %#v", got, want)
	}
}

func TestResolveRouteMapsInvestigateHardcodedSecrets(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("investigate_hardcoded_secrets", map[string]any{
		"repo_id":            "repo-1",
		"language":           "go",
		"finding_kinds":      []any{"api_token", "password_literal"},
		"include_suppressed": true,
		"limit":              25,
		"offset":             50,
	})
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	if got, want := route.method, "POST"; got != want {
		t.Fatalf("route.method = %q, want %q", got, want)
	}
	if got, want := route.path, "/api/v0/code/security/secrets/investigate"; got != want {
		t.Fatalf("route.path = %q, want %q", got, want)
	}
	body := route.body.(map[string]any)
	if got, want := body["repo_id"], "repo-1"; got != want {
		t.Fatalf("body.repo_id = %#v, want %#v", got, want)
	}
	if got, want := body["include_suppressed"], true; got != want {
		t.Fatalf("body.include_suppressed = %#v, want %#v", got, want)
	}
	kinds := body["finding_kinds"].([]any)
	if got, want := len(kinds), 2; got != want {
		t.Fatalf("finding_kinds length = %d, want %d", got, want)
	}
}
