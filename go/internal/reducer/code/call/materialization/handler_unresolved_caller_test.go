// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package materialization

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
)

// TestCodeCallMaterializationHandlerReportsUnresolvedCallers proves the
// #7640 operator signal: a call whose callee resolves but whose caller has no
// containing entity in its own file shows up in the
// unresolved_caller_calls SubSignal and in the
// code_call_unresolved_caller_count field of the completion log.
func TestCodeCallMaterializationHandlerReportsUnresolvedCallers(t *testing.T) {
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, nil)))
	defer slog.SetDefault(previous)

	now := time.Date(2026, time.April, 15, 12, 0, 0, 0, time.UTC)
	loader := &stubFactLoader{envelopes: []facts.Envelope{
		{FactKind: "repository", Payload: map[string]any{
			"repo_id": "repo-a", "source_run_id": "run-a", "graph_id": "repo-a",
			"graph_kind": "repository", "name": "repo-a",
		}},
		{FactKind: "file", Payload: map[string]any{
			"repo_id":       "repo-a",
			"relative_path": "app/util.py",
			"parsed_file_data": map[string]any{
				"path": "app/util.py",
				"functions": []any{
					map[string]any{"name": "helper", "uid": "repo-a:helper", "line_number": 10, "end_line": 12},
				},
				"function_calls": []any{
					map[string]any{"name": "helper", "full_name": "helper", "lang": "python", "line_number": 5},
					map[string]any{"name": "helper", "full_name": "helper", "lang": "python", "line_number": 11},
				},
			},
		}},
	}}
	writer := &recordingCodeCallIntentWriter{}
	handler := Handler{FactLoader: loader, IntentWriter: writer}

	result, err := handler.Handle(context.Background(), reducercontract.Intent{
		IntentID:     "intent-unresolved-caller",
		ScopeID:      "scope-1",
		GenerationID: "gen-1",
		Domain:       reducercontract.DomainCodeCallMaterialization,
		EnqueuedAt:   now,
		AvailableAt:  now,
		Status:       reducercontract.IntentStatusPending,
	})
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	// The call at line 11 sits inside helper, so it resolves and emits a row:
	// the handler takes its success path, and the unresolved top-level call
	// at line 5 is still counted there.
	if len(writer.rows) == 0 {
		t.Fatalf("expected the in-function call to emit a row, got none")
	}
	if got, want := result.SubSignals[SubSignalUnresolvedCallerCalls], 1.0; got != want {
		t.Fatalf("SubSignals[%q] = %v, want %v", SubSignalUnresolvedCallerCalls, got, want)
	}

	found := false
	for _, line := range bytes.Split(logs.Bytes(), []byte("\n")) {
		var record map[string]any
		if json.Unmarshal(line, &record) != nil || record["msg"] != "code call materialization completed" {
			continue
		}
		found = true
		if got, want := record["code_call_unresolved_caller_count"], 1.0; got != want {
			t.Fatalf("code_call_unresolved_caller_count = %v, want %v", got, want)
		}
	}
	if !found {
		t.Fatalf("missing completion log in %s", logs.String())
	}
}

// TestCodeCallMaterializationHandlerReportsUnresolvedCloudActionCallers
// proves the INVOKES_CLOUD_ACTION twin of the signal above: an SDK call with
// no containing function in its own file emits no intent, and the drop shows
// up in the unresolved_cloud_action_callers SubSignal and the
// code_call_unresolved_cloud_action_caller_count log field. A second
// repository's in-function call still emits its intent and is not counted.
// The handler's empty exit cannot be driven here: every repository with a
// projection context gets a refresh intent, so intentRows is never empty once
// a context exists.
func TestCodeCallMaterializationHandlerReportsUnresolvedCloudActionCallers(t *testing.T) {
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, nil)))
	defer slog.SetDefault(previous)

	topLevelCall := []map[string]any{
		{"name": "PutObject", "receiver_sdk_service": "s3", "line_number": 10},
	}
	now := time.Date(2026, time.April, 15, 12, 0, 0, 0, time.UTC)
	writer := &recordingCodeCallIntentWriter{}
	handler := Handler{
		FactLoader: &stubFactLoader{envelopes: []facts.Envelope{
			invokesCloudActionRepoEnvelope("repo-a"),
			invokesCloudActionFileEnvelope("repo-a", "cmd/main.go", callerFunction("repo-a:handler"), topLevelCall),
			invokesCloudActionRepoEnvelope("repo-b"),
			invokesCloudActionFileEnvelope("repo-b", "cmd/main.go", nil, topLevelCall),
		}},
		IntentWriter: writer,
	}

	result, err := handler.Handle(context.Background(), reducercontract.Intent{
		IntentID:     "intent-unresolved-cloud-action",
		ScopeID:      "scope-1",
		GenerationID: "gen-1",
		Domain:       reducercontract.DomainCodeCallMaterialization,
		EnqueuedAt:   now,
		AvailableAt:  now,
		Status:       reducercontract.IntentStatusPending,
	})
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}

	edges := 0
	for _, row := range writer.rows {
		if row.ProjectionDomain == reducercontract.DomainInvokesCloudAction && row.Payload["action"] != "refresh" {
			edges++
			if row.RepositoryID != "repo-a" {
				t.Fatalf("INVOKES_CLOUD_ACTION intent for %q, want only repo-a", row.RepositoryID)
			}
		}
	}
	if edges != 1 {
		t.Fatalf("INVOKES_CLOUD_ACTION edge intents = %d, want 1 (repo-a)", edges)
	}
	if got, want := result.SubSignals[SubSignalUnresolvedCloudActionCallers], 1.0; got != want {
		t.Fatalf("SubSignals[%q] = %v, want %v", SubSignalUnresolvedCloudActionCallers, got, want)
	}

	found := false
	for _, line := range bytes.Split(logs.Bytes(), []byte("\n")) {
		var record map[string]any
		if json.Unmarshal(line, &record) != nil || record["msg"] != "code call materialization completed" {
			continue
		}
		found = true
		if got, want := record["code_call_unresolved_cloud_action_caller_count"], 1.0; got != want {
			t.Fatalf("code_call_unresolved_cloud_action_caller_count = %v, want %v", got, want)
		}
	}
	if !found {
		t.Fatalf("missing completion log in %s", logs.String())
	}
}
