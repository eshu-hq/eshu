// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// The probe seeds 64 running consumers in the full disposable schema. Explain
// each shipped statement against that same state, rolling back before the next
// plan or race. Run this helper unchanged with baseline production files for a
// comparable BEFORE plan; ESHU_ACK_FANOUT_PLAN_ONLY=1 omits contention trials.
func explainAckFanoutProbe(t *testing.T, ctx context.Context, db *sql.DB, now time.Time, intents []reducer.Intent, lease reducer.CrossScopeCompletionLease) {
	t.Helper()
	ackArgs := []any{now, "ack-6488"}
	for _, intent := range intents {
		ackArgs = append(ackArgs, intent.IntentID)
	}
	edges := reducer.CrossScopeCompletionEdges()
	producers, consumers := make([]string, 0, len(edges)), make([]string, 0, len(edges))
	for _, edge := range edges {
		producers = append(producers, string(edge.Producer))
		consumers = append(consumers, string(edge.Consumer))
	}
	for _, statement := range []struct {
		name, query string
		args        []any
	}{
		{"generic_ack_64", ackReducerWorkBatchQuery(len(intents)), ackArgs},
		{"fanout_64", fanoutCrossScopeCompletionQuery, []any{now, lease.EventID, lease.ProducerDomain, lease.LeaseOwner, lease.ClaimEpoch, 1, producers, consumers}},
	} {
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		var plan string
		err = tx.QueryRowContext(ctx, "EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) "+statement.query, statement.args...).Scan(&plan)
		rollbackErr := tx.Rollback()
		if err != nil {
			t.Fatalf("explain %s: %v", statement.name, err)
		}
		if rollbackErr != nil {
			t.Fatal(rollbackErr)
		}
		t.Logf("6488 plan=%s consumers=%d (rolled back): %s", statement.name, len(intents), plan)
	}
}
