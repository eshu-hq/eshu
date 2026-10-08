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

// TestCodeCallMaterializationHandlerReportsUnresolvedCallees proves the
// #7642 operator signal: a call whose callee has no declaration in the call
// file's own repository and path shows up in the
// unresolved_callee_calls SubSignal and in the
// code_call_unresolved_callee_count field of the completion log.
func TestCodeCallMaterializationHandlerReportsUnresolvedCallees(t *testing.T) {
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
					map[string]any{"name": "main", "uid": "repo-a:main", "line_number": 1, "end_line": 20},
					map[string]any{"name": "other", "uid": "repo-a:other", "line_number": 30, "end_line": 32},
				},
				"function_calls": []any{
					map[string]any{"name": "missing", "full_name": "missing", "lang": "python", "line_number": 5},
					map[string]any{"name": "other", "full_name": "other", "lang": "python", "line_number": 6},
				},
			},
		}},
	}}
	writer := &recordingCodeCallIntentWriter{}
	handler := Handler{FactLoader: loader, IntentWriter: writer}

	result, err := handler.Handle(context.Background(), reducercontract.Intent{
		IntentID:     "intent-unresolved-callee",
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
	// The call to other resolves and emits a row: the handler takes its
	// success path, and the call to the undeclared callee is still counted
	// there.
	if len(writer.rows) == 0 {
		t.Fatalf("expected the resolving call to emit a row, got none")
	}
	if got, want := result.SubSignals[SubSignalUnresolvedCalleeCalls], 1.0; got != want {
		t.Fatalf("SubSignals[%q] = %v, want %v", SubSignalUnresolvedCalleeCalls, got, want)
	}

	found := false
	for _, line := range bytes.Split(logs.Bytes(), []byte("\n")) {
		var record map[string]any
		if json.Unmarshal(line, &record) != nil || record["msg"] != "code call materialization completed" {
			continue
		}
		found = true
		if got, want := record["code_call_unresolved_callee_count"], 1.0; got != want {
			t.Fatalf("code_call_unresolved_callee_count = %v, want %v", got, want)
		}
	}
	if !found {
		t.Fatalf("missing completion log in %s", logs.String())
	}
}
