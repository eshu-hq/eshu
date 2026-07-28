// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

func TestListActiveSupplyChainImpactFactsQueryNormalizesSuppressionScopeSiblings(t *testing.T) {
	t.Parallel()

	// #5466 round-3 review F-6: scopeAnchorMatches
	// (go/internal/reducer/supply_chain_suppression_scope_match.go) compares
	// every vulnerability.suppression scope anchor -- CVEID, PackageID,
	// PURL, RepositoryID, SubjectDigest -- with strings.TrimSpace +
	// strings.EqualFold, so a payload of {"cve_id":"cve-2026-1234"}
	// (lowercase) or a whitespace-padded purl decodes and matches in Go but
	// was never SELECTED by the original exact-match
	// ->'scope'->>'X' = ANY($N) predicates. New placeholders ($16-$20)
	// carry lower(btrim(...)) normalized copies of $1/$2/$3/$5/$8 so the
	// exact-match top-level (non-"scope") siblings serving other fact kinds
	// keep their existing behavior unchanged. (Placeholders are shifted by
	// one relative to this branch's pre-rebase numbering because #5465's
	// AdvisoryIDs predicate was inserted at $4.)
	for _, want := range []string{
		"lower(btrim(fact.payload->'scope'->>'package_id', E' \\t\\n\\v\\f\\r')) = ANY($16::text[])",
		"lower(btrim(fact.payload->'scope'->>'purl', E' \\t\\n\\v\\f\\r')) = ANY($17::text[])",
		"lower(btrim(fact.payload->'scope'->>'cve_id', E' \\t\\n\\v\\f\\r')) = ANY($18::text[])",
		"lower(btrim(fact.payload->'scope'->>'subject_digest', E' \\t\\n\\v\\f\\r')) = ANY($19::text[])",
		"lower(btrim(fact.payload->'scope'->>'repository_id', E' \\t\\n\\v\\f\\r')) = ANY($20::text[])",
	} {
		if !strings.Contains(listActiveSupplyChainImpactFactsQuery, want) {
			t.Fatalf("listActiveSupplyChainImpactFactsQuery missing %q:\n%s", want, listActiveSupplyChainImpactFactsQuery)
		}
	}
	// The exact-match top-level siblings sharing $1/$2/$3/$5/$8 must be
	// unchanged -- these serve non-suppression fact kinds and must not be
	// normalized.
	for _, want := range []string{
		"fact.payload->>'package_id' = ANY($1::text[])",
		"fact.payload->>'purl' = ANY($2::text[])",
		"fact.payload->>'cve_id' = ANY($3::text[])",
		"fact.payload->>'subject_digest' = ANY($5::text[])",
		"fact.payload->>'repository_id' = ANY($8::text[])",
	} {
		if !strings.Contains(listActiveSupplyChainImpactFactsQuery, want) {
			t.Fatalf("listActiveSupplyChainImpactFactsQuery missing unchanged top-level sibling %q:\n%s", want, listActiveSupplyChainImpactFactsQuery)
		}
	}
}

func TestListActiveSupplyChainImpactFactsBindsNormalizedSuppressionScopeSiblings(t *testing.T) {
	t.Parallel()

	// Hermetic (no DSN required) bind-position proof for F-6's five new
	// placeholders: asserts the bound $16-$20 argument values directly,
	// mirroring TestListActiveSupplyChainImpactFactsBindsWorkloadAndServiceIDsToDistinctPlaceholders
	// for $13/$14.
	db := &fakeExecQueryer{queryResponses: []queueFakeRows{{rows: nil}}}
	store := NewFactStore(db)

	_, err := store.ListActiveSupplyChainImpactFacts(context.Background(), reducer.SupplyChainImpactFactFilter{
		PackageIDs:     []string{"PKG:NPM/Left-Pad"},
		PURLs:          []string{" pkg:npm/left-pad@1.3.0 "},
		CVEIDs:         []string{"CVE-2026-1234"},
		SubjectDigests: []string{"SHA256:ABCDEF"},
		RepositoryIDs:  []string{"Repo://Acme/Api"},
	})
	if err != nil {
		t.Fatalf("ListActiveSupplyChainImpactFacts() error = %v, want nil", err)
	}
	if got, want := len(db.queries), 1; got != want {
		t.Fatalf("query count = %d, want %d", got, want)
	}

	// args are 0-indexed; $16 is args[15] ... $20 is args[19].
	cases := []struct {
		placeholder string
		argIndex    int
		want        []string
	}{
		{"$16 (package_id)", 15, []string{"pkg:npm/left-pad"}},
		{"$17 (purl)", 16, []string{"pkg:npm/left-pad@1.3.0"}},
		{"$18 (cve_id)", 17, []string{"cve-2026-1234"}},
		{"$19 (subject_digest)", 18, []string{"sha256:abcdef"}},
		{"$20 (repository_id)", 19, []string{"repo://acme/api"}},
	}
	for _, c := range cases {
		got, ok := db.queries[0].args[c.argIndex].([]string)
		if !ok {
			t.Fatalf("%s (index %d) arg type = %T, want []string", c.placeholder, c.argIndex, db.queries[0].args[c.argIndex])
		}
		if !slices.Equal(got, c.want) {
			t.Fatalf("%s arg = %v, want %v", c.placeholder, got, c.want)
		}
	}
}

func TestListActiveSupplyChainImpactFactsQueryNormalizesSuppressionScopeAdvisoryID(t *testing.T) {
	t.Parallel()

	// #5466 round-4 review F-10: advisory_id had NO load-path predicate at
	// all -- unlike package_id/purl/cve_id/subject_digest/repository_id
	// (F-6), which at least had a stale exact-match predicate before being
	// normalized, advisory_id was a dead scope key from the start. New
	// placeholder $21 closes it with the same lower(btrim(...)) treatment.
	for _, want := range []string{
		"lower(btrim(fact.payload->'scope'->>'advisory_id', E' \\t\\n\\v\\f\\r')) = ANY($21::text[])",
	} {
		if !strings.Contains(listActiveSupplyChainImpactFactsQuery, want) {
			t.Fatalf("listActiveSupplyChainImpactFactsQuery missing %q:\n%s", want, listActiveSupplyChainImpactFactsQuery)
		}
	}
}

func TestListActiveSupplyChainImpactFactsBindsNormalizedSuppressionScopeAdvisoryID(t *testing.T) {
	t.Parallel()

	// Hermetic (no DSN required) bind-position proof for F-10's $21
	// placeholder, mirroring the $16-$20 bind-position test above.
	db := &fakeExecQueryer{queryResponses: []queueFakeRows{{rows: nil}}}
	store := NewFactStore(db)

	_, err := store.ListActiveSupplyChainImpactFacts(context.Background(), reducer.SupplyChainImpactFactFilter{
		AdvisoryIDs: []string{" GHSA-Demo-1111-2222 "},
	})
	if err != nil {
		t.Fatalf("ListActiveSupplyChainImpactFacts() error = %v, want nil", err)
	}
	if got, want := len(db.queries), 1; got != want {
		t.Fatalf("query count = %d, want %d", got, want)
	}

	// args are 0-indexed; $21 is args[20].
	got, ok := db.queries[0].args[20].([]string)
	if !ok {
		t.Fatalf("$21 (index 20) arg type = %T, want []string", db.queries[0].args[20])
	}
	if want := []string{"ghsa-demo-1111-2222"}; !slices.Equal(got, want) {
		t.Fatalf("$21 (AdvisoryIDs) arg = %v, want %v", got, want)
	}
}
