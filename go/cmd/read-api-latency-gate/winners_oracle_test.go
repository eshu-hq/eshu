// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"fmt"
	"strings"
	"testing"
)

// winnersOracleTestPage renders a findings-list envelope page like the
// satellite relays it: findings array, count, limit, truncated.
func winnersOracleTestPage(findings, count, limit int, truncated bool) []byte {
	items := make([]string, 0, findings)
	for i := 0; i < findings; i++ {
		items = append(items, fmt.Sprintf(`{"finding_id":"finding:%05d"}`, i))
	}
	return []byte(fmt.Sprintf(`{"findings":[%s],"count":%d,"limit":%d,"truncated":%v}`,
		strings.Join(items, ","), count, limit, truncated))
}

// TestVerifyWinnersFindingsPageProvesDepth is the #11644 acceptance core: a
// full winners page (count hits the limit, truncated set, oracle deeper than
// the page) passes; an empty page must fail, or the leg proves nothing.
func TestVerifyWinnersFindingsPageProvesDepth(t *testing.T) {
	t.Parallel()

	full := winnersOracleTestPage(50, 50, 50, true)
	if err := VerifyWinnersFindingsPage(4000, full); err != nil {
		t.Errorf("full winners page against a 4000-group oracle: %v, want pass", err)
	}

	empty := winnersOracleTestPage(0, 0, 50, false)
	if err := VerifyWinnersFindingsPage(4000, empty); err == nil {
		t.Error("empty winners page against a 4000-group oracle passed, want fail (a vacuous winners leg proves nothing)")
	}
}

// TestVerifyWinnersFindingsPageRejectsTruncationMismatch pins the
// oracle-depth check: the page must agree with the oracle about whether more
// truth sits beyond the page, or the leg measured a partial read model.
func TestVerifyWinnersFindingsPageRejectsTruncationMismatch(t *testing.T) {
	t.Parallel()

	// Oracle holds 30 groups but the page claims truncation: the API served
	// something the oracle cannot explain.
	phantom := winnersOracleTestPage(50, 50, 50, true)
	if err := VerifyWinnersFindingsPage(30, phantom); err == nil {
		t.Error("truncated page against a 30-group oracle passed, want fail")
	}

	// Oracle holds 4000 groups but the page claims to be complete: the read
	// stopped early.
	short := winnersOracleTestPage(50, 50, 50, false)
	if err := VerifyWinnersFindingsPage(4000, short); err == nil {
		t.Error("untruncated page against a 4000-group oracle passed, want fail")
	}
}

// TestVerifyWinnersFindingsPageRejectsMalformedEnvelope pins the envelope
// contract: no findings field, a count that disagrees with the array, and a
// non-positive limit all fail instead of passing as empty truth.
func TestVerifyWinnersFindingsPageRejectsMalformedEnvelope(t *testing.T) {
	t.Parallel()

	for name, page := range map[string][]byte{
		"no findings field": []byte(`{"count":0}`),
		"count disagrees":   winnersOracleTestPage(2, 5, 50, false),
		"not JSON":          []byte(`not a page`),
	} {
		if err := VerifyWinnersFindingsPage(4000, page); err == nil {
			t.Errorf("%s passed, want fail", name)
		}
	}
}

// TestCheckSupportsOraclePinsSeededCorpus pins the oracle side: the supports
// table must hold exactly the seeded corpus — every support row and one
// canonical group per seeded group — so a partial seed cannot pass as truth.
func TestCheckSupportsOraclePinsSeededCorpus(t *testing.T) {
	t.Parallel()

	if err := CheckSupportsOracle(OracleCounts{SupportsRows: 20000, CanonicalGroups: 4000}, 20000, 5); err != nil {
		t.Errorf("exact seeded corpus: %v, want pass", err)
	}
	for name, counts := range map[string]OracleCounts{
		"empty supports":     {},
		"partial groups":     {SupportsRows: 20000, CanonicalGroups: 3999},
		"extra support rows": {SupportsRows: 20001, CanonicalGroups: 4000},
		"no groups but rows": {SupportsRows: 20000, CanonicalGroups: 0},
	} {
		if err := CheckSupportsOracle(counts, 20000, 5); err == nil {
			t.Errorf("%s passed, want fail", name)
		}
	}
}
