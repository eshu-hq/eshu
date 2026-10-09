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
	"github.com/eshu-hq/eshu/go/internal/reducer/factdecode"
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
		if got := record["quarantined_file_count"]; got != float64(1) {
			t.Fatalf("quarantined_file_count = %v, want 1", got)
		}
	}
	if !found {
		t.Fatalf("missing quarantine file-identity log in %s", logs.String())
	}
}

// TestLogCodeCallQuarantinedFilesBoundsAndFallsBack pins the log-formatting
// branches of the #7736 F4 signal that the Handle-level test does not reach:
// the 32-entry cap on quarantined_files (with the true total still carried
// by quarantined_file_count) and the fact-ID fallback for quarantined facts
// whose payload lacks file identity or whose envelope is absent.
func TestLogCodeCallQuarantinedFilesBoundsAndFallsBack(t *testing.T) {
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, nil)))
	defer slog.SetDefault(previous)

	envelopes := []facts.Envelope{
		{FactID: "fact-known", FactKind: "file", Payload: map[string]any{
			"repo_id": "repo-a", "relative_path": "app/known.py",
		}},
		{FactID: "fact-noidentity", FactKind: "file", Payload: map[string]any{
			"repo_id": "repo-a",
		}},
	}
	quarantined := []factdecode.QuarantinedFact{
		{FactID: "fact-known", FactKind: "file", Field: "parsed_file_data", Classification: "input_invalid"},
		{FactID: "fact-noidentity", FactKind: "file", Field: "parsed_file_data", Classification: "input_invalid"},
		{FactID: "fact-missing-envelope", FactKind: "file", Field: "parsed_file_data", Classification: "input_invalid"},
	}
	for i := 0; i < 40; i++ {
		quarantined = append(quarantined, factdecode.QuarantinedFact{
			FactID: "fact-pad", FactKind: "file", Field: "parsed_file_data", Classification: "input_invalid",
		})
	}
	now := time.Date(2026, time.October, 9, 12, 0, 0, 0, time.UTC)
	logCodeCallQuarantinedFiles(context.Background(), reducercontract.Intent{
		IntentID:     "intent-cap",
		ScopeID:      "scope-1",
		GenerationID: "gen-f",
		Domain:       reducercontract.DomainCodeCallMaterialization,
		EnqueuedAt:   now,
		AvailableAt:  now,
		Status:       reducercontract.IntentStatusPending,
	}, envelopes, quarantined)

	var record map[string]any
	found := false
	for _, line := range bytes.Split(logs.Bytes(), []byte("\n")) {
		if json.Unmarshal(line, &record) != nil || record["msg"] != "code call file quarantined, edges excluded" {
			continue
		}
		found = true
		break
	}
	if !found {
		t.Fatalf("missing quarantine file-identity log in %s", logs.String())
	}
	files, _ := record["quarantined_files"].([]any)
	if len(files) != 32 {
		t.Fatalf("len(quarantined_files) = %d, want 32 (capped)", len(files))
	}
	if got := record["quarantined_file_count"]; got != float64(len(quarantined)) {
		t.Fatalf("quarantined_file_count = %v, want %d (true total)", got, len(quarantined))
	}
	first, _ := files[0].(string)
	if first != "repo-a:app/known.py" {
		t.Fatalf("quarantined_files[0] = %q, want repo-a:app/known.py", first)
	}
	second, _ := files[1].(string)
	if second != "fact-noidentity" {
		t.Fatalf("quarantined_files[1] = %q, want fact-ID fallback", second)
	}
	third, _ := files[2].(string)
	if third != "fact-missing-envelope" {
		t.Fatalf("quarantined_files[2] = %q, want fact-ID fallback", third)
	}
}
