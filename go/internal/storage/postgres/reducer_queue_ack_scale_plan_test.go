// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// This opt-in diagnostic preserves the existing scale fixture and executes each
// explained statement in a rolled-back transaction. Its timings are plan costs,
// not the uninstrumented scale gate's latency or WAL measurements.
func TestReducerAckFanoutScalePlanProbe(t *testing.T) {
	if os.Getenv("ESHU_ACK_FANOUT_SCALE_PLAN") != "1" {
		t.Skip("set ESHU_ACK_FANOUT_SCALE_PLAN=1 for the 900-scope plan diagnostic")
	}
	db := openContainerImageIdentityAckCapabilityProofDB(t)
	db.SetMaxOpenConns(12)
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Minute)
	defer cancel()
	const owner = "reducer-5740-scale"
	seedCrossScopeCompletionScale(t, ctx, db, 900, 25, owner)
	probe := &ackScalePlanDB{SQLDB: SQLDB{DB: db}, t: t, calls: make(map[string]int)}
	queue := ReducerQueue{db: probe, LeaseOwner: owner, LeaseDuration: time.Minute}
	store := NewCrossScopeCompletionStore(probe)
	store.Now = func() time.Time { return time.Now().UTC().Add(3 * time.Second) }
	runner := reducer.CrossScopeCompletionRunner{
		Queue: store, LeaseOwner: "fanout-5740-scale", LeaseTTL: time.Minute,
		BatchSize: 500, Now: store.Now,
	}
	ackCrossScopeCompletionScaleDomain(t, ctx, db, queue, reducer.DomainContainerImageIdentity, 900, 57)
	if processed, _, err := runner.RunOnce(ctx); err != nil || !processed {
		t.Fatalf("identity fanout: processed=%t err=%v", processed, err)
	}
	claimCrossScopeCompletionScaleDomain(t, ctx, db, reducer.DomainCICDRunCorrelation, owner)
	claimCrossScopeCompletionScaleDomain(t, ctx, db, reducer.DomainSupplyChainImpact, owner)
	ackCrossScopeCompletionScaleDomain(t, ctx, db, queue, reducer.DomainSupplyChainImpact, 900, 57)
	ackCrossScopeCompletionScaleDomain(t, ctx, db, queue, reducer.DomainCICDRunCorrelation, 900, 57)
	if processed, _, err := runner.RunOnce(ctx); err != nil || !processed {
		t.Fatalf("CI/CD fanout: processed=%t err=%v", processed, err)
	}
	claimCrossScopeCompletionScaleDomain(t, ctx, db, reducer.DomainSupplyChainImpact, owner)
	ackCrossScopeCompletionScaleDomain(t, ctx, db, queue, reducer.DomainSupplyChainImpact, 900, 57)
	assertCrossScopeCompletionScaleTerminal(t, ctx, db, 900, 25)
}

type ackScalePlanDB struct {
	SQLDB
	t     *testing.T
	calls map[string]int
}

func (db *ackScalePlanDB) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	domain := "generic"
	if strings.Contains(query, "domain = 'container_image_identity'") {
		domain = "identity"
	} else if strings.Contains(query, "domain = 'ci_cd_run_correlation'") {
		domain = "cicd"
	}
	db.calls[domain]++
	if db.calls[domain] == 1 || db.calls[domain] == 30 {
		name := fmt.Sprintf("%s_ack_%d", domain, db.calls[domain])
		db.explain(ctx, name, query, args...)
		if strings.Contains(query, "WITH locked_work AS MATERIALIZED") {
			// Diagnostic-only candidate: reuse the tuple locked by this statement.
			// This is never substituted into the production execution below.
			variant := strings.Replace(query, "SELECT work_item_id\n    FROM fact_work_items", "SELECT work_item_id, ctid AS locked_tid\n    FROM fact_work_items", 1)
			variant = strings.Replace(variant, "work.work_item_id IN (SELECT work_item_id FROM locked_work)", "work.ctid = ANY(ARRAY(SELECT locked_tid FROM locked_work))", 1)
			variant = strings.Replace(variant, "WHERE work_item_id IN (SELECT work_item_id FROM locked_work)", "WHERE ctid = ANY(ARRAY(SELECT locked_tid FROM locked_work))", 1)
			db.explain(ctx, name+"_tuple_target_shim", variant, args...)
			// Compare immutable-ID targeting after the locking SELECT has fully
			// checked eligibility. The production query still runs unchanged.
			const barrier = "AND (SELECT count(*) FROM locked_work) > 0"
			end := strings.Index(query, barrier) + len(barrier)
			idVariant := query[:end] + "\n"
			if returning := strings.Index(query[end:], "RETURNING"); returning >= 0 {
				idVariant += query[end+returning:]
			}
			db.explain(ctx, name+"_locked_id_target_shim", idVariant, args...)
		}
	}
	return db.SQLDB.ExecContext(ctx, query, args...)
}

func (db *ackScalePlanDB) QueryContext(ctx context.Context, query string, args ...any) (Rows, error) {
	if query == fanoutCrossScopeCompletionQuery {
		db.explain(ctx, fmt.Sprintf("fanout_%v", args[2]), query, args...)
	}
	return db.SQLDB.QueryContext(ctx, query, args...)
}

func (db *ackScalePlanDB) explain(ctx context.Context, name, query string, args ...any) {
	db.t.Helper()
	tx, err := db.DB.BeginTx(ctx, nil)
	if err != nil {
		db.t.Fatal(err)
	}
	var plan string
	err = tx.QueryRowContext(ctx, "EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) "+query, args...).Scan(&plan)
	rollbackErr := tx.Rollback()
	if err != nil || rollbackErr != nil {
		db.t.Fatalf("explain %s: query=%v rollback=%v", name, err, rollbackErr)
	}
	db.t.Logf("6488 scale plan=%s (rolled back): %s", name, plan)
}
