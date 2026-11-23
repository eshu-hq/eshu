// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #10880 PR-B2 supports not-ready deferral on the real ReducerQueue claim, fail and retry SQL, the real FactStore loader and the real SecurityAlertReconciliationHandler: a reconciliation whose supports table is not ready defers with the non-counting class across four claim cycles with attempt_count frozen at 1, nothing written and no dead letter, then, once the supports backfill reaches ready, runs at attempt 1 and writes exactly the decisions the finding-fact read control writes
//eshu:readiness:env ESHU_POSTGRES_DSN ESHU_POSTGRES_DISPOSABLE

import (
	"context"
	"database/sql"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/reducer/securityalert"
	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/backfill"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// supportsDeferWriter records every reconciliation write.
type supportsDeferWriter struct {
	mu        sync.Mutex
	calls     int
	decisions []securityalert.SecurityAlertReconciliationDecision
}

func (w *supportsDeferWriter) WriteSecurityAlertReconciliations(
	_ context.Context,
	write securityalert.SecurityAlertReconciliationWrite,
) (securityalert.SecurityAlertReconciliationWriteResult, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.calls++
	w.decisions = append([]securityalert.SecurityAlertReconciliationDecision(nil), write.Decisions...)
	return securityalert.SecurityAlertReconciliationWriteResult{CanonicalWrites: len(write.Decisions)}, nil
}

// countingExecQueryer counts the statements a deferral cycle sends, so the
// proof can report the poll cost (statements, wall time, WAL bytes) per cycle.
type countingExecQueryer struct {
	SQLDB
	statements int
}

func (c *countingExecQueryer) QueryContext(ctx context.Context, query string, args ...any) (db.Rows, error) {
	c.statements++
	return c.SQLDB.QueryContext(ctx, query, args...)
}

func (c *countingExecQueryer) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	c.statements++
	return c.SQLDB.ExecContext(ctx, query, args...)
}

func walLSN(t *testing.T, ctx context.Context, database *sql.DB) string {
	t.Helper()
	var lsn string
	if err := database.QueryRowContext(ctx, `SELECT pg_current_wal_lsn()::text`).Scan(&lsn); err != nil {
		t.Fatalf("read WAL lsn: %v", err)
	}
	return lsn
}

func walBytesBetween(t *testing.T, ctx context.Context, database *sql.DB, from, to string) int64 {
	t.Helper()
	var bytes int64
	if err := database.QueryRowContext(ctx, `SELECT pg_wal_lsn_diff($1::pg_lsn, $2::pg_lsn)::bigint`, to, from).Scan(&bytes); err != nil {
		t.Fatalf("diff WAL lsn: %v", err)
	}
	return bytes
}

// supportsControlLoader is the control: the same store, but the active evidence
// is read from facts, impact findings included (the statement the loader ran
// before #11198), so its decisions are what the finding-fact read produces.
type supportsControlLoader struct {
	FactStore
	db *sql.DB
	t  *testing.T
}

func (l supportsControlLoader) ListActiveSecurityAlertReconciliationFacts(
	ctx context.Context,
	filter securityalert.SecurityAlertReconciliationFactFilter,
) ([]facts.Envelope, error) {
	rows, err := l.db.QueryContext(ctx, legacyReconciliationOracleQuery(l.t),
		cleanStringFilterValues(filter.RepositoryIDs), cleanStringFilterValues(filter.PackageIDs),
		cleanStringFilterValues(filter.CVEIDs), cleanStringFilterValues(filter.GHSAIDs), "", 100000)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []facts.Envelope
	for rows.Next() {
		envelope, err := backfill.ScanFactEnvelope(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, envelope)
	}
	return out, rows.Err()
}

func supportsDeferConsumptions(
	[]securityalert.ProviderSecurityAlert,
	[]facts.Envelope,
) []securityalert.SecurityAlertConsumption {
	return []securityalert.SecurityAlertConsumption{{
		FactID: "consume-b2c", EvidenceKind: "manifest", RepositoryID: "repo:r1", PackageID: "pkg:npm/p1",
		RelativePath: "package-lock.json", ObservedVersion: "1.0.0", ObservedAt: time.Date(2026, 11, 20, 10, 0, 0, 0, time.UTC),
	}}
}

// TestSupplyChainImpactSupportsNotReadyDefersNonCountingLive is the live proof
// of arbiter-b2c's required queue behavior (#10880 PR-B2). The queue, the loader
// and the handler are production code on real PostgreSQL: while the supports
// table is not ready the reconciliation is claimed, refused and deferred four
// times with attempt_count frozen at 1, nothing written, no dead letter; when
// the backfill reaches ready (through the production RunStep) it runs at attempt
// 1 and writes the decisions the finding-fact control writes.
func TestSupplyChainImpactSupportsNotReadyDefersNonCountingLive(t *testing.T) {
	db, ctx := openIsolatedLiveDB(t, "b2c_queue_defer",
		workflowControlIntegrationDSNEnv+" is not set; skipping Postgres integration test")
	b2cSeed(t, ctx, db)
	now, _ := time.Parse(time.RFC3339, b2cNow)
	counting := &countingExecQueryer{SQLDB: SQLDB{DB: db}}
	store := NewFactStore(counting)
	if err := store.UpsertFacts(ctx, []facts.Envelope{{
		FactID: "alert-b2c", ScopeID: b2cScopeA, GenerationID: b2cGenA, FactKind: facts.SecurityAlertRepositoryAlertFactKind,
		StableFactKey: "alert:b2c", SchemaVersion: facts.SecurityAlertSchemaVersionV1, CollectorKind: "synthetic",
		SourceConfidence: facts.SourceConfidenceReported, ObservedAt: now,
		Payload: map[string]any{
			"repository_id": "repo:r1", "provider": "github_dependabot", "provider_alert_number": int64(7),
			"provider_state": "open", "package_id": "pkg:npm/p1", "package_name": "p1", "ecosystem": "npm",
			"manifest_path": "package-lock.json", "cve_ids": []string{"CVE-2026-0001"}, "ghsa_ids": []string{"GHSA-1111"},
			"vulnerable_range": "<2.0.0", "patched_version": "2.0.0",
		},
	}}); err != nil {
		t.Fatalf("seed alert: %v", err)
	}
	insertReducerFairnessWorkItem(t, ctx, db, reducerFairnessWorkItem{
		workItemID: "recon-b2c", scopeID: b2cScopeA, generationID: b2cGenA,
		domain: string(reducer.DomainSecurityAlertReconciliation), conflictDomain: "scope", conflictKey: b2cScopeA,
		sourceSystem: "security_alert", updatedAt: now.Add(-time.Hour),
	})

	clock := time.Now().UTC()
	queue := ReducerQueue{
		database: counting, LeaseOwner: "recon-worker", LeaseDuration: time.Minute, RetryDelay: time.Minute,
		MaxAttempts: 3, ClaimDomains: []reducercontract.Domain{reducer.DomainSecurityAlertReconciliation},
		Now: func() time.Time { return clock },
	}
	writer := &supportsDeferWriter{}
	handler := securityalert.SecurityAlertReconciliationHandler{
		FactLoader: store, Writer: writer, ExtractManifestConsumptions: supportsDeferConsumptions,
		Now: func() time.Time { return clock },
	}

	for cycle := 1; cycle <= 4; cycle++ {
		statementsBefore, started, walBefore := counting.statements, time.Now(), walLSN(t, ctx, db)
		intent, claimed, err := queue.Claim(ctx)
		if err != nil || !claimed {
			t.Fatalf("cycle %d: Claim() claimed=%v err=%v, want the deferred row", cycle, claimed, err)
		}
		if intent.AttemptCount != 1 {
			t.Fatalf("cycle %d: AttemptCount = %d, want 1 frozen under %s", cycle, intent.AttemptCount, supplychaincore.SupplyChainImpactSupportsNotReadyFailureClass)
		}
		_, handleErr := handler.Handle(ctx, intent)
		if got := failureClassName(handleErr); got != supplychaincore.SupplyChainImpactSupportsNotReadyFailureClass {
			t.Fatalf("cycle %d: Handle() error = %v (class %q), want the supports not-ready deferral", cycle, handleErr, got)
		}
		if !reducercontract.IsExpectedWait(handleErr) {
			t.Fatalf("cycle %d: Handle() error is not an expected wait", cycle)
		}
		if err := queue.Fail(ctx, intent, handleErr); err != nil {
			t.Fatalf("cycle %d: Fail() error = %v", cycle, err)
		}
		status, class, attempts := readRuntimeFenceItem(t, ctx, db, "recon-b2c")
		if status != "retrying" || class != supplychaincore.SupplyChainImpactSupportsNotReadyFailureClass || attempts != 1 {
			t.Fatalf("cycle %d: row = %s/%s/attempt %d, want retrying/%s/1", cycle, status, class, attempts, supplychaincore.SupplyChainImpactSupportsNotReadyFailureClass)
		}
		// Poll cost of one deferral cycle: the claim, the handler's alert fact load
		// and readiness probe, and the retry update. Logged, not gated.
		t.Logf("deferral cycle %d: %d statements, %s wall, %d WAL bytes", cycle,
			counting.statements-statementsBefore, time.Since(started).Round(time.Millisecond),
			walBytesBetween(t, ctx, db, walBefore, walLSN(t, ctx, db)))
		clock = clock.Add(2 * time.Minute)
	}
	if writer.calls != 0 {
		t.Fatalf("reconciliation writer calls = %d, want 0 while the supports are not ready", writer.calls)
	}
	var deadLetters int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM fact_work_items WHERE status = 'dead_letter'`).Scan(&deadLetters); err != nil || deadLetters != 0 {
		t.Fatalf("dead letters = %d (err %v), want 0", deadLetters, err)
	}

	b2cMakeReady(t, ctx, db)
	intent, claimed, err := queue.Claim(ctx)
	if err != nil || !claimed || intent.AttemptCount != 1 {
		t.Fatalf("ready claim = claimed %v attempt %d err %v, want the deferred row at attempt 1", claimed, intent.AttemptCount, err)
	}
	if _, err := handler.Handle(ctx, intent); err != nil {
		t.Fatalf("Handle() after ready error = %v, want nil", err)
	}
	if writer.calls != 1 || len(writer.decisions) == 0 {
		t.Fatalf("after ready: writer calls = %d, decisions = %d, want one write with decisions", writer.calls, len(writer.decisions))
	}

	controlWriter := &supportsDeferWriter{}
	control := securityalert.SecurityAlertReconciliationHandler{
		FactLoader: supportsControlLoader{FactStore: *store, db: db, t: t}, Writer: controlWriter,
		ExtractManifestConsumptions: supportsDeferConsumptions, Now: func() time.Time { return clock },
	}
	if _, err := control.Handle(ctx, intent); err != nil {
		t.Fatalf("control Handle() error = %v, want nil", err)
	}
	if !reflect.DeepEqual(writer.decisions, controlWriter.decisions) {
		t.Fatalf("decisions differ from the finding-fact control:\n got %+v\nwant %+v", writer.decisions, controlWriter.decisions)
	}
}
