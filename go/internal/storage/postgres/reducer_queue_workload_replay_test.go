// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"strings"
	"testing"
)

func TestReducerQueueReplayWorkloadMaterializationDirtiesInFlightWork(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{
		execResults: []sql.Result{rowsAffectedResult{rowsAffected: 1}},
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
	if got, want := len(db.execs), 1; got != want {
		t.Fatalf("exec count = %d, want %d", got, want)
	}
	query := db.execs[0].query
	for _, want := range []string{
		"status IN ('claimed', 'running')",
		"cross_scope_replay_required",
		"THEN TRUE",
		"status = 'succeeded'",
		"THEN 'pending'",
	} {
		if !strings.Contains(query, want) {
			t.Fatalf("workload replay query missing %q:\n%s", want, query)
		}
	}
}
