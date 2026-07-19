// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// selfLoopRepoEnvelopes builds one file fact for a single genuinely recursive
// function: it declares "fib" and also calls "fib" from within its own
// [line_number, end_line] span, so the same-file bare-name resolver attributes
// both the caller and the callee to the same entity.
func selfLoopRepoEnvelopes(repoID string, path string, entity string, lang string) []facts.Envelope {
	return []facts.Envelope{
		{FactKind: "repository", Payload: map[string]any{"repo_id": repoID}},
		{
			FactKind: "file",
			Payload: map[string]any{
				"repo_id":       repoID,
				"relative_path": path,
				"parsed_file_data": map[string]any{
					"path": path,
					"functions": []any{
						map[string]any{"name": "fib", "line_number": 1, "end_line": 5, "uid": entity},
					},
					"function_calls": []any{
						map[string]any{"name": "fib", "full_name": "fib", "line_number": 3, "lang": lang},
					},
				},
			},
		},
	}
}

// TestExtractCodeCallRowsWritesGenuineSelfLoop is the overcorrection guard for
// eshu-hq/eshu#5332: a call row whose caller and callee resolve to the same
// entity is real recursion, not the parser bug the fix removed (a declaration
// misread as a call to itself). The materialization path must still WRITE
// this row - filtering it would trade one accuracy bug for another.
func TestExtractCodeCallRowsWritesGenuineSelfLoop(t *testing.T) {
	t.Parallel()

	_, rows := ExtractCodeCallRows(
		selfLoopRepoEnvelopes("repo-fib", "fib.dart", "content-entity:fib", "dart"),
	)

	if len(rows) != 1 {
		t.Fatalf("len(rows) = %d, want 1 in %#v", len(rows), rows)
	}
	row := rows[0]
	if got, want := row["caller_entity_id"], "content-entity:fib"; got != want {
		t.Fatalf("caller_entity_id = %#v, want %#v", got, want)
	}
	if got, want := row["callee_entity_id"], "content-entity:fib"; got != want {
		t.Fatalf("callee_entity_id = %#v, want %#v", got, want)
	}
	if row["caller_entity_id"] != row["callee_entity_id"] {
		t.Fatalf("expected a self-loop row (caller == callee), got caller=%#v callee=%#v", row["caller_entity_id"], row["callee_entity_id"])
	}
	if got, want := row["lang"], "dart"; got != want {
		t.Fatalf("lang = %#v, want %#v (must ride through appendCodeCallRow for the self-loop tally)", got, want)
	}
}

// TestExtractCodeCallRowsLogsSelfLoopTallyByLanguage proves the observe-only
// telemetry added alongside the #5332 fix: a materialized self-loop row is
// tallied and logged per language, without being dropped from the returned
// rows (see recordCodeCallSelfLoopWritten in code_call_materialization_extract.go).
func TestExtractCodeCallRowsLogsSelfLoopTallyByLanguage(t *testing.T) {
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, nil)))
	defer slog.SetDefault(previous)

	_, rows := ExtractCodeCallRows(
		selfLoopRepoEnvelopes("repo-fib-log", "fib.dart", "content-entity:fib-log", "dart"),
	)
	if len(rows) != 1 {
		t.Fatalf("len(rows) = %d, want 1", len(rows))
	}

	var found bool
	for _, line := range strings.Split(strings.TrimSpace(logs.String()), "\n") {
		if line == "" {
			continue
		}
		var entry map[string]any
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Fatalf("unmarshal log line %q: %v", line, err)
		}
		if entry["event"] != "code_call_self_loop_written" {
			continue
		}
		found = true
		if got, want := entry["total"], float64(1); got != want {
			t.Fatalf("total = %#v, want %#v in %#v", got, want, entry)
		}
		byLang, ok := entry["by_lang"].(map[string]any)
		if !ok {
			t.Fatalf("by_lang = %T, want map[string]any in %#v", entry["by_lang"], entry)
		}
		if got, want := byLang["dart"], float64(1); got != want {
			t.Fatalf("by_lang[dart] = %#v, want %#v in %#v", got, want, entry)
		}
	}
	if !found {
		t.Fatalf("expected a code_call_self_loop_written log entry, got logs: %s", logs.String())
	}
}
