// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package materialization

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
)

// TestCodeCallMaterializationHandlerNamesQuarantinedFiles proves the #7736 F4
// operator signal: when a successor generation quarantines a file, the drain
// may drop that file's last-valid edges until the next valid generation
// re-emits them (accepted transient loss, documented in
// 7165-emitted-full-successor-drain.md). The factdecode error log carries fact
// IDs; this WARN names the repo-relative path so an operator can correlate a
// transient edge loss with the quarantining generation without a fact_records
// lookup.
func TestCodeCallMaterializationHandlerNamesQuarantinedFiles(t *testing.T) {
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, nil)))
	defer slog.SetDefault(previous)

	now := time.Date(2026, time.October, 9, 12, 0, 0, 0, time.UTC)
	loader := &stubFactLoader{envelopes: []facts.Envelope{
		{FactID: "fact-repo-a", FactKind: "repository", Payload: map[string]any{
			"repo_id": "repo-a", "source_run_id": "run-a", "graph_id": "repo-a",
			"graph_kind": "repository", "name": "repo-a",
		}},
		// Quarantined: identity present, parsed body missing. The path must
		// still be named.
		{FactID: "fact-file-x", FactKind: "file", Payload: map[string]any{
			"repo_id":       "repo-a",
			"relative_path": "app/dropped.py",
		}},
		{FactKind: "file", Payload: map[string]any{
			"repo_id":       "repo-a",
			"relative_path": "app/kept.py",
			"parsed_file_data": map[string]any{
				"path": "app/kept.py",
				"functions": []any{
					map[string]any{"name": "helper", "uid": "repo-a:helper", "line_number": 10, "end_line": 12},
				},
				"function_calls": []any{
					map[string]any{"name": "helper", "full_name": "helper", "lang": "python", "line_number": 11},
				},
			},
		}},
	}}
	writer := &recordingCodeCallIntentWriter{}
	handler := Handler{FactLoader: loader, IntentWriter: writer}

	_, err := handler.Handle(context.Background(), reducercontract.Intent{
		IntentID:     "intent-quarantine-signal",
		ScopeID:      "scope-1",
		GenerationID: "gen-f",
		Domain:       reducercontract.DomainCodeCallMaterialization,
		EnqueuedAt:   now,
		AvailableAt:  now,
		Status:       reducercontract.IntentStatusPending,
	})
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}

	found := false
	for _, line := range bytes.Split(logs.Bytes(), []byte("\n")) {
		var record map[string]any
		if json.Unmarshal(line, &record) != nil || record["msg"] != "code call file quarantined, edges excluded" {
			continue
		}
		found = true
		if got := record["scope_id"]; got != "scope-1" {
			t.Fatalf("scope_id = %v, want scope-1", got)
		}
		if got := record["generation_id"]; got != "gen-f" {
			t.Fatalf("generation_id = %v, want gen-f", got)
		}
		files, _ := record["quarantined_files"].([]any)
		if len(files) != 1 {
			t.Fatalf("quarantined_files = %v, want exactly 1 entry", record["quarantined_files"])
		}
		entry, _ := files[0].(string)
		if !strings.Contains(entry, "app/dropped.py") {
			t.Fatalf("quarantined_files[0] = %q, want it to name app/dropped.py", entry)
		}
		if !strings.Contains(entry, "repo-a") {
			t.Fatalf("quarantined_files[0] = %q, want it to name repo-a", entry)
		}
	}
	if !found {
		t.Fatalf("missing quarantine file-identity log in %s", logs.String())
	}
}
