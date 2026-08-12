// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"reflect"
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
// premise against the file fact contract.
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

// TestSupplyChainImpactUngatedIdentityKeysStayInLockstepWithQuery fails when a
// key is added to or renamed in the shipped disjunction without updating the
// list above, so the exactness premise below cannot silently go stale.
func TestSupplyChainImpactUngatedIdentityKeysStayInLockstepWithQuery(t *testing.T) {
	for _, key := range supplyChainImpactUngatedIdentityPayloadKeys {
		if !strings.Contains(listActiveSupplyChainImpactFactsQuery, "fact.payload->>'"+key+"'") {
			t.Errorf("payload key %q is listed as an ungated identity predicate but no longer appears in the query", key)
		}
	}
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
	if gateAt > disjunctionAt {
		t.Errorf("file-kind gate must precede the identity disjunction so a file row short-circuits before any JSONB extraction (gate at %d, disjunction at %d)", gateAt, disjunctionAt)
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
