// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package indexretention_test

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #8231 disposable PostgreSQL 18 proof of the superseded-generation search index sweep: only a superseded generation with a live active pointer elsewhere loses rows while the active generation's rows and its active-generation reader view stay byte-identical, and generations with running work, a NULL active pointer, a missing generation row, or the active pointer itself lose nothing; enrolled in the live-postgres-readiness runner; reads ESHU_GENERATION_RETENTION_PROOF_DSN (with its DISPOSABLE flag) and creates one isolated bootstrapped schema per test. local run passed both tests in 6.5s wall on PostgreSQL 18 (container postgres:18) on 2026-10-18; wall time is logged, not gated
//eshu:readiness:env ESHU_GENERATION_RETENTION_PROOF_DISPOSABLE ESHU_GENERATION_RETENTION_PROOF_DSN ESHU_POSTGRES_DSN

import (
	"testing"
	"time"

	indexretention "github.com/eshu-hq/eshu/go/internal/storage/postgres/search/index/retention"
)

// seedStats writes one generation's stats row for docs documents.
func (f *liveFixture) seedStats(t *testing.T, generation string, docs int) {
	t.Helper()
	f.exec(t, `INSERT INTO eshu_search_index_stats (scope_id, generation_id, document_count, average_document_length, updated_at)
VALUES ($1, $2, $3, 10.0, now())`, f.scope, generation, docs)
}

// statsCount returns the generation's stats rows.
func (f *liveFixture) statsCount(t *testing.T, generation string) int64 {
	t.Helper()
	var count int64
	if err := f.db.QueryRowContext(f.ctx, `SELECT count(*) FROM eshu_search_index_stats
WHERE scope_id = $1 AND generation_id = $2`, f.scope, generation).Scan(&count); err != nil {
		t.Fatalf("count stats rows: %v", err)
	}
	return count
}

// activeReaderDocumentCount reads the active generation's document count the
// way every search index reader does: through the scope's active pointer, so
// a superseded generation's rows are invisible without any sweep.
func (f *liveFixture) activeReaderDocumentCount(t *testing.T) int64 {
	t.Helper()
	var count int64
	if err := f.db.QueryRowContext(f.ctx, `SELECT stats.document_count
FROM eshu_search_index_stats AS stats
JOIN ingestion_scopes AS scope
  ON scope.scope_id = stats.scope_id
WHERE stats.scope_id = $1
  AND scope.active_generation_id IS NOT NULL
  AND scope.active_generation_id = stats.generation_id`, f.scope).Scan(&count); err != nil {
		t.Fatalf("read active generation stats: %v", err)
	}
	return count
}

// TestSweepSupersededLiveKeepsTheActiveReaderUnchanged is the #8231 live
// proof: a superseded generation's terms, documents, and stats rows are
// swept while the active generation keeps every row and its reader-visible
// stats read returns the same count before and after.
func TestSweepSupersededLiveKeepsTheActiveReaderUnchanged(t *testing.T) {
	f := openLiveFixture(t)
	f.seedIndex(t, f.old, 300, 5)
	f.seedIndex(t, f.active, 300, 5)
	f.seedStats(t, f.old, 300)
	f.seedStats(t, f.active, 300)

	before := f.activeReaderDocumentCount(t)
	if before != 300 {
		t.Fatalf("active reader document count = %d, want 300", before)
	}

	sweep, err := indexretention.SweepSuperseded(f.ctx, f.database, time.Time{}, 0, nil)
	if err != nil {
		t.Fatalf("SweepSuperseded() error = %v", err)
	}
	if sweep.Candidates != 1 || sweep.Unswept != 0 {
		t.Fatalf("sweep = %+v, want the one superseded generation swept", sweep)
	}
	if sweep.RowsSwept[indexretention.TermsTable] != 1500 ||
		sweep.RowsSwept[indexretention.DocumentsTable] != 300 ||
		sweep.RowsSwept[indexretention.StatsTable] != 1 {
		t.Fatalf("RowsSwept = %v, want 1500 terms, 300 documents, 1 stats", sweep.RowsSwept)
	}

	if terms, documents := f.counts(t, f.old); terms != 0 || documents != 0 {
		t.Fatalf("superseded generation keeps %d terms, %d documents, want none", terms, documents)
	}
	if got := f.statsCount(t, f.old); got != 0 {
		t.Fatalf("superseded generation keeps %d stats rows, want none", got)
	}
	if terms, documents := f.counts(t, f.active); terms != 1500 || documents != 300 {
		t.Fatalf("active generation has %d terms, %d documents, want 1500 and 300", terms, documents)
	}
	if got := f.statsCount(t, f.active); got != 1 {
		t.Fatalf("active generation has %d stats rows, want 1", got)
	}
	if after := f.activeReaderDocumentCount(t); after != before {
		t.Fatalf("active reader document count = %d after the sweep, want %d (unchanged)", after, before)
	}
}

// TestSweepSupersededLiveSkipsPinnedAndInflight is the #8231 fail-closed
// proof: a generation with running search-document work (in flight), a scope
// whose active pointer is NULL, a generation whose row is missing, and a
// superseded generation the active pointer still names are never listed and
// lose no row. (A dependency pin has no live-store signal — every
// generation-referencing row class cascades or is batch-deleted with the
// prune — so the pin vocabulary rides on the active-pointer and live-work
// guards this test exercises.)
func TestSweepSupersededLiveSkipsPinnedAndInflight(t *testing.T) {
	t.Run("running work", func(t *testing.T) {
		f := openLiveFixture(t)
		f.seedIndex(t, f.old, 200, 5)
		f.seedStats(t, f.old, 200)
		f.exec(t, `INSERT INTO fact_work_items (work_item_id, scope_id, generation_id, stage, domain, status, created_at, updated_at)
VALUES ($1, $2, $3, 'reducer', 'eshu_search_document', 'running', now(), now())`, f.old+"-work", f.scope, f.old)

		sweep, err := indexretention.SweepSuperseded(f.ctx, f.database, time.Time{}, 0, nil)
		if err != nil {
			t.Fatalf("SweepSuperseded() error = %v", err)
		}
		if sweep.Candidates != 0 {
			t.Fatalf("sweep = %+v, want no candidate while work is in flight", sweep)
		}
		if terms, documents := f.counts(t, f.old); terms != 1000 || documents != 200 {
			t.Fatalf("generation with running work keeps %d terms, %d documents, want 1000 and 200", terms, documents)
		}
		if got := f.statsCount(t, f.old); got != 1 {
			t.Fatalf("generation with running work keeps %d stats rows, want 1", got)
		}
	})

	t.Run("null active pointer", func(t *testing.T) {
		f := openLiveFixture(t)
		f.seedIndex(t, f.old, 200, 5)
		f.seedStats(t, f.old, 200)
		f.exec(t, `UPDATE ingestion_scopes SET active_generation_id = NULL WHERE scope_id = $1`, f.scope)

		sweep, err := indexretention.SweepSuperseded(f.ctx, f.database, time.Time{}, 0, nil)
		if err != nil {
			t.Fatalf("SweepSuperseded() error = %v", err)
		}
		if sweep.Candidates != 0 {
			t.Fatalf("sweep = %+v, want no candidate with a NULL active pointer", sweep)
		}
		if terms, documents := f.counts(t, f.old); terms != 1000 || documents != 200 {
			t.Fatalf("generation under a NULL pointer keeps %d terms, %d documents, want 1000 and 200", terms, documents)
		}
	})

	t.Run("missing generation row", func(t *testing.T) {
		f := openLiveFixture(t)
		f.dropGenerationForeignKeys(t)
		f.seedIndex(t, f.old, 200, 5)
		f.orphan(t, f.old)

		sweep, err := indexretention.SweepSuperseded(f.ctx, f.database, time.Time{}, 0, nil)
		if err != nil {
			t.Fatalf("SweepSuperseded() error = %v", err)
		}
		if sweep.Candidates != 0 {
			t.Fatalf("sweep = %+v, want no candidate for a missing generation row (orphan sweep owns it)", sweep)
		}
		if terms, documents := f.counts(t, f.old); terms != 1000 || documents != 200 {
			t.Fatalf("orphan keeps %d terms, %d documents, want 1000 and 200 for the orphan sweep", terms, documents)
		}
	})

	t.Run("active pointer names it", func(t *testing.T) {
		f := openLiveFixture(t)
		f.seedIndex(t, f.old, 200, 5)
		f.exec(t, `UPDATE ingestion_scopes SET active_generation_id = $1 WHERE scope_id = $2`, f.old, f.scope)

		sweep, err := indexretention.SweepSuperseded(f.ctx, f.database, time.Time{}, 0, nil)
		if err != nil {
			t.Fatalf("SweepSuperseded() error = %v", err)
		}
		if sweep.Candidates != 0 {
			t.Fatalf("sweep = %+v, want no candidate behind the active pointer", sweep)
		}
		if terms, _ := f.counts(t, f.old); terms != 1000 {
			t.Fatalf("generation behind the active pointer keeps %d terms, want 1000", terms)
		}
	})
}
