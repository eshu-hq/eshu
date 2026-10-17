// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package indexretention_test

import (
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	indexretention "github.com/eshu-hq/eshu/go/internal/storage/postgres/search/index/retention"
)

// dropGenerationForeignKeys removes every search index generation foreign key
// in the test's isolated schema, so a row of either table can outlive its
// scope_generations row. Migration 191 (#7427 PR3) already dropped the terms
// key on the bootstrapped schema; the documents key stays in production and
// is dropped here only to exercise the documents half of the census.
func (f *liveFixture) dropGenerationForeignKeys(t *testing.T) {
	t.Helper()
	if got := f.termsGenerationForeignKeys(t); len(got) != 0 {
		t.Fatalf("terms generation foreign keys after bootstrap = %v, want none (migration 191)", got)
	}
	f.exec(t, `ALTER TABLE eshu_search_index_documents DROP CONSTRAINT eshu_search_index_documents_generation_id_fkey`)
}

// orphan deletes the generation's scope_generations row and leaves its
// search index rows: the shape a late writer leaves once the foreign key is
// gone.
func (f *liveFixture) orphan(t *testing.T, generation string) {
	t.Helper()
	f.exec(t, `DELETE FROM scope_generations WHERE generation_id = $1`, generation)
}

// TestOrphanCensusLiveReadsZeroOnACleanTree is the clean-tree proof: while
// every search index row's generation exists the census reads 0 for both
// tables while it walks every pair. It runs on the bootstrapped schema, where
// the terms generation foreign key is gone (migration 191) and the documents
// one stays.
func TestOrphanCensusLiveReadsZeroOnACleanTree(t *testing.T) {
	f := openLiveFixture(t)
	f.seedIndex(t, f.active, 200, 3)
	f.seedIndex(t, f.old, 200, 3)
	f.seedIndex(t, f.pending, 200, 3)

	census, err := indexretention.TakeCensus(f.ctx, f.database)
	if err != nil {
		t.Fatalf("TakeCensus() error = %v", err)
	}
	for _, table := range []string{indexretention.TermsTable, indexretention.DocumentsTable} {
		if census.OrphanGenerations[table] != 0 || census.PairsScanned[table] != 3 {
			t.Fatalf("census %s = %d orphans of %d pairs, want 0 of 3", table,
				census.OrphanGenerations[table], census.PairsScanned[table])
		}
	}
	if len(census.Orphans) != 0 {
		t.Fatalf("Orphans = %v, want none", census.Orphans)
	}
}

// TestOrphanSweepLiveRemovesOnlyMissingGenerations: an orphan generation's
// rows are swept, while the active, pending and an existing superseded
// generation keep every row, and a second sweep finds nothing (idempotent).
func TestOrphanSweepLiveRemovesOnlyMissingGenerations(t *testing.T) {
	f := openLiveFixture(t)
	f.dropGenerationForeignKeys(t)
	lost := "gen-lost-" + f.old
	f.exec(t, `INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, superseded_at)
VALUES ($1, $2, 'snapshot', now() - interval '20 days', now() - interval '20 days', 'superseded', now() - interval '19 days')`, lost, f.scope)
	for _, generation := range []string{f.active, f.old, f.pending, lost} {
		f.seedIndex(t, generation, 2500, 10)
	}
	f.orphan(t, lost)

	// A row cap of one batch (#8281 review) stops the sweep after its first
	// committed batch and leaves the orphan unswept for the next sweep.
	capped, err := indexretention.SweepOrphans(f.ctx, f.database, time.Time{}, indexretention.BatchRows, nil)
	if err != nil {
		t.Fatalf("capped SweepOrphans() error = %v", err)
	}
	if got := capped.RowsSwept; got[indexretention.TermsTable] != indexretention.BatchRows || got[indexretention.DocumentsTable] != 0 || capped.Unswept != 1 {
		t.Fatalf("capped sweep = %+v, want one %d-row terms batch and the orphan unswept", capped, indexretention.BatchRows)
	}

	sweep, err := indexretention.SweepOrphans(f.ctx, f.database, time.Time{}, 0, nil)
	if err != nil {
		t.Fatalf("SweepOrphans() error = %v", err)
	}
	if got := sweep.Census.OrphanGenerations; got[indexretention.TermsTable] != 1 || got[indexretention.DocumentsTable] != 1 {
		t.Fatalf("census orphans = %v, want 1 per table", got)
	}
	if got := sweep.RowsSwept; got[indexretention.TermsTable] != 25000-indexretention.BatchRows || got[indexretention.DocumentsTable] != 2500 || sweep.Unswept != 0 {
		t.Fatalf("sweep = %+v, want the remaining %d terms and 2500 documents swept, none unswept", sweep, 25000-indexretention.BatchRows)
	}
	if terms, documents := f.counts(t, lost); terms != 0 || documents != 0 {
		t.Fatalf("orphan keeps %d terms, %d documents, want none", terms, documents)
	}
	for _, generation := range []string{f.active, f.old, f.pending} {
		if terms, documents := f.counts(t, generation); terms != 25000 || documents != 2500 {
			t.Fatalf("%s keeps %d terms, %d documents, want 25000 and 2500", generation, terms, documents)
		}
	}

	again, err := indexretention.SweepOrphans(f.ctx, f.database, time.Time{}, 0, nil)
	if err != nil {
		t.Fatalf("second SweepOrphans() error = %v", err)
	}
	if again.Census.OrphanGenerations[indexretention.TermsTable] != 0 || again.RowsSwept[indexretention.TermsTable] != 0 || again.Batches != 0 {
		t.Fatalf("second sweep = %+v, want a clean census and no batch", again)
	}
}

// TestOrphanSweepLiveNeverDeletesAnExistingGeneration passes every existing
// generation, and a missing generation the scope's active pointer still
// names, straight to SweepOrphan: each is ineligible and loses no row.
func TestOrphanSweepLiveNeverDeletesAnExistingGeneration(t *testing.T) {
	f := openLiveFixture(t)
	f.dropGenerationForeignKeys(t)
	for _, generation := range []string{f.active, f.old, f.pending} {
		f.seedIndex(t, generation, 300, 5)
		report, err := indexretention.SweepOrphan(f.ctx, f.database, f.generation(generation), unbounded())
		if err != nil {
			t.Fatalf("SweepOrphan(%s) error = %v", generation, err)
		}
		if report.Outcome != indexretention.OutcomeIneligible || report.Rows() != 0 {
			t.Fatalf("SweepOrphan(%s) = %+v, want ineligible with no row deleted", generation, report)
		}
		if terms, documents := f.counts(t, generation); terms != 1500 || documents != 300 {
			t.Fatalf("%s keeps %d terms, %d documents, want 1500 and 300", generation, terms, documents)
		}
	}

	// A dangling active pointer: the row is gone but readers still join the
	// generation, so its rows are visible and must stay.
	f.orphan(t, f.active)
	report, err := indexretention.SweepOrphan(f.ctx, f.database, f.generation(f.active), unbounded())
	if err != nil {
		t.Fatalf("SweepOrphan(dangling active) error = %v", err)
	}
	if report.Outcome != indexretention.OutcomeIneligible {
		t.Fatalf("SweepOrphan(dangling active) = %+v, want ineligible", report)
	}
	if terms, _ := f.counts(t, f.active); terms != 1500 {
		t.Fatalf("generation behind the dangling pointer keeps %d terms, want 1500", terms)
	}
}

// TestOrphanSweepLiveConcurrentSweepsDeleteOnce runs two sweeps of the same
// orphans at once: each row is deleted once, neither returns an error, and no
// deadlock (40P01) surfaces; the advisory lock makes the loser skip.
func TestOrphanSweepLiveConcurrentSweepsDeleteOnce(t *testing.T) {
	f := openLiveFixture(t)
	f.dropGenerationForeignKeys(t)
	f.seedIndex(t, f.old, 3300, 10)
	f.orphan(t, f.old)

	var wg sync.WaitGroup
	results := make([]indexretention.OrphanSweep, 2)
	errs := make([]error, 2)
	for i := range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[i], errs[i] = indexretention.SweepOrphans(f.ctx, f.database, time.Time{}, 0, nil)
		}()
	}
	wg.Wait()
	for i, err := range errs {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "40P01" {
			t.Fatalf("sweep %d deadlocked: %v", i, err)
		}
		if err != nil {
			t.Fatalf("sweep %d error = %v", i, err)
		}
	}
	total := results[0].RowsSwept[indexretention.TermsTable] + results[1].RowsSwept[indexretention.TermsTable]
	if total != 33000 {
		t.Fatalf("terms swept by both sweeps = %d, want 33000 exactly once", total)
	}
	// A loser that skipped leaves nothing behind: the winner drained it.
	if terms, documents := f.counts(t, f.old); terms != 0 || documents != 0 {
		t.Fatalf("orphan keeps %d terms, %d documents after both sweeps", terms, documents)
	}
}

// TestOrphanSweepLiveSweepsALateWriter inserts rows for the orphan
// generation between two batches of the sweep, behind its cursor: the
// from-start re-sweep removes them in the same sweep. A row written after a
// sweep is found by the next census.
func TestOrphanSweepLiveSweepsALateWriter(t *testing.T) {
	f := openLiveFixture(t)
	f.dropGenerationForeignKeys(t)
	f.seedIndex(t, f.old, 3000, 10)
	f.orphan(t, f.old)

	budget := unbounded()
	inserted := false
	indexretention.SetAfterBatch(budget, func() {
		if inserted {
			return
		}
		inserted = true
		f.exec(t, `INSERT INTO eshu_search_index_terms (scope_id, generation_id, document_id, term_key, term, term_frequency)
VALUES ($1, $2, 'doc-late', 'a-late', 'late', 1)`, f.scope, f.old)
	})
	report, err := indexretention.SweepOrphan(f.ctx, f.database, f.generation(f.old), budget)
	if err != nil {
		t.Fatalf("SweepOrphan() error = %v", err)
	}
	if !report.Drained() || report.TermsDeleted != 30001 {
		t.Fatalf("report = %+v, want drained with 30001 terms (the late row included)", report)
	}

	f.exec(t, `INSERT INTO eshu_search_index_terms (scope_id, generation_id, document_id, term_key, term, term_frequency)
VALUES ($1, $2, 'doc-later', 'a-later', 'later', 1)`, f.scope, f.old)
	sweep, err := indexretention.SweepOrphans(f.ctx, f.database, time.Time{}, 0, nil)
	if err != nil {
		t.Fatalf("SweepOrphans() error = %v", err)
	}
	if sweep.Census.OrphanGenerations[indexretention.TermsTable] != 1 || sweep.RowsSwept[indexretention.TermsTable] != 1 {
		t.Fatalf("next sweep = %+v, want the later row found and swept", sweep)
	}
}

// TestOrphanLiveUnknownScopeWriteIsRejected is the c3 unknown-scope proof:
// with the generation foreign keys gone, the scope_id foreign key that
// stays rejects a search index row for a scope that does not exist, so the
// freshness check's unknown-scope (true, nil) cannot leave an orphan.
func TestOrphanLiveUnknownScopeWriteIsRejected(t *testing.T) {
	f := openLiveFixture(t)
	f.dropGenerationForeignKeys(t)
	for _, statement := range []string{
		`INSERT INTO eshu_search_index_terms (scope_id, generation_id, document_id, term_key, term, term_frequency)
VALUES ('scope-unknown-7427', 'gen-unknown-7427', 'doc', 'tk', 'term', 1)`,
		`INSERT INTO eshu_search_index_documents (scope_id, generation_id, document_id, fact_id, repo_id,
    source_kind, document, document_length, updated_at)
VALUES ('scope-unknown-7427', 'gen-unknown-7427', 'doc', 'fact', 'repo', 'file', '{}'::jsonb, 1, now())`,
	} {
		_, err := f.db.ExecContext(f.ctx, statement)
		var pgErr *pgconn.PgError
		if !errors.As(err, &pgErr) || pgErr.Code != "23503" || !strings.Contains(pgErr.ConstraintName, "scope_id_fkey") {
			t.Fatalf("insert for an unknown scope: err = %v, want 23503 on the scope_id foreign key", err)
		}
	}
}
