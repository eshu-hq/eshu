package mcp

import "testing"

func TestNoCachePromptToolsAdvertiseBounds(t *testing.T) {
	t.Parallel()

	for _, name := range []string{
		"analyze_code_relationships",
		"find_blast_radius",
		"find_change_surface",
		"trace_resource_to_code",
		"compare_environments",
	} {
		tool := requireMCPTool(t, name)
		properties := tool.InputSchema.(map[string]any)["properties"].(map[string]any)
		limit, ok := properties["limit"].(map[string]any)
		if !ok {
			t.Fatalf("%s schema missing integer limit", name)
		}
		if got, want := limit["maximum"], 200; got != want {
			t.Fatalf("%s limit maximum = %#v, want %#v", name, got, want)
		}
	}
}

func TestAnalyzeCodeRelationshipsSchemaRequiresTargetExceptRepoScopedOverrides(t *testing.T) {
	t.Parallel()

	tool := requireMCPTool(t, "analyze_code_relationships")
	schema := tool.InputSchema.(map[string]any)
	anyOf, ok := schema["anyOf"].([]map[string]any)
	if !ok {
		t.Fatalf("analyze_code_relationships schema anyOf type = %T, want []map[string]any", schema["anyOf"])
	}
	if len(anyOf) != 2 {
		t.Fatalf("analyze_code_relationships schema anyOf len = %d, want 2", len(anyOf))
	}
	targetRequired := anyOf[0]["required"].([]string)
	if len(targetRequired) != 2 || targetRequired[0] != "query_type" || targetRequired[1] != "target" {
		t.Fatalf("target branch required = %#v, want query_type,target", targetRequired)
	}
	overrideRequired := anyOf[1]["required"].([]string)
	if len(overrideRequired) != 2 || overrideRequired[0] != "query_type" || overrideRequired[1] != "repo_id" {
		t.Fatalf("override branch required = %#v, want query_type,repo_id", overrideRequired)
	}
	properties := anyOf[1]["properties"].(map[string]any)
	queryType := properties["query_type"].(map[string]any)
	enum := queryType["enum"].([]string)
	if len(enum) != 1 || enum[0] != "overrides" {
		t.Fatalf("override branch query_type enum = %#v, want [overrides]", enum)
	}
}

func TestNoCachePromptRoutesPassBounds(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		args map[string]any
	}{
		{
			name: "analyze_code_relationships",
			args: map[string]any{
				"query_type": "find_callers",
				"target":     "handler",
				"limit":      float64(25),
			},
		},
		{
			name: "find_blast_radius",
			args: map[string]any{
				"target":      "payments",
				"target_type": "repository",
				"limit":       float64(25),
			},
		},
		{
			name: "find_change_surface",
			args: map[string]any{
				"target": "workload:api",
				"limit":  float64(25),
			},
		},
		{
			name: "trace_resource_to_code",
			args: map[string]any{
				"start":     "resource:queue",
				"max_depth": float64(4),
				"limit":     float64(25),
			},
		},
		{
			name: "compare_environments",
			args: map[string]any{
				"workload_id": "workload:api",
				"left":        "qa",
				"right":       "prod",
				"limit":       float64(25),
			},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			route, err := resolveRoute(tt.name, tt.args)
			if err != nil {
				t.Fatalf("resolveRoute() error = %v, want nil", err)
			}
			body, ok := route.body.(map[string]any)
			if !ok {
				t.Fatalf("route.body type = %T, want map[string]any", route.body)
			}
			if got, want := body["limit"], 25; got != want {
				t.Fatalf("body[limit] = %#v, want %#v", got, want)
			}
		})
	}
}

func requireMCPTool(t *testing.T, name string) ToolDefinition {
	t.Helper()

	for _, tool := range ReadOnlyTools() {
		if tool.Name == name {
			return tool
		}
	}
	t.Fatalf("tool %q is not registered", name)
	return ToolDefinition{}
}
