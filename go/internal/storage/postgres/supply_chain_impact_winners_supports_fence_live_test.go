// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres_test

import (
	"sort"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// TestSupplyChainImpactWinnersResweepSupportsDirtyFenceScannerLive is the Q7 proof of the C1
// pre-open ruling (a): a writer that does not carry the supports maintainer (an old one during a
// roll) writes a finding fact, which marks its scope dirty in the old-writer fence. A scanner
// classification that retracts that finding lands while the scope is dirty, when the finding has no
// support yet. After the backfill drains the scope and one keyed tick runs, the stored winners must
// equal the winners select over the finding facts (EXCEPT ALL both ways is empty).
//
// Two variants. "with the old writer's mark": the old writer also stages its own winner mark, as
// the real one does, and writes a second plain finding that the mark alone must carry into the
// winners (so the equality is not vacuous: a tick that consumed the mark against stale supports
// would leave that finding out). "scanner row alone": no old-writer mark; what, if anything, capture
// 353 stages is observed. The count and the tuples capture 353 staged are logged in both and are an
// observation; the end-state equality is the assertion. The staged rows, if any, must name the
// retracted finding's identifiers.
func TestSupplyChainImpactWinnersResweepSupportsDirtyFenceScannerLive(t *testing.T) {
	for _, withMark := range []bool{true, false} {
		name := "scanner row alone"
		if withMark {
			name = "with the old writer's mark"
		}
		t.Run(name, func(t *testing.T) { runDirtyFenceScanner(t, withMark) })
	}
}

func dirtyFenceTuples(h *keyedHarness, query string) []string {
	h.t.Helper()
	rows, err := h.db.QueryContext(h.ctx, query)
	if err != nil {
		h.t.Fatalf("read staged rows: %v", err)
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var source, pkg, repo, digest, c0, advisory string
		if err := rows.Scan(&source, &pkg, &repo, &digest, &c0, &advisory); err != nil {
			h.t.Fatalf("scan staged row: %v", err)
		}
		out = append(out, strings.Join([]string{source, pkg, repo, digest, c0, advisory}, "|"))
	}
	sort.Strings(out)
	return out
}

func runDirtyFenceScanner(t *testing.T, withMark bool) {
	h := newKeyedHarness(t, "#10880 C1 dirty fence scanner proof")
	scopeA, _ := keyedSeedSettled(h)
	generation := scopeA + "/g1"
	h.dropForeignKeysOf("scanner_image_result_fact_classifications")

	// The scanner capture in effect reads the supports table (migration 353), not the facts.
	var definition string
	if err := h.db.QueryRowContext(h.ctx, `SELECT pg_get_functiondef('supply_chain_impact_winners_capture_scanner'::regproc)`).Scan(&definition); err != nil {
		t.Fatalf("read the scanner capture function: %v", err)
	}
	if !strings.Contains(definition, "supply_chain_impact_finding_supports") || strings.Contains(definition, "fact_records") {
		t.Fatalf("the scanner capture in effect does not read the supports table only (migration 353 not applied?):\n%s", definition)
	}

	insert := func(fact, finding, cve, pkg string) {
		h.exec(`INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, schema_version,
  source_system, source_fact_key, observed_at, ingested_at, is_tombstone, payload)
VALUES ($1, $2, $3, $4, $1, '1.0.0', 'synthetic', $1, now(), now(), false,
  jsonb_build_object('finding_id', $5::text, 'cve_id', $6::text, 'package_id', $7::text,
    'repository_id', 'repo-9805', 'subject_digest', '', 'advisory_id', '', 'impact_status', 'affected_exact',
    'ecosystem', 'npm', 'observed_version', '1.0.0', 'priority_score', 50,
    'service_ids', '[]'::jsonb, 'workload_ids', '[]'::jsonb, 'environments', '[]'::jsonb))`,
			fact, scopeA, generation, facts.ReducerSupplyChainImpactFindingFactKind, finding, cve, pkg)
	}
	mark := func(cve, pkg string) {
		h.exec(`INSERT INTO supply_chain_impact_winners_dirty_staging (kind, source, package_id, repository_id, subject_digest, c0, advisory_id)
VALUES ('fact', 'fact_upsert', $2, 'repo-9805', '', $1, '')`, cve, pkg)
	}

	// A write without the supports writer setting: the old writer. The AFTER INSERT trigger marks
	// the scope dirty.
	const retractedFact = "f-old-writer"
	insert(retractedFact, "finding:old-writer", "CVE-2026-9910", "pkg:npm/old-writer")
	if withMark {
		insert("f-old-writer-plain", "finding:old-writer-plain", "CVE-2026-9911", "pkg:npm/old-writer-plain")
	}
	if dirty := h.count(`SELECT count(*) FROM supply_chain_impact_finding_supports_dirty WHERE scope_id = $1`, scopeA); dirty != 1 {
		t.Fatalf("a write without the writer setting left %d dirty fence rows, want 1: the old-writer simulation did not take", dirty)
	}
	if withMark {
		mark("CVE-2026-9910", "pkg:npm/old-writer")
		mark("CVE-2026-9911", "pkg:npm/old-writer-plain")
	}

	// The scanner association lands while the scope is dirty and the finding has no support.
	h.exec(`INSERT INTO scanner_image_result_fact_classifications (finding_fact_id, source_class, lifecycle)
VALUES ($1, 'static', 'retracted')`, retractedFact)
	staged := dirtyFenceTuples(h, `SELECT source, package_id, repository_id, subject_digest, c0, advisory_id
FROM supply_chain_impact_winners_dirty_staging WHERE source = 'scanner'`)
	t.Logf("capture 353 staged %d scanner rows with the finding unsupported (mark present: %v): %v", len(staged), withMark, staged)
	want := "scanner|pkg:npm/old-writer|repo-9805||CVE-2026-9910|"
	for _, row := range staged {
		if row != want {
			t.Errorf("capture 353 staged %q, want only the retracted finding's identifiers %q", row, want)
		}
	}
	allStaged := dirtyFenceTuples(h, `SELECT source, package_id, repository_id, subject_digest, c0, advisory_id
FROM supply_chain_impact_winners_dirty_staging`)
	t.Logf("all staged rows before the drain (%d): %v", len(allStaged), allStaged)

	// The backfill drains the dirty scope: the old writer's findings get their supports.
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
	if n := h.count(`SELECT count(*) FROM supply_chain_impact_finding_supports WHERE fact_id = $1`, retractedFact); n != 1 {
		t.Fatalf("after the drain the old writer's finding has %d supports, want 1", n)
	}
	afterDrain := dirtyFenceTuples(h, `SELECT source, package_id, repository_id, subject_digest, c0, advisory_id
FROM supply_chain_impact_winners_dirty_staging`)
	t.Logf("staged rows after the drain (%d, the drain stages no winner mark): %v", len(afterDrain), afterDrain)

	// One maintainer tick consumes whatever is staged against the drained supports.
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
	if withMark {
		if n := h.count(`SELECT count(*) FROM supply_chain_impact_canonical_winners WHERE cve_id = 'CVE-2026-9911'`); n != 1 {
			t.Errorf("the old writer's plain finding is %d stored winners, want 1: its mark did not reach the winners", n)
		}
	}
	t.Logf("end state equal to the facts select; variant mark=%v", withMark)
}
