// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build integration

package postgres

import (
	"context"
	"fmt"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// TestListActiveSupplyChainImpactFactsCapNeverTruncatesCoreEvidenceLive is
// the #5466 round-8 review F-3 fix proof, run against a REAL Postgres
// instance (not the hermetic fake queryer, which cannot catch a genuine SQL
// ORDER BY/keyset-cursor bug): a filter that matches BOTH one
// vulnerability.affected_package row AND far more vulnerability.suppression
// rows than maxSupplyChainImpactActiveEvidenceRowsPerCall must still return
// the affected_package row -- core evidence for a finding must never be
// crowded out of the result by suppression noise sharing the same filter
// value, no matter how many suppression rows exist or what order Postgres
// would return them in under a naive fact_id-only sort. This also proves
// the compound keyset cursor (fact_kind = 'vulnerability.suppression',
// fact_id) pagination is genuinely correct across MULTIPLE pages spanning
// the non-suppression/suppression boundary: a bug there would silently
// skip or duplicate rows rather than erroring. 1,200 suppression rows (3
// pages of the 500-row listFactsByKindPageSize) against a 600-row cap means
// the cap crosses mid-stream, after the second page -- not on the very
// first page, which would not exercise the cursor's cross-page behavior.
func TestListActiveSupplyChainImpactFactsCapNeverTruncatesCoreEvidenceLive(t *testing.T) {
	db := newSupplyChainImpactScopeLiveTestDB(t, "eshu_5466_row_cap_never_truncates_core")

	originalCap := maxSupplyChainImpactActiveEvidenceRowsPerCall
	const lowCap = 600
	maxSupplyChainImpactActiveEvidenceRowsPerCall = lowCap
	t.Cleanup(func() { maxSupplyChainImpactActiveEvidenceRowsPerCall = originalCap })

	const cveID = "CVE-2026-5466-F3"
	const affectedPackageFactID = "vuln-affected-package:f3-core"
	const suppressionRowCount = 1200

	ctx := context.Background()
	if _, err := db.ExecContext(ctx, `
INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key, collector_kind, partition_key, observed_at, ingested_at, status, active_generation_id)
VALUES ('vex-scope:f3', 'vex_document', 'vex', 'f3', 'vulnerability_intelligence', 'p0', now(), now(), 'active', 'gen-f3');
INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status)
VALUES ('gen-f3', 'vex-scope:f3', 'poll', now(), now(), 'active');
`); err != nil {
		t.Fatalf("seed f3 ingestion scope: %v", err)
	}

	// The ONE core-evidence row this test proves is never crowded out.
	if _, err := db.ExecContext(ctx, `
INSERT INTO fact_records (
  fact_id, scope_id, generation_id, fact_kind, stable_fact_key,
  schema_version, collector_kind, fencing_token, source_confidence,
  source_system, source_fact_key, observed_at, ingested_at, is_tombstone, payload
) VALUES (
  $1, 'vex-scope:f3', 'gen-f3', 'vulnerability.affected_package',
  'stable-f3-core', '1.0.0', 'vex', 1, 'observed', 'vex', $1,
  now(), now(), FALSE, $2::jsonb
)`, affectedPackageFactID, fmt.Sprintf(`{"cve_id":"%s","package_id":"pkg:npm/f3-core"}`, cveID)); err != nil {
		t.Fatalf("seed affected_package core evidence: %v", err)
	}

	// Bulk-seed suppression rows FAR exceeding the (lowered) cap, all
	// sharing the same cve_id filter value, so under a naive fact_id-only
	// sort a large enough burst of them (whichever fact_ids happen to sort
	// first) could crowd the affected_package row out of a capped result.
	if _, err := db.ExecContext(ctx, `
INSERT INTO fact_records (
  fact_id, scope_id, generation_id, fact_kind, stable_fact_key,
  schema_version, collector_kind, fencing_token, source_confidence,
  source_system, source_fact_key, observed_at, ingested_at, is_tombstone, payload
)
SELECT
  format('vuln-suppression:f3-noise-%06s', n),
  'vex-scope:f3', 'gen-f3', 'vulnerability.suppression',
  format('stable-f3-noise-%06s', n), '1.0.0', 'vex', 1, 'observed', 'vex',
  format('vuln-suppression:f3-noise-%06s', n), now(), now(), FALSE,
  jsonb_build_object(
    'suppression_id', format('SUP-F3-NOISE-%06s', n),
    'source', 'eshu_policy', 'justification', 'not_affected',
    'author', 'security-bot', 'authored_at', '2026-06-20T00:00:00Z',
    'scope', jsonb_build_object('cve_id', $1::text)
  )
FROM generate_series(1, $2::integer) AS n
`, cveID, suppressionRowCount); err != nil {
		t.Fatalf("bulk-seed suppression noise rows: %v", err)
	}

	store := NewFactStore(SQLDB{DB: db})

	loaded, truncated, err := store.ListActiveSupplyChainImpactFacts(ctx, reducer.SupplyChainImpactFactFilter{
		CVEIDs: []string{cveID},
	})
	if err != nil {
		t.Fatalf("ListActiveSupplyChainImpactFacts: %v", err)
	}
	if !truncated {
		t.Fatalf("truncated = false, want true (%d suppression rows against a %d-row cap)", suppressionRowCount, lowCap)
	}

	var (
		foundAffectedPackage bool
		suppressionCount     int
		seenSuppressionIDs   = map[string]bool{}
	)
	for _, envelope := range loaded {
		if envelope.FactID == affectedPackageFactID {
			foundAffectedPackage = true
			continue
		}
		if envelope.FactKind == "vulnerability.suppression" {
			suppressionCount++
			if seenSuppressionIDs[envelope.FactID] {
				t.Fatalf("duplicate suppression FactID %q in result -- compound keyset cursor pagination bug", envelope.FactID)
			}
			seenSuppressionIDs[envelope.FactID] = true
		}
	}
	if !foundAffectedPackage {
		t.Fatalf(
			"affected_package row %q missing from %d loaded rows -- core evidence was crowded out by suppression noise sharing the same cve_id filter (#5466 round-8 review F-3 regression)",
			affectedPackageFactID, len(loaded),
		)
	}
	// Page 1 holds the core row plus 499 suppressions. Page 2 supplies the
	// remaining 101 suppressions up to the exact cap and one sentinel proving
	// more rows exist. The sentinel is not returned.
	wantSuppressionCount := lowCap
	if got := suppressionCount; got != wantSuppressionCount {
		t.Fatalf("suppression rows loaded = %d, want exact cap %d", got, wantSuppressionCount)
	}
	if got, want := len(loaded), 1+wantSuppressionCount; got != want {
		t.Fatalf("total loaded = %d, want %d (1 affected_package + %d suppression)", got, want, wantSuppressionCount)
	}
}
