// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// TestSecurityAlertReconciliationKeyRowsMapsAllSixArms proves every legacy
// six-arm predicate value lands in the sidecar verbatim: repository_id,
// package_id, each cve_ids element plus the scalar cve_id in one 'cve'
// space, each ghsa_ids element plus the scalar advisory_id in one
// 'advisory' space (#10875).
func TestSecurityAlertReconciliationKeyRowsMapsAllSixArms(t *testing.T) {
	t.Parallel()

	rows := securityAlertReconciliationKeyRows([]facts.Envelope{{
		FactID:       "fact-1",
		ScopeID:      "scope",
		GenerationID: "gen",
		FactKind:     "security_alert.repository_alert",
		Payload: map[string]any{
			"repository_id": "  acme/app  ",
			"package_id":    "pkg:npm/left-pad",
			"cve_ids":       []any{"CVE-2024-0001", "CVE-2024-0002"},
			"cve_id":        "CVE-2024-0003",
			"ghsa_ids":      []any{"GHSA-aaaa-bbbb-cccc"},
			"advisory_id":   "GHSA-dddd-eeee-ffff",
		},
	}})
	got := make(map[string]map[string]bool)
	for _, row := range rows {
		if got[row.KeyKind] == nil {
			got[row.KeyKind] = make(map[string]bool)
		}
		got[row.KeyKind][row.KeyValue] = true
	}
	for kind, want := range map[string][]string{
		"repository": {"  acme/app  "},
		"package":    {"pkg:npm/left-pad"},
		"cve":        {"CVE-2024-0001", "CVE-2024-0002", "CVE-2024-0003"},
		"advisory":   {"GHSA-aaaa-bbbb-cccc", "GHSA-dddd-eeee-ffff"},
	} {
		for _, value := range want {
			if !got[kind][value] {
				t.Errorf("missing (%s, %q) in %v", kind, value, rows)
			}
		}
	}
	if len(rows) != 7 {
		t.Fatalf("len(rows) = %d, want 7", len(rows))
	}
}

// TestSecurityAlertReconciliationKeyRowsMirrorExistsSemantics proves the
// extraction mirrors ?| exactly: array string elements become values,
// object keys become values, and any other array/object shape yields nothing
// (#10875). Scalar arms instead coerce numbers and bools the way ->>
// renders them, so the numeric cve_id below lands as its text form.
func TestSecurityAlertReconciliationKeyRowsMirrorExistsSemantics(t *testing.T) {
	t.Parallel()

	rows := securityAlertReconciliationKeyRows([]facts.Envelope{{
		FactID:       "fact-1",
		ScopeID:      "scope",
		GenerationID: "gen",
		FactKind:     "reducer_package_consumption_correlation",
		Payload: map[string]any{
			"cve_ids":    map[string]any{"CVE-2024-0001": map[string]any{}, "other": 1},
			"ghsa_ids":   []any{"GHSA-1", 42, nil, map[string]any{}},
			"cve_id":     42,
			"package_id": []any{"not-a-string"},
		},
	}})
	got := make(map[string]bool)
	for _, row := range rows {
		got[row.KeyKind+"\x00"+row.KeyValue] = true
	}
	for _, want := range []string{"cve\x00CVE-2024-0001", "cve\x00other", "cve\x0042", "advisory\x00GHSA-1"} {
		if !got[want] {
			t.Errorf("missing %q in %v", want, rows)
		}
	}
	if len(rows) != 4 {
		t.Fatalf("len(rows) = %d, want 4 (only array/object shapes yield nothing)", len(rows))
	}
}

// TestSecurityAlertReconciliationKeyRowsCoerceScalarIDsLikeArrowArrow proves
// the scalar arms coerce JSON scalars the way the legacy query's payload->>
// renders them: numbers and bools become their text form instead of
// vanishing, while null, missing, and compound values still yield nothing
// (issue #10875). The sibling relationshipReferencePayloadText sets the
// contract (fmt.Sprint); this writer matches it.
//
// Boundary pinned here, not hidden: float64 cannot preserve a scaled lexeme
// (12345.0 decodes to 12345 and renders "12345" while ->> renders
// "12345.0") nor >2^53 precision (9007199254740993 rounds before either
// side renders). A corpus census over the QA environment (88M facts, every
// id field of all 7.7M security-kind facts 100% strings) shows these shapes
// do not occur in practice, so keyed/legacy exactness holds on real data;
// the coercion below is defense-in-depth for off-path payloads.
func TestSecurityAlertReconciliationKeyRowsCoerceScalarIDsLikeArrowArrow(t *testing.T) {
	t.Parallel()

	rows := securityAlertReconciliationKeyRows([]facts.Envelope{{
		FactID:       "fact-1",
		ScopeID:      "scope",
		GenerationID: "gen",
		FactKind:     "security_alert.repository_alert",
		Payload: map[string]any{
			"repository_id": float64(12345),
			"package_id":    true,
			"cve_id":        "CVE-2024-0001",
			"advisory_id":   nil,
		},
	}, {
		FactID:       "fact-2",
		ScopeID:      "scope",
		GenerationID: "gen",
		FactKind:     "security_alert.repository_alert",
		Payload: map[string]any{
			"repository_id": map[string]any{"nested": "object"},
			"package_id":    []any{"array"},
		},
	}, {
		FactID:       "fact-3",
		ScopeID:      "scope",
		GenerationID: "gen",
		FactKind:     "security_alert.repository_alert",
		Payload: map[string]any{
			"repository_id": float64(12345),
			"package_id":    1e21,
			"cve_id":        float64(9007199254740993),
		},
	}})
	got := make(map[string]map[string]bool)
	for _, row := range rows {
		if got[row.FactID] == nil {
			got[row.FactID] = make(map[string]bool)
		}
		got[row.FactID][row.KeyKind+"\x00"+row.KeyValue] = true
	}
	for _, want := range []string{
		"repository\x0012345",
		"package\x00true",
		"cve\x00CVE-2024-0001",
	} {
		if !got["fact-1"][want] {
			t.Errorf("missing %q in %v", want, rows)
		}
	}
	for _, want := range []string{
		"repository\x0012345",
		"package\x001000000000000000000000",
		"cve\x009007199254740992",
	} {
		if !got["fact-3"][want] {
			t.Errorf("missing %q in %v", want, rows)
		}
	}
	if len(rows) != 6 {
		t.Fatalf("len(rows) = %d, want 6 (nil and compound scalars yield nothing)", len(rows))
	}
}

// TestSecurityAlertReconciliationKeyRowsSkipIneligible proves tombstones,
// other kinds, and empty identifiers yield no rows (#10875).
func TestSecurityAlertReconciliationKeyRowsSkipIneligible(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		{
			FactID: "tombstone", ScopeID: "scope", GenerationID: "gen",
			FactKind: "security_alert.repository_alert", IsTombstone: true,
			Payload: map[string]any{"repository_id": "acme/app"},
		},
		{
			FactID: "other-kind", ScopeID: "scope", GenerationID: "gen",
			FactKind: "content",
			Payload:  map[string]any{"repository_id": "acme/app"},
		},
		{
			FactID: "empty", ScopeID: "scope", GenerationID: "gen",
			FactKind: "security_alert.repository_alert",
			Payload:  map[string]any{"repository_id": "", "cve_ids": []any{}},
		},
	}
	if rows := securityAlertReconciliationKeyRows(envelopes); len(rows) != 0 {
		t.Fatalf("securityAlertReconciliationKeyRows() = %v, want no rows", rows)
	}
}

// TestSecurityAlertReconciliationKindFilterMatchesQuery proves the Go kind
// filter and the SQL IN list name the same two kinds: a kind the query
// reads but the writer skips (or vice versa) is a wrong answer in one
// direction (#10875). The supply-chain impact finding kind is gone from both
// (#11198, #11200): the reconciliation loader reads those rows from the
// per-support table.
func TestSecurityAlertReconciliationKindFilterMatchesQuery(t *testing.T) {
	t.Parallel()

	for _, kind := range securityAlertReconciliationFactKinds {
		if !strings.Contains(listActiveSecurityAlertReconciliationFactsQuery, "'"+kind+"'") {
			t.Errorf("query lacks kind %q named by the writer filter", kind)
		}
	}
	for _, kind := range []string{
		"security_alert.repository_alert",
		"reducer_package_consumption_correlation",
	} {
		if !isSecurityAlertReconciliationFactKind(kind) {
			t.Errorf("writer filter lacks kind %q read by the query", kind)
		}
	}
	if got := len(securityAlertReconciliationFactKinds); got != 2 {
		t.Errorf("writer filter names %d kinds, want 2", got)
	}
}

// TestSecurityAlertReconciliationPathsReadNoFindingFacts is the #11198 and
// #11200 census: none of the facts-side statements of the reconciliation path,
// the legacy and keyed loaders and the three sidecar backfill statements, names
// the supply-chain impact finding kind, so the path keeps working once those
// facts are no longer written and the backfill no longer builds keys for them.
func TestSecurityAlertReconciliationPathsReadNoFindingFacts(t *testing.T) {
	t.Parallel()

	statements := map[string]string{
		"backfill scopes":  lookupKeyBackfillScopesSQL,
		"backfill page":    lookupKeyBackfillPageSQL,
		"backfill missing": lookupKeyBackfillMissingSQL,
	}
	for name, statement := range statements {
		if strings.Contains(statement, "reducer_supply_chain_impact_finding") {
			t.Errorf("%s names reducer_supply_chain_impact_finding; the path must not read or key finding facts", name)
		}
	}
	// The fact statements read facts only: the impact findings come from the
	// supports statement, drained on its own cursor.
	for name, statement := range map[string]string{
		"legacy fact statement": listActiveSecurityAlertReconciliationFactsQuery,
		"keyed fact statement":  listActiveSecurityAlertReconciliationFactsKeyedQuery,
	} {
		if strings.Contains(statement, "reducer_supply_chain_impact_finding") {
			t.Errorf("%s names reducer_supply_chain_impact_finding", name)
		}
		if strings.Contains(statement, "supply_chain_impact_finding_supports") || strings.Contains(statement, "UNION ALL") {
			t.Errorf("%s reads the supports table or merges a second stream; the streams drain separately", name)
		}
	}
}

// TestRefreshSecurityAlertReconciliationKeysReplacesRows proves refresh
// deletes the batch's rows before inserting the derived keys in the same
// call, so a retyped fact leaves no stale keys (#10875).
func TestRefreshSecurityAlertReconciliationKeysReplacesRows(t *testing.T) {
	t.Parallel()

	database := &relationshipReferenceExecRecorder{}
	err := refreshSecurityAlertReconciliationKeys(context.Background(), database, []facts.Envelope{
		{
			FactID:       "fact-live",
			ScopeID:      "scope",
			GenerationID: "gen",
			FactKind:     "security_alert.repository_alert",
			Payload:      map[string]any{"repository_id": "acme/app"},
		},
		{
			FactID:       "fact-retyped",
			ScopeID:      "scope",
			GenerationID: "gen",
			FactKind:     "content",
			Payload:      map[string]any{"repository_id": "acme/app"},
		},
	})
	if err != nil {
		t.Fatalf("refreshSecurityAlertReconciliationKeys() error = %v, want nil", err)
	}
	if got, want := len(database.execs), 2; got != want {
		t.Fatalf("exec count = %d, want %d", got, want)
	}
	if !strings.Contains(database.execs[0].query, "DELETE FROM security_alert_reconciliation_keys") {
		t.Fatalf("first exec query = %q, want security key delete", database.execs[0].query)
	}
	if !strings.Contains(database.execs[1].query, "INSERT INTO security_alert_reconciliation_keys") {
		t.Fatalf("second exec query = %q, want security key insert", database.execs[1].query)
	}
}

// TestListActiveSecurityAlertReconciliationFactsKeyedQueryIsSemiJoin proves
// the keyed reader uses EXISTS, never a bare JOIN: a join would duplicate
// rows across multi-valued keys and corrupt the ORDER BY fact_id cursor
// paging (#10875).
func TestListActiveSecurityAlertReconciliationFactsKeyedQueryIsSemiJoin(t *testing.T) {
	t.Parallel()

	if !strings.Contains(listActiveSecurityAlertReconciliationFactsKeyedQuery, "EXISTS (") {
		t.Fatal("keyed query lacks an EXISTS semi-join")
	}
	if strings.Contains(strings.ToUpper(listActiveSecurityAlertReconciliationFactsKeyedQuery), "JOIN SECURITY_ALERT_RECONCILIATION_KEYS") {
		t.Fatal("keyed query joins the key table; use EXISTS so cursor paging stays exact")
	}
	for _, clause := range []string{"ORDER BY fact.fact_id ASC", "LIMIT $6", "($5 = '' OR fact.fact_id > $5)"} {
		if !strings.Contains(listActiveSecurityAlertReconciliationFactsKeyedQuery, clause) {
			t.Errorf("keyed query lacks paging clause %q", clause)
		}
	}
}

// TestSecurityAlertReconciliationKeyRowsStoreVerbatimValues proves padded
// values land in the sidecar untouched: the legacy arms compare exact values
// and the filter side already trims its inputs, so the fact side must not
// normalize (issue #10875). A whitespace-only value can never match a cleaned
// filter, but storing it verbatim keeps the sidecar a pure function of the
// fact instead of reimplementing filter cleaning.
func TestSecurityAlertReconciliationKeyRowsStoreVerbatimValues(t *testing.T) {
	t.Parallel()

	rows := securityAlertReconciliationKeyRows([]facts.Envelope{{
		FactID:       "fact-1",
		ScopeID:      "scope",
		GenerationID: "gen",
		FactKind:     "security_alert.repository_alert",
		Payload:      map[string]any{"repository_id": "   "},
	}})
	if len(rows) != 1 || rows[0].KeyValue != "   " {
		t.Fatalf("securityAlertReconciliationKeyRows() = %v, want one verbatim row", rows)
	}
}
