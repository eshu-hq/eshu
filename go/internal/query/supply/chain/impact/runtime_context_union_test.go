// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

import (
	"strings"
	"testing"
)

// TestSelectRuntimeContextQueryIsPerKindUnion pins the #11447 Plan C
// production shape: UNION ALL of one scope-closure arm (all four kinds)
// plus one bounded outside-closure arm per kind. Every arm evaluates the
// byte-identical decoder and the same final decoded-membership predicate;
// arms are superset prefilters only, never arm-specific semantics. The
// grant predicate is the unchanged text, and the empty-candidates early
// return stays in ListSupplyChainImpactRuntimeContext (pinned by
// TestCov8ListRuntimeContext).
func TestSelectRuntimeContextQueryIsPerKindUnion(t *testing.T) {
	t.Parallel()

	decoder := supplyChainRuntimeRepositoryDecoderJoin(
		"fact.payload",
		"fact.scope_id",
		"runtime_repository",
	)
	if got := strings.Count(SelectRuntimeContextQuery, decoder); got != 5 {
		t.Errorf("decoder join occurrences = %d, want 5 (one per arm, byte-identical)", got)
	}

	const decodedPredicate = "runtime_repository.repository_id = ANY($1::text[])"
	if got := strings.Count(SelectRuntimeContextQuery, decodedPredicate); got != 5 {
		t.Errorf("decoded-membership predicate occurrences = %d, want 5 (one per arm)", got)
	}

	const grantPredicate = `(
          COALESCE(cardinality($2::text[]), 0) = 0
          AND COALESCE(cardinality($3::text[]), 0) = 0
        )
        OR runtime_repository.repository_id = ANY($2::text[])
        OR fact.scope_id = ANY($3::text[])`
	if got := strings.Count(SelectRuntimeContextQuery, grantPredicate); got != 5 {
		t.Errorf("grant predicate occurrences = %d, want 5 (unchanged text, one per arm)", got)
	}

	if got := strings.Count(SelectRuntimeContextQuery, "UNION ALL"); got != 4 {
		t.Errorf("UNION ALL occurrences = %d, want 4 (closure arm + 4 per-kind outside arms)", got)
	}

	const closurePrefilter = "scope.scope_id = ANY($4::text[])"
	if got := strings.Count(SelectRuntimeContextQuery, closurePrefilter); got != 1 {
		t.Errorf("closure prefilter occurrences = %d, want 1 (closure arm only)", got)
	}

	const outsidePrefilter = "NOT (fact.scope_id = ANY($4::text[]))"
	if got := strings.Count(SelectRuntimeContextQuery, outsidePrefilter); got != 4 {
		t.Errorf("outside-closure prefilter occurrences = %d, want 4 (one per kind)", got)
	}

	for _, kind := range RuntimeContextFactKinds {
		want := "fact.fact_kind = '" + kind + "'"
		if got := strings.Count(SelectRuntimeContextQuery, want); got != 1 {
			t.Errorf("outside arm for kind %q occurrences = %d, want 1", kind, got)
		}
		if !strings.Contains(SelectRuntimeContextQuery, "'"+kind+"'") {
			t.Errorf("closure arm does not cover kind %q", kind)
		}
	}
	if len(RuntimeContextFactKinds) != 4 {
		t.Errorf("RuntimeContextFactKinds = %d kinds, want 4 (closed kind set)", len(RuntimeContextFactKinds))
	}

	if strings.Contains(SelectRuntimeContextQuery, "fact.fact_kind = ANY(") {
		t.Error("query still filters kinds by parameter; kinds must be inline literals so the planner can prove kind-partial index predicates")
	}
}

// TestSupplyChainRuntimeContextClosureScopes pins the closure derivation
// the scope-closure arm probes and the outside arms exclude: trimmed
// candidates plus their git-repository-scope envelope forms, deduplicated,
// blanks dropped.
func TestSupplyChainRuntimeContextClosureScopes(t *testing.T) {
	t.Parallel()

	got := supplyChainRuntimeContextClosureScopes([]string{
		"repository:r_a",
		"  repository:r_b  ",
		"",
		"   ",
		"repository:r_a",
	})
	want := []string{
		"repository:r_a",
		"git-repository-scope:repository:r_a",
		"repository:r_b",
		"git-repository-scope:repository:r_b",
	}
	if len(got) != len(want) {
		t.Fatalf("closure scopes = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("closure scopes = %v, want %v", got, want)
		}
	}
	if got := supplyChainRuntimeContextClosureScopes(nil); len(got) != 0 {
		t.Fatalf("nil closure scopes = %v, want empty", got)
	}
}
