// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"sort"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestCodeFlowSQLReadsLatestFactPerStableKeyThroughActiveGeneration(t *testing.T) {
	t.Parallel()

	query := listActiveCodeFlowFactsSQL
	for _, want := range []string{
		"active_scope AS",
		"active_generation.status = 'active'",
		"fact.payload->>'repo_id' = $2",
		"generation.ingested_at <= active_scope.active_ingested_at",
		"ROW_NUMBER() OVER (",
		"PARTITION BY fact.scope_id, fact.stable_fact_key",
		"ORDER BY generation.ingested_at DESC, generation.generation_id DESC, fact.observed_at DESC, fact.fact_id DESC",
		"candidate.rn = 1",
		"candidate.is_tombstone = FALSE",
	} {
		if !strings.Contains(query, want) {
			t.Fatalf("code-flow SQL missing %q", want)
		}
	}
	if strings.Contains(query, "scope.active_generation_id = fact.generation_id") {
		t.Fatal("code-flow SQL must not read only the active delta generation")
	}
}

// TestCodeFlowSQLKeepsLiteralKindConjunctForPartialIndex guards the #5280
// fix: the read filters fact_kind by both the parameterized $1 subset AND a
// literal `fact_kind IN (...)` conjunct. The literal is what lets PostgreSQL
// prove the fact_records_code_flow_repo_idx partial-index predicate at plan
// time; without it a generic prepared plan cannot prove $1 is limited to these
// kinds and silently falls back to the all-scope over-fetch (proven under
// plan_cache_mode=force_generic_plan: 10,137 buffers vs 728 with the conjunct).
// Removing this line reverts the index win on the real pgx production path.
func TestCodeFlowSQLKeepsLiteralKindConjunctForPartialIndex(t *testing.T) {
	t.Parallel()

	query := listActiveCodeFlowFactsSQL
	if !strings.Contains(query, "fact.fact_kind = ANY($1::text[])") {
		t.Fatal("code-flow SQL must keep the parameterized $1 kind filter for the exact per-read subset")
	}
	if !strings.Contains(query, "fact.fact_kind IN (") {
		t.Fatal("code-flow SQL must keep the literal fact_kind IN (...) conjunct so a generic prepared plan can use the partial index fact_records_code_flow_repo_idx (#5280)")
	}

	// The literal conjunct must cover EXACTLY the canonical code-flow read set,
	// not merely a superset: extracting and set-comparing (rather than a
	// substring check) fails when a kind is missing OR when an extra kind drifts
	// in, keeping the query literal, the partial index predicate, and
	// facts.CodeFlowReadFactKinds in lockstep across package boundaries (#5284).
	got := extractFactKindInList(t, query)
	want := codeFlowSortedKinds(facts.CodeFlowReadFactKinds())
	if !codeFlowEqualKinds(got, want) {
		t.Fatalf("code-flow SQL literal conjunct set = %v, want the canonical facts.CodeFlowReadFactKinds() = %v", got, want)
	}

	// The canonical set must in turn cover every fact kind codeFlowFactKinds can
	// return across all CodeFlowKind values, so the $1 subset is always drawn
	// from the same set the literal conjunct (and the index) constrain — the
	// conjunct is redundant (result-neutral) rather than a filter that could
	// drop rows the $1 subset would have returned.
	canonical := map[string]bool{}
	for _, k := range facts.CodeFlowReadFactKinds() {
		canonical[k] = true
	}
	// Range the production dispatch map itself, not a hand-copied list of
	// CodeFlowKind values: a new kind wired into codeFlowKindFactKinds is picked
	// up here automatically, so it cannot add a fact kind the canonical set (and
	// therefore the literal conjunct and index) miss without failing this guard.
	union := map[string]bool{}
	for kind := range codeFlowKindFactKinds {
		for _, k := range codeFlowFactKinds(kind) {
			union[k] = true
			if !canonical[k] {
				t.Fatalf("codeFlowFactKinds(%q) can select kind %q that is not in facts.CodeFlowReadFactKinds(); the read would query a kind the literal conjunct and partial index do not cover", kind, k)
			}
		}
	}
	if len(union) != len(canonical) {
		t.Fatalf("codeFlowFactKinds union has %d kinds but facts.CodeFlowReadFactKinds() has %d; the canonical set must equal the kinds the read can actually select", len(union), len(canonical))
	}
}

// TestCodeFlowFactKindsDispatch pins the per-kind fact-kind dispatch so the
// map-backed lookup keeps the exact behavior of the switch it replaced: taint
// selects the taint+interproc evidence kinds, the three dataflow-derived kinds
// select the dataflow function kind, and an unknown kind returns nil (which
// ListCodeFlow short-circuits to an empty read). Order within a kind is part of
// the contract because it becomes the $1 array the read binds.
func TestCodeFlowFactKindsDispatch(t *testing.T) {
	t.Parallel()

	cases := []struct {
		kind CodeFlowKind
		want []string
	}{
		{CodeFlowKindTaintPath, []string{facts.CodeTaintEvidenceFactKind, facts.CodeInterprocEvidenceFactKind}},
		{CodeFlowKindReachingDef, []string{facts.CodeDataflowFunctionFactKind}},
		{CodeFlowKindCFGSummary, []string{facts.CodeDataflowFunctionFactKind}},
		{CodeFlowKindPDGSummary, []string{facts.CodeDataflowFunctionFactKind}},
		{CodeFlowKind("unknown_kind"), nil},
	}
	for _, tc := range cases {
		got := codeFlowFactKinds(tc.kind)
		if len(got) != len(tc.want) {
			t.Fatalf("codeFlowFactKinds(%q) = %v, want %v", tc.kind, got, tc.want)
		}
		for i := range tc.want {
			if got[i] != tc.want[i] {
				t.Fatalf("codeFlowFactKinds(%q)[%d] = %q, want %q", tc.kind, i, got[i], tc.want[i])
			}
		}
	}
}

// extractFactKindInList pulls the parenthesised kind list from the single
// `fact_kind IN ( ... )` conjunct in a code-flow SQL statement and returns the
// unquoted kinds sorted. It anchors on `fact_kind IN (` so it ignores unrelated
// `... IN (...)` predicates (e.g. generation.status). Fails the test when the
// conjunct is absent or malformed rather than silently returning an empty set.
func extractFactKindInList(t *testing.T, sql string) []string {
	t.Helper()
	start := strings.Index(sql, "fact_kind IN (")
	if start < 0 {
		t.Fatal("no `fact_kind IN (` conjunct found")
	}
	open := start + len("fact_kind IN (")
	end := strings.Index(sql[open:], ")")
	if end < 0 {
		t.Fatal("unterminated `fact_kind IN (` conjunct")
	}
	kinds := []string{}
	for _, part := range strings.Split(sql[open:open+end], ",") {
		kind := strings.Trim(strings.TrimSpace(part), "'")
		if kind != "" {
			kinds = append(kinds, kind)
		}
	}
	sort.Strings(kinds)
	return kinds
}

func codeFlowSortedKinds(in []string) []string {
	out := append([]string(nil), in...)
	sort.Strings(out)
	return out
}

func codeFlowEqualKinds(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
