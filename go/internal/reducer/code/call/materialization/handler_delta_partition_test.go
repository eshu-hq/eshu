// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package materialization

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	codecall "github.com/eshu-hq/eshu/go/internal/reducer/code/call"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/reducer/payloadcore"
	"github.com/eshu-hq/eshu/go/internal/reducer/sharedintent"
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
	handler := Handler{
		FactLoader:   loader,
		IntentWriter: writer,
	}

	result, err := handler.Handle(context.Background(), reducercontract.Intent{
		IntentID:     "intent-code-call-delta-1",
		ScopeID:      "scope-1",
		GenerationID: "gen-1",
		SourceSystem: "git",
		Domain:       reducercontract.DomainCodeCallMaterialization,
		Cause:        "parser follow-up required",
		EnqueuedAt:   now,
		AvailableAt:  now,
		Status:       reducercontract.IntentStatusPending,
	})
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if result.Status != reducercontract.ResultStatusSucceeded {
		t.Fatalf("result.Status = %q, want %q", result.Status, reducercontract.ResultStatusSucceeded)
	}
	if got, want := len(writer.rows), 3; got != want {
		t.Fatalf("len(writer.rows) = %d, want %d", got, want)
	}

	var refreshRows []sharedintent.Row
	for _, row := range writer.rows {
		if row.Payload["intent_type"] != "repo_refresh" {
			continue
		}
		refreshRows = append(refreshRows, row)
		gotPaths := payloadcore.SemanticPayloadStringSlice(row.Payload, "delta_file_paths")
		wantPaths := []string{"/repo/caller.py", "/repo/models.py"}
		if !reflect.DeepEqual(gotPaths, wantPaths) {
			t.Fatalf("refresh delta_file_paths = %#v, want %#v", gotPaths, wantPaths)
		}
	}
	if got, want := len(refreshRows), 1; got != want {
		t.Fatalf("len(refreshRows) = %d, want %d", got, want)
	}

	var codeCallRow, metaclassRow sharedintent.Row
	for _, row := range writer.rows {
		switch row.Payload["evidence_source"] {
		case codecall.EvidenceSource:
			codeCallRow = row
		case codecall.PythonMetaclassEvidenceSource:
			metaclassRow = row
		}
	}

	wantCallerPartition := codecall.RefreshPartitionKeyForDelta("repo-a", []string{"caller.py"})
	if got, want := codeCallRow.PartitionKey, wantCallerPartition; got != want {
		t.Fatalf("code-call PartitionKey = %q, want caller partition %q", got, want)
	}
	if gotPaths := payloadcore.SemanticPayloadStringSlice(codeCallRow.Payload, "delta_file_paths"); !reflect.DeepEqual(gotPaths, []string{"/repo/caller.py"}) {
		t.Fatalf("code-call delta_file_paths = %#v, want [/repo/caller.py]", gotPaths)
	}
	wantModelsPartition := codecall.RefreshPartitionKeyForDelta("repo-a", []string{"models.py"})
	if got, want := metaclassRow.PartitionKey, wantModelsPartition; got != want {
		t.Fatalf("metaclass PartitionKey = %q, want models partition %q", got, want)
	}
	if gotPaths := payloadcore.SemanticPayloadStringSlice(metaclassRow.Payload, "delta_file_paths"); !reflect.DeepEqual(gotPaths, []string{"/repo/models.py"}) {
		t.Fatalf("metaclass delta_file_paths = %#v, want [/repo/models.py]", gotPaths)
	}
}
