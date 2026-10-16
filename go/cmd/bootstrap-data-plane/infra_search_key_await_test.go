// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
)

// TestIndexStateDecidesAfterAnAwaitError pins how backfillInfraSearchKeys
// treats a db.awaitIndex error. The state read that follows is a separate,
// successful statement, so its answer is the ground truth: ONLINE passes even
// though the wait errored (a slow population that finished, or a dropped
// connection during the wait), any other state fails with both errors, a
// FAILED index fails closed with the runbook, and a failed state read fails.
func TestIndexStateDecidesAfterAnAwaitError(t *testing.T) {
	t.Parallel()

	awaitErr := errors.New("index infra_search_key_ft still populating after 108 seconds")
	for _, tc := range []struct {
		name      string
		state     string
		stateErr  error
		wantErr   bool
		wantParts []string
		wantLog   string
	}{
		{name: "online after await error", state: "ONLINE", wantLog: `"await_error":"infra search key index wait: ` + awaitErr.Error()},
		{name: "populating after await error", state: "POPULATING", wantErr: true, wantParts: []string{"is POPULATING", awaitErr.Error()}},
		{name: "failed after await error", state: "FAILED", wantErr: true, wantParts: []string{"is FAILED", infraSearchKeyRunbook}},
		{name: "state read fails", stateErr: errors.New("connection reset"), wantErr: true, wantParts: []string{"index state", "connection reset", awaitErr.Error()}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var events []string
			var logs bytes.Buffer
			runner := &fakeGraphRunner{events: &events, indexState: tc.state, awaitErr: awaitErr, stateErr: tc.stateErr}
			err := backfillInfraSearchKeys(context.Background(), runner, slog.New(slog.NewJSONHandler(&logs, nil)), 30)
			if (err != nil) != tc.wantErr {
				t.Fatalf("backfillInfraSearchKeys() error = %v, want error %v", err, tc.wantErr)
			}
			for _, part := range tc.wantParts {
				if !strings.Contains(err.Error(), part) {
					t.Errorf("error %q does not contain %q", err, part)
				}
			}
			if tc.wantLog != "" && !strings.Contains(logs.String(), tc.wantLog) {
				t.Errorf("index state log does not carry the await error %q:\n%s", tc.wantLog, logs.String())
			}
			if !strings.Contains(strings.Join(events, ","), "await_index,index_state") {
				t.Errorf("events = %v, want the wait then the state read", events)
			}
		})
	}
}
