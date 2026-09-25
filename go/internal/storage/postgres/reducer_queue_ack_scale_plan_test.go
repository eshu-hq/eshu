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

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/scope/completion"
)

// This opt-in diagnostic preserves the existing scale fixture and executes each
// explained statement in a rolled-back transaction. Its timings are plan costs,
// not the uninstrumented scale gate's latency or WAL measurements.
func TestReducerAckFanoutScalePlanProbe(t *testing.T) {
	if os.Getenv("ESHU_ACK_FANOUT_SCALE_PLAN") != "1" {
		t.Skip("set ESHU_ACK_FANOUT_SCALE_PLAN=1 for the 900-scope plan diagnostic")
	}
	database := openContainerImageIdentityAckCapabilityProofDB(t)
	database.SetMaxOpenConns(12)
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Minute)
	defer cancel()
	const owner = "reducer-5740-scale"
	seedCrossScopeCompletionScale(t, ctx, database, 900, 25, owner)
	probe := &ackScalePlanDB{SQLDB: SQLDB{DB: database}, t: t, calls: make(map[string]int)}
	queue := ReducerQueue{database: probe, LeaseOwner: owner, LeaseDuration: time.Minute}
	store := completionstore.NewCrossScopeCompletionStore(probe)
	store.Now = func() time.Time { return time.Now().UTC().Add(3 * time.Second) }
	runner := reducer.CrossScopeCompletionRunner{
		Queue: store, LeaseOwner: "fanout-5740-scale", LeaseTTL: time.Minute,
		BatchSize: 500, Now: store.Now,
	}
	ackCrossScopeCompletionScaleDomain(t, ctx, database, queue, reducer.DomainContainerImageIdentity, 900, 57)
	if processed, _, err := runner.RunOnce(ctx); err != nil || !processed {
		t.Fatalf("identity fanout: processed=%t err=%v", processed, err)
	}
	claimCrossScopeCompletionScaleDomain(t, ctx, database, reducer.DomainCICDRunCorrelation, owner)
	claimCrossScopeCompletionScaleDomain(t, ctx, database, reducer.DomainSupplyChainImpact, owner)
	ackCrossScopeCompletionScaleDomain(t, ctx, database, queue, reducer.DomainSupplyChainImpact, 900, 57)
	ackCrossScopeCompletionScaleDomain(t, ctx, database, queue, reducer.DomainCICDRunCorrelation, 900, 57)
	if processed, _, err := runner.RunOnce(ctx); err != nil || !processed {
		t.Fatalf("CI/CD fanout: processed=%t err=%v", processed, err)
	}
	claimCrossScopeCompletionScaleDomain(t, ctx, database, reducer.DomainSupplyChainImpact, owner)
	ackCrossScopeCompletionScaleDomain(t, ctx, database, queue, reducer.DomainSupplyChainImpact, 900, 57)
	assertCrossScopeCompletionScaleTerminal(t, ctx, database, 900, 25)
}

type ackScalePlanDB struct {
	SQLDB
	t     *testing.T
	calls map[string]int
}

func (database *ackScalePlanDB) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	domain := "generic"
	if strings.Contains(query, "domain = 'container_image_identity'") {
		domain = "identity"
	} else if strings.Contains(query, "domain = 'ci_cd_run_correlation'") ||
		strings.Contains(query, "'ci_cd_run_correlation'::text AS expected_domain") {
		domain = "cicd"
	}
	database.calls[domain]++
	if database.calls[domain] == 1 || database.calls[domain] == 30 {
		name := fmt.Sprintf("%s_ack_%d", domain, database.calls[domain])
		database.explain(ctx, name, query, args...)
	}
	return database.SQLDB.ExecContext(ctx, query, args...)
}

func (database *ackScalePlanDB) QueryContext(ctx context.Context, query string, args ...any) (db.Rows, error) {
	if query == completionstore.FanoutCrossScopeCompletionQuery {
		database.explain(ctx, fmt.Sprintf("fanout_%v", args[2]), query, args...)
	}
	return database.SQLDB.QueryContext(ctx, query, args...)
}

func (database *ackScalePlanDB) explain(ctx context.Context, name, query string, args ...any) {
	database.t.Helper()
	tx, err := database.DB.BeginTx(ctx, nil)
	if err != nil {
		database.t.Fatal(err)
	}
	var plan string
	err = tx.QueryRowContext(ctx, "EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) "+query, args...).Scan(&plan)
	rollbackErr := tx.Rollback()
	if err != nil || rollbackErr != nil {
		database.t.Fatalf("explain %s: query=%v rollback=%v", name, err, rollbackErr)
	}
	database.t.Logf("6488 scale plan=%s (rolled back): %s", name, plan)
}
