// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package sharedintent

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestUnusableDeltaScopeErrorMessages(t *testing.T) {
	t.Parallel()

	rowErr := NewUnusableDeltaScopeRowError("repo-1")
	if got := rowErr.Error(); !strings.Contains(got, "delta retract requires delta_file_paths") || !strings.Contains(got, `"repo-1"`) {
		t.Fatalf("row error = %q, want the delta_file_paths prefix and the repository", got)
	}
	batchErr := NewUnusableDeltaScopeBatchError(3)
	if got := batchErr.Error(); !strings.Contains(got, "delta retract requires delta_file_paths") || !strings.Contains(got, "3") {
		t.Fatalf("batch error = %q, want the delta_file_paths prefix and the row count", got)
	}
}

func TestIsUnusableDeltaScopeErrorMatchesThroughWrapping(t *testing.T) {
	t.Parallel()

	typed := NewUnusableDeltaScopeRowError("repo-1")
	if !IsUnusableDeltaScopeError(typed) {
		t.Fatalf("IsUnusableDeltaScopeError(bare typed error) = false, want true")
	}
	wrapped := fmt.Errorf("retract edges: %w", typed)
	if !IsUnusableDeltaScopeError(wrapped) {
		t.Fatalf("IsUnusableDeltaScopeError(wrapped typed error) = false, want true")
	}
	// A plain error whose text merely mentions delta paths is a backend fault
	// until proven otherwise: matching on text would terminalize transient
	// failures that must keep retrying.
	plain := errors.New(`delta retract requires delta_file_paths: repository "repo-1" carries none`)
	if IsUnusableDeltaScopeError(plain) {
		t.Fatalf("IsUnusableDeltaScopeError(plain text error) = true, want false")
	}
	if IsUnusableDeltaScopeError(nil) {
		t.Fatalf("IsUnusableDeltaScopeError(nil) = true, want false")
	}
	if IsUnusableDeltaScopeError(errors.New("connection refused")) {
		t.Fatalf("IsUnusableDeltaScopeError(transient error) = true, want false")
	}
}

func TestRowHasUnusableDeltaScope(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		payload map[string]any
		usable  bool
	}{
		{name: "nil payload is usable", payload: nil, usable: true},
		{name: "no delta flag is usable", payload: map[string]any{"repo_id": "r"}, usable: true},
		{name: "explicit false flag is usable", payload: map[string]any{"delta_projection": false}, usable: true},
		{
			name:    "string true flag is not a delta row",
			payload: map[string]any{"delta_projection": "true"},
			usable:  true,
		},
		{
			name:    "flagged with paths is usable",
			payload: map[string]any{"delta_projection": true, "delta_file_paths": []string{"src/a.py"}},
			usable:  true,
		},
		{
			name:    "flagged with any-list paths is usable",
			payload: map[string]any{"delta_projection": true, "delta_file_paths": []any{"src/a.py"}},
			usable:  true,
		},
		{
			name:    "flagged with missing paths is unusable",
			payload: map[string]any{"delta_projection": true},
			usable:  false,
		},
		{
			name:    "flagged with empty paths is unusable",
			payload: map[string]any{"delta_projection": true, "delta_file_paths": []string{}},
			usable:  false,
		},
		{
			name:    "flagged with blank-only paths is unusable",
			payload: map[string]any{"delta_projection": true, "delta_file_paths": []string{"", "   "}},
			usable:  false,
		},
		{
			name:    "flagged with scalar paths is unusable",
			payload: map[string]any{"delta_projection": true, "delta_file_paths": "src/a.py"},
			usable:  false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			row := Row{IntentID: "intent-1", Payload: tc.payload}
			if got := RowHasUsableDeltaScope(row); got != tc.usable {
				t.Fatalf("RowHasUsableDeltaScope() = %v, want %v", got, tc.usable)
			}
			if got := RowHasUnusableDeltaScope(row); got == tc.usable {
				t.Fatalf("RowHasUnusableDeltaScope() = %v, want %v", got, !tc.usable)
			}
		})
	}
}

func TestSplitUnusableDeltaScopeRowsPreservesOrder(t *testing.T) {
	t.Parallel()

	usable := Row{IntentID: "ok-1", Payload: map[string]any{"delta_projection": true, "delta_file_paths": []string{"a"}}}
	poisoned := Row{IntentID: "bad-1", Payload: map[string]any{"delta_projection": true, "delta_file_paths": []string{}}}
	plain := Row{IntentID: "plain-1", Payload: map[string]any{"action": "upsert"}}

	gotUsable, gotUnusable := SplitUnusableDeltaScopeRows([]Row{poisoned, usable, plain})
	if len(gotUsable) != 2 || gotUsable[0].IntentID != "ok-1" || gotUsable[1].IntentID != "plain-1" {
		t.Fatalf("usable = %v, want [ok-1 plain-1] in order", intentIDsOf(gotUsable))
	}
	if len(gotUnusable) != 1 || gotUnusable[0].IntentID != "bad-1" {
		t.Fatalf("unusable = %v, want [bad-1]", intentIDsOf(gotUnusable))
	}
}

func intentIDsOf(rows []Row) []string {
	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.IntentID)
	}
	return ids
}
