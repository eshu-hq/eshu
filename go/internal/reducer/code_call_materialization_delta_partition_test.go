// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestCodeCallMaterializationHandlerAlignsDeltaEdgePartitions(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.June, 14, 17, 0, 0, 0, time.UTC)
	loader := &stubFactLoader{
		envelopes: []facts.Envelope{
			{
				FactKind: "repository",
				Payload: map[string]any{
					"repo_id":              "repo-a",
					"source_run_id":        "run-a",
					"graph_id":             "repo-a",
					"graph_kind":           "repository",
					"name":                 "repo-a",
					"path":                 "/repo",
					"delta_generation":     true,
					"delta_relative_paths": []string{"caller.py", "models.py"},
				},
			},
			{
				FactKind: "file",
				Payload: map[string]any{
					"repo_id":       "repo-a",
					"relative_path": "caller.py",
					"parsed_file_data": map[string]any{
						"path": "caller.py",
						"functions": []any{
							map[string]any{
								"name":        "handle",
								"line_number": 3,
								"uid":         "entity:handle",
							},
						},
						"function_calls_scip": []any{
							map[string]any{
								"caller_file":   "caller.py",
								"caller_line":   3,
								"caller_symbol": "pkg/caller#handle().",
								"callee_file":   "callee.py",
								"callee_line":   1,
								"callee_symbol": "pkg/callee#callee().",
							},
						},
					},
				},
			},
			{
				FactKind: "file",
				Payload: map[string]any{
					"repo_id":       "repo-a",
					"relative_path": "callee.py",
					"parsed_file_data": map[string]any{
						"path": "callee.py",
						"functions": []any{
							map[string]any{
								"name":        "callee",
								"line_number": 1,
								"uid":         "entity:callee",
							},
						},
					},
				},
			},
			{
				FactKind: "file",
				Payload: map[string]any{
					"repo_id":       "repo-a",
					"relative_path": "models.py",
					"parsed_file_data": map[string]any{
						"path": "models.py",
						"classes": []any{
							map[string]any{
								"name":      "Widget",
								"uid":       "entity:widget",
								"metaclass": "Meta",
							},
							map[string]any{
								"name": "Meta",
								"uid":  "entity:meta",
							},
						},
					},
				},
			},
		},
	}

	writer := &recordingCodeCallIntentWriter{}
	handler := CodeCallMaterializationHandler{
		FactLoader:   loader,
		IntentWriter: writer,
	}

	result, err := handler.Handle(context.Background(), Intent{
		IntentID:     "intent-code-call-delta-1",
		ScopeID:      "scope-1",
		GenerationID: "gen-1",
		SourceSystem: "git",
		Domain:       DomainCodeCallMaterialization,
		Cause:        "parser follow-up required",
		EnqueuedAt:   now,
		AvailableAt:  now,
		Status:       IntentStatusPending,
	})
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if result.Status != ResultStatusSucceeded {
		t.Fatalf("result.Status = %q, want %q", result.Status, ResultStatusSucceeded)
	}
	if got, want := len(writer.rows), 3; got != want {
		t.Fatalf("len(writer.rows) = %d, want %d", got, want)
	}

	var refreshRows []SharedProjectionIntentRow
	for _, row := range writer.rows {
		if row.Payload["intent_type"] != "repo_refresh" {
			continue
		}
		refreshRows = append(refreshRows, row)
		gotPaths := semanticPayloadStringSlice(row.Payload, "delta_file_paths")
		wantPaths := []string{"/repo/caller.py", "/repo/models.py"}
		if !reflect.DeepEqual(gotPaths, wantPaths) {
			t.Fatalf("refresh delta_file_paths = %#v, want %#v", gotPaths, wantPaths)
		}
	}
	if got, want := len(refreshRows), 1; got != want {
		t.Fatalf("len(refreshRows) = %d, want %d", got, want)
	}

	var codeCallRow, metaclassRow SharedProjectionIntentRow
	for _, row := range writer.rows {
		switch row.Payload["evidence_source"] {
		case codeCallEvidenceSource:
			codeCallRow = row
		case pythonMetaclassEvidenceSource:
			metaclassRow = row
		}
	}

	wantCallerPartition := codeCallRefreshPartitionKeyForDelta("repo-a", []string{"caller.py"})
	if got, want := codeCallRow.PartitionKey, wantCallerPartition; got != want {
		t.Fatalf("code-call PartitionKey = %q, want caller partition %q", got, want)
	}
	if gotPaths := semanticPayloadStringSlice(codeCallRow.Payload, "delta_file_paths"); !reflect.DeepEqual(gotPaths, []string{"/repo/caller.py"}) {
		t.Fatalf("code-call delta_file_paths = %#v, want [/repo/caller.py]", gotPaths)
	}
	wantModelsPartition := codeCallRefreshPartitionKeyForDelta("repo-a", []string{"models.py"})
	if got, want := metaclassRow.PartitionKey, wantModelsPartition; got != want {
		t.Fatalf("metaclass PartitionKey = %q, want models partition %q", got, want)
	}
	if gotPaths := semanticPayloadStringSlice(metaclassRow.Payload, "delta_file_paths"); !reflect.DeepEqual(gotPaths, []string{"/repo/models.py"}) {
		t.Fatalf("metaclass delta_file_paths = %#v, want [/repo/models.py]", gotPaths)
	}
}

func TestBuildCodeCallSharedIntentRowsCarriesDeltaPartitionForSourceFile(t *testing.T) {
	t.Parallel()

	createdAt := time.Date(2026, time.June, 14, 16, 0, 0, 0, time.UTC)
	contextByRepoID := map[string]ProjectionContext{
		"repo-a": {
			ScopeID:          "scope:git:repo-a",
			AcceptanceUnitID: "repository:repo-a",
			SourceRunID:      "run-a",
			GenerationID:     "gen-a",
		},
	}
	deltaFileScopesByRepoID := map[string]codeCallDeltaFileScope{
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
			"action":            IntentActionUpsert,
		},
		{
			"repo_id":           "repo-a",
			"caller_entity_id":  "entity:caller",
			"callee_entity_id":  "entity:callee",
			"caller_file":       "src/caller.go",
			"callee_file":       "src/callee.go",
			"relationship_type": "INSTANTIATES",
			"action":            IntentActionUpsert,
		},
		{
			"repo_id":           "repo-a",
			"caller_entity_id":  "entity:unsafe-caller",
			"callee_entity_id":  "entity:unsafe-callee",
			"caller_file":       "../outside.go",
			"callee_file":       "src/callee.go",
			"relationship_type": "CALLS",
			"action":            IntentActionUpsert,
		},
	}

	intents := buildCodeCallSharedIntentRows(
		rows,
		contextByRepoID,
		createdAt,
		codeCallEvidenceSource,
		deltaFileScopesByRepoID,
	)
	if got, want := len(intents), 3; got != want {
		t.Fatalf("len(intents) = %d, want %d", got, want)
	}

	var deltaIntents []SharedProjectionIntentRow
	var fallbackIntent SharedProjectionIntentRow
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

	wantPartitionKey := codeCallRefreshPartitionKeyForDelta("repo-a", []string{"src/caller.go"})
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
