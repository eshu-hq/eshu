// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

const (
	goldenServiceChangedSincePriorSentinel   = "__runtime_service_changed_since_prior_generation__"
	goldenServiceChangedSinceCurrentSentinel = "__runtime_service_changed_since_current_generation__"
	goldenServiceChangedSinceOldOwnerKey     = "ownership:component:default/deployable-config:group:default/platform"
	goldenServiceChangedSinceNewOwnerKey     = "ownership:component:default/deployable-config:group:default/runtime-platform"
)

func TestGoldenServiceChangedSinceLeafUsesRealCorpusAndReadOnlyLineage(t *testing.T) {
	t.Parallel()

	repoRoot := filepath.Join("..", "..", "..")
	helperPath := filepath.Join(repoRoot, "scripts", "lib", "golden-corpus-service-changed-since.sh")
	body, err := os.ReadFile(helperPath)
	if err != nil {
		t.Fatalf("read %s: %v", helperPath, err)
	}
	helper := string(body)
	for _, want := range []string{
		"golden_service_changed_since_capture_prior",
		"golden_service_changed_since_mutate_owner",
		"golden_service_changed_since_validate_current",
		"golden_service_changed_since_compose_snapshot",
		"${corpus_dir}/deployable-config/catalog-info.yaml",
		"group:default/runtime-platform",
		"status = 'superseded'",
		"status = 'active'",
		goldenServiceChangedSincePriorSentinel,
		goldenServiceChangedSinceCurrentSentinel,
	} {
		if !strings.Contains(helper, want) {
			t.Errorf("service changed-since leaf helper missing %q", want)
		}
	}
	for _, forbidden := range []string{
		"INSERT INTO service_materialization_generations",
		"UPDATE service_materialization_generations",
		"INSERT INTO service_evidence_snapshots",
		"UPDATE service_evidence_snapshots",
		"run_maintenance_drain_cycles",
	} {
		if strings.Contains(helper, forbidden) {
			t.Errorf("service changed-since leaf helper contains forbidden bypass/orchestration %q", forbidden)
		}
	}
}

func TestGoldenSnapshotServiceChangedSinceIsNonVacuous(t *testing.T) {
	t.Parallel()

	snapshot, err := LoadSnapshot(goldenSnapshotPath())
	if err != nil {
		t.Fatalf("LoadSnapshot() error = %v", err)
	}
	shape := snapshot.QueryShapes.MCP["get_service_changed_since"]
	if got, want := shape.MinimumResults, 7; got != want {
		t.Errorf("MinimumResults = %d, want %d", got, want)
	}
	if got, want := shape.MaximumResults, 7; got != want {
		t.Errorf("MaximumResults = %d, want %d", got, want)
	}
	if got, want := shape.ResultsField, "categories"; got != want {
		t.Errorf("ResultsField = %q, want %q", got, want)
	}
	wantArguments := map[string]any{
		"service_id":          "component:default/deployable-config",
		"since_generation_id": goldenServiceChangedSincePriorSentinel,
		"sample_limit":        float64(10),
	}
	if !reflect.DeepEqual(shape.Arguments, wantArguments) {
		t.Errorf("Arguments = %#v, want %#v", shape.Arguments, wantArguments)
	}
	for path, want := range map[string]any{
		"service_id":                   "component:default/deployable-config",
		"since_generation_id":          goldenServiceChangedSincePriorSentinel,
		"current_active_generation_id": goldenServiceChangedSinceCurrentSentinel,
		"sample_limit":                 float64(10),
		"unavailable":                  false,
	} {
		if got := shape.RequiredJSONValues[path]; !reflect.DeepEqual(got, want) {
			t.Errorf("RequiredJSONValues[%q] = %#v, want %#v", path, got, want)
		}
	}
	wantMatches := []map[string]any{
		{
			"category": "ownership",
			"counts": map[string]any{
				"added": float64(1), "updated": float64(0), "unchanged": float64(0),
				"retired": float64(0), "superseded": float64(1),
			},
			"samples": map[string]any{
				"added": []any{map[string]any{
					"stable_fact_key": goldenServiceChangedSinceNewOwnerKey,
					"fact_kind":       "ownership",
				}},
				"superseded": []any{map[string]any{
					"stable_fact_key": goldenServiceChangedSinceOldOwnerKey,
					"fact_kind":       "ownership",
				}},
			},
			"unavailable": false,
		},
		{
			"category": "deployment",
			"counts": map[string]any{
				"added": float64(0), "updated": float64(0), "unchanged": float64(0),
				"retired": float64(0), "superseded": float64(0),
			},
			"unavailable": false,
		},
	}
	if got := shape.RequiredJSONObjectMatches["categories[]"]; !reflect.DeepEqual(got, wantMatches) {
		t.Errorf("RequiredJSONObjectMatches[categories[]] = %#v, want %#v", got, wantMatches)
	}
}
