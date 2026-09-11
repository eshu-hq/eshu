// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package call

import (
	"reflect"
	"testing"
	"time"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/reducer/sharedintent"
)

func TestBuildCodeCallSharedIntentRowsCarriesDeltaPartitionForSourceFile(t *testing.T) {
	t.Parallel()

	createdAt := time.Date(2026, time.June, 14, 16, 0, 0, 0, time.UTC)
	contextByRepoID := map[string]sharedintent.ProjectionContext{
		"repo-a": {
			ScopeID:          "scope:git:repo-a",
			AcceptanceUnitID: "repository:repo-a",
			SourceRunID:      "run-a",
			GenerationID:     "gen-a",
		},
	}
	deltaFileScopesByRepoID := map[string]DeltaFileScope{
		"repo-a": {
			filePaths:      []string{"/repo/src/caller.go", "/repo/src/models.py"},
			partitionPaths: []string{"src/caller.go", "src/models.py"},
		},
	}

	rows := []map[string]any{
		{
			"repo_id":           "repo-a",
			"caller_entity_id":  "entity:caller",
			"callee_entity_id":  "entity:callee",
			"caller_file":       "src/caller.go",
			"callee_file":       "src/callee.go",
			"relationship_type": "CALLS",
			"action":            reducercontract.IntentActionUpsert,
		},
		{
			"repo_id":           "repo-a",
			"caller_entity_id":  "entity:caller",
			"callee_entity_id":  "entity:callee",
			"caller_file":       "src/caller.go",
			"callee_file":       "src/callee.go",
			"relationship_type": "INSTANTIATES",
			"action":            reducercontract.IntentActionUpsert,
		},
		{
			"repo_id":           "repo-a",
			"caller_entity_id":  "entity:unsafe-caller",
			"callee_entity_id":  "entity:unsafe-callee",
			"caller_file":       "../outside.go",
			"callee_file":       "src/callee.go",
			"relationship_type": "CALLS",
			"action":            reducercontract.IntentActionUpsert,
		},
	}

	intents := BuildSharedIntentRows(
		rows,
		contextByRepoID,
		createdAt,
		EvidenceSource,
		deltaFileScopesByRepoID,
	)
	if got, want := len(intents), 3; got != want {
		t.Fatalf("len(intents) = %d, want %d", got, want)
	}

	var deltaIntents []sharedintent.Row
	var fallbackIntent sharedintent.Row
	for _, row := range intents {
		switch row.Payload["caller_entity_id"] {
		case "entity:caller":
			deltaIntents = append(deltaIntents, row)
		case "entity:unsafe-caller":
			fallbackIntent = row
		}
	}
	if got, want := len(deltaIntents), 2; got != want {
		t.Fatalf("len(deltaIntents) = %d, want %d", got, want)
	}

	wantPartitionKey := RefreshPartitionKeyForDelta("repo-a", []string{"src/caller.go"})
	seenIntentIDs := make(map[string]struct{}, len(deltaIntents))
	for _, deltaIntent := range deltaIntents {
		if got := deltaIntent.PartitionKey; got != wantPartitionKey {
			t.Fatalf("delta PartitionKey = %q, want %q", got, wantPartitionKey)
		}
		if _, exists := seenIntentIDs[deltaIntent.IntentID]; exists {
			t.Fatalf("duplicate delta IntentID %q for same-file edges", deltaIntent.IntentID)
		}
		seenIntentIDs[deltaIntent.IntentID] = struct{}{}
		if got, want := deltaIntent.Payload["delta_projection"], true; got != want {
			t.Fatalf("delta_projection = %#v, want %#v", got, want)
		}
		gotPaths, ok := deltaIntent.Payload["delta_file_paths"].([]string)
		if !ok {
			t.Fatalf("delta_file_paths type = %T, want []string", deltaIntent.Payload["delta_file_paths"])
		}
		if wantPaths := []string{"/repo/src/caller.go"}; !reflect.DeepEqual(gotPaths, wantPaths) {
			t.Fatalf("delta_file_paths = %#v, want %#v", gotPaths, wantPaths)
		}
	}

	if got, want := fallbackIntent.PartitionKey, "entity:unsafe-caller->entity:unsafe-callee"; got != want {
		t.Fatalf("fallback PartitionKey = %q, want %q", got, want)
	}
	if _, ok := fallbackIntent.Payload["delta_projection"]; ok {
		t.Fatal("fallback intent unexpectedly carries delta_projection")
	}
}
