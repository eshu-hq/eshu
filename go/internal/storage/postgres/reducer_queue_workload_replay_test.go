// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"strings"
	"testing"
)

func TestReducerQueueReplayWorkloadMaterializationDirtiesInFlightWork(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{
		// The schedule matches a claimed row and newly flags it for replay.
		queryResponses: []queueFakeRows{{rows: [][]any{{true}}}},
	}
	queue := ReducerQueue{database: db}

	replayed, err := queue.ReplayWorkloadMaterialization(
		context.Background(),
		"scope-1",
		"gen-1",
		"repo:service-gha",
	)
	if err != nil {
		t.Fatalf("ReplayWorkloadMaterialization() error = %v, want nil", err)
	}
	if !replayed {
		t.Fatal("ReplayWorkloadMaterialization() replayed = false, want true")
	}
	if got, want := len(db.queries), 1; got != want {
		t.Fatalf("query count = %d, want %d", got, want)
	}
	if got := len(db.execs); got != 0 {
		t.Fatalf("exec count = %d, want 0: a scheduled replay needs no enqueue", got)
	}
	query := db.queries[0].query
	for _, want := range []string{
		"status IN ('claimed', 'running')",
		"cross_scope_replay_required",
		"THEN TRUE",
		"status = 'succeeded'",
		"THEN 'pending'",
		"last_requeue_reason",
		"'cross_scope_replay'",
		"AS requeued",
	} {
		if !strings.Contains(query, want) {
			t.Fatalf("workload replay query missing %q:\n%s", want, query)
		}
	}
}
