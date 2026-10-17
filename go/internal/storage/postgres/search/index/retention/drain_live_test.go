// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package indexretention_test

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #7427 disposable PostgreSQL 18 proof of the search index pre-delete batches: only the superseded generation loses rows while the active generation's re-minted document ids survive, pending, active and still-pointed-at generations lose none, running eshu_search_document work blocks the drain, a drain cancelled mid-loop converges on rerun, the from-start re-sweep removes a row inserted behind the cursor, and a held advisory lock makes a drain skip while two concurrent drains delete each row once without deadlock; enrolled in the live-postgres-readiness runner; reads ESHU_GENERATION_RETENTION_PROOF_DSN (with its DISPOSABLE flag) and creates one isolated bootstrapped schema per test. local run passed all 6 tests in 15.3s wall on PostgreSQL 18.6 on 2026-10-15
//eshu:readiness:env ESHU_GENERATION_RETENTION_PROOF_DISPOSABLE ESHU_GENERATION_RETENTION_PROOF_DSN ESHU_POSTGRES_DSN

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	indexretention "github.com/eshu-hq/eshu/go/internal/storage/postgres/search/index/retention"
)

// TestDrainLiveRemovesOnlyTheSupersededGeneration drains 45,000 terms (two
// full pages and a short one) and 4,500 documents of a superseded generation
// while the active generation re-mints the same document ids on the same
// hash leaf. The active rows must survive untouched (#7427 c7).
func TestDrainLiveRemovesOnlyTheSupersededGeneration(t *testing.T) {
	f := openLiveFixture(t)
	f.seedIndex(t, f.old, 4500, 10)
	f.seedIndex(t, f.active, 4500, 2)

	report, err := indexretention.Drain(f.ctx, f.database, f.generation(f.old), unbounded())
	if err != nil {
		t.Fatalf("Drain() error = %v", err)
	}
	if !report.Drained() || report.TermsDeleted != 45000 || report.DocumentsDeleted != 4500 {
		t.Fatalf("report = %+v, want drained with 45000 terms and 4500 documents", report)
	}
	// terms: two full pages, one short page, one empty from-start sweep;
	// documents: one short page, one empty from-start sweep.
	if report.Batches != 6 {
		t.Fatalf("Batches = %d, want 6", report.Batches)
	}
	if terms, documents := f.counts(t, f.old); terms != 0 || documents != 0 {
		t.Fatalf("superseded generation keeps %d terms, %d documents, want none", terms, documents)
	}
	if terms, documents := f.counts(t, f.active); terms != 9000 || documents != 4500 {
		t.Fatalf("active generation has %d terms, %d documents, want 9000 and 4500 (re-minted ids must survive)", terms, documents)
	}
}

// TestDrainLiveLeavesNonTerminalGenerationsWhole is the #7427 c4 proof: a
// pending generation, the active generation, and a superseded generation the
// scope still points at are each passed as a candidate and lose zero rows.
func TestDrainLiveLeavesNonTerminalGenerationsWhole(t *testing.T) {
	f := openLiveFixture(t)
	f.seedIndex(t, f.pending, 300, 5)
	f.seedIndex(t, f.active, 300, 5)
	f.seedIndex(t, f.old, 300, 5)

	for _, id := range []string{f.pending, f.active} {
		report, err := indexretention.Drain(f.ctx, f.database, f.generation(id), unbounded())
		if err != nil {
			t.Fatalf("Drain(%s) error = %v", id, err)
		}
		if report.Outcome != indexretention.OutcomeIneligible || report.Rows() != 0 {
			t.Fatalf("Drain(%s) = %+v, want ineligible with no row deleted", id, report)
		}
		if terms, documents := f.counts(t, id); terms != 1500 || documents != 300 {
			t.Fatalf("%s keeps %d terms, %d documents, want 1500 and 300", id, terms, documents)
		}
	}

	// A superseded row the scope's active pointer still names is not
	// terminal history either.
	f.exec(t, `UPDATE ingestion_scopes SET active_generation_id = $1 WHERE scope_id = $2`, f.old, f.scope)
	report, err := indexretention.Drain(f.ctx, f.database, f.generation(f.old), unbounded())
	if err != nil {
		t.Fatalf("Drain(old as active pointer) error = %v", err)
	}
	if report.Outcome != indexretention.OutcomeIneligible {
		t.Fatalf("Drain(old as active pointer) = %+v, want ineligible", report)
	}
	if terms, _ := f.counts(t, f.old); terms != 1500 {
		t.Fatalf("superseded generation behind the active pointer keeps %d terms, want 1500", terms)
	}
}

// TestDrainLiveWaitsForRunningSearchDocumentWork is the #7427 c5 proof: a
// running eshu_search_document work item for the generation blocks the
// pre-delete; once it finishes, the next drain removes the rows.
func TestDrainLiveWaitsForRunningSearchDocumentWork(t *testing.T) {
	f := openLiveFixture(t)
	f.seedIndex(t, f.old, 200, 5)
	f.exec(t, `INSERT INTO fact_work_items (work_item_id, scope_id, generation_id, stage, domain, status, created_at, updated_at)
VALUES ($1, $2, $3, 'reducer', 'eshu_search_document', 'running', now(), now())`, f.old+"-work", f.scope, f.old)

	report, err := indexretention.Drain(f.ctx, f.database, f.generation(f.old), unbounded())
	if err != nil {
		t.Fatalf("Drain() error = %v", err)
	}
	if report.Outcome != indexretention.OutcomeIneligible || report.Rows() != 0 {
		t.Fatalf("Drain() with running work = %+v, want ineligible", report)
	}
	if terms, documents := f.counts(t, f.old); terms != 1000 || documents != 200 {
		t.Fatalf("generation with running work keeps %d terms, %d documents, want 1000 and 200", terms, documents)
	}

	f.exec(t, `UPDATE fact_work_items SET status = 'succeeded' WHERE work_item_id = $1`, f.old+"-work")
	report, err = indexretention.Drain(f.ctx, f.database, f.generation(f.old), unbounded())
	if err != nil || !report.Drained() {
		t.Fatalf("Drain() after the work finished = %+v, %v, want drained", report, err)
	}
}

// TestDrainLiveCancelledMidLoopConverges is #7427 c6(a): a drain cancelled
// after its first committed batch keeps that batch's deletes, and a rerun
// finishes the generation.
func TestDrainLiveCancelledMidLoopConverges(t *testing.T) {
	f := openLiveFixture(t)
	f.seedIndex(t, f.old, 3000, 10)
	ctx, cancel := context.WithCancel(f.ctx)
	budget := unbounded()
	indexretention.SetAfterBatch(budget, cancel)
	report, err := indexretention.Drain(ctx, f.database, f.generation(f.old), budget)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Drain() error = %v, want context.Canceled", err)
	}
	if report.TermsDeleted != indexretention.BatchRows {
		t.Fatalf("cancelled drain deleted %d terms, want one batch of %d", report.TermsDeleted, indexretention.BatchRows)
	}
	if terms, _ := f.counts(t, f.old); terms != 30000-indexretention.BatchRows {
		t.Fatalf("after cancel %d terms remain, want %d", terms, 30000-indexretention.BatchRows)
	}
	report, err = indexretention.Drain(f.ctx, f.database, f.generation(f.old), unbounded())
	if err != nil || !report.Drained() {
		t.Fatalf("rerun = %+v, %v, want drained", report, err)
	}
	if terms, documents := f.counts(t, f.old); terms != 0 || documents != 0 {
		t.Fatalf("after rerun %d terms, %d documents remain, want none", terms, documents)
	}
}

// TestDrainLiveResweepRemovesARowInsertedBehindTheCursor is #7427 c6(b): a
// row committed behind the keyset cursor after the first batch is removed by
// the from-start re-sweep before the generation counts as drained.
func TestDrainLiveResweepRemovesARowInsertedBehindTheCursor(t *testing.T) {
	f := openLiveFixture(t)
	f.seedIndex(t, f.old, 3000, 10)
	budget := unbounded()
	inserted := false
	indexretention.SetAfterBatch(budget, func() {
		if inserted {
			return
		}
		inserted = true
		// '' sorts before every seeded key, so the row lands behind the cursor.
		f.exec(t, `INSERT INTO eshu_search_index_terms (scope_id, generation_id, document_id, term_key, term, term_frequency)
VALUES ($1, $2, '', '', 'late', 1)`, f.scope, f.old)
	})
	report, err := indexretention.Drain(f.ctx, f.database, f.generation(f.old), budget)
	if err != nil || !report.Drained() {
		t.Fatalf("Drain() = %+v, %v, want drained", report, err)
	}
	if report.TermsDeleted != 30001 {
		t.Fatalf("TermsDeleted = %d, want 30001 (the late row included)", report.TermsDeleted)
	}
	if terms, _ := f.counts(t, f.old); terms != 0 {
		t.Fatalf("%d terms remain, want none", terms)
	}
}

// TestDrainLiveSkipsAGenerationAnotherSessionHolds is #7427 c6(c) and D4: a
// session holding the generation's advisory lock (a prune transaction or
// another pass) makes the drain skip without deleting or waiting, and two
// concurrent drains of one generation never deadlock: each batch either
// takes the lock or skips, and together they drain the generation.
func TestDrainLiveSkipsAGenerationAnotherSessionHolds(t *testing.T) {
	f := openLiveFixture(t)
	f.seedIndex(t, f.old, 3000, 10)

	holder, err := f.database.Begin(f.ctx)
	if err != nil {
		t.Fatalf("begin holder: %v", err)
	}
	locked, err := indexretention.TryLockGenerations(f.ctx, holder, []string{f.old})
	if err != nil || !locked[f.old] {
		t.Fatalf("holder lock = %v, %v, want held", locked, err)
	}
	report, err := indexretention.Drain(f.ctx, f.database, f.generation(f.old), unbounded())
	if err != nil {
		t.Fatalf("Drain() error = %v", err)
	}
	if report.Outcome != indexretention.OutcomeLockBusy || report.Rows() != 0 {
		t.Fatalf("Drain() under a held lock = %+v, want advisory_lock_busy with nothing deleted", report)
	}
	if again, err := indexretention.TryLockGenerations(f.ctx, f.database, []string{f.old}); err != nil || again[f.old] {
		t.Fatalf("second try lock from another session = %v, %v, want refused", again, err)
	}
	_ = holder.Rollback()

	var wg sync.WaitGroup
	reports := make([]indexretention.Report, 2)
	errs := make([]error, 2)
	start := make(chan struct{})
	for i := range reports {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			reports[i], errs[i] = indexretention.Drain(f.ctx, f.database, f.generation(f.old), unbounded())
		}(i)
	}
	close(start)
	wg.Wait()
	drained := 0
	for i, err := range errs {
		if err != nil {
			if strings.Contains(err.Error(), "40P01") {
				t.Fatalf("concurrent drain %d deadlocked: %v", i, err)
			}
			t.Fatalf("concurrent drain %d error = %v", i, err)
		}
		if reports[i].Drained() {
			drained++
		} else if reports[i].Outcome != indexretention.OutcomeLockBusy {
			t.Fatalf("concurrent drain %d = %+v, want drained or advisory_lock_busy", i, reports[i])
		}
	}
	if total := reports[0].Rows() + reports[1].Rows(); total != 33000 {
		t.Fatalf("concurrent drains deleted %d rows together, want 33000 exactly once", total)
	}
	final, err := indexretention.Drain(f.ctx, f.database, f.generation(f.old), unbounded())
	if err != nil || !final.Drained() || final.Rows() != 0 {
		t.Fatalf("final drain = %+v, %v, want drained with nothing left (drained by %d racers)", final, err, drained)
	}
}
