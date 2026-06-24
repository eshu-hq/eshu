// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/projector"
	"github.com/eshu-hq/eshu/go/internal/reducer"
)

func TestReducerQueuePreservesIntentPayloadMetadata(t *testing.T) {
	t.Parallel()
	db := &reducerRecordingDB{}
	queue := NewReducerQueue(db, "worker-1", time.Minute)
	now := time.Date(2026, time.June, 18, 2, 30, 0, 0, time.UTC)
	intent := projector.ReducerIntent{
		ScopeID:      "scope-1",
		GenerationID: "gen-1",
		Domain:       reducer.DomainCodeFunctionSummary,
		EntityKey:    "code_function_summary:scope-1",
		Reason:       "value-flow gate scanned; reconcile function summaries",
		FactID:       "marker-1",
		SourceSystem: "git",
		Payload: map[string]any{
			"repo_id":       "repo-1",
			"full_snapshot": true,
			"source_system": "malicious-overwrite",
		},
	}

	if err := queue.enqueueReducerBatch(context.Background(), []projector.ReducerIntent{intent}, now); err != nil {
		t.Fatalf("enqueueReducerBatch error: %v", err)
	}
	payload, err := unmarshalPayload(db.execs[0].args[7].([]byte))
	if err != nil {
		t.Fatalf("unmarshal enqueue payload: %v", err)
	}
	if payload["repo_id"] != "repo-1" || payload["full_snapshot"] != true {
		t.Fatalf("payload = %#v, want summary metadata", payload)
	}
	if payload["source_system"] != "git" {
		t.Fatalf("source_system = %#v, want reserved key to win", payload["source_system"])
	}

	rows := &queueFakeRows{rows: [][]any{{
		"work-1",
		"scope-1",
		"gen-1",
		string(reducer.DomainCodeFunctionSummary),
		1,
		now,
		now,
		db.execs[0].args[7].([]byte),
	}}}
	claimed, err := scanReducerIntent(rows)
	if err != nil {
		t.Fatalf("scanReducerIntent error: %v", err)
	}
	if claimed.Payload["repo_id"] != "repo-1" || claimed.Payload["full_snapshot"] != true {
		t.Fatalf("claimed payload = %#v, want metadata preserved", claimed.Payload)
	}
}
