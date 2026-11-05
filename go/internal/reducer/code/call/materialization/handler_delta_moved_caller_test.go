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
	"github.com/eshu-hq/eshu/go/internal/reducer/factload"
	"github.com/eshu-hq/eshu/go/internal/reducer/payloadcore"
	"github.com/eshu-hq/eshu/go/internal/reducer/sharedintent"
)

// deltaMovedCallerIntent is the code_call_materialization work item a delta
// generation's new marker enqueues (#10049).
func deltaMovedCallerIntent(now time.Time) reducercontract.Intent {
	return reducercontract.Intent{
		IntentID:     "intent-code-call-delta-moved",
		ScopeID:      "scope-1",
		GenerationID: "gen-delta",
		SourceSystem: "git",
		Domain:       reducercontract.DomainCodeCallMaterialization,
		Cause:        "parser follow-up required",
		EnqueuedAt:   now,
		AvailableAt:  now,
		Status:       reducercontract.IntentStatusPending,
	}
}

// TestCodeCallMaterializationHandlerDeltaMovedCallerKeepsUnchangedCalleeEdge
// is the #10049 unit regression: a delta generation carries only the changed
// caller file (the caller moved, so it has a new entity id), while the callee
// lives in an unchanged file whose definition fact arrives only through the
// active symbol-definition load (the #10003 row 1 delta overlay in
// production). The handler must still emit the CALLS edge, bounded by a
// file-scoped refresh over the changed file only, so the projection runner
// retracts and rewrites just that file's edges.
func TestCodeCallMaterializationHandlerDeltaMovedCallerKeepsUnchangedCalleeEdge(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.November, 5, 10, 0, 0, 0, time.UTC)
	loader := &activeCodeCallSymbolFactLoader{
		primary: []facts.Envelope{
			{
				FactKind: factload.FactKindRepository,
				Payload: map[string]any{
					"repo_id":              "repo-a",
					"source_run_id":        "run-a",
					"graph_id":             "repo-a",
					"graph_kind":           "repository",
					"name":                 "repo-a",
					"path":                 "/repo",
					"delta_generation":     true,
					"delta_relative_paths": []string{"moved.py"},
				},
			},
			{
				FactKind: factload.FactKindFile,
				Payload: map[string]any{
					"repo_id":       "repo-a",
					"relative_path": "moved.py",
					"parsed_file_data": map[string]any{
						"path": "moved.py",
						"functions": []any{
							map[string]any{
								"name":        "handle",
								"line_number": 30,
								"uid":         "entity:handle-moved",
							},
						},
						"function_calls_scip": []any{
							map[string]any{
								"caller_file":   "moved.py",
								"caller_line":   30,
								"caller_symbol": "pkg/moved#handle().",
								"callee_file":   "callee.py",
								"callee_line":   1,
								"callee_symbol": "pkg/callee#callee().",
							},
						},
					},
				},
			},
		},
		activeDefinitions: []facts.Envelope{
			{
				FactKind: factload.FactKindFile,
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
		},
	}
	writer := &recordingCodeCallIntentWriter{}
	handler := Handler{FactLoader: loader, IntentWriter: writer}

	result, err := handler.Handle(context.Background(), deltaMovedCallerIntent(now))
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if result.Status != reducercontract.ResultStatusSucceeded {
		t.Fatalf("result.Status = %q, want %q", result.Status, reducercontract.ResultStatusSucceeded)
	}
	if got, want := loader.activeSymbolCalls, 1; got != want {
		t.Fatalf("active symbol loader calls = %d, want %d", got, want)
	}

	var edgeRows, refreshRows []sharedintent.Row
	for _, row := range writer.rows {
		switch {
		case row.Payload["intent_type"] == "repo_refresh" && row.ProjectionDomain == reducercontract.DomainCodeCalls:
			refreshRows = append(refreshRows, row)
		case row.Payload["evidence_source"] == codecall.EvidenceSource:
			edgeRows = append(edgeRows, row)
		}
	}
	if got, want := len(edgeRows), 1; got != want {
		t.Fatalf("code-call edge rows = %d, want %d; rows=%#v", got, want, writer.rows)
	}
	edge := edgeRows[0]
	if got, want := edge.Payload["caller_entity_id"], "entity:handle-moved"; got != want {
		t.Fatalf("caller_entity_id = %#v, want %#v", got, want)
	}
	if got, want := edge.Payload["callee_entity_id"], "entity:callee"; got != want {
		t.Fatalf("callee_entity_id = %#v, want %#v (callee in unchanged callee.py)", got, want)
	}
	if got, want := edge.PartitionKey, codecall.RefreshPartitionKeyForDelta("repo-a", []string{"moved.py"}); got != want {
		t.Fatalf("edge PartitionKey = %q, want file-scoped %q", got, want)
	}
	if got, want := len(refreshRows), 1; got != want {
		t.Fatalf("code_calls refresh rows = %d, want %d", got, want)
	}
	if gotPaths := payloadcore.SemanticPayloadStringSlice(refreshRows[0].Payload, "delta_file_paths"); !reflect.DeepEqual(gotPaths, []string{"/repo/moved.py"}) {
		t.Fatalf("refresh delta_file_paths = %#v, want [/repo/moved.py] only", gotPaths)
	}
}

// deltaRuntimeRepoEnvelope builds the repository fact for the runtime-domain
// gate test, on a delta or a full generation.
func deltaRuntimeRepoEnvelope(delta bool) facts.Envelope {
	payload := map[string]any{
		"repo_id":       "repo-a",
		"source_run_id": "run-1",
		"graph_id":      "repo-a",
		"graph_kind":    "repository",
		"name":          "repo-a",
		"path":          "/repo",
	}
	if delta {
		payload["delta_generation"] = true
		payload["delta_relative_paths"] = []string{"server.js", "main.go"}
	}
	return facts.Envelope{FactKind: factload.FactKindRepository, ScopeID: "scope-1", Payload: payload}
}

// deltaRuntimeEnvelopes carries one route file (handles_route and runs_in
// rows) and one SDK-calling file (an invokes_cloud_action row).
func deltaRuntimeEnvelopes(delta bool) []facts.Envelope {
	return []facts.Envelope{
		deltaRuntimeRepoEnvelope(delta),
		handlesRouteFileEnvelope(
			"repo-a",
			"server.js",
			[]map[string]any{
				{"name": "getWidgets", "uid": "content-entity:gw", "line_number": 10, "end_line": 20},
			},
			"express",
			[]any{
				map[string]any{"method": "GET", "path": "/widgets", "handler": "getWidgets"},
			},
		),
		invokesCloudActionFileEnvelope(
			"repo-a",
			"main.go",
			callerFunction("content-entity:handler"),
			[]map[string]any{
				{"name": "PutObject", "receiver_sdk_service": "s3", "line_number": 10},
			},
		),
	}
}

// TestCodeCallMaterializationHandlerDeltaGatesRuntimeDomains proves the
// #10049 addendum gate: handles_route, runs_in, and invokes_cloud_action
// retract repo-wide, so a delta generation — which carries changed files
// only — must emit no rows for them at all, neither per-edge rows nor the
// paired repo-wide refresh that would delete every unchanged file's edges.
// The full-generation control emits both shapes for every domain.
func TestCodeCallMaterializationHandlerDeltaGatesRuntimeDomains(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.November, 5, 10, 30, 0, 0, time.UTC)
	runtimeDomains := []string{
		reducercontract.DomainHandlesRoute,
		reducercontract.DomainRunsIn,
		reducercontract.DomainInvokesCloudAction,
	}
	run := func(t *testing.T, delta bool) []sharedintent.Row {
		t.Helper()
		writer := &recordingCodeCallIntentWriter{}
		handler := Handler{
			FactLoader:   &stubFactLoader{envelopes: deltaRuntimeEnvelopes(delta)},
			IntentWriter: writer,
		}
		result, err := handler.Handle(context.Background(), deltaMovedCallerIntent(now))
		if err != nil {
			t.Fatalf("Handle() error = %v", err)
		}
		if result.Status != reducercontract.ResultStatusSucceeded {
			t.Fatalf("result.Status = %q, want %q", result.Status, reducercontract.ResultStatusSucceeded)
		}
		return writer.rows
	}

	t.Run("delta emits no runtime rows", func(t *testing.T) {
		t.Parallel()
		rows := run(t, true)
		for _, row := range rows {
			for _, domain := range runtimeDomains {
				if row.ProjectionDomain == domain {
					t.Fatalf("delta generation emitted a %s row: %#v", domain, row)
				}
			}
		}
		// The code_calls side of the same pass still refreshes, file-scoped.
		foundRefresh := false
		for _, row := range rows {
			if row.ProjectionDomain == reducercontract.DomainCodeCalls && row.Payload["intent_type"] == "repo_refresh" {
				foundRefresh = true
				if gotPaths := payloadcore.SemanticPayloadStringSlice(row.Payload, "delta_file_paths"); len(gotPaths) == 0 {
					t.Fatalf("delta code_calls refresh carries no delta_file_paths: %#v", row.Payload)
				}
			}
		}
		if !foundRefresh {
			t.Fatal("delta generation emitted no code_calls refresh row")
		}
	})

	t.Run("full emits runtime rows and refreshes", func(t *testing.T) {
		t.Parallel()
		rows := run(t, false)
		for _, domain := range runtimeDomains {
			edges, refreshes := 0, 0
			for _, row := range rows {
				if row.ProjectionDomain != domain {
					continue
				}
				if sharedintent.IsRepoRefreshRow(row) {
					refreshes++
				} else {
					edges++
				}
			}
			if edges == 0 || refreshes != 1 {
				t.Fatalf("full generation %s rows: edges=%d refreshes=%d, want >=1 edge and 1 refresh", domain, edges, refreshes)
			}
		}
	})
}
