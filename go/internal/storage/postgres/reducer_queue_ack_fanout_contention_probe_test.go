// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/scope/completion"
	"github.com/jackc/pgx/v5/pgconn"
)

// This bounded diagnostic uses unmodified production statements and no added
// row locks. A successful run is a negative observation, not a lock-order proof.
func TestReducerContentionGateAckFanoutProbe(t *testing.T) {
	dsn := reducerDomainFairnessDSN()
	if dsn == "" {
		t.Skip("set ESHU_POSTGRES_DSN for the ACK/fanout contention probe")
	}
	t.Setenv("ESHU_POSTGRES_TEST_DSN", dsn)
	database := openContainerImageIdentityAckCapabilityProofDB(t)
	// Two dedicated writers plus an independent observer/reset connection.
	database.SetMaxOpenConns(4)
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	const count = 64
	now := time.Now().UTC()
	intents := make([]reducer.Intent, count)
	for i := range count {
		scope := fmt.Sprintf("repository:6488-%03d", i)
		generation := fmt.Sprintf("generation:6488-%03d", i)
		seedContainerImageIdentityAckScope(t, ctx, database, scope)
		seedContainerImageIdentityAckGeneration(t, ctx, database, scope, generation)
		if _, err := database.ExecContext(ctx, `UPDATE ingestion_scopes SET active_generation_id=$2 WHERE scope_id=$1`, scope, generation); err != nil {
			t.Fatal(err)
		}
		// Reverse the lexical identity relative to heap insertion and scope order.
		id := fmt.Sprintf("ack-fanout-6488-%03d", count-i)
		insertCrossScopeCompletionBaseConsumer(t, ctx, database, id, scope, generation, reducer.DomainSupplyChainImpact, now)
		intents[i] = reducer.Intent{IntentID: id, Domain: reducer.DomainSupplyChainImpact, ClaimedAt: &now}
	}
	if _, err := database.ExecContext(ctx, `ANALYZE fact_work_items; ANALYZE ingestion_scopes; ANALYZE scope_generations`); err != nil {
		t.Fatal(err)
	}
	ackConn := ackFanoutProbeConnection(t, ctx, database, "ack-6488")
	fanoutConn := ackFanoutProbeConnection(t, ctx, database, "fanout-6488")
	queue := ReducerQueue{database: ackConn, LeaseOwner: "ack-6488", LeaseDuration: time.Minute, Now: func() time.Time { return now }}
	store := completionstore.NewCrossScopeCompletionStore(fanoutConn)
	store.Now = func() time.Time { return now }
	for trial := range 40 {
		// The probe measures only its own 64 rows: the standing eshu:global
		// refresh singleton migration 115 seeds stays succeeded and out of
		// every count below.
		if _, err := database.ExecContext(ctx, `UPDATE fact_work_items SET status='running', attempt_count=1, lease_owner='ack-6488', claim_until=$1, last_attempt_at=$2, cross_scope_replay_required=FALSE WHERE scope_id <> 'eshu:global'`, now.Add(time.Hour), now); err != nil {
			t.Fatal(err)
		}
		event := insertCrossScopeCompletionEvent(t, ctx, database, reducer.DomainCICDRunCorrelation, "claimed", "fanout-6488", now.Add(time.Hour), 1, now)
		lease := reducer.CrossScopeCompletionLease{EventID: event, ProducerDomain: reducer.DomainCICDRunCorrelation, LeaseOwner: "fanout-6488", ClaimEpoch: 1}
		if trial == 0 {
			explainAckFanoutProbe(t, ctx, database, now, intents, lease)
			if os.Getenv("ESHU_ACK_FANOUT_PLAN_ONLY") == "1" {
				return
			}
		}
		// The one-row arm cannot form a two-writer row-lock cycle. The larger arm
		// tests overlapping batches across scopes, without forcing a planner shape.
		batch := intents
		if trial < 5 {
			batch = intents[:1]
		}
		start := make(chan struct{})
		results := make(chan error, 2)
		go func() { <-start; results <- queue.AckBatch(ctx, batch, nil) }()
		go func() {
			<-start
			result, err := store.Fanout(ctx, lease, 1)
			if err == nil && (result.EventsProcessed != 1 || result.ProducerItemsProcessed != 1 || result.IntentsEnqueued != count) {
				err = fmt.Errorf("fanout result=%+v", result)
			}
			results <- err
		}()
		close(start)
		for range 2 {
			err := <-results
			if err != nil {
				var pgErr *pgconn.PgError
				if errors.As(err, &pgErr) {
					t.Logf("trial=%d batch=%d SQLSTATE=%s message=%s detail=%s where=%s", trial, len(batch), pgErr.Code, pgErr.Message, pgErr.Detail, pgErr.Where)
				}
				t.Errorf("trial=%d batch=%d production ACK/fanout: %v", trial, len(batch), err)
			}
		}
		if t.Failed() {
			return
		}
		var pending, running, replay, events int
		if err := database.QueryRowContext(ctx, `SELECT count(*) FILTER(WHERE status='pending'), count(*) FILTER(WHERE status='running'), count(*) FILTER(WHERE cross_scope_replay_required) FROM fact_work_items WHERE scope_id <> 'eshu:global'`).Scan(&pending, &running, &replay); err != nil {
			t.Fatal(err)
		}
		if err := database.QueryRowContext(ctx, `SELECT count(*) FROM cross_scope_completion_events`).Scan(&events); err != nil {
			t.Fatal(err)
		}
		if pending != len(batch) || running != count-len(batch) || replay != count-len(batch) || events != 0 {
			t.Fatalf("trial=%d terminal pending=%d running=%d replay=%d events=%d batch=%d", trial, pending, running, replay, events, len(batch))
		}
	}
	t.Log("40 production overlap trials completed; no deadlock observed. This is not proof that lock ordering is safe.")
}

type ackFanoutProbeConn struct{ *sql.Conn }

func (c ackFanoutProbeConn) QueryContext(ctx context.Context, query string, args ...any) (db.Rows, error) {
	return c.Conn.QueryContext(ctx, query, args...)
}

func ackFanoutProbeConnection(t *testing.T, ctx context.Context, database *sql.DB, name string) ackFanoutProbeConn {
	t.Helper()
	conn, err := database.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	if _, err = conn.ExecContext(ctx, `SELECT set_config('application_name',$1,false),set_config('lock_timeout','5s',false),set_config('deadlock_timeout','100ms',false)`, name); err != nil {
		t.Fatal(err)
	}
	return ackFanoutProbeConn{conn}
}
