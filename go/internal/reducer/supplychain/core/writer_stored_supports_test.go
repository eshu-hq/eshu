// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
)

// legacyStoredFindingsQueryFrozen is the stored read the writer ran before
// #10880 PR-C1 b moved it to the supports table: the rows of the generation being
// written and of the scope's previous generation, read from fact_records with the
// row's own fencing token. It is kept, frozen and digest-pinned, as the oracle the
// supports read is compared to (the live equality proof runs both on one seed).
const legacyStoredFindingsQueryFrozen = `
SELECT fact_id,
       generation_id,
       stable_fact_key,
       COALESCE(payload #>> '{provenance,content_digest}', ''),
       COALESCE(payload ->> 'cve_id', ''),
       COALESCE(payload ->> 'advisory_id', ''),
       fencing_token,
       COALESCE(payload #>> '{provenance,advisory_group_members}', '[]'),
       COALESCE(NULLIF(payload ->> 'suppression_state', ''), 'active'),
       COALESCE(payload #>> '{evidence_fact_ids}', '[]')
FROM fact_records
WHERE fact_kind = 'reducer_supply_chain_impact_finding'
  AND scope_id = $1
  AND is_tombstone = FALSE
  AND generation_id IN ($2, (
    SELECT generation.generation_id
    FROM scope_generations AS generation
    WHERE generation.scope_id = $1
      AND generation.generation_id <> $2
      AND generation.status IN ('active', 'superseded')
    ORDER BY generation.activated_at DESC NULLS LAST, generation.ingested_at DESC
    LIMIT 1
))
`

// legacyStoredFindingsQueryDigest is the sha256 of the query at origin/main
// f1f45ddad3, before C1 b. The frozen text above must still hash to it.
const legacyStoredFindingsQueryDigest = "00d9ad56e624ab3f780d8da4c4af8e6f72906ed47c00530ef83f4bc06a91622e"

func TestLegacyStoredFindingsQueryStaysFrozen(t *testing.T) {
	t.Parallel()

	sum := sha256.Sum256([]byte(legacyStoredFindingsQueryFrozen))
	if got := hex.EncodeToString(sum[:]); got != legacyStoredFindingsQueryDigest {
		t.Fatalf("the frozen legacy stored read hashes to %s, want %s: the oracle was edited", got, legacyStoredFindingsQueryDigest)
	}
}

// legacyPayloadExpressions returns the payload-derived select lines of the
// frozen legacy read, by the column they feed.
func legacyPayloadExpressions(t *testing.T) map[string]string {
	t.Helper()
	out := map[string]string{}
	for name, needle := range map[string]string{
		"digest":     "content_digest",
		"cve":        "'cve_id'",
		"advisory":   "'advisory_id'",
		"members":    "advisory_group_members",
		"suppressed": "suppression_state",
		"evidence":   "evidence_fact_ids",
	} {
		for _, line := range strings.Split(legacyStoredFindingsQueryFrozen, "\n") {
			if strings.Contains(line, needle) {
				out[name] = strings.TrimSuffix(strings.TrimSpace(line), ",")
			}
		}
		if out[name] == "" {
			t.Fatalf("the frozen legacy read has no %s expression", name)
		}
	}
	return out
}

// TestStoredFindingsSupportsQueryReadsSupportsWithTheFactKeyTwin pins the shape
// of the C1 b read: the supports table is the row source, the stable key falls
// back to the fact's own key through a primary-key join, a row with neither key
// reads as an empty key, the fencing token is the constant 0 (the per-row guard is
// redundant with whole-pass admission), and the generation window is the shared
// previous-generation subquery.
func TestStoredFindingsSupportsQueryReadsSupportsWithTheFactKeyTwin(t *testing.T) {
	t.Parallel()

	q := listStoredSupplyChainImpactFindingsQuery
	for name, want := range map[string]string{
		"supports row source":            "FROM supply_chain_impact_finding_supports AS s",
		"twin joined by primary key":     "LEFT JOIN fact_records AS twin ON twin.fact_id = s.fact_id",
		"key with twin fallback":         "COALESCE(s.stable_fact_key, twin.stable_fact_key, '')",
		"token 0":                        "0::bigint",
		"scope predicate":                "s.scope_id = $1",
		"generation window":              "s.generation_id IN ($2, " + priorGenerationSupplyChainImpactSQL + ")",
		"no kind filter on the supports": "",
	} {
		if want != "" && !strings.Contains(q, want) {
			t.Errorf("the stored supports read lacks %s: %q", name, want)
		}
	}
	if strings.Contains(q, "FROM fact_records\n") || strings.Contains(q, "is_tombstone") || strings.Contains(q, "fencing_token") {
		t.Fatalf("the stored supports read still reads fact rows, tombstones or fencing tokens:\n%s", q)
	}
	// The twin is joined only by its primary key: no other fact_records predicate.
	if got := strings.Count(q, "fact_records"); got != 1 {
		t.Fatalf("the stored supports read names fact_records %d times, want exactly the one twin join", got)
	}
}

// TestStoredFindingsSupportsQueryDerivesFromTheLegacyExpressions pins that every
// payload-derived column is the legacy expression with the supports payload
// substituted: the digest, the member set, the suppression state and the
// evidence ids are read from the support's own payload copy (the fact payload
// verbatim), by the same text the fact read used.
func TestStoredFindingsSupportsQueryDerivesFromTheLegacyExpressions(t *testing.T) {
	t.Parallel()

	q := listStoredSupplyChainImpactFindingsQuery
	expr := legacyPayloadExpressions(t)
	for _, name := range []string{"digest", "members", "suppressed", "evidence"} {
		want := strings.Replace(expr[name], "payload", "s.payload", 1)
		if !strings.Contains(q, want) {
			t.Errorf("the supports read does not carry the legacy %s expression over s.payload: want %q", name, want)
		}
	}
	// cve_id and advisory_id are the support's derived columns, defined as the
	// same COALESCE over the same payload keys (supplyChainImpactSupportColumns).
	for _, column := range []string{"s.cve_id", "s.advisory_id"} {
		if !strings.Contains(q, column) {
			t.Errorf("the supports read does not select %s", column)
		}
	}
	// Same column count and order as the legacy read, so the one scan serves both.
	if got, want := selectedColumnCount(q), selectedColumnCount(legacyStoredFindingsQueryFrozen); got != want {
		t.Fatalf("the supports read selects %d columns, the legacy read %d", got, want)
	}
}

// selectedColumnCount counts the top-level select-list lines between SELECT and
// FROM: one per line in both queries.
func selectedColumnCount(query string) int {
	_, rest, _ := strings.Cut(query, "SELECT")
	list, _, _ := strings.Cut(rest, "\nFROM")
	return strings.Count(list, ",\n") + 1
}

// storedKeyRecorderTx is a SupplyChainImpactTx that serves canned stored rows and
// records the missing-key count the read reports.
type storedKeyRecorderTx struct {
	fakeImpactTx
	missing int64
	calls   int
}

func (tx *storedKeyRecorderTx) RecordStoredKeyMissing(_ context.Context, rows int64) {
	tx.calls++
	tx.missing += rows
}

// TestReadStoredFindingsTreatsAMissingKeyAsChangedAndCountsIt proves the "both
// keys missing" rule: a stored row whose key is empty (NULL on the support and no
// twin) gets a key no derived row can match, so the pass treats it as changed;
// each such row is counted once, and a keyed row is untouched.
func TestReadStoredFindingsTreatsAMissingKeyAsChangedAndCountsIt(t *testing.T) {
	t.Parallel()

	state := &fakeImpactTxState{stored: []fakeStoredFinding{
		{factID: "fact:keyed", generationID: "gen-1", stableKey: "key-1", digest: "d1", cveID: "CVE-1"},
		{factID: "fact:nokey-a", generationID: "gen-1", stableKey: "", digest: "d2", cveID: "CVE-2"},
		{factID: "fact:nokey-b", generationID: "gen-1", stableKey: "", digest: "d3", cveID: "CVE-3"},
	}}
	tx := &storedKeyRecorderTx{fakeImpactTx: fakeImpactTx{state: state}}

	stored, err := readStoredSupplyChainImpactFindings(context.Background(), tx, "scope-1", "gen-1")
	if err != nil {
		t.Fatalf("readStoredSupplyChainImpactFindings() error = %v", err)
	}
	if len(stored) != 3 {
		t.Fatalf("stored rows = %d, want 3", len(stored))
	}
	if stored[0].stableKey != "key-1" {
		t.Fatalf("keyed row key = %q, want it kept", stored[0].stableKey)
	}
	seen := map[string]bool{}
	for _, row := range stored[1:] {
		if row.stableKey == "" || seen[row.stableKey] {
			t.Fatalf("a keyless row got key %q: it must be non-empty and unique so no two keyless rows (and no derived row) share a baseline", row.stableKey)
		}
		seen[row.stableKey] = true
		if row.fencingToken != 0 {
			t.Fatalf("stored fencing token = %d, want 0", row.fencingToken)
		}
	}
	if tx.missing != 2 || tx.calls != 1 {
		t.Fatalf("missing-key count = %d in %d calls, want 2 in 1", tx.missing, tx.calls)
	}

	// A keyless stored row is a changed finding: the derived row with the same
	// stable key finds no baseline, so its CVE is named changed.
	derived := []changedRow{{factID: "fact:new", stableKey: "key-2", digest: "d2", cveID: "CVE-2"}}
	changed := changedCVEIDs(derived, stored, "gen-1", 0, false)
	if len(changed) != 1 || changed[0] != "CVE-2" {
		t.Fatalf("changedCVEIDs with a keyless stored baseline = %v, want [CVE-2]", changed)
	}

	// A transaction that does not implement the recorder reads the same rows.
	plain := fakeImpactTx{state: state}
	if _, err := readStoredSupplyChainImpactFindings(context.Background(), plain, "scope-1", "gen-1"); err != nil {
		t.Fatalf("read without a recorder error = %v", err)
	}
}
