// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package call

import (
	"reflect"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/reducer/factload"
	"github.com/eshu-hq/eshu/go/internal/reducer/sharedintent"
)

func TestBuildCodeCallSharedIntentRowsDeduplicatesIntentIdentity(t *testing.T) {
	t.Parallel()

	createdAt := time.Date(2026, time.April, 17, 15, 0, 0, 0, time.UTC)
	contextByRepoID := map[string]sharedintent.ProjectionContext{
		"repo-a": {
			ScopeID:          "scope:git:repo-a",
			AcceptanceUnitID: "repository:repo-a",
			SourceRunID:      "run-a",
			GenerationID:     "gen-a",
		},
	}

	rows := []map[string]any{
		{
			"repo_id":          "repo-a",
			"caller_entity_id": "entity:caller",
			"callee_entity_id": "entity:callee",
			"caller_file":      "caller.py",
			"callee_file":      "callee.py",
			"callsite_line":    10,
			"evidence_source":  EvidenceSource,
			"edge_observation": "callsite-a",
			"source_entity_id": "entity:caller",
			"target_entity_id": "entity:callee",
		},
		{
			"repo_id":          "repo-a",
			"caller_entity_id": "entity:caller",
			"callee_entity_id": "entity:callee",
			"caller_file":      "caller.py",
			"callee_file":      "callee.py",
			"callsite_line":    42,
			"evidence_source":  EvidenceSource,
			"edge_observation": "callsite-b",
			"source_entity_id": "entity:caller",
			"target_entity_id": "entity:callee",
		},
	}

	intents := BuildSharedIntentRows(rows, contextByRepoID, createdAt, EvidenceSource, nil)
	if got, want := len(intents), 1; got != want {
		t.Fatalf("len(intents) = %d, want %d", got, want)
	}
	if got, want := intents[0].IntentID, sharedintent.Build(sharedintent.Input{
		ProjectionDomain: reducercontract.DomainCodeCalls,
		PartitionKey:     "entity:caller->entity:callee",
		ScopeID:          "scope:git:repo-a",
		AcceptanceUnitID: "repository:repo-a",
		RepositoryID:     "repo-a",
		SourceRunID:      "run-a",
		GenerationID:     "gen-a",
		Payload: map[string]any{
			"repo_id":          "repo-a",
			"caller_entity_id": "entity:caller",
			"callee_entity_id": "entity:callee",
			"caller_file":      "caller.py",
			"callee_file":      "callee.py",
			"callsite_line":    10,
			"evidence_source":  EvidenceSource,
			"edge_observation": "callsite-a",
			"source_entity_id": "entity:caller",
			"target_entity_id": "entity:callee",
		},
		CreatedAt: createdAt,
	}).IntentID; got != want {
		t.Fatalf("IntentID = %q, want %q", got, want)
	}
}

func TestBuildCodeCallRefreshIntentsCarriesDeltaFileScope(t *testing.T) {
	t.Parallel()

	createdAt := time.Date(2026, time.June, 13, 8, 0, 0, 0, time.UTC)
	contextByRepoID := map[string]sharedintent.ProjectionContext{
		"repo-a": {
			ScopeID:      "scope-a",
			SourceRunID:  "run-1",
			GenerationID: "gen-2",
		},
	}
	deltaFileScopesByRepoID := map[string]DeltaFileScope{
		"repo-a": {
			filePaths:      []string{"/repo/src/changed.go", "/repo/src/deleted.go"},
			partitionPaths: []string{"src/changed.go", "src/deleted.go"},
		},
	}

	intents := BuildRefreshIntentsWithDeltaFileScopes(contextByRepoID, deltaFileScopesByRepoID, nil, createdAt)
	if got, want := len(intents), 1; got != want {
		t.Fatalf("len(intents) = %d, want %d", got, want)
	}

	payload := intents[0].Payload
	if got, want := payload["delta_projection"], true; got != want {
		t.Fatalf("delta_projection = %#v, want %#v", got, want)
	}
	gotPaths, ok := payload["delta_file_paths"].([]string)
	if !ok {
		t.Fatalf("delta_file_paths type = %T, want []string", payload["delta_file_paths"])
	}
	wantPaths := []string{"/repo/src/changed.go", "/repo/src/deleted.go"}
	if !reflect.DeepEqual(gotPaths, wantPaths) {
		t.Fatalf("delta_file_paths = %#v, want %#v", gotPaths, wantPaths)
	}
}

func TestBuildCodeCallDeltaFilePathsByRepoIDUsesRepositoryDeltaFact(t *testing.T) {
	t.Parallel()

	got := buildCodeCallDeltaFilePathsByRepoID([]facts.Envelope{
		{
			FactKind: factload.FactKindRepository,
			Payload: map[string]any{
				"repo_id":                        "repo-a",
				"path":                           "/repo",
				"delta_generation":               true,
				"delta_relative_paths":           []string{"src/changed.go"},
				"delta_deleted_relative_paths":   []string{"src/deleted.go", "src/changed.go"},
				"unrelated_delta_relative_paths": []string{"src/ignored.go"},
			},
		},
		{
			FactKind: factload.FactKindRepository,
			Payload: map[string]any{
				"repo_id":              "repo-b",
				"path":                 "/repo-b",
				"delta_relative_paths": []string{"src/full-refresh.go"},
			},
		},
	})
	want := map[string][]string{
		"repo-a": {"/repo/src/changed.go", "/repo/src/deleted.go"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("delta file paths = %#v, want %#v", got, want)
	}
}

// TestBuildCodeCallRefreshIntentsDeadLettersUnusableDeltaScope is the #10023
// gap 1 regression. repo-a runs on a delta generation (a member of
// deltaGenerationRepoIDs) but its delta file scope is unusable (absent from
// deltaScopesByRepoID: no local path, or paths that cannot be normalized).
// Falling back to the whole-scope partition key would retract the whole
// repository while only the delta's rows are rewritten. Instead the refresh
// carries delta_projection with empty paths, so collectDeltaFilePaths fails
// the partition closed and the intent dead-letters like the fenced domains.
// A repository on a full generation with no scope keeps the whole-scope
// fallback.
func TestBuildCodeCallRefreshIntentsDeadLettersUnusableDeltaScope(t *testing.T) {
	t.Parallel()

	createdAt := time.Date(2026, time.June, 13, 8, 0, 0, 0, time.UTC)
	contextByRepoID := map[string]sharedintent.ProjectionContext{
		"repo-a": {
			ScopeID:      "scope-a",
			SourceRunID:  "run-1",
			GenerationID: "gen-delta",
		},
		"repo-b": {
			ScopeID:      "scope-b",
			SourceRunID:  "run-1",
			GenerationID: "gen-full",
		},
	}
	deltaGenerationRepoIDs := map[string]struct{}{"repo-a": {}}

	intents := BuildRefreshIntentsWithDeltaFileScopes(contextByRepoID, nil, deltaGenerationRepoIDs, createdAt)
	if got, want := len(intents), 2; got != want {
		t.Fatalf("len(intents) = %d, want %d", got, want)
	}
	byRepo := make(map[string]sharedintent.Row, len(intents))
	for _, intent := range intents {
		byRepo[intent.RepositoryID] = intent
	}

	dead := byRepo["repo-a"].Payload
	if got, want := dead["delta_projection"], true; got != want {
		t.Fatalf("repo-a delta_projection = %#v, want %#v (must fail closed, not whole-scope retract)", got, want)
	}
	if paths, ok := dead["delta_file_paths"].([]string); !ok || len(paths) != 0 {
		t.Fatalf("repo-a delta_file_paths = %#v, want empty (the writer rejects it)", dead["delta_file_paths"])
	}
	if got, want := dead["intent_type"], sharedintent.RepoRefreshIntentType; got != want {
		t.Fatalf("repo-a intent_type = %#v, want %#v", got, want)
	}

	full := byRepo["repo-b"].Payload
	if _, flagged := full["delta_projection"]; flagged {
		t.Fatalf("repo-b carries delta_projection = %#v, want no flag (full generation keeps the whole-scope refresh)", full["delta_projection"])
	}
}

// TestDeltaGenerationRepoIDsCollectsDeltaRepositories proves the emission
// input: repositories whose repository fact carried delta_generation are
// reported whether or not their delta file scope was usable. A non-delta
// repository fact and a fact without an identity never join the set.
func TestDeltaGenerationRepoIDsCollectsDeltaRepositories(t *testing.T) {
	t.Parallel()

	got := DeltaGenerationRepoIDs([]facts.Envelope{
		{
			FactKind: factload.FactKindRepository,
			Payload: map[string]any{
				"repo_id":          "repo-a",
				"delta_generation": true,
			},
		},
		{
			FactKind: factload.FactKindRepository,
			Payload: map[string]any{
				"repo_id":          "repo-b",
				"delta_generation": false,
			},
		},
		{
			FactKind: factload.FactKindFile,
			Payload: map[string]any{
				"repo_id":          "repo-c",
				"delta_generation": true,
			},
		},
	})
	want := map[string]struct{}{"repo-a": {}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("DeltaGenerationRepoIDs() = %#v, want %#v", got, want)
	}
}
