// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package summary_test

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #7009 disposable PostgreSQL 18 bloat and read-buffer proof of the terraform_state status summary row (also a blocking step of the reducer-contention-gate workflow, fail-closed through ESHU_REQUIRE_STATUS_SUMMARY_PROOF): 4,000 upserts of a 72-entry (about 35 KB, the QA size) and of a 141-entry (about 69 KB) TOASTed payload then a manual VACUUM with autovacuum disabled on the proof table (a merge-group run failed the TOAST bound when autovacuum vacuumed the TOAST relation during round one) leave a one-page heap, a bounded TOAST relation, HOT updates, and a read that touches only status_summary_snapshots in at most 8 plan buffers and 30 TOAST buffers; a 5 MB worst-case row stays bounded and is read in 151 TOAST buffers
//eshu:readiness:env ESHU_STATUS_SUMMARY_PROOF_DISPOSABLE ESHU_STATUS_SUMMARY_PROOF_DSN

import (
	"fmt"
	"math/rand"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/status/summary"
)

// terraformBloatRow builds a terraform_state row shaped like the stored
// entries of statestore.SummaryEntries: serials last_serial rows and warnings
// recent_warning rows with 64-character locator hashes, generation ids, and
// RFC 3339 timestamps, so the payload compresses like the real one does.
func terraformBloatRow(random *rand.Rand, asOf time.Time, serials, warnings int) summary.Row {
	const hex = "0123456789abcdef"
	hash := func() string {
		out := make([]byte, 64)
		for i := range out {
			out[i] = hex[random.Intn(len(hex))]
		}
		return string(out)
	}
	row := summary.Row{
		ModelKey:      summary.ModelTerraformState,
		SchemaVersion: summary.SchemaVersion,
		SourceSHA256:  hash(),
		AsOf:          asOf,
		PassDuration:  346 * time.Millisecond,
		RowCount:      serials + warnings,
	}
	locators := make([]string, serials)
	for i := range locators {
		locators[i] = hash()
		row.Entries = append(row.Entries, summary.Entry{
			Section: "last_serial",
			Ordinal: int64(i + 1),
			JSON: fmt.Sprintf(`{"safe_locator_hash":%q,"backend_kind":"s3","lineage":"%08x-0000-4000-8000-%012x","serial":%d,"generation_id":"terraform_state:state_snapshot:s3:%s:lineage:serial:%d","observed_at":"2026-10-07T09:%02d:15.123456Z"}`,
				locators[i], random.Uint32(), random.Uint64()&0xffffffffffff, 40+i, locators[i], 40+i, i%60),
		})
	}
	for i := 0; i < warnings; i++ {
		locator := locators[i%serials]
		row.Entries = append(row.Entries, summary.Entry{
			Section: "recent_warning",
			Ordinal: int64(i + 1),
			JSON: fmt.Sprintf(`{"safe_locator_hash":%q,"backend_kind":"s3","warning_kind":%q,"reason":"reason-%d","severity":"warning","actionability":"operator","source":"terraform_state","source_handle":"handle-%08x","generation_id":"terraform_state:state_snapshot:s3:%s:lineage:serial:%d","observed_at":"2026-10-07T09:%02d:%02d.654321Z"}`,
				locator, []string{"state_missing", "sensitive_skip", "output_value_dropped"}[i%3], i%4, random.Uint32(), locator, 40+i%serials, i%60, i%60),
		})
	}
	return row
}

// TestStatusSummaryBloatTerraformLive is the bloat and read-buffer proof at the
// terraform_state model's payload: 30 last-serial rows and 111 warning rows
// (the QA figure), about 35 KB, which TOASTs. 4,000 upserts and VACUUM must
// keep the heap and TOAST relation bounded with HOT updates, and the keyed
// read must touch only status_summary_snapshots and its TOAST relation.
func TestStatusSummaryBloatTerraformLive(t *testing.T) {
	random := rand.New(rand.NewSource(7009))
	runBloatProof(t, func(i int) summary.Row {
		return terraformBloatRow(random, proofAsOf.Add(time.Duration(i)*time.Second), 30, 111)
	}, bloatBounds{
		model: summary.ModelTerraformState, wantToast: true,
		minPayload: 25000, maxPayload: 100000, maxReadBuffers: 8, maxToastBuffers: 30,
	})
}

// TestStatusSummaryBloatTerraformQAScaleLive is the same proof at the QA
// payload: about 111 warning rows at roughly 300 bytes each, about 35 KB. The
// proof's rows are larger per entry (full 64-character hashes and generation
// ids), so 20 serials and 52 warnings land at that size.
func TestStatusSummaryBloatTerraformQAScaleLive(t *testing.T) {
	random := rand.New(rand.NewSource(7011))
	runBloatProof(t, func(i int) summary.Row {
		return terraformBloatRow(random, proofAsOf.Add(time.Duration(i)*time.Second), 20, 52)
	}, bloatBounds{
		model: summary.ModelTerraformState, wantToast: true,
		minPayload: 28000, maxPayload: 45000, maxReadBuffers: 8, maxToastBuffers: 20,
	})
}

// TestStatusSummaryBloatTerraformWorstCaseLive measures the worst case the
// statement allows on the fixture: about 9,800 warning rows, 4.8-5.1 MB of
// payload. It upserts far fewer times (each write is megabytes) and only
// reports the heap, TOAST and read buffers; the proof is that the table stays
// bounded and the read stays on its own relation, not a speed claim.
func TestStatusSummaryBloatTerraformWorstCaseLive(t *testing.T) {
	random := rand.New(rand.NewSource(7010))
	ctx, database := openProofDatabase(t)
	applySummaryMigration(ctx, t, database)
	store := summary.NewStore(poolStore{database})
	for i := 0; i < 60; i++ {
		row := terraformBloatRow(random, proofAsOf.Add(time.Duration(i)*time.Second), 40, 9800)
		if advanced, err := store.Upsert(ctx, row); err != nil || !advanced {
			t.Fatalf("upsert %d = %v, %v, want true, nil", i, advanced, err)
		}
		if i%20 == 19 {
			if _, err := database.ExecContext(ctx, `VACUUM status_summary_snapshots`); err != nil {
				t.Fatalf("VACUUM: %v", err)
			}
		}
	}
	m := measureBloat(ctx, t, database)
	hits, reads, toastHits, toastReads := explainReadBuffers(ctx, t, database, summary.ModelTerraformState)
	t.Logf("worst case after 60 upserts: heap=%d pages toast=%d pages dead=%d hot_ratio=%.3f; read shared hit=%d read=%d serialization hit=%d read=%d",
		m.heapPages, m.toastPages, m.dead, m.hotRatio(), hits, reads, toastHits, toastReads)
	if m.heapPages > 8 {
		t.Errorf("heap = %d pages, want <= 8", m.heapPages)
	}
	if hits+reads > 8 {
		t.Errorf("model read touched %d plan buffers, want <= 8", hits+reads)
	}
	// Measured at 151 serialization buffers for the 5 MB worst case.
	if toastHits+toastReads > 300 {
		t.Errorf("model read detoasted through %d buffers, want <= 300", toastHits+toastReads)
	}
}
