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

	intents := BuildRefreshIntentsWithDeltaFileScopes(contextByRepoID, deltaFileScopesByRepoID, createdAt)
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
