package query

import "testing"

func TestBuildRepositorySemanticOverviewCountsTSXAdvancedSignals(t *testing.T) {
	t.Parallel()

	overview := buildRepositorySemanticOverview([]EntityContent{
		{
			EntityID:   "component-1",
			RepoID:     "repo-1",
			EntityType: "Component",
			EntityName: "Screen",
			Language:   "tsx",
			Metadata: map[string]any{
				"framework":              "react",
				"jsx_fragment_shorthand": true,
			},
		},
		{
			EntityID:   "component-2",
			RepoID:     "repo-1",
			EntityType: "Component",
			EntityName: "MemoButton",
			Language:   "tsx",
			Metadata: map[string]any{
				"framework":              "react",
				"component_wrapper_kind": "memo",
			},
		},
		{
			EntityID:   "variable-1",
			RepoID:     "repo-1",
			EntityType: "Variable",
			EntityName: "Dynamic",
			Language:   "tsx",
			Metadata: map[string]any{
				"component_type_assertion": "ComponentType",
			},
		},
	})

	if overview == nil {
		t.Fatal("buildRepositorySemanticOverview() = nil, want non-nil")
	}

	signalCounts, ok := overview["signal_counts"].(map[string]int)
	if !ok {
		t.Fatalf("signal_counts type = %T, want map[string]int", overview["signal_counts"])
	}
	if got, want := signalCounts["jsx_fragment"], 1; got != want {
		t.Fatalf("signal_counts[jsx_fragment] = %d, want %d", got, want)
	}
	if got, want := signalCounts["component_type_assertion"], 1; got != want {
		t.Fatalf("signal_counts[component_type_assertion] = %d, want %d", got, want)
	}
	if got, want := signalCounts["component_wrapper_kind"], 1; got != want {
		t.Fatalf("signal_counts[component_wrapper_kind] = %d, want %d", got, want)
	}

	surfaceKinds, ok := overview["surface_kind_counts"].(map[string]int)
	if !ok {
		t.Fatalf("surface_kind_counts type = %T, want map[string]int", overview["surface_kind_counts"])
	}
	if got, want := surfaceKinds["framework_component"], 1; got != want {
		t.Fatalf("surface_kind_counts[framework_component] = %d, want %d", got, want)
	}
	if got, want := surfaceKinds["component_wrapper"], 1; got != want {
		t.Fatalf("surface_kind_counts[component_wrapper] = %d, want %d", got, want)
	}
	if got, want := surfaceKinds["component_type_assertion"], 1; got != want {
		t.Fatalf("surface_kind_counts[component_type_assertion] = %d, want %d", got, want)
	}
}
