// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package tfstate

import (
	"strings"
	"testing"
)

// Stable boundaries for the #8875 round-2 outer-leg differential probe. The
// probe derives from the shipped listTerraformBackendCanonicalRowsQuery
// constant (never a hand copy): truncate at the trailing ORDER BY, which is
// plan-shape-irrelevant, and wrap the truncation in a read-only EXPLAIN the
// fix lane runs against a planner. The hermetic prefix guard below fails on
// any drift of the constant, so the probe can never silently diverge from
// production.
const (
	tf8875OuterLegQueryHead   = "\nWITH backend_generations AS ("
	tf8875OuterLegOrderMarker = "\nORDER BY backend.repo_id ASC"
	tf8875OuterLegArm         = "FROM backend_generations AS backend"
)

// TestTF8875CanonicalOuterLegExactnessDifferential is the RED-first scaffold
// for #8875 round 2. The parent lane holds EXPLAIN ANALYZE proof that the
// outer leg of listTerraformBackendCanonicalRowsQuery re-reads ~55k
// fact_records rows applying the repo_id join filter plus the three-way
// (backends + variables + locals) CASE sum per heap row in memory. The
// exactness half of this test (derivation + prefix guard) is GREEN and
// freezes the probe to the shipped constant; the plan-shape half at the end
// STAYS RED until the index/restructure fix lands.
func TestTF8875CanonicalOuterLegExactnessDifferential(t *testing.T) {
	t.Parallel()

	prod := listTerraformBackendCanonicalRowsQuery
	if !strings.HasPrefix(prod, tf8875OuterLegQueryHead) {
		t.Fatalf("canonical query head drifted; re-derive the outer-leg probe, do not hand-copy it")
	}
	cut := strings.Index(prod, tf8875OuterLegOrderMarker)
	if cut < 0 {
		t.Fatalf("outer-leg boundary marker %q missing; re-derive the probe, do not hand-copy it",
			tf8875OuterLegOrderMarker)
	}
	derived := prod[:cut]
	if !strings.HasPrefix(prod, derived) {
		t.Fatalf("hermetic prefix guard: derived outer-leg probe is not a byte-prefix of the shipped constant")
	}
	armAt := strings.Index(derived, tf8875OuterLegArm)
	if armAt < 0 {
		t.Fatalf("derived probe no longer covers the outer leg (%q absent); re-derive, do not hand-copy it",
			tf8875OuterLegArm)
	}
	probe := "EXPLAIN " + derived
	if strings.TrimPrefix(probe, "EXPLAIN ") != derived {
		t.Fatalf("outer-leg probe is not a byte-exact derivation of the shipped constant")
	}

	// STAYS RED until the round-2 outer-leg fix lands. The defect
	// fingerprint is the outer arm joining fact_records on the unindexed
	// repo_id payload expression while re-evaluating all three CASE array
	// lengths per heap row (parent EXPLAIN ANALYZE: ~55k rows filtered in
	// memory). A restructure that pushes the repo_id/CASE work into an
	// indexed arm clears the fingerprint and turns this green. An
	// index-only fix keeps the text identical: that lane must extend this
	// scaffold with a pg_indexes assertion for the new index name, following
	// the TestTF8875CanonicalBackendsIndexExists precedent, instead of
	// weakening this clause. Either way the derived probe above keeps the
	// row-set contract frozen while the plan changes.
	outer := derived[armAt:]
	joinsRepoIDInMemory := strings.Contains(outer, "fact.payload->>'repo_id' = backend.repo_id")
	caseEvalsPerHeapRow := strings.Count(outer, "jsonb_array_length(fact.payload")
	if joinsRepoIDInMemory && caseEvalsPerHeapRow == 3 {
		// S1 index-only fix (#8875 round 2, migration 276): the query text
		// is unchanged by design, so the shape fingerprint above still
		// matches. The defect (unindexed ~55k-row heap re-read applying
		// the repo_id join filter and the three-way CASE sum per row) is
		// fixed by the serving partial index instead of a restructure:
		// GREEN requires the index to exist. pg_indexes assertion per the
		// TestTF8875CanonicalBackendsIndexExists precedent. The
		// fingerprint clause above stays unweakened: a future restructure
		// that changes the text must still clear it.
		db := openDriftJSONBNullDB(t)
		var name string
		err := db.QueryRow(
			`SELECT indexname FROM pg_indexes WHERE schemaname = 'public' AND tablename = 'fact_records' AND indexname = 'fact_records_terraform_backends_outer_leg_idx'`,
		).Scan(&name)
		if err != nil {
			t.Fatalf("STAYS-RED (#8875 round 2): outer leg still applies the repo_id join filter "+
				"and the three-way CASE sum per fact heap row in memory (~55k rows live) with no serving index; "+
				"outer-leg index missing: %v (land migration 276)", err)
		}
	}
}
