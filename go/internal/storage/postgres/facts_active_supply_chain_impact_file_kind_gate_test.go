// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"reflect"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"

	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
	factimpact "github.com/eshu-hq/eshu/go/internal/storage/postgres/facts/impact"
	codegraphv1 "github.com/eshu-hq/eshu/sdk/go/factschema/codegraph/v1"
	ociregistryv1 "github.com/eshu-hq/eshu/sdk/go/factschema/ociregistry/v1"
	packageregistryv1 "github.com/eshu-hq/eshu/sdk/go/factschema/packageregistry/v1"
	reducerderivedv1 "github.com/eshu-hq/eshu/sdk/go/factschema/reducerderived/v1"
	sbomv1 "github.com/eshu-hq/eshu/sdk/go/factschema/sbom/v1"
	securityalertv1 "github.com/eshu-hq/eshu/sdk/go/factschema/securityalert/v1"
	vulnerabilityv1 "github.com/eshu-hq/eshu/sdk/go/factschema/vulnerability/v1"
)

// supplyChainImpactUngatedIdentityPayloadKeys are the payload keys the
// retired predicate compares WITHOUT first narrowing to a fact kind. The
// #8880 catalog keeps that predicate verbatim in the foreign-key guard
// statement, so the set is derived from the guard SQL.
//
// This list is the premise the #5237 file-kind gate rests on: a `file` fact
// carrying one of these keys would be selected only through the guard, which
// keeps the gate. TestSupplyChainImpactFileFactCarriesNoUngatedIdentityKey
// pins the file contract against the keys the guard admits file rows on.
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

// supplyChainImpactPlaceholder returns the $n a branch binds a Param at.
func supplyChainImpactPlaceholder(t *testing.T, branch factimpact.Branch, param factimpact.Param) string {
	t.Helper()
	for i, bound := range branch.Params {
		if bound == param {
			return "$" + strconv.Itoa(i+1) + "::text[]"
		}
	}
	t.Fatalf("branch %s does not bind param %d", branch.Name, param)
	return ""
}

// TestSupplyChainImpactUngatedIdentityKeysStayInLockstepWithQuery fails when
// the guard statement's ungated key set drifts from the list above in EITHER
// direction. The expected set is DERIVED from the shipped SQL, so a new
// ungated predicate fails the build instead of quietly escaping the premise.
func TestSupplyChainImpactUngatedIdentityKeysStayInLockstepWithQuery(t *testing.T) {
	t.Parallel()

	guard := supplyChainImpactBranch(t, "foreign_key_guard")
	derived := supplyChainImpactUngatedIdentityKeysInQuery(t, guard.SQL, supplyChainImpactPlaceholder(t, guard, factimpact.FileRepositoryIDs))
	declared := append([]string(nil), supplyChainImpactUngatedIdentityPayloadKeys...)
	sort.Strings(declared)
	if !reflect.DeepEqual(derived, declared) {
		t.Errorf("ungated identity predicate set drifted from the declared list.\nderived from guard: %v\ndeclared in test:   %v", derived, declared)
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
func supplyChainImpactUngatedIdentityKeysInQuery(t *testing.T, query, filePlaceholder string) []string {
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

			if !supplyChainImpactPredicateIsUnreachableForFileRow(t, query, at, filePlaceholder) {
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
func supplyChainImpactPredicateIsUnreachableForFileRow(t *testing.T, query string, at int, filePlaceholder string) bool {
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
			if !strings.Contains(group, filePlaceholder) {
				t.Errorf(
					"a fact_kind = 'file' group at offset %d does not filter on FileRepositoryIDs (%s); "+
						"the #5237 gate would skip rows this group can still match",
					open, filePlaceholder,
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

// supplyChainImpactJSONKeys returns a payload struct's top-level JSON keys.
func supplyChainImpactJSONKeys(value any) []string {
	var keys []string
	typ := reflect.TypeOf(value)
	for i := 0; i < typ.NumField(); i++ {
		name := strings.TrimSpace(strings.Split(typ.Field(i).Tag.Get("json"), ",")[0])
		if name != "" && name != "-" {
			keys = append(keys, name)
		}
	}
	return keys
}

// TestSupplyChainImpactFileFactCarriesNoUngatedIdentityKey pins the premise
// the guard index rests on for `file`: the file contract carries none of the
// keys the guard admits file rows on, so the guard holds no file rows and the
// file branches (JS/TS repo_id probe and resolved scopes) serve every file
// match. A contract change that adds one fails here before the guard fills.
func TestSupplyChainImpactFileFactCarriesNoUngatedIdentityKey(t *testing.T) {
	t.Parallel()

	guarded := factimpact.OmittedKeys(factimpact.KindFile)
	for _, key := range supplyChainImpactUngatedIdentityPayloadKeys {
		if !slices.Contains(guarded, key) {
			t.Fatalf("ungated key %s is probed for file rows; the guard premise changed", key)
		}
	}
	for _, key := range supplyChainImpactJSONKeys(codegraphv1.File{}) {
		if slices.Contains(guarded, key) {
			t.Errorf("file fact payload key %q is a guarded identity key: every file row would land in the foreign-key guard index", key)
		}
	}
}

// TestSupplyChainImpactGrowthContractsCarryNoGuardedKey is the #5237-style
// lockstep for the growth kinds (#8880 ruling 1.4): no factschema contract
// carries a key the guard admits its kind on. The guard keeps the row set
// exact either way; this keeps the guard index empty and fails the build when
// a contract grows a key its branch does not probe.
func TestSupplyChainImpactGrowthContractsCarryNoGuardedKey(t *testing.T) {
	t.Parallel()

	for kind, contract := range map[string]any{
		factimpact.KindCVE:                    vulnerabilityv1.CVE{},
		factimpact.KindAffectedPackage:        vulnerabilityv1.AffectedPackage{},
		factimpact.KindAffectedProduct:        vulnerabilityv1.AffectedProduct{},
		factimpact.KindEPSSScore:              vulnerabilityv1.EPSSScore{},
		factimpact.KindKnownExploited:         vulnerabilityv1.KnownExploited{},
		factimpact.KindSecurityAlert:          securityalertv1.RepositoryAlert{},
		factimpact.KindPackageVersion:         packageregistryv1.PackageVersion{},
		factimpact.KindVulnerabilityHint:      packageregistryv1.VulnerabilityHint{},
		factimpact.KindSBOMComponent:          sbomv1.Component{},
		factimpact.KindOCIImageManifest:       ociregistryv1.ImageManifest{},
		factimpact.KindOCIImageIndex:          ociregistryv1.ImageIndex{},
		factimpact.KindOCIImageTagObservation: ociregistryv1.TagObservation{},
		factimpact.KindOCIImageReferrer:       ociregistryv1.ImageReferrer{},
		factimpact.KindPackageConsumption:     reducerderivedv1.PackageConsumptionCorrelation{},
	} {
		guarded := factimpact.OmittedKeys(kind)
		for _, key := range supplyChainImpactJSONKeys(contract) {
			if slices.Contains(guarded, key) {
				t.Errorf("%s contract carries %q, which its branches do not probe: add a key probe or reclassify the kind", kind, key)
			}
		}
	}
}

// TestListActiveSupplyChainImpactFactsGatesFileKindOnFileRepositoryIDs is the
// #5237 regression guard on the #8880 catalog: with FileRepositoryIDs empty no
// file statement is issued, and the guard keeps the gate conjunct ahead of
// the retired predicate, as a sibling conjunct, never as one more arm.
func TestListActiveSupplyChainImpactFactsGatesFileKindOnFileRepositoryIDs(t *testing.T) {
	t.Parallel()

	guard := supplyChainImpactBranch(t, "foreign_key_guard")
	gate := "(fact.fact_kind <> 'file' OR COALESCE(cardinality(" + supplyChainImpactPlaceholder(t, guard, factimpact.FileRepositoryIDs) + "), 0) > 0)"
	gateAt := strings.Index(guard.SQL, gate)
	disjunctionAt := strings.Index(guard.SQL, "fact.payload->>'package_id' = ANY(")
	if gateAt < 0 || disjunctionAt < 0 || gateAt > disjunctionAt {
		t.Fatalf("guard must carry the #5237 gate as a conjunct before the retired predicate (gate at %d, predicate at %d):\n%s", gateAt, disjunctionAt, guard.SQL)
	}

	routed := &supplyChainImpactRoutingDB{scopes: map[string][2]string{"repository:a": {"git:a", "gen:a"}}}
	if _, _, err := NewFactStore(routed).ListActiveSupplyChainImpactFacts(context.Background(), supplychaincore.SupplyChainImpactFactFilter{
		PURLs: []string{"pkg:maven/org.example/lib@1.0.0"}, CVEIDs: []string{"CVE-2026-52370"},
	}); err != nil {
		t.Fatalf("maven load: %v", err)
	}
	for _, label := range routed.labels() {
		if label == "file_repo_id" || label == "file_scope" || label == "scope_resolution" {
			t.Fatalf("maven intent issued %s; the #5237 gate requires no file statement without FileRepositoryIDs (calls %v)", label, routed.labels())
		}
	}
	routed.calls = nil
	if _, _, err := NewFactStore(routed).ListActiveSupplyChainImpactFacts(context.Background(), supplychaincore.SupplyChainImpactFactFilter{
		PackageIDs: []string{"npm://registry.npmjs.org/axios"}, FileRepositoryIDs: []string{"repository:a"},
	}); err != nil {
		t.Fatalf("npm load: %v", err)
	}
	if len(routed.callsFor("file_repo_id")) != 1 || len(routed.callsFor("file_scope")) != 1 {
		t.Fatalf("npm intent must issue the JS/TS repo_id and resolved-scope file statements once each: %v", routed.labels())
	}
	if got := boundList(t, routed.callsFor("file_scope")[0], factimpact.FileRepositoryScopeIDs); !slices.Equal(got, []string{"git:a"}) {
		t.Fatalf("file_scope bound scopes %v, want the resolved git:a", got)
	}
}
