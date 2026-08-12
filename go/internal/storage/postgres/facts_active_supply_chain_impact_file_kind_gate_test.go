// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"reflect"
	"sort"
	"strings"
	"testing"

	codegraphv1 "github.com/eshu-hq/eshu/sdk/go/factschema/codegraph/v1"
)

// supplyChainImpactUngatedIdentityPayloadKeys are the payload keys
// listActiveSupplyChainImpactFactsQuery compares WITHOUT first narrowing to a
// fact kind. Every row the query visits — including every `file` row in every
// active scope — pays one JSONB extraction per key in this list, and each
// extraction detoasts the whole payload again.
//
// This list is the premise the #5237 file-kind gate rests on: if a `file`
// fact could carry any of these keys, skipping `file` rows when
// $10 (FileRepositoryIDs) is empty would drop a row the shipped query
// returns. TestSupplyChainImpactFileFactCarriesNoUngatedIdentityKey pins that
// premise against the file fact contract, and
// TestSupplyChainImpactUngatedIdentityKeysStayInLockstepWithQuery pins this
// list itself against the shipped query in both directions — it derives the
// ungated set from the SQL rather than spot-checking the entries, so a NEW
// ungated predicate fails the build instead of quietly escaping the premise.
var supplyChainImpactUngatedIdentityPayloadKeys = []string{
	"package_id",
	"purl",
	"cve_id",
	"advisory_id",
	"subject_digest",
	"digest",
	"artifact_digest",
	"referrer_digest",
	"resolved_digest",
	"cpe",
	"criteria",
	"document_id",
	"image_ref",
}

// TestSupplyChainImpactUngatedIdentityKeysStayInLockstepWithQuery fails when
// the shipped disjunction's ungated key set drifts from the list above in
// EITHER direction: a listed key removed or renamed, and — the direction that
// actually breaks the gate — a NEW ungated predicate added.
//
// The expected set is DERIVED from the shipped query constant by
// supplyChainImpactUngatedIdentityKeysInQuery rather than re-asserted key by
// key, because a one-directional "every listed key still appears" check cannot
// see an addition. An ungated predicate added on a key a `file` fact does carry
// (`relative_path`, say) would make the #5237 gate drop rows the ungated query
// returns, and a forward-only check stays green through it.
func TestSupplyChainImpactUngatedIdentityKeysStayInLockstepWithQuery(t *testing.T) {
	derived := supplyChainImpactUngatedIdentityKeysInQuery(t, listActiveSupplyChainImpactFactsQuery)

	declared := append([]string(nil), supplyChainImpactUngatedIdentityPayloadKeys...)
	sort.Strings(declared)

	if !reflect.DeepEqual(derived, declared) {
		t.Errorf(
			"ungated identity predicate set drifted from the declared list.\n"+
				"derived from query: %v\ndeclared in test:   %v\n"+
				"Every key in the derived set is compared against EVERY row the query visits, "+
				"including every `file` row. Add or remove keys in "+
				"supplyChainImpactUngatedIdentityPayloadKeys to match, and re-check the #5237 "+
				"file-kind gate's exactness premise against the new set.",
			derived, declared,
		)
	}
}

// supplyChainImpactUngatedIdentityKeysInQuery returns, sorted and deduped,
// every top-level payload key the query compares against a `file` row outside
// the $10-guarded file branch.
//
// For each `fact.payload->>'key'` / `fact.payload->'scope'->>'key'` occurrence
// it walks outward through the enclosing parenthesised groups looking for a
// `fact.fact_kind` restriction that is ANDed with the predicate:
//
//   - a restriction whose kind set excludes 'file' means a `file` row never
//     reaches the predicate, so the key is irrelevant to the gate;
//   - a restriction of exactly {'file'} is the reachability branch itself — it
//     must also carry the $10 guard, which is what makes the gate exact;
//   - no such restriction means the predicate runs for every `file` row, so the
//     key is ungated and belongs in the declared list.
func supplyChainImpactUngatedIdentityKeysInQuery(t *testing.T, query string) []string {
	t.Helper()

	// Derive from the statement Postgres actually executes. Parsing the raw
	// constant would also read the SQL comments, where a future `--` line
	// quoting a `fact.payload->>'key'` predicate would register as a real one.
	query = sqlWithoutLineComments(query)

	seen := make(map[string]struct{})
	for _, prefix := range []string{"fact.payload->>'", "fact.payload->'scope'->>'"} {
		for offset := 0; ; {
			rel := strings.Index(query[offset:], prefix)
			if rel < 0 {
				break
			}
			at := offset + rel
			offset = at + len(prefix)

			end := strings.Index(query[offset:], "'")
			if end < 0 {
				t.Fatalf("unterminated payload key literal at offset %d", at)
			}
			key := query[offset : offset+end]

			if !supplyChainImpactPredicateIsUnreachableForFileRow(t, query, at) {
				seen[key] = struct{}{}
			}
		}
	}

	keys := make([]string, 0, len(seen))
	for key := range seen {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// supplyChainImpactPredicateIsUnreachableForFileRow reports whether an
// enclosing group keeps the predicate at `at` from ever selecting a `file` row
// once the #5237 gate is in place.
func supplyChainImpactPredicateIsUnreachableForFileRow(t *testing.T, query string, at int) bool {
	t.Helper()

	for cursor := at; ; {
		open := supplyChainImpactEnclosingGroupOpen(query, cursor)
		if open < 0 {
			return false
		}
		body := strings.TrimLeft(query[open+1:], " \t\r\n")

		switch {
		case strings.HasPrefix(body, "fact.fact_kind = '"):
			rest := strings.TrimPrefix(body, "fact.fact_kind = '")
			end := strings.Index(rest, "'")
			if end < 0 {
				t.Fatalf("unterminated fact_kind literal near offset %d", open)
			}
			kind := rest[:end]
			if kind != "file" {
				return true
			}
			// The file branch: exact only because it also requires $10.
			group := query[open:supplyChainImpactGroupClose(t, query, open)]
			if !strings.Contains(group, "$10::text[]") {
				t.Errorf(
					"a fact_kind = 'file' branch at offset %d does not filter on $10; "+
						"the #5237 gate would skip rows this branch can still match",
					open,
				)
			}
			return true

		case strings.HasPrefix(body, "fact.fact_kind IN ("):
			rest := body[len("fact.fact_kind IN ("):]
			end := strings.Index(rest, ")")
			if end < 0 {
				t.Fatalf("unterminated fact_kind IN list near offset %d", open)
			}
			if !strings.Contains(rest[:end], "'file'") {
				return true
			}
			return false
		}

		cursor = open
	}
}

// supplyChainImpactEnclosingGroupOpen returns the index of the innermost
// unclosed '(' before `at`, or -1 when `at` sits at the query's top level.
func supplyChainImpactEnclosingGroupOpen(query string, at int) int {
	depth := 0
	for i := at - 1; i >= 0; i-- {
		switch query[i] {
		case ')':
			depth++
		case '(':
			if depth == 0 {
				return i
			}
			depth--
		}
	}
	return -1
}

// supplyChainImpactGroupClose returns the index just past the ')' matching the
// '(' at `open`.
func supplyChainImpactGroupClose(t *testing.T, query string, open int) int {
	t.Helper()

	depth := 0
	for i := open; i < len(query); i++ {
		switch query[i] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return i + 1
			}
		}
	}
	t.Fatalf("unbalanced parentheses from offset %d", open)
	return 0
}

// TestSupplyChainImpactFileFactCarriesNoUngatedIdentityKey pins the premise the
// #5237 file-kind gate depends on: a `file` fact's payload contract carries
// none of the identity keys the ungated predicates compare, so a `file` row can
// only ever be selected through the query's own `fact_kind = 'file'` branch —
// which requires $10 (FileRepositoryIDs) to be non-empty.
//
// If a future contract change adds one of those keys to the file payload, this
// test fails and points at the gate that must be revisited, rather than letting
// the gate start dropping rows the query used to return.
func TestSupplyChainImpactFileFactCarriesNoUngatedIdentityKey(t *testing.T) {
	ungated := make(map[string]struct{}, len(supplyChainImpactUngatedIdentityPayloadKeys))
	for _, key := range supplyChainImpactUngatedIdentityPayloadKeys {
		ungated[key] = struct{}{}
	}

	fileType := reflect.TypeOf(codegraphv1.File{})
	for i := 0; i < fileType.NumField(); i++ {
		tag := fileType.Field(i).Tag.Get("json")
		name := strings.TrimSpace(strings.Split(tag, ",")[0])
		if name == "" || name == "-" {
			continue
		}
		if _, clash := ungated[name]; clash {
			t.Errorf(
				"file fact payload key %q is also an ungated supply-chain identity predicate: "+
					"the #5237 file-kind gate in listActiveSupplyChainImpactFactsQuery would now drop "+
					"rows the query returns and must be revisited",
				name,
			)
		}
	}
}

// TestListActiveSupplyChainImpactFactsGatesFileKindOnFileRepositoryIDs is the
// #5237 regression guard. `file` is in the query's scanned fact-kind list only
// to serve the JS/TS reachability branch, and that branch cannot match unless
// $10 (FileRepositoryIDs) is non-empty. $10 is populated only for npm-ecosystem
// affected packages (npmAffectedPackages in
// go/internal/reducer/supply_chain_impact_active_filter.go), so without this
// gate every non-npm intent reads and detoasts every `file` payload in every
// active scope for nothing.
func TestListActiveSupplyChainImpactFactsGatesFileKindOnFileRepositoryIDs(t *testing.T) {
	const gate = "AND (fact.fact_kind <> 'file' OR COALESCE(cardinality($10::text[]), 0) > 0)"
	if !strings.Contains(listActiveSupplyChainImpactFactsQuery, gate) {
		t.Fatalf("query is missing the #5237 file-kind gate %q", gate)
	}

	gateAt := strings.Index(listActiveSupplyChainImpactFactsQuery, gate)
	disjunctionAt := strings.Index(listActiveSupplyChainImpactFactsQuery, "fact.payload->>'package_id' = ANY($1::text[])")
	if disjunctionAt < 0 {
		t.Fatal("query no longer contains the package_id identity predicate")
	}
	// The gate has to sit ahead of the disjunction as a sibling conjunct of it,
	// never as one more branch inside it -- an OR'd `fact_kind <> 'file'` would
	// admit every row of every other kind unconditionally and change what the
	// query returns. Text order is what makes that structurally checkable here;
	// it is not what makes the gate cheap. Postgres orders quals within a filter
	// by its own cost estimate, and the evidence that it puts this cheap
	// non-TOAST comparison first is the measured buffer drop (28,252,679 ->
	// 14,737 in docs/internal/evidence/5237-supply-chain-impact-file-fact-scan.md),
	// not this assertion.
	if gateAt > disjunctionAt {
		t.Errorf("file-kind gate must precede the identity disjunction as a conjunct of it, not a branch within it (gate at %d, disjunction at %d)", gateAt, disjunctionAt)
	}

	// The gate must bind the SAME placeholder the file branch filters on, or
	// it would skip file rows the branch could still match.
	fileBranchAt := strings.Index(listActiveSupplyChainImpactFactsQuery, "fact.fact_kind = 'file'")
	if fileBranchAt < 0 {
		t.Fatal("query no longer contains the file branch")
	}
	fileBranch := listActiveSupplyChainImpactFactsQuery[fileBranchAt:]
	if end := strings.Index(fileBranch, "OR fact.payload->>'image_ref'"); end > 0 {
		fileBranch = fileBranch[:end]
	}
	if !strings.Contains(fileBranch, "$10::text[]") {
		t.Error("file branch no longer filters on $10; the gate's placeholder must match it")
	}
}
