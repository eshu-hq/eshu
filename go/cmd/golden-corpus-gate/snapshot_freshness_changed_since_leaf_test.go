// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
)

const (
	goldenChangedSinceScopeID         = "git-repository-scope:repository:r_b11b6e25"
	goldenChangedSinceStableFactKey   = "content:repository:r_b11b6e25:config/freshness.cfg"
	goldenChangedSincePriorSentinel   = "__runtime_changed_since_prior_generation__"
	goldenChangedSinceCurrentSentinel = "__runtime_changed_since_current_generation__"
	goldenChangedSinceAddedSentinel   = "__runtime_changed_since_facts_added_count__"
	goldenChangedSinceUpdatedSentinel = "__runtime_changed_since_facts_updated_count__"
	goldenChangedSinceSameSentinel    = "__runtime_changed_since_facts_unchanged_count__"
	goldenChangedSinceRetiredSentinel = "__runtime_changed_since_facts_retired_count__"
	goldenChangedSinceGoneSentinel    = "__runtime_changed_since_facts_superseded_count__"
)

func TestGoldenChangedSinceLeafUsesPublicSourceAndReadOnlyLineage(t *testing.T) {
	t.Parallel()

	repoRoot := filepath.Join("..", "..", "..")
	fixturePath := filepath.Join(repoRoot, "tests", "fixtures", "ecosystems", "supply-chain-demo-db", "config", "freshness.cfg")
	fixture, err := os.ReadFile(fixturePath)
	if err != nil {
		t.Fatalf("read %s: %v", fixturePath, err)
	}
	if got, want := string(fixture), "release_marker = \"baseline\"\n"; got != want {
		t.Fatalf("fixture bytes = %q, want %q", got, want)
	}

	helperPath := filepath.Join(repoRoot, "scripts", "lib", "golden-corpus-changed-since.sh")
	body, err := os.ReadFile(helperPath)
	if err != nil {
		t.Fatalf("read %s: %v", helperPath, err)
	}
	helper := string(body)
	for _, want := range []string{
		"golden_changed_since_capture_prior",
		"golden_changed_since_mutate_fixture",
		"golden_changed_since_validate_current",
		"golden_changed_since_compose_snapshot",
		"${corpus_dir}/supply-chain-demo-db/config/freshness.cfg",
		goldenChangedSinceScopeID,
		goldenChangedSinceStableFactKey,
		goldenChangedSincePriorSentinel,
		goldenChangedSinceCurrentSentinel,
		goldenChangedSinceAddedSentinel,
		goldenChangedSinceUpdatedSentinel,
		goldenChangedSinceSameSentinel,
		goldenChangedSinceRetiredSentinel,
		goldenChangedSinceGoneSentinel,
	} {
		if !strings.Contains(helper, want) {
			t.Errorf("repository changed-since helper missing %q", want)
		}
	}
	for _, forbidden := range []string{
		"INSERT INTO",
		"UPDATE ingestion_scopes",
		"UPDATE scope_generations",
		"DELETE FROM",
		"run_maintenance_drain_cycles",
	} {
		if strings.Contains(helper, forbidden) {
			t.Errorf("repository changed-since helper contains forbidden write/orchestration %q", forbidden)
		}
	}
}

func TestGoldenSnapshotChangedSincePinsRealUpdatedContent(t *testing.T) {
	t.Parallel()

	snapshot, err := LoadSnapshot(goldenSnapshotPath())
	if err != nil {
		t.Fatalf("LoadSnapshot() error = %v", err)
	}
	shape := snapshot.QueryShapes.MCP["get_changed_since"]
	if got, want := shape.MinimumResults, 3; got != want {
		t.Errorf("MinimumResults = %d, want %d", got, want)
	}
	if got, want := shape.MaximumResults, 3; got != want {
		t.Errorf("MaximumResults = %d, want %d", got, want)
	}
	if got, want := shape.ResultsField, "categories"; got != want {
		t.Errorf("ResultsField = %q, want %q", got, want)
	}
	wantArguments := map[string]any{
		"scope_id":            goldenChangedSinceScopeID,
		"since_generation_id": goldenChangedSincePriorSentinel,
		"sample_limit":        float64(200),
	}
	if !reflect.DeepEqual(shape.Arguments, wantArguments) {
		t.Errorf("Arguments = %#v, want %#v", shape.Arguments, wantArguments)
	}
	for _, field := range []string{
		"scope_id", "scope_kind", "since_generation_id", "current_active_generation_id",
		"sample_limit", "categories", "unavailable",
	} {
		if !slices.Contains(shape.RequiredResponseFields, field) {
			t.Errorf("RequiredResponseFields missing %q", field)
		}
	}
	for path, want := range map[string]any{
		"scope_id":                     goldenChangedSinceScopeID,
		"scope_kind":                   "repository",
		"since_generation_id":          goldenChangedSincePriorSentinel,
		"current_active_generation_id": goldenChangedSinceCurrentSentinel,
		"sample_limit":                 float64(200),
		"unavailable":                  false,
	} {
		if got := shape.RequiredJSONValues[path]; !reflect.DeepEqual(got, want) {
			t.Errorf("RequiredJSONValues[%q] = %#v, want %#v", path, got, want)
		}
	}
	wantUpdated := []map[string]any{{
		"stable_fact_key": goldenChangedSinceStableFactKey,
		"fact_kind":       "content",
	}}
	if got := shape.RequiredJSONObjectMatches["categories[].samples.updated[]"]; !reflect.DeepEqual(got, wantUpdated) {
		t.Errorf("updated sample matches = %#v, want %#v", got, wantUpdated)
	}
	wantFacts := []map[string]any{{
		"category": "facts",
		"counts": map[string]any{
			"added":      goldenChangedSinceAddedSentinel,
			"updated":    goldenChangedSinceUpdatedSentinel,
			"unchanged":  goldenChangedSinceSameSentinel,
			"retired":    goldenChangedSinceRetiredSentinel,
			"superseded": goldenChangedSinceGoneSentinel,
		},
	}}
	if got := shape.RequiredJSONObjectMatches["categories[]"]; !reflect.DeepEqual(got, wantFacts) {
		t.Errorf("facts category matches = %#v, want %#v", got, wantFacts)
	}
}

func TestGoldenSnapshotChangedSinceBITES(t *testing.T) {
	t.Parallel()

	snapshot, err := LoadSnapshot(goldenSnapshotPath())
	if err != nil {
		t.Fatalf("LoadSnapshot() error = %v", err)
	}
	shape := snapshot.QueryShapes.MCP["get_changed_since"]
	runtimeShape := shape
	runtimeShape.RequiredJSONObjectMatches = map[string][]map[string]any{
		"categories[]": {{
			"category": "facts",
			"counts": map[string]any{
				"added": float64(0), "updated": float64(13), "unchanged": float64(4),
				"retired": float64(0), "superseded": float64(0),
			},
		}},
		"categories[].samples.updated[]": shape.RequiredJSONObjectMatches["categories[].samples.updated[]"],
	}
	positive := []byte(`{"scope_id":"git-repository-scope:repository:r_b11b6e25","scope_kind":"repository","since_generation_id":"__runtime_changed_since_prior_generation__","current_active_generation_id":"__runtime_changed_since_current_generation__","sample_limit":200,"categories":[{"category":"files","counts":{"added":0,"updated":2,"unchanged":1,"retired":0,"superseded":0},"unavailable":false},{"category":"content_entities","counts":{"added":0,"updated":3,"unchanged":1,"retired":0,"superseded":0},"unavailable":false},{"category":"facts","counts":{"added":0,"updated":13,"unchanged":4,"retired":0,"superseded":0},"samples":{"updated":[{"stable_fact_key":"content:repository:r_b11b6e25:config/freshness.cfg","fact_kind":"content"}]},"unavailable":false}],"unavailable":false}`)
	if finding := EvaluateQueryShape("changed-since-positive", runtimeShape, positive); !finding.OK {
		t.Fatalf("positive changed-since response failed: %+v", finding)
	}
	empty := []byte(`{"scope_id":"git-repository-scope:repository:r_b11b6e25","scope_kind":"repository","since_generation_id":"__runtime_changed_since_prior_generation__","current_active_generation_id":"__runtime_changed_since_current_generation__","sample_limit":200,"categories":[],"unavailable":false}`)
	if finding := EvaluateQueryShape("changed-since-empty", runtimeShape, empty); finding.OK {
		t.Fatalf("empty changed-since response passed: %+v", finding)
	}
	wrongKey := []byte(strings.Replace(string(positive), goldenChangedSinceStableFactKey, "content:repository:r_b11b6e25:config/unrelated.cfg", 1))
	if finding := EvaluateQueryShape("changed-since-wrong-key", runtimeShape, wrongKey); finding.OK {
		t.Fatalf("unrelated updated fact passed: %+v", finding)
	}
	wrongCounts := []byte(strings.Replace(string(positive), `"updated":13`, `"updated":12`, 1))
	if finding := EvaluateQueryShape("changed-since-wrong-counts", runtimeShape, wrongCounts); finding.OK {
		t.Fatalf("wrong facts count bucket passed: %+v", finding)
	}
}
