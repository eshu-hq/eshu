// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

// Real-queue proof for the #5709 cross-scope readiness deferral on
// supply_chain_impact.
//
// A round of review on #6093 made the point this file answers: the handler
// returns a NON-COUNTING retry error, and a fake queue cannot establish that the
// real fact_work_items path freezes attempt_count, keeps the writer quiet while
// it waits, hands the row back on the next claim, and still reaches a terminal
// answer. A fake that cannot produce the failing row passes forever.
//
// So everything on the queue side here is production code driven against real
// PostgreSQL: the real ReducerQueue Claim/Fail/Ack SQL against the real
// fact_work_items DDL, the real CrossScopeProducerReadinessStore reading real
// ingestion_scopes rows through the committed quiescence probe, the real
// SupplyChainImpactHandler, and the real crossScopeProducerNotReadyError that
// carries the failure class. What stays a fake is the FACT SOURCE -- the
// handler's evidence loader -- because the evidence a supply-chain pass reads
// comes through a different store than the one under test, and seeding it would
// prove nothing about the queue.
//
// Naming matters here: the prefix puts this test in the reducer contention
// gate's -run filter (.github/workflows/reducer-contention-gate.yml), which
// runs a real PostgreSQL service on every PR touching go/internal/reducer/** or
// go/internal/storage/postgres/**. A DSN-gated test that only skips in CI would
// be a guard that proves nothing there.

// crossScopeReadinessProofLoader is the handler's evidence source. It carries a
// vulnerability generation whose filter includes a repository ID, which is one
// of the three dimensions the active-evidence query can match a producer row
// on, so the readiness floor is armed for this pass.
//
// It resolves NO producer-owned envelope, which is the shape the floor exists
// for: the producer's reducer row may have succeeded, but until its scope
// generation activates, the cross-scope read sees nothing.
type crossScopeReadinessProofLoader struct{}

func (crossScopeReadinessProofLoader) ListFacts(
	context.Context,
	string,
	string,
) ([]facts.Envelope, error) {
	return crossScopeReadinessProofScopeFacts(), nil
}

func (crossScopeReadinessProofLoader) ListFactsByKind(
	context.Context,
	string,
	string,
	[]string,
) ([]facts.Envelope, error) {
	return crossScopeReadinessProofScopeFacts(), nil
}

func (crossScopeReadinessProofLoader) ListActiveSupplyChainImpactFacts(
	context.Context,
	reducer.SupplyChainImpactFactFilter,
) ([]facts.Envelope, bool, error) {
	return nil, false, nil
}

// crossScopeReadinessProofScopeFacts is one CVE, one affected package, and the
// package-consumption correlation that carries the repository ID.
func crossScopeReadinessProofScopeFacts() []facts.Envelope {
	return []facts.Envelope{
		{
			FactID:   "cve-6093",
			FactKind: facts.VulnerabilityCVEFactKind,
			Payload: map[string]any{
				"cve_id":      "CVE-2026-6093",
				"advisory_id": "CVE-2026-6093",
				"cvss_score":  9.8,
				"aliases":     []any{"CVE-2026-6093"},
			},
		},
		{
			FactID:   "affected-6093",
			FactKind: facts.VulnerabilityAffectedPackageFactKind,
			Payload: map[string]any{
				"cve_id":            "CVE-2026-6093",
				"advisory_id":       "CVE-2026-6093",
				"package_id":        "pkg:npm/example",
				"ecosystem":         "npm",
				"package_name":      "example",
				"affected_versions": []any{"1.2.3"},
				"fixed_versions":    []any{"1.3.0"},
			},
		},
		{
			FactID:   "consume-6093",
			FactKind: crossScopeReadinessProofConsumptionFactKind,
			Payload: map[string]any{
				"package_id":        "pkg:npm/example",
				"relationship_kind": "consumption",
				"repository_id":     "repository:github.com/acme/api",
				"dependency_range":  "1.2.3",
				"canonical_writes":  1,
			},
		},
	}
}

// countingSupplyChainImpactWriter records durable publication attempts. The
// count is an assertion in its own right: a deferral that had already written
// findings would leave under-evidenced rows behind for the retry to find.
type countingSupplyChainImpactWriter struct {
	calls int
}

func (w *countingSupplyChainImpactWriter) WriteSupplyChainImpactFindings(
	_ context.Context,
	write reducer.SupplyChainImpactWrite,
) (reducer.SupplyChainImpactWriteResult, error) {
	w.calls++
	return reducer.SupplyChainImpactWriteResult{FactsWritten: len(write.Findings)}, nil
}

// TestReducerContentionGateCrossScopeReadinessDeferralKeepsItsAttemptBudget is
// the real-queue proof of the non-counting deferral, end to end.
//
// Phases, each asserting something a fake could not establish:
//
//  1. Claim, run the real handler with both producer scopes registered and not
//     yet activated, and Fail with the error the handler actually returned. The
//     row lands in 'retrying' under cross_scope_producer_not_ready.
//  2. Repeat the claim/handle/fail cycle more times than MaxAttempts. If the
//     class were counting, the row would dead-letter; the assertion is that
//     attempt_count stays at 1 and the row stays reclaimable, and that the
//     writer is never called while it waits.
//  3. Activate the producer scopes. The same handler, reading the same real
//     readiness store, now commits: the writer runs once and Ack drives the row
//     to 'succeeded'. A floor that could not converge would be its own bug.
func TestReducerContentionGateCrossScopeReadinessDeferralKeepsItsAttemptBudget(t *testing.T) {
	dsn := reducerDomainFairnessDSN()
	if dsn == "" {
		t.Skip("set ESHU_REDUCER_FAIRNESS_PROOF_DSN or ESHU_POSTGRES_DSN to run the contention gate")
	}

	ctx := context.Background()
	db := openCrossScopeReadinessProofDB(t, ctx, dsn)

	// The consumer's own cycle starts a minute ago, well inside the 30-minute
	// elapsed bound, so readiness is what governs rather than the fallback.
	enqueuedAt := time.Now().UTC().Add(-time.Minute)
	seedReducerFairnessScope(t, ctx, db, crossScopeReadinessProofConsumerScope, enqueuedAt)
	seedCrossScopeProducerScope(t, ctx, db, "oci_registry:ghcr.io/acme", string(scope.CollectorOCIRegistry), enqueuedAt)
	seedCrossScopeProducerScope(t, ctx, db, "ci_cd_run:github.com/acme/api", string(scope.CollectorCICDRun), enqueuedAt)
	insertReducerFairnessWorkItem(t, ctx, db, reducerFairnessWorkItem{
		workItemID:     "supply-chain-impact-6093",
		scopeID:        crossScopeReadinessProofConsumerScope,
		generationID:   "gen-fair",
		domain:         string(reducer.DomainSupplyChainImpact),
		conflictDomain: "scope",
		conflictKey:    crossScopeReadinessProofConsumerScope,
		sourceSystem:   "vulnerability_intelligence",
		updatedAt:      enqueuedAt,
	})

	writer := &countingSupplyChainImpactWriter{}
	handler := reducer.SupplyChainImpactHandler{
		FactLoader:        crossScopeReadinessProofLoader{},
		Writer:            writer,
		ProducerReadiness: CrossScopeProducerReadinessStore{DB: SQLDB{DB: db}},
	}
	// The queue clock is injected so each cycle's retry delay elapses without
	// sleeping. created_at stays where it was seeded, so the handler's own
	// wall-clock elapsed bound is nowhere near reached across these cycles.
	clock := time.Now().UTC()
	queue := ReducerQueue{
		db:            SQLDB{DB: db},
		LeaseOwner:    "cross-scope-readiness-proof",
		LeaseDuration: time.Minute,
		RetryDelay:    time.Second,
		MaxAttempts:   3,
		ClaimDomains:  []reducer.Domain{reducer.DomainSupplyChainImpact},
		Now:           func() time.Time { return clock },
	}

	// More cycles than MaxAttempts: a counting class would have dead-lettered
	// this row by cycle four.
	const deferCycles = 5
	for cycle := 1; cycle <= deferCycles; cycle++ {
		intent, claimed, err := queue.Claim(ctx)
		if err != nil {
			t.Fatalf("cycle %d: Claim() error = %v", cycle, err)
		}
		if !claimed {
			t.Fatalf("cycle %d: Claim() claimed = false, want the deferred row back: a non-counting retry must stay reclaimable", cycle)
		}
		if intent.AttemptCount != 1 {
			t.Fatalf(
				"cycle %d: claimed Intent.AttemptCount = %d, want 1 frozen: cross_scope_producer_not_ready is enrolled non-counting",
				cycle, intent.AttemptCount,
			)
		}

		_, handleErr := handler.Handle(ctx, intent)
		if handleErr == nil {
			t.Fatalf("cycle %d: Handle() error = nil, want a deferral while the producer scopes have not activated", cycle)
		}
		// The failure class is read off the error the same way the queue reads
		// it, so this pins the value Fail is about to persist rather than a
		// restatement of it.
		var classified interface{ FailureClass() string }
		if !errors.As(handleErr, &classified) ||
			classified.FailureClass() != reducer.CrossScopeProducerNotReadyFailureClass {
			t.Fatalf(
				"cycle %d: Handle() error = %#v, want one self-classifying as %q",
				cycle, handleErr, reducer.CrossScopeProducerNotReadyFailureClass,
			)
		}
		if writer.calls != 0 {
			t.Fatalf("cycle %d: writer calls = %d, want 0 while the consumer is deferring", cycle, writer.calls)
		}

		if err := queue.Fail(ctx, intent, handleErr); err != nil {
			t.Fatalf("cycle %d: Fail() error = %v", cycle, err)
		}
		status, failureClass, attemptCount := readCrossScopeProofWorkItem(t, ctx, db)
		if status != "retrying" {
			t.Fatalf(
				"cycle %d: status = %q, want retrying: a deferred consumer must not dead-letter while it waits",
				cycle, status,
			)
		}
		if failureClass != reducer.CrossScopeProducerNotReadyFailureClass {
			t.Fatalf("cycle %d: failure_class = %q, want %q", cycle, failureClass, reducer.CrossScopeProducerNotReadyFailureClass)
		}
		if attemptCount != 1 {
			t.Fatalf(
				"cycle %d: attempt_count = %d, want 1: the readiness class must not erode the retry budget",
				cycle, attemptCount,
			)
		}
		// Past next_attempt_at so the next claim is due.
		clock = clock.Add(2 * time.Second)
	}

	// The producers activate. Nothing about the intent changed; only the
	// ingestion_scopes rows the real readiness store reads did.
	activateCrossScopeProducerScope(t, ctx, db, "oci_registry:ghcr.io/acme")
	activateCrossScopeProducerScope(t, ctx, db, "ci_cd_run:github.com/acme/api")

	intent, claimed, err := queue.Claim(ctx)
	if err != nil {
		t.Fatalf("post-activation Claim() error = %v", err)
	}
	if !claimed {
		t.Fatal("post-activation Claim() claimed = false, want the deferred row back")
	}
	if _, err := handler.Handle(ctx, intent); err != nil {
		t.Fatalf("post-activation Handle() error = %v, want nil once the producer scopes are quiescent-active", err)
	}
	if writer.calls != 1 {
		t.Fatalf("writer calls = %d, want exactly 1 once the producers activated", writer.calls)
	}
	if err := queue.Ack(ctx, intent, reducer.Result{}); err != nil {
		t.Fatalf("Ack() error = %v", err)
	}
	if status, _, attemptCount := readCrossScopeProofWorkItem(t, ctx, db); status != "succeeded" || attemptCount != 1 {
		t.Fatalf(
			"terminal status = %q attempt_count = %d, want succeeded at 1: the row converged without spending its budget",
			status, attemptCount,
		)
	}
}

// TestReducerContentionGateCrossScopeReadinessConvergesAtTheElapsedBound is the
// other terminal path: a producer scope that never activates.
//
// The deferral's failure class freezes attempt_count by design, so no retry
// budget will ever end this wait. Only the elapsed-time bound does, and it is
// measured against the row's own cycle start (COALESCE(reopened_at, created_at))
// as the real claim query returns it. A row enqueued past the bound must commit
// its best available answer on the FIRST claim rather than defer.
func TestReducerContentionGateCrossScopeReadinessConvergesAtTheElapsedBound(t *testing.T) {
	dsn := reducerDomainFairnessDSN()
	if dsn == "" {
		t.Skip("set ESHU_REDUCER_FAIRNESS_PROOF_DSN or ESHU_POSTGRES_DSN to run the contention gate")
	}

	ctx := context.Background()
	db := openCrossScopeReadinessProofDB(t, ctx, dsn)

	// Enqueued well past crossScopeProducerReadinessMaxWait (30 minutes), and
	// the producer scopes stay registered-but-unactivated for the whole test.
	enqueuedAt := time.Now().UTC().Add(-45 * time.Minute)
	seedReducerFairnessScope(t, ctx, db, crossScopeReadinessProofConsumerScope, enqueuedAt)
	seedCrossScopeProducerScope(t, ctx, db, "oci_registry:ghcr.io/acme", string(scope.CollectorOCIRegistry), enqueuedAt)
	seedCrossScopeProducerScope(t, ctx, db, "ci_cd_run:github.com/acme/api", string(scope.CollectorCICDRun), enqueuedAt)
	insertReducerFairnessWorkItem(t, ctx, db, reducerFairnessWorkItem{
		workItemID:     "supply-chain-impact-6093-stuck",
		scopeID:        crossScopeReadinessProofConsumerScope,
		generationID:   "gen-fair",
		domain:         string(reducer.DomainSupplyChainImpact),
		conflictDomain: "scope",
		conflictKey:    crossScopeReadinessProofConsumerScope,
		sourceSystem:   "vulnerability_intelligence",
		updatedAt:      enqueuedAt,
	})

	writer := &countingSupplyChainImpactWriter{}
	handler := reducer.SupplyChainImpactHandler{
		FactLoader:        crossScopeReadinessProofLoader{},
		Writer:            writer,
		ProducerReadiness: CrossScopeProducerReadinessStore{DB: SQLDB{DB: db}},
	}
	queue := ReducerQueue{
		db:            SQLDB{DB: db},
		LeaseOwner:    "cross-scope-readiness-bound-proof",
		LeaseDuration: time.Minute,
		ClaimDomains:  []reducer.Domain{reducer.DomainSupplyChainImpact},
	}

	intent, claimed, err := queue.Claim(ctx)
	if err != nil {
		t.Fatalf("Claim() error = %v", err)
	}
	if !claimed {
		t.Fatal("Claim() claimed = false, want the seeded row")
	}
	if _, err := handler.Handle(ctx, intent); err != nil {
		t.Fatalf(
			"Handle() error = %v, want nil: past the elapsed bound the consumer must commit rather than wait forever in a class that never dead-letters",
			err,
		)
	}
	if writer.calls != 1 {
		t.Fatalf("writer calls = %d, want 1: the bound is what makes a permanently stuck producer converge", writer.calls)
	}
	if err := queue.Ack(ctx, intent, reducer.Result{}); err != nil {
		t.Fatalf("Ack() error = %v", err)
	}
	if status, _, _ := readCrossScopeProofWorkItem(t, ctx, db); status != "succeeded" {
		t.Fatalf("terminal status = %q, want succeeded", status)
	}
}

// openCrossScopeReadinessProofDB builds the fairness schema and then applies
// the two later fact_work_items migrations this proof needs.
//
// Retry and acknowledgement both write columns the base fairness fixture does
// not create: cross_scope_replay_required (migration 093, the completion
// fanout's replay flag) and provenance_edge_identity_upgrade_required
// (migration 096). Without them Fail and Ack fail with "column ... does not
// exist" -- which is a fixture gap, not a product defect, and exactly the kind
// of thing only a real-Postgres run surfaces.
func openCrossScopeReadinessProofDB(t *testing.T, ctx context.Context, dsn string) *sql.DB {
	t.Helper()

	db, _ := openReducerFairnessDBWithSchema(t, ctx, dsn)
	for _, migration := range []string{
		"cross_scope_completion_queue",
		"provenance_edge_identity_upgrade_seed",
	} {
		if _, err := db.ExecContext(ctx, MigrationSQL(migration)); err != nil {
			t.Fatalf("apply migration %s: %v", migration, err)
		}
	}
	return db
}

// crossScopeReadinessProofConsumerScope is the vulnerability-intelligence scope
// the consumer's work item belongs to. It is deliberately NOT one of the
// producer collector kinds: the producer runs in a different scope, which is
// what makes this a cross-scope dependency at all.
const crossScopeReadinessProofConsumerScope = "vuln-intel:osv:acme"

// crossScopeReadinessProofConsumptionFactKind is the reducer package-consumption
// correlation kind, which is what carries the repository ID that arms the floor
// for this pass.
const crossScopeReadinessProofConsumptionFactKind = "reducer_package_consumption_correlation"

// seedCrossScopeProducerScope registers a producer scope with NO active
// generation, which is the registered-but-not-quiescent shape the readiness
// probe must read as "not ready". A kind with no registered scope at all reads
// as ready, so registering the row is what arms the wait.
func seedCrossScopeProducerScope(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
	scopeID string,
	collectorKind string,
	now time.Time,
) {
	t.Helper()

	if _, err := db.ExecContext(ctx, `
INSERT INTO ingestion_scopes (
    scope_id, scope_kind, source_system, source_key, parent_scope_id,
    collector_kind, partition_key, observed_at, ingested_at, status,
    active_generation_id, payload
) VALUES ($1, $2, $2, $1, NULL, $2, $1, $3, $3, 'active', NULL, '{}'::jsonb)`,
		scopeID, collectorKind, now); err != nil {
		t.Fatalf("insert producer scope %q: %v", scopeID, err)
	}
	if _, err := db.ExecContext(ctx, `
INSERT INTO scope_generations (
    generation_id, scope_id, trigger_kind, freshness_hint, observed_at,
    ingested_at, status, activated_at, superseded_at, payload
) VALUES ($1, $2, 'snapshot', 'producer', $3, $3, 'pending', NULL, NULL, '{}'::jsonb)`,
		crossScopeProducerGenerationID(scopeID), scopeID, now); err != nil {
		t.Fatalf("insert producer generation for %q: %v", scopeID, err)
	}
}

// activateCrossScopeProducerScope points the scope at its generation, which is
// the transition the floor is waiting on: a producer's reducer row reaches
// 'succeeded' before this happens, and until it does the consumer's cross-scope
// read joins to nothing.
func activateCrossScopeProducerScope(t *testing.T, ctx context.Context, db *sql.DB, scopeID string) {
	t.Helper()

	if _, err := db.ExecContext(ctx, `
UPDATE scope_generations SET status = 'active', activated_at = now() WHERE generation_id = $1`,
		crossScopeProducerGenerationID(scopeID)); err != nil {
		t.Fatalf("activate producer generation for %q: %v", scopeID, err)
	}
	if _, err := db.ExecContext(ctx, `
UPDATE ingestion_scopes SET active_generation_id = $2 WHERE scope_id = $1`,
		scopeID, crossScopeProducerGenerationID(scopeID)); err != nil {
		t.Fatalf("activate producer scope %q: %v", scopeID, err)
	}
}

func crossScopeProducerGenerationID(scopeID string) string {
	return "gen-producer-" + scopeID
}

// readCrossScopeProofWorkItem reads the durable row back, because the queue's
// return values and the table's contents are separate claims and this proof is
// about the table.
func readCrossScopeProofWorkItem(t *testing.T, ctx context.Context, db *sql.DB) (string, string, int) {
	t.Helper()

	var (
		status       string
		failureClass sql.NullString
		attemptCount int
	)
	if err := db.QueryRowContext(
		ctx, `
SELECT status, failure_class, attempt_count
FROM fact_work_items
WHERE stage = 'reducer' AND domain = $1`,
		string(reducer.DomainSupplyChainImpact),
	).Scan(&status, &failureClass, &attemptCount); err != nil {
		t.Fatalf("read supply_chain_impact work item: %v", err)
	}
	return status, failureClass.String, attemptCount
}
