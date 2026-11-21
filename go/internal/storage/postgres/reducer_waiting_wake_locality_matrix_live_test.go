// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #11114 no-lost-wake matrix for same-scope producer-terminal waits once the cross-scope leg no longer reaches them, with no foreign ack present: park-after-ack race on separate connections, duplicate ack, ack without wake then one audit pass, mixed AckBatch across cross-scope and same-scope domains, and a superseded generation's ack; disposable database per test, about 20 s locally; enrolled in the live-postgres-readiness runner
//eshu:readiness:env ESHU_REDUCER_FAIRNESS_PROOF_DSN ESHU_POSTGRES_DSN

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
)

// assertReopened fails unless the waiter has no wait row, is retrying at
// its parked attempt count, and is claimable by wakeAt.
func assertReopened(t *testing.T, ctx context.Context, db *sql.DB, id string, attempts int, wakeAt time.Time) {
	t.Helper()
	got := readWaitingRow(t, ctx, db, id)
	if got.waitRows != 0 || got.status != "retrying" || got.attempts != attempts || got.visibleAt.Time.After(wakeAt) {
		t.Fatalf("%s not reopened: %+v (want claimable by %s at attempt %d)", id, got, wakeAt, attempts)
	}
}

// TestProducerTerminalWakeLocalityParkAfterAckRaceLive: for eight scopes at
// once, the reconciliation Fail and its own scope's impact claim and Ack run
// on separate connections. The impact row becomes claimable the moment the
// Fail's retry UPDATE commits, so its Ack and post-commit wake can land
// before, between, or after the wait INSERT and the park re-check. Every
// waiter must end reopened: either the keyed wake sees the committed wait
// row or the re-check sees the committed Ack.
func TestProducerTerminalWakeLocalityParkAfterAckRaceLive(t *testing.T) {
	ctx := context.Background()
	base, dsn := openWaitingDB(t, ctx)
	clock := time.Now().UTC().Truncate(time.Second)
	const scopes = 8
	type pair struct {
		waiter, scopeID string
		queue           ReducerQueue
		intent          reducercontract.Intent
	}
	pairs := make([]pair, 0, scopes)
	for i := 0; i < scopes; i++ {
		scopeID := fmt.Sprintf("waiting-11114-race-%02d", i)
		waiter := fmt.Sprintf("recon-11114-race-%02d", i)
		gen := fmt.Sprintf("race-g1-%02d", i)
		addOwnGateGenerationSeed(t, ctx, base, scopeID, fmt.Sprintf("race-g0-%02d", i), gen, clock)
		addOwnGateItem(t, ctx, base, waiter, scopeID, gen, reducercontract.DomainSecurityAlertReconciliation, clock)
		queue, intent := claimWaitingRow(t, ctx, base, &clock, reducercontract.DomainSecurityAlertReconciliation)
		addOwnGateItem(t, ctx, base, "impact-"+waiter, intent.ScopeID, gen, reducercontract.DomainSupplyChainImpact, clock)
		pairs = append(pairs, pair{waiter: intent.IntentID, scopeID: intent.ScopeID, queue: queue, intent: intent})
	}
	open := func() *sql.DB {
		conn, err := sql.Open("pgx", dsn)
		if err != nil {
			t.Fatalf("open connection: %v", err)
		}
		t.Cleanup(func() { _ = conn.Close() })
		return conn
	}
	failConn, ackConn := open(), open()
	var wg sync.WaitGroup
	errs := make(chan error, 2*scopes)
	for _, p := range pairs {
		wg.Add(2)
		go func() {
			defer wg.Done()
			queue := p.queue
			queue.database = SQLDB{DB: failConn}
			if err := queue.Fail(ctx, p.intent, impactNotReadyCause{}); err != nil {
				errs <- fmt.Errorf("fail %s: %w", p.waiter, err)
			}
		}()
		go func() {
			defer wg.Done()
			queue := newOwnGateQueue(ackConn, &clock, reducercontract.DomainSupplyChainImpact)
			deadline := time.Now().Add(30 * time.Second)
			for time.Now().Before(deadline) {
				var live int
				if err := ackConn.QueryRowContext(ctx, `SELECT count(*) FROM fact_work_items
WHERE work_item_id = $1 AND status IN ('claimed', 'running')`, p.waiter).Scan(&live); err != nil {
					errs <- err
					return
				}
				if live == 0 {
					break
				}
				time.Sleep(time.Millisecond)
			}
			for time.Now().Before(deadline) {
				intent, ok, err := queue.Claim(ctx)
				if err != nil {
					errs <- fmt.Errorf("claim impact for %s: %w", p.scopeID, err)
					return
				}
				if !ok {
					time.Sleep(time.Millisecond)
					continue
				}
				if err := queue.Ack(ctx, intent, reducercontract.Result{}); err != nil {
					errs <- fmt.Errorf("ack impact %s: %w", intent.IntentID, err)
				}
				return
			}
			errs <- fmt.Errorf("impact for %s never claimable", p.scopeID)
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
	if t.Failed() {
		return
	}
	var acked int
	if err := base.QueryRowContext(ctx, `SELECT count(*) FROM fact_work_items
WHERE domain = 'supply_chain_impact' AND status = 'succeeded'`).Scan(&acked); err != nil || acked != scopes {
		t.Fatalf("acked impact rows = %d (err %v), want %d", acked, err, scopes)
	}
	for _, p := range pairs {
		assertReopened(t, ctx, base, p.waiter, 1, clock)
	}
}

// TestProducerTerminalWakeLocalityDuplicateAckLive: a repeated Ack of the
// same impact claim is rejected by the lease fence before any wake runs, so
// the waiter its first Ack reopened is not touched again.
func TestProducerTerminalWakeLocalityDuplicateAckLive(t *testing.T) {
	ctx := context.Background()
	base, _ := openWaitingDB(t, ctx)
	clock := time.Now().UTC().Truncate(time.Second)
	const scopeID = "waiting-11114-dup"
	addOwnGateGenerationSeed(t, ctx, base, scopeID, "g0", "g1", clock)
	addOwnGateItem(t, ctx, base, "recon-11114-dup", scopeID, "g1", reducercontract.DomainSecurityAlertReconciliation, clock)
	parkNarrowedWaiter(t, ctx, base, &clock, "recon-11114-dup",
		reducercontract.DomainSecurityAlertReconciliation, impactNotReadyCause{})
	addOwnGateItem(t, ctx, base, "impact-11114-dup", scopeID, "g1", reducercontract.DomainSupplyChainImpact, clock)
	clock = clock.Add(time.Minute)
	queue, intent := claimWaitingRow(t, ctx, base, &clock, reducercontract.DomainSupplyChainImpact)
	if err := queue.Ack(ctx, intent, reducercontract.Result{}); err != nil {
		t.Fatalf("first Ack: %v", err)
	}
	assertReopened(t, ctx, base, "recon-11114-dup", 1, clock)
	first := readWaitingRow(t, ctx, base, "recon-11114-dup")

	clock = clock.Add(time.Minute)
	if err := queue.Ack(ctx, intent, reducercontract.Result{}); !errors.Is(err, ErrReducerClaimRejected) {
		t.Fatalf("duplicate Ack = %v, want ErrReducerClaimRejected", err)
	}
	if again := readWaitingRow(t, ctx, base, "recon-11114-dup"); again != first {
		t.Fatalf("duplicate Ack moved the waiter:\nfirst = %+v\nagain = %+v", first, again)
	}
}

// TestProducerTerminalWakeLocalityAuditReopensMissedWakeLive: the impact row
// reaches succeeded with no Ack wake (a crash between the Ack commit and its
// post-commit wake). The wait stays parked until one audit pass, which reads
// the waiter's own scope and generation and reopens it.
func TestProducerTerminalWakeLocalityAuditReopensMissedWakeLive(t *testing.T) {
	ctx := context.Background()
	base, _ := openWaitingDB(t, ctx)
	clock := time.Now().UTC().Truncate(time.Second)
	const scopeID = "waiting-11114-audit"
	addOwnGateGenerationSeed(t, ctx, base, scopeID, "g0", "g1", clock)
	addOwnGateItem(t, ctx, base, "recon-11114-audit", scopeID, "g1", reducercontract.DomainSecurityAlertReconciliation, clock)
	parked := parkNarrowedWaiter(t, ctx, base, &clock, "recon-11114-audit",
		reducercontract.DomainSecurityAlertReconciliation, impactNotReadyCause{})
	addOwnGateItem(t, ctx, base, "impact-11114-audit", scopeID, "g1", reducercontract.DomainSupplyChainImpact, clock)
	if _, err := base.ExecContext(ctx, `UPDATE fact_work_items SET status = 'succeeded' WHERE work_item_id = 'impact-11114-audit'`); err != nil {
		t.Fatalf("finish impact without a wake: %v", err)
	}
	if got := readWaitingRow(t, ctx, base, "recon-11114-audit"); got.waitRows != 1 || !got.visibleAt.Time.Equal(parked.visibleAt.Time) {
		t.Fatalf("waiter moved without a wake: %+v", got)
	}
	clock = clock.Add(DefaultWaitingAuditInterval)
	reopened, _, _, err := AuditWaitingItems(ctx, SQLDB{DB: base}, nil, clock, DefaultWaitingAuditInterval, 100)
	if err != nil {
		t.Fatalf("audit: %v", err)
	}
	if reopened != 1 {
		t.Fatalf("audit reopened = %d, want 1", reopened)
	}
	assertReopened(t, ctx, base, "recon-11114-audit", 1, clock)
}

// TestProducerTerminalWakeLocalityMixedAckBatchLive: one AckBatch carries a
// workload_materialization Ack (repository scope R, cross-scope domain) and
// a supply_chain_impact Ack (scope A, same-scope domain). The catalog wait
// reopens through the cross-scope leg, scope A's reconciliation through its
// keyed wake, and scope X's reconciliation, whose impact never acked, stays
// parked with visible_at and attempt_count unchanged.
func TestProducerTerminalWakeLocalityMixedAckBatchLive(t *testing.T) {
	ctx := context.Background()
	base, _ := openWaitingDB(t, ctx)
	clock := time.Now().UTC().Truncate(time.Second)
	for _, name := range []string{"catalog", "repo", "a", "x"} {
		addOwnGateGenerationSeed(t, ctx, base, "waiting-11114-mix-"+name, name+"0", name+"1", clock)
	}
	addOwnGateItem(t, ctx, base, "catalog-11114-mix", "waiting-11114-mix-catalog", "catalog1", reducercontract.DomainServiceCatalogCorrelation, clock)
	parkNarrowedWaiter(t, ctx, base, &clock, "catalog-11114-mix", reducercontract.DomainServiceCatalogCorrelation, runtimeNotReadyCause{})
	addOwnGateItem(t, ctx, base, "recon-11114-mix-a", "waiting-11114-mix-a", "a1", reducercontract.DomainSecurityAlertReconciliation, clock)
	parkNarrowedWaiter(t, ctx, base, &clock, "recon-11114-mix-a", reducercontract.DomainSecurityAlertReconciliation, impactNotReadyCause{})
	addOwnGateItem(t, ctx, base, "recon-11114-mix-x", "waiting-11114-mix-x", "x1", reducercontract.DomainSecurityAlertReconciliation, clock)
	parkedX := parkNarrowedWaiter(t, ctx, base, &clock, "recon-11114-mix-x", reducercontract.DomainSecurityAlertReconciliation, impactNotReadyCause{})

	clock = clock.Add(time.Minute)
	addOwnGateItem(t, ctx, base, "workload-11114-mix", "waiting-11114-mix-repo", "repo1", reducercontract.DomainWorkloadMaterialization, clock)
	addOwnGateItem(t, ctx, base, "impact-11114-mix-a", "waiting-11114-mix-a", "a1", reducercontract.DomainSupplyChainImpact, clock)
	_, workload := claimWaitingRow(t, ctx, base, &clock, reducercontract.DomainWorkloadMaterialization)
	batchQueue, impact := claimWaitingRow(t, ctx, base, &clock, reducercontract.DomainSupplyChainImpact)
	if err := batchQueue.AckBatch(ctx, []reducercontract.Intent{workload, impact}, nil); err != nil {
		t.Fatalf("AckBatch: %v", err)
	}
	assertReopened(t, ctx, base, "catalog-11114-mix", 1, clock)
	assertReopened(t, ctx, base, "recon-11114-mix-a", 1, clock)
	if got := readWaitingRow(t, ctx, base, "recon-11114-mix-x"); got != parkedX {
		t.Fatalf("scope X's reconciliation moved on an unrelated batch:\nparked = %+v\nafter  = %+v", parkedX, got)
	}
}

// TestProducerTerminalWakeLocalitySupersededGenerationLive: the waiter waits
// on generation g1. An impact Ack for the superseded generation g0 of the
// same scope names a different key, so it leaves the wait parked; the g1
// impact Ack reopens it.
func TestProducerTerminalWakeLocalitySupersededGenerationLive(t *testing.T) {
	ctx := context.Background()
	base, _ := openWaitingDB(t, ctx)
	clock := time.Now().UTC().Truncate(time.Second)
	const scopeID = "waiting-11114-gen"
	addOwnGateGenerationSeed(t, ctx, base, scopeID, "g0", "g1", clock)
	addOwnGateItem(t, ctx, base, "recon-11114-gen", scopeID, "g1", reducercontract.DomainSecurityAlertReconciliation, clock)
	parked := parkNarrowedWaiter(t, ctx, base, &clock, "recon-11114-gen",
		reducercontract.DomainSecurityAlertReconciliation, impactNotReadyCause{})
	if _, err := base.ExecContext(ctx, `UPDATE scope_generations SET status = 'superseded' WHERE scope_id = $1 AND generation_id = 'g0'`, scopeID); err != nil {
		t.Fatalf("supersede g0: %v", err)
	}

	clock = clock.Add(time.Minute)
	ackProducer(t, ctx, base, &clock, "impact-11114-gen-g0", scopeID, "g0", reducercontract.DomainSupplyChainImpact)
	if got := readWaitingRow(t, ctx, base, "recon-11114-gen"); got != parked {
		t.Fatalf("superseded g0 Ack moved the g1 waiter:\nparked = %+v\nafter  = %+v", parked, got)
	}
	clock = clock.Add(time.Minute)
	ackProducer(t, ctx, base, &clock, "impact-11114-gen-g1", scopeID, "g1", reducercontract.DomainSupplyChainImpact)
	assertReopened(t, ctx, base, "recon-11114-gen", 1, clock)
}
