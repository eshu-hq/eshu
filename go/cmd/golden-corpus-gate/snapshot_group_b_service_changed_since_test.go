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
	goldenServiceChangedSincePriorSentinel      = "__runtime_service_changed_since_prior_generation__"
	goldenServiceChangedSinceCurrentSentinel    = "__runtime_service_changed_since_current_generation__"
	goldenServiceChangedSinceAddedSentinel      = "__runtime_service_changed_since_deployment_added_count__"
	goldenServiceChangedSinceUpdatedSentinel    = "__runtime_service_changed_since_deployment_updated_count__"
	goldenServiceChangedSinceUnchangedSentinel  = "__runtime_service_changed_since_deployment_unchanged_count__"
	goldenServiceChangedSinceRetiredSentinel    = "__runtime_service_changed_since_deployment_retired_count__"
	goldenServiceChangedSinceSupersededSentinel = "__runtime_service_changed_since_deployment_superseded_count__"
	goldenServiceChangedSinceOldOwnerKey        = "ownership:component:default/deployable-config:group:default/platform"
	goldenServiceChangedSinceNewOwnerKey        = "ownership:component:default/deployable-config:group:default/runtime-platform"
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
		"golden_service_changed_since_capture_deployment_counts",
		"golden_service_changed_since_compose_snapshot",
		"${corpus_dir}/deployable-config/catalog-info.yaml",
		"group:default/runtime-platform",
		"status = 'superseded'",
		"status = 'active'",
		goldenServiceChangedSincePriorSentinel,
		goldenServiceChangedSinceCurrentSentinel,
		goldenServiceChangedSinceAddedSentinel,
		goldenServiceChangedSinceUpdatedSentinel,
		goldenServiceChangedSinceUnchangedSentinel,
		goldenServiceChangedSinceRetiredSentinel,
		goldenServiceChangedSinceSupersededSentinel,
		"WITH prior_keys AS",
		"current_active_keys AS",
		"current_tombstones AS",
		"prior.payload_hash IS DISTINCT FROM current.payload_hash",
		"evidence_family = 'deployment'",
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
	if !shape.Envelope {
		t.Error("changed-since shape must preserve the semantic truth envelope")
	}
	if got := shape.RequiredJSONValues["truth.basis"]; got != "semantic_facts" {
		t.Errorf("truth basis = %#v, want semantic_facts", got)
	}
	bound, ok := shape.RequiredJSONArrayBounds["data.categories"]
	if !ok || bound.MinimumResults != 7 || bound.MaximumResults != 7 ||
		!reflect.DeepEqual(bound.ResultItemRequiredFields, []string{"category", "counts", "unavailable"}) {
		t.Errorf("nested category bound = %+v (present=%t), want seven typed categories", bound, ok)
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
		"data.service_id":                   "component:default/deployable-config",
		"data.since_generation_id":          goldenServiceChangedSincePriorSentinel,
		"data.current_active_generation_id": goldenServiceChangedSinceCurrentSentinel,
		"data.sample_limit":                 float64(10),
		"data.unavailable":                  false,
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
				"added": goldenServiceChangedSinceAddedSentinel, "updated": goldenServiceChangedSinceUpdatedSentinel,
				"unchanged": goldenServiceChangedSinceUnchangedSentinel, "retired": goldenServiceChangedSinceRetiredSentinel,
				"superseded": goldenServiceChangedSinceSupersededSentinel,
			},
			"unavailable": false,
		},
	}
	// #7816: the owner mutation changes nothing about the deployable-config
	// workloads, so both generations carry the same two WorkloadInstance rows.
	// added=0 pins that the prior generation was not committed from a runtime
	// read taken before the producer's WorkloadInstance MERGE.
	wantMatches = append(wantMatches, map[string]any{
		"category": "runtime",
		"counts": map[string]any{
			"added": float64(0), "updated": float64(0), "unchanged": float64(2),
			"retired": float64(0), "superseded": float64(0),
		},
		"unavailable": false,
	})
	for _, category := range []string{"dependencies", "docs", "incidents", "vulnerabilities"} {
		wantMatches = append(wantMatches, map[string]any{"category": category})
	}
	if got := shape.RequiredJSONObjectMatches["data.categories[]"]; !reflect.DeepEqual(got, wantMatches) {
		t.Errorf("RequiredJSONObjectMatches[data.categories[]] = %#v, want %#v", got, wantMatches)
	}
}
