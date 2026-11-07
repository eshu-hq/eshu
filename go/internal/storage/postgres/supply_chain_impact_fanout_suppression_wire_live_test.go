// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts/supply/chain"
	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// Wiring-condition proofs for the cve_id suppression fan-out (#10019):
// the statement bound, the plan anchor, and the no-new-statement rule.
// The fixtures live in
// supply_chain_impact_fanout_suppression_cveid_live_test.go.
// recordingBeginner wraps the production beginner and records every
// statement the writer transaction runs, in order. It delegates everything,
// so the pass commits for real; it only observes. One deliberate gap: the
// wrapper does not implement the winners-capture port, so a recorded pass
// skips the dirty-log capture it would otherwise run.
type recordingBeginner struct {
	inner supplychaincore.SupplyChainImpactBeginner
	mu    sync.Mutex
	calls []recordingCall
}

type recordingCall struct {
	query string
	args  []any
}

func (b *recordingBeginner) BeginSupplyChainImpactTx(ctx context.Context) (supplychaincore.SupplyChainImpactTx, error) {
	tx, err := b.inner.BeginSupplyChainImpactTx(ctx)
	if err != nil {
		return nil, err
	}
	return &recordingTx{inner: tx, recorder: b}, nil
}

func (b *recordingBeginner) record(query string, args []any) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.calls = append(b.calls, recordingCall{query: query, args: args})
}

func (b *recordingBeginner) queries() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]string, 0, len(b.calls))
	for _, call := range b.calls {
		out = append(out, call.query)
	}
	return out
}

type recordingTx struct {
	inner    supplychaincore.SupplyChainImpactTx
	recorder *recordingBeginner
}

func (t *recordingTx) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	t.recorder.record(query, args)
	return t.inner.ExecContext(ctx, query, args...)
}

func (t *recordingTx) QueryContext(ctx context.Context, query string, args ...any) (supplychaincore.SupplyChainImpactRows, error) {
	t.recorder.record(query, args)
	return t.inner.QueryContext(ctx, query, args...)
}

func (t *recordingTx) Commit() error   { return t.inner.Commit() }
func (t *recordingTx) Rollback() error { return t.inner.Rollback() }

// TestSuppressionDiffIssuesNoStatementForSignalScopesLive pins wiring
// condition 4 on the live path: an EPSS scope's pass issues no suppression
// statement at all.
func TestSuppressionDiffIssuesNoStatementForSignalScopesLive(t *testing.T) {
	c := newConvergence(t, false)
	recorder := &recordingBeginner{inner: postgres.SupplyChainImpactBeginner{Beginner: postgres.SQLDB{DB: c.db}}}
	c.handler.Writer = supplychaincore.PostgresSupplyChainImpactWriter{
		DB:  recorder,
		Now: c.handler.Writer.(supplychaincore.PostgresSupplyChainImpactWriter).Now,
	}
	c.drain()
	for _, q := range recorder.queries() {
		if strings.Contains(q, "vulnerability.suppression") {
			t.Fatalf("EPSS/source pass issued a suppression statement: %.120s", q)
		}
	}
	if len(recorder.queries()) == 0 {
		t.Fatal("recorded no statements: the writer never ran")
	}
}

// timeoutWriter overrides the suppression diff statement bound for one
// pass. Production leaves the timeout unset; tests shorten it to prove the
// bound.
type timeoutWriter struct {
	inner   supplychaincore.PostgresSupplyChainImpactWriter
	timeout time.Duration
}

func (w timeoutWriter) WriteSupplyChainImpactFindings(ctx context.Context, in supplychaincore.SupplyChainImpactWrite) (supplychaincore.SupplyChainImpactWriteResult, error) {
	in.SuppressionDiffTimeout = w.timeout
	return w.inner.WriteSupplyChainImpactFindings(ctx, in)
}

// supRetryableFailure classifies a handler error the way the queue does.
type supRetryableFailure interface {
	Retryable() bool
	FailureClass() string
}

// TestSuppressionDiffBoundFailsThePassAndRecoversLive pins wiring condition
// 2 on the live path: with fact_records held exclusively, the diff
// statement exceeds its bound and the write fails as the existing retryable
// fan-out class, so the queue retries instead of proceeding without the
// holders. The retry after the lock releases names the same set. The write
// runs through the production writer; the evidence load that precedes it in
// a full pass is lock-free here on purpose, so the bound under proof is the
// diff's own.
func TestSuppressionDiffBoundFailsThePassAndRecoversLive(t *testing.T) {
	c := newConvergence(t, false)
	seedSupScanScope(t, c)
	seedSupHolder(t, c)
	c.drain()
	inner, ok := c.handler.Writer.(supplychaincore.PostgresSupplyChainImpactWriter)
	if !ok {
		t.Fatalf("handler writer = %T, want the production writer", c.handler.Writer)
	}
	writer := timeoutWriter{inner: inner, timeout: 150 * time.Millisecond}
	gen := seedSupOperatorCVEID(t, c)
	write := supplychaincore.SupplyChainImpactWrite{
		IntentID:       "intent-sup-bound",
		ScopeID:        supOpScope,
		GenerationID:   gen,
		SourceSystem:   "synthetic",
		ScopeFactKinds: []string{chain.VulnerabilitySuppressionFactKind},
		FencingToken:   4242,
	}

	lockTx, err := c.db.BeginTx(c.ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = lockTx.Rollback() }()
	if _, err := lockTx.ExecContext(c.ctx, `LOCK TABLE fact_records IN ACCESS EXCLUSIVE MODE`); err != nil {
		t.Fatalf("lock fact_records: %v", err)
	}
	started := time.Now()
	_, err = writer.WriteSupplyChainImpactFindings(c.ctx, write)
	if err == nil {
		t.Fatal("Write() = nil under the locked table, want the bounded read to fail")
	}
	if elapsed := time.Since(started); elapsed > 30*time.Second {
		t.Fatalf("the bounded read held for %v: the timeout did not fire", elapsed)
	}
	var classifier supRetryableFailure
	if !errors.As(err, &classifier) {
		t.Fatalf("Write() error %T does not classify a failure", err)
	}
	if !classifier.Retryable() || classifier.FailureClass() != supplychaincore.SupplyChainImpactFanOutFailureClass {
		t.Fatalf("error classifies retryable=%v class=%q, want true/%q",
			classifier.Retryable(), classifier.FailureClass(), supplychaincore.SupplyChainImpactFanOutFailureClass)
	}
	if err := lockTx.Rollback(); err != nil {
		t.Fatalf("release the lock: %v", err)
	}
	result, err := writer.WriteSupplyChainImpactFindings(c.ctx, write)
	if err != nil {
		t.Fatalf("retry Write() error = %v", err)
	}
	if len(result.ChangedCVEIDs) != 1 || result.ChangedCVEIDs[0] != supCVEID {
		t.Fatalf("retry ChangedCVEIDs = %v, want the suppression CVE", result.ChangedCVEIDs)
	}
}

// seedSupPlanScope seeds an operator scope with one suppression generation
// of 5,000 rows for the plan-anchor proof, plus the analyzed stats the
// planner decides on. supPlanRescan adds the second generation. Four older
// background generations of 50,000 narrow rows each give the table the size
// the index choice needs: the pair filter names one generation of a large
// table, the shape the shim measured, and each generation sits contiguously
// on the heap as the store writes it.
func seedSupPlanScope(t *testing.T, c *convergence, scope, gen1 string) {
	t.Helper()
	now := time.Date(2026, 10, 31, 0, 0, 0, 0, time.UTC)
	if _, err := c.db.ExecContext(c.ctx, `
INSERT INTO ingestion_scopes (
  scope_id, scope_kind, source_system, source_key, collector_kind,
  partition_key, observed_at, ingested_at, status, payload)
VALUES ($1, 'vulnerability_intelligence', 'eshu_policy', $1, 'vulnerability_intelligence',
  $1, $2, $2, 'active', '{}'::jsonb)`, scope, now); err != nil {
		t.Fatalf("insert plan scope: %v", err)
	}
	for bg := 1; bg <= 4; bg++ {
		gen := fmt.Sprintf("bg%d", bg)
		at := now.Add(time.Duration(bg-5) * time.Hour)
		if _, err := c.db.ExecContext(c.ctx, `
INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, activated_at, payload)
VALUES ($1, $2, 'synthetic', $3, $3, 'superseded', $3, '{}'::jsonb)`, fanOutGen(scope, gen), scope, at); err != nil {
			t.Fatalf("insert plan background generation %s: %v", gen, err)
		}
		if _, err := c.db.ExecContext(c.ctx, `
INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key,
  schema_version, collector_kind, source_confidence, source_system, source_fact_key,
  observed_at, ingested_at, is_tombstone, payload, fencing_token)
SELECT 'sup-plan-' || $4::text || '-' || n, $1, $2, 'vulnerability.suppression', 'sup-plan-' || n,
  '1.0.0', 'synthetic', 'reported', 'synthetic', 'sup-plan-' || n, $3::timestamptz, $3::timestamptz, FALSE,
  jsonb_build_object('suppression_id', 'sup-plan-' || n,
    'scope', jsonb_build_object('cve_id', 'CVE-2023-38039')),
  0
FROM generate_series(1, 50000) AS n`, scope, fanOutGen(scope, gen), at, gen); err != nil {
			t.Fatalf("seed plan background suppressions %s: %v", gen, err)
		}
	}
	if _, err := c.db.ExecContext(c.ctx, `
INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, activated_at, payload)
VALUES ($1, $2, 'synthetic', $3, $3, 'active', $3, '{}'::jsonb)`, fanOutGen(scope, gen1), scope, now); err != nil {
		t.Fatalf("insert plan generation %s: %v", gen1, err)
	}
	if _, err := c.db.ExecContext(c.ctx, `UPDATE ingestion_scopes SET active_generation_id = $2 WHERE scope_id = $1`, scope, fanOutGen(scope, gen1)); err != nil {
		t.Fatalf("activate plan scope: %v", err)
	}
	if _, err := c.db.ExecContext(c.ctx, `
INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key,
  schema_version, collector_kind, source_confidence, source_system, source_fact_key,
  observed_at, ingested_at, is_tombstone, payload, fencing_token)
SELECT 'sup-plan-' || $4::text || '-' || n, $1, $2, 'vulnerability.suppression', 'sup-plan-' || n,
  '1.0.0', 'synthetic', 'reported', 'synthetic', 'sup-plan-' || n, $3::timestamptz, $3::timestamptz, FALSE,
  jsonb_build_object(
    'suppression_id', 'sup-plan-' || n,
    'scope', jsonb_build_object('cve_id', CASE WHEN n % 100 = 0 THEN 'CVE-2023-99999' ELSE 'CVE-2023-38039' END),
    'justification', 'accepted_risk', 'authored_at', '2026-10-31T11:30:00Z',
    'reason', 'plan anchor', 'source', 'eshu_policy'),
  0
FROM generate_series(1, 5000) AS n`, scope, fanOutGen(scope, gen1), now, gen1); err != nil {
		t.Fatalf("seed plan suppressions %s: %v", gen1, err)
	}
	if _, err := c.db.ExecContext(c.ctx, `ANALYZE fact_records`); err != nil {
		t.Fatalf("analyze fact_records: %v", err)
	}
}

// supPlanRescan supersedes the plan scope's first generation and inserts the
// second with its suppression rows, like a collector rescan.
func supPlanRescan(t *testing.T, c *convergence, scope, gen1, gen2 string) {
	t.Helper()
	now := time.Date(2026, 10, 31, 1, 0, 0, 0, time.UTC)
	if _, err := c.db.ExecContext(c.ctx, `UPDATE scope_generations SET status='superseded', superseded_at=now() WHERE generation_id = $1`, fanOutGen(scope, gen1)); err != nil {
		t.Fatalf("supersede plan generation: %v", err)
	}
	if _, err := c.db.ExecContext(c.ctx, `
INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, activated_at, payload)
VALUES ($1, $2, 'synthetic', $3, $3, 'active', $3, '{}'::jsonb)`, fanOutGen(scope, gen2), scope, now); err != nil {
		t.Fatalf("insert plan generation %s: %v", gen2, err)
	}
	if _, err := c.db.ExecContext(c.ctx, `UPDATE ingestion_scopes SET active_generation_id = $2 WHERE scope_id = $1`, scope, fanOutGen(scope, gen2)); err != nil {
		t.Fatalf("activate plan generation: %v", err)
	}
	if _, err := c.db.ExecContext(c.ctx, `
INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key,
  schema_version, collector_kind, source_confidence, source_system, source_fact_key,
  observed_at, ingested_at, is_tombstone, payload, fencing_token)
SELECT 'sup-plan-' || $4::text || '-' || n, $1, $2, 'vulnerability.suppression', 'sup-plan-' || n,
  '1.0.0', 'synthetic', 'reported', 'synthetic', 'sup-plan-' || n, $3::timestamptz, $3::timestamptz, FALSE,
  jsonb_build_object(
    'suppression_id', 'sup-plan-' || n,
    'scope', jsonb_build_object('cve_id', CASE WHEN (n + 1) % 100 = 0 THEN 'CVE-2023-99999' ELSE 'CVE-2023-38039' END),
    'justification', 'accepted_risk', 'authored_at', '2026-10-31T11:30:00Z',
    'reason', 'plan anchor', 'source', 'eshu_policy'),
  0
FROM generate_series(1, 5000) AS n`, scope, fanOutGen(scope, gen2), now, gen2); err != nil {
		t.Fatalf("seed plan suppressions %s: %v", gen2, err)
	}
	if _, err := c.db.ExecContext(c.ctx, `ANALYZE fact_records`); err != nil {
		t.Fatalf("analyze fact_records: %v", err)
	}
}

// TestSuppressionDiffPairPlanUsesTheScopeGenerationIndexLive pins wiring
// condition 3: the shipped pair statement plans an index access on
// fact_records_scope_generation_idx on both arms with no sequential scan,
// under both plan_cache_mode settings. The statement text is captured from
// the production pass at runtime, never hand-copied, so the proof cannot
// drift from the shipped shape.
func TestSuppressionDiffPairPlanUsesTheScopeGenerationIndexLive(t *testing.T) {
	c := newConvergence(t, false)
	const planScope = "scope:10019:plan"
	seedSupPlanScope(t, c, planScope, "g1")
	recorder := &recordingBeginner{inner: postgres.SupplyChainImpactBeginner{Beginner: postgres.SQLDB{DB: c.db}}}
	inner, ok := c.handler.Writer.(supplychaincore.PostgresSupplyChainImpactWriter)
	if !ok {
		t.Fatalf("handler writer = %T, want the production writer", c.handler.Writer)
	}
	inner.DB = recorder
	c.handler.Writer = inner
	fanOutSeedItem(t, c.ctx, c.db, planScope, "g1")
	c.drain()
	supPlanRescan(t, c, planScope, "g1", "g2")
	fanOutSeedItem(t, c.ctx, c.db, planScope, "g2")
	c.drain()

	var pairQuery string
	var pairArgs []any
	for _, call := range recorder.calls {
		if strings.Contains(call.query, "FULL JOIN") && len(call.args) == 3 {
			if scope, ok := call.args[0].(string); ok && scope == planScope {
				pairQuery = call.query
				pairArgs = call.args
			}
		}
	}
	if pairQuery == "" {
		t.Fatal("no pair diff statement recorded: the second pass did not walk a pair")
	}
	if len(pairArgs) != 3 {
		t.Fatalf("pair args = %v, want (scope, current, prior)", pairArgs)
	}
	for _, mode := range []string{"force_generic_plan", "force_custom_plan"} {
		tx, err := c.db.BeginTx(c.ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		rows, err := tx.QueryContext(c.ctx, `SELECT set_config('plan_cache_mode', $1, true)`, mode)
		if err != nil {
			_ = tx.Rollback()
			t.Fatalf("set plan_cache_mode %s: %v", mode, err)
		}
		_ = rows.Close()
		planRows, err := tx.QueryContext(c.ctx, "EXPLAIN "+pairQuery, pairArgs...)
		if err != nil {
			_ = tx.Rollback()
			t.Fatalf("explain under %s: %v", mode, err)
		}
		var lines []string
		for planRows.Next() {
			var line string
			if err := planRows.Scan(&line); err != nil {
				_ = planRows.Close()
				_ = tx.Rollback()
				t.Fatalf("scan plan under %s: %v", mode, err)
			}
			lines = append(lines, line)
		}
		_ = planRows.Close()
		if err := planRows.Err(); err != nil {
			_ = tx.Rollback()
			t.Fatalf("read plan under %s: %v", mode, err)
		}
		if err := tx.Rollback(); err != nil {
			t.Fatalf("rollback explain under %s: %v", mode, err)
		}
		plan := strings.Join(lines, "\n")
		if strings.Contains(plan, "Seq Scan on fact_records") {
			t.Fatalf("plan under %s uses a sequential scan:\n%s", mode, plan)
		}
		if got := strings.Count(plan, "fact_records_scope_generation_idx"); got < 2 {
			t.Fatalf("plan under %s names the scope-generation index %d times, want both arms:\n%s", mode, got, plan)
		}
	}
}
