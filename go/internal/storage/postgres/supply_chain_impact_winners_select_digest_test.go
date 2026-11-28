// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres_test

import (
	"fmt"
	"regexp"
	"strings"
	"testing"

	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// servedDigestExcludedKey is the digest-excluded top-level payload key the
// stored winner payload serves (findings_decode.go), so the winner select keeps
// it. Under the #11598 Option C arbiter amendment to #10880 Decision 4, neither
// evidence_fact_ids nor nested provenance.selected_epss_score_date triggers
// dirty-log capture on content-identical rescans (accepted staleness on
// canonical_winners until the next semantic change; supports.payload stays
// fresh for Explain and AuditWinners masks both fields). Every other excluded
// key is volatile: the select strips it from the stored payload (decision 3),
// so neither a per-key read nor a wholesale copy of it may reach the winners
// table.
const servedDigestExcludedKey = "evidence_fact_ids"

// winnerPayloadStripExpr renders the payload normalization the winner
// select must apply before the suppression bake: the volatile
// digest-excluded keys dropped, plus the nested provenance digest.
func winnerPayloadStripExpr() string {
	expr := "(source.payload"
	for _, key := range supplychaincore.DigestExcludedPayloadKeys() {
		if key != servedDigestExcludedKey {
			expr += " - '" + key + "'"
		}
	}
	return expr + ") #- '{provenance,content_digest}'"
}

// winnerSelectDigestViolations reports every way a winners select lets
// a digest-excluded payload key move its output: a per-key read of an
// excluded key, a wholesale copy of source.payload (an occurrence not
// followed by a key read or by the strip), or a missing strip.
func winnerSelectDigestViolations(sql string) []string {
	var violations []string
	excluded := map[string]bool{}
	for _, key := range supplychaincore.DigestExcludedPayloadKeys() {
		excluded[key] = true
	}
	read := regexp.MustCompile(`payload\s*(?:->>|->)\s*'([a-z_]+)'|payload\s*#>>\s*'\{([a-z_]+)`)
	for _, m := range read.FindAllStringSubmatch(sql, -1) {
		key := m[1]
		if key == "" {
			key = m[2]
		}
		if excluded[key] {
			violations = append(violations, fmt.Sprintf("reads payload key %q, which the content digest excludes", key))
		}
	}
	keyReadOrStrip := regexp.MustCompile(`^\s*(?:->|#>>|- ')`)
	for _, loc := range regexp.MustCompile(`\bsource\.payload`).FindAllStringIndex(sql, -1) {
		if keyReadOrStrip.MatchString(sql[loc[1]:]) {
			continue
		}
		violations = append(violations, fmt.Sprintf("wholesale copy of source.payload at byte %d carries digest-excluded keys into the stored winner payload", loc[0]))
	}
	if !strings.Contains(sql, winnerPayloadStripExpr()) {
		violations = append(violations, "the stored payload projection never applies the volatile-key strip "+winnerPayloadStripExpr())
	}
	return violations
}

// TestWinnerSelectReadsNoDigestExcludedPayloadField is the select side of the
// #9805 capture-filter soundness argument. The writer skips the capture of a
// rewrite whose content digest is unchanged, so the winners select must not
// read a payload key the digest leaves out. The digest side (every other key
// moves the digest) is TestContentDigestCoversEveryPayloadFieldExceptTheExcludedOnes.
//
// Since #10880 slice 1 the select also projects the winner payload itself.
// A wholesale `source.payload` copy evades the per-key read check while
// carrying every excluded key into the stored row, so the guard also rejects
// any wholesale copy and requires the volatile-key strip (#10880 arbiter
// ruling 2026-11-21, decision 3). The seeded arms prove both checks bite.
func TestWinnerSelectReadsNoDigestExcludedPayloadField(t *testing.T) {
	t.Parallel()

	shipped := postgres.WinnerSelectSQLForTest
	read := regexp.MustCompile(`payload\s*(?:->>|->)\s*'([a-z_]+)'|payload\s*#>>\s*'\{([a-z_]+)`)
	matches := read.FindAllStringSubmatch(shipped, -1)
	// Single-detoast (#11120) moved every text-field read into the record:
	// the only payload reads left are the documented exceptions — the shared
	// severity inputs, the three jsonb arrays, and the operator override
	// reads. Any new read fails here with its key.
	gotKeys := map[string]int{}
	for _, m := range matches {
		key := m[1]
		if key == "" {
			key = m[2]
		}
		gotKeys[key]++
	}
	wantKeys := map[string]bool{
		// cvss.BucketSQL("source.payload"): one score read per threshold
		// arm plus the label read.
		"cvss_score": true, "provenance": true,
		// The three jsonb arrays keep `->` (JSON-null semantics).
		"service_ids": true, "workload_ids": true, "environments": true,
		// The winner-scale operator override reads.
		"suppression": true,
	}
	for key := range gotKeys {
		if !wantKeys[key] {
			t.Errorf("unexpected payload read %q in the winners select: single-detoast allows only %v", key, wantKeys)
		}
	}
	for key := range wantKeys {
		if gotKeys[key] == 0 {
			t.Errorf("documented payload read %q is gone from the winners select (the pattern is stale)", key)
		}
	}
	for _, violation := range winnerSelectDigestViolations(shipped) {
		t.Errorf("the winners select %s: a rewrite that changes it would not be captured", violation)
	}

	// Seeded RED: removing the strip recreates the bare wholesale copy the
	// per-key check alone passed vacuously.
	bare := strings.ReplaceAll(shipped, winnerPayloadStripExpr(), "source.payload")
	if got := winnerSelectDigestViolations(bare); len(got) == 0 {
		t.Error("seeded violation undetected: a select with the strip removed reports no violation")
	}
	// Seeded RED: reading one excluded key by name is caught too.
	leaked := strings.Replace(shipped, "source.payload->>'cvss_score'", "source.payload->>'intent_id'", 1)
	if leaked == shipped {
		t.Fatal("seeded read replacement found no cvss_score read to mutate (the pattern is stale)")
	}
	if got := winnerSelectDigestViolations(leaked); len(got) == 0 {
		t.Error("seeded violation undetected: a select reading intent_id reports no violation")
	}
}

// TestWinnersAuditDiffMasksEvidenceAndScoreDate pins the #11598 Option C audit
// alignment: winnersAuditDiffSelect must mask evidence_fact_ids and nested
// provenance.selected_epss_score_date on both sides of the ROW comparison so
// AuditWinners does not loop repairs on accepted staleness, while keeping
// SupplyChainImpactWinnersContractVersion at 7 and leaving semantic fields
// compared verbatim.
func TestWinnersAuditDiffMasksEvidenceAndScoreDate(t *testing.T) {
	t.Parallel()

	// The version gate forces this re-examination on every contract bump:
	// the ruled #11691 leniency changes which rows the resweep writes,
	// never the payload shape the mask strips, and the v6 healing proof
	// (TestSupplyChainImpactWinnersUnlessCurrentHealsStoredOnlyKeysLive)
	// runs AuditWinners over the healed table with zero repairs. The v7
	// lane-A re-rank re-examined here changes which copy per key wins (the
	// TOP-1 prune) and which identifiers a tick visits (the budget
	// redefinition), never the payload shape the mask strips: the TOP-1
	// winner select ends in the shared final projection verbatim
	// (TestWinnersTop1SharesStreamedFinalSelect pins it), so the masked
	// keys and the verbatim semantic columns are unchanged.
	if postgres.SupplyChainImpactWinnersContractVersion != 7 {
		t.Fatalf("SupplyChainImpactWinnersContractVersion = %d, want 7 (re-examine the audit mask on every winners contract bump)", postgres.SupplyChainImpactWinnersContractVersion)
	}
	check := func(sql string) []string {
		var errs []string
		for _, alias := range []string{"n", "w"} {
			want := "((" + alias + ".payload - 'evidence_fact_ids') #- '{provenance,selected_epss_score_date}')"
			if !strings.Contains(sql, want) {
				errs = append(errs, "missing audit payload mask "+want)
			}
			if strings.Contains(sql, ", "+alias+".payload,") {
				errs = append(errs, "unmasked "+alias+".payload still present in audit ROW compare")
			}
		}
		for _, required := range []string{"n.priority_score", "w.priority_score", "n.winner_fact_id", "w.winner_fact_id", "n.suppression_state", "w.suppression_state"} {
			if !strings.Contains(sql, required) {
				errs = append(errs, "semantic column missing from audit diff: "+required)
			}
		}
		return errs
	}

	for _, tc := range []struct {
		name string
		sql  string
	}{
		{name: "full", sql: postgres.WinnersAuditDiffSelectForTest},
		{name: "range_first", sql: postgres.WinnersAuditRangeFirstSQLForTest},
		{name: "range_next", sql: postgres.WinnersAuditRangeNextSQLForTest},
		{name: "single_scan_first", sql: postgres.WinnersAuditSingleScanFirstSQLForTest},
		{name: "single_scan_next", sql: postgres.WinnersAuditSingleScanNextSQLForTest},
	} {
		if errs := check(tc.sql); len(errs) != 0 {
			t.Fatalf("%s audit diff mask check failed: %v\nSQL:\n%s", tc.name, errs, tc.sql)
		}
	}
	shipped := postgres.WinnersAuditDiffSelectForTest
	// Seeded RED: removing the mask on either side (n.payload or w.payload) must be caught.
	unmaskedN := strings.ReplaceAll(shipped, "((n.payload - 'evidence_fact_ids') #- '{provenance,selected_epss_score_date}')", "n.payload")
	if errs := check(unmaskedN); len(errs) == 0 {
		t.Fatal("seeded violation undetected: unmasked n.payload passed audit mask guard")
	}
	unmaskedW := strings.ReplaceAll(shipped, "((w.payload - 'evidence_fact_ids') #- '{provenance,selected_epss_score_date}')", "w.payload")
	if errs := check(unmaskedW); len(errs) == 0 {
		t.Fatal("seeded violation undetected: unmasked w.payload passed audit mask guard")
	}
}
