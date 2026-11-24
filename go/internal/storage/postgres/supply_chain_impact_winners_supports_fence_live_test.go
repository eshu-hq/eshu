// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres_test

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// TestSupplyChainImpactWinnersResweepSupportsDirtyFenceScannerLive is the Q7 proof of the C1
// pre-open ruling (a): a writer that does not carry the supports maintainer (an old one during a
// roll) writes a finding fact, which marks its scope dirty in the old-writer fence, and stages its
// own winner mark. A scanner classification that retracts that finding lands while the scope is
// dirty, when the finding has no support yet, so capture 353 stages nothing and the writer's mark
// is what covers it. After the backfill drains the scope and one keyed tick, the stored winners
// equal the winners select over the finding facts (EXCEPT ALL both ways is empty): the retracted
// finding is not a winner and the settled winners are untouched.
func TestSupplyChainImpactWinnersResweepSupportsDirtyFenceScannerLive(t *testing.T) {
	h := newKeyedHarness(t, "#10880 C1 dirty fence scanner proof")
	scopeA, _ := keyedSeedSettled(h)
	generation := scopeA + "/g1"
	h.dropForeignKeysOf("scanner_image_result_fact_classifications")

	// A write without the supports writer setting: the old writer. The AFTER INSERT trigger marks
	// the scope dirty, and the writer stages its own winner mark with the fact.
	const oldFact = "f-old-writer"
	h.exec(`INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, schema_version,
  source_system, source_fact_key, observed_at, ingested_at, is_tombstone, payload)
VALUES ($1, $2, $3, $4, $1, '1.0.0', 'synthetic', $1, now(), now(), false,
  jsonb_build_object('finding_id', 'finding:old-writer', 'cve_id', 'CVE-2026-9910', 'package_id', 'pkg:npm/old-writer',
    'repository_id', 'repo-9805', 'subject_digest', '', 'advisory_id', '', 'impact_status', 'affected_exact',
    'ecosystem', 'npm', 'observed_version', '1.0.0', 'priority_score', 50,
    'service_ids', '[]'::jsonb, 'workload_ids', '[]'::jsonb, 'environments', '[]'::jsonb))`,
		oldFact, scopeA, generation, facts.ReducerSupplyChainImpactFindingFactKind)
	if dirty := h.count(`SELECT count(*) FROM supply_chain_impact_finding_supports_dirty WHERE scope_id = $1`, scopeA); dirty != 1 {
		t.Fatalf("a write without the writer setting left %d dirty fence rows, want 1: the old-writer simulation did not take", dirty)
	}
	h.exec(`INSERT INTO supply_chain_impact_winners_dirty_staging (kind, source, package_id, repository_id, subject_digest, c0, advisory_id)
VALUES ('fact', 'fact_upsert', 'pkg:npm/old-writer', 'repo-9805', '', 'CVE-2026-9910', '')`)

	// The scanner association lands while the scope is dirty and the finding has no support.
	h.exec(`INSERT INTO scanner_image_result_fact_classifications (finding_fact_id, source_class, lifecycle)
VALUES ($1, 'static', 'retracted')`, oldFact)
	if staged := h.count(`SELECT count(*) FROM supply_chain_impact_winners_dirty_staging WHERE source = 'scanner'`); staged != 0 {
		t.Fatalf("capture 353 staged %d rows for a finding with no support yet: the writer's mark is not what covers it, the premise of the ruling is wrong", staged)
	}

	// The backfill drains the dirty scope: the finding gets its support.
	backfill := postgres.SupplyChainImpactSupportsBackfill{DB: postgres.SQLDB{DB: h.db}}
	for step := 0; ; step++ {
		report, err := backfill.RunStep(h.ctx)
		if err != nil {
			t.Fatalf("backfill step: %v", err)
		}
		if report.DirtyScopes == 0 && report.Complete {
			break
		}
		if step > 300 {
			t.Fatal("the backfill did not drain the dirty scope")
		}
	}
	if n := h.count(`SELECT count(*) FROM supply_chain_impact_finding_supports WHERE fact_id = $1`, oldFact); n != 1 {
		t.Fatalf("after the drain the old writer's finding has %d supports, want 1", n)
	}

	// One maintainer tick consumes the writer's mark against the drained supports.
	h.keyed()

	fromFacts, _ := postgres.WinnersProofSelects()
	columns := "canonical_key, " + strings.Join(postgres.WinnersProofDataColumns(), ", ")
	table := `SELECT ` + columns + ` FROM supply_chain_impact_canonical_winners`
	onFacts := `SELECT ` + columns + ` FROM (` + fromFacts + `) AS f`
	if n := h.count(`SELECT count(*) FROM (` + onFacts + `) AS q`); n == 0 {
		t.Fatal("the facts select returned no winners: the seed is empty")
	}
	if n := h.count(`SELECT count(*) FROM ((` + onFacts + `) EXCEPT ALL (` + table + `)) AS d`); n != 0 {
		t.Errorf("facts select EXCEPT ALL the stored winners = %d rows, want 0", n)
	}
	if n := h.count(`SELECT count(*) FROM ((` + table + `) EXCEPT ALL (` + onFacts + `)) AS d`); n != 0 {
		t.Errorf("stored winners EXCEPT ALL the facts select = %d rows, want 0", n)
	}
	if n := h.count(`SELECT count(*) FROM supply_chain_impact_canonical_winners WHERE cve_id = 'CVE-2026-9910'`); n != 0 {
		t.Errorf("the retracted old-writer finding is a stored winner (%d rows), want none", n)
	}
}
