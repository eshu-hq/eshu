package query

import "testing"

func TestBuildRepositorySemanticOverviewCountsTypeScriptAdvancedSignals(t *testing.T) {
	t.Parallel()

	overview := buildRepositorySemanticOverview([]EntityContent{
		{
			EntityID:   "alias-1",
			RepoID:     "repo-1",
			EntityType: "TypeAlias",
			EntityName: "ReadonlyMap",
			Language:   "typescript",
			Metadata: map[string]any{
				"type_alias_kind": "mapped_type",
				"type_parameters": []any{"T"},
			},
		},
		{
			EntityID:   "alias-2",
			RepoID:     "repo-1",
			EntityType: "TypeAlias",
			EntityName: "Response",
			Language:   "typescript",
			Metadata: map[string]any{
				"type_alias_kind": "conditional_type",
				"type_parameters": []any{"T"},
			},
		},
		{
			EntityID:   "module-1",
			RepoID:     "repo-1",
			EntityType: "Module",
			EntityName: "API",
			Language:   "typescript",
			Metadata: map[string]any{
				"module_kind": "namespace",
			},
		},
		{
			EntityID:   "class-1",
			RepoID:     "repo-1",
			EntityType: "Class",
			EntityName: "Service",
			Language:   "typescript",
			Metadata: map[string]any{
				"declaration_merge_group": "Service",
				"declaration_merge_count": 2,
				"declaration_merge_kinds": []any{"class", "namespace"},
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
	if got, want := signalCounts["mapped_type"], 1; got != want {
		t.Fatalf("signal_counts[mapped_type] = %d, want %d", got, want)
	}
	if got, want := signalCounts["conditional_type"], 1; got != want {
		t.Fatalf("signal_counts[conditional_type] = %d, want %d", got, want)
	}
	if got, want := signalCounts["namespace"], 1; got != want {
		t.Fatalf("signal_counts[namespace] = %d, want %d", got, want)
	}
	if got, want := signalCounts["declaration_merge"], 1; got != want {
		t.Fatalf("signal_counts[declaration_merge] = %d, want %d", got, want)
	}

	surfaceKinds, ok := overview["surface_kind_counts"].(map[string]int)
	if !ok {
		t.Fatalf("surface_kind_counts type = %T, want map[string]int", overview["surface_kind_counts"])
	}
	if got, want := surfaceKinds["mapped_type_alias"], 1; got != want {
		t.Fatalf("surface_kind_counts[mapped_type_alias] = %d, want %d", got, want)
	}
	if got, want := surfaceKinds["conditional_type_alias"], 1; got != want {
		t.Fatalf("surface_kind_counts[conditional_type_alias] = %d, want %d", got, want)
	}
	if got, want := surfaceKinds["namespace_module"], 1; got != want {
		t.Fatalf("surface_kind_counts[namespace_module] = %d, want %d", got, want)
	}
	if got, want := surfaceKinds["declaration_merge"], 1; got != want {
		t.Fatalf("surface_kind_counts[declaration_merge] = %d, want %d", got, want)
	}
}
