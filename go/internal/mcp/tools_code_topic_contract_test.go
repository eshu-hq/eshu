package mcp

import "testing"

func TestInvestigateCodeTopicToolAdvertisesBoundedContract(t *testing.T) {
	t.Parallel()

	var tool *ToolDefinition
	for _, candidate := range codebaseTools() {
		if candidate.Name == "investigate_code_topic" {
			candidate := candidate
			tool = &candidate
			break
		}
	}
	if tool == nil {
		t.Fatal("investigate_code_topic tool is not registered")
	}

	inputSchema, ok := tool.InputSchema.(map[string]any)
	if !ok {
		t.Fatalf("input schema type = %T, want map", tool.InputSchema)
	}
	properties, ok := inputSchema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("properties type = %T, want map", inputSchema["properties"])
	}
	for _, field := range []string{"topic", "intent", "repo_id", "language", "limit", "offset"} {
		if _, ok := properties[field]; !ok {
			t.Fatalf("investigate_code_topic schema missing %q", field)
		}
	}
	limit, ok := properties["limit"].(map[string]any)
	if !ok {
		t.Fatalf("limit schema type = %T, want map", properties["limit"])
	}
	if got, want := limit["maximum"], 200; got != want {
		t.Fatalf("limit.maximum = %#v, want %#v", got, want)
	}
}

func TestResolveRouteMapsInvestigateCodeTopic(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("investigate_code_topic", map[string]any{
		"topic":    "repo sync authentication",
		"intent":   "explain auth flow",
		"repo_id":  "repo-1",
		"language": "go",
		"limit":    float64(25),
		"offset":   float64(50),
	})
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	if got, want := route.method, "POST"; got != want {
		t.Fatalf("route.method = %q, want %q", got, want)
	}
	if got, want := route.path, "/api/v0/code/topics/investigate"; got != want {
		t.Fatalf("route.path = %q, want %q", got, want)
	}
	body, ok := route.body.(map[string]any)
	if !ok {
		t.Fatalf("route.body type = %T, want map[string]any", route.body)
	}
	for key, want := range map[string]any{
		"topic":    "repo sync authentication",
		"intent":   "explain auth flow",
		"repo_id":  "repo-1",
		"language": "go",
		"limit":    25,
		"offset":   50,
	} {
		if got := body[key]; got != want {
			t.Fatalf("body[%s] = %#v, want %#v", key, got, want)
		}
	}
}
