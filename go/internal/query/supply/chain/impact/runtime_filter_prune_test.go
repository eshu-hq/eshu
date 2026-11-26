// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// sprintfRuntimeFilterCTE renders the shipped filter template directly: the
// pre-pruning builder output, derived from the shipped constant rather than
// hand-copied, so the byte-identity test tracks production text.
func sprintfRuntimeFilterCTE(serviceParam, workloadParam, environmentParam, allowedRepositoriesParam, allowedScopesParam string) string {
	return fmt.Sprintf(
		supplyChainImpactRuntimeFilterCTESQL,
		serviceParam,
		workloadParam,
		environmentParam,
		allowedRepositoriesParam,
		allowedScopesParam,
		supplyChainRuntimeRepositoryDecoderJoin(
			"runtime_fact.payload",
			"runtime_fact.scope_id",
			"runtime_repository",
		),
	)
}

// TestSupplyChainImpactRuntimeFilterPrunesLiteralEmptyEnvironmentDimension
// is the failing-first shape proof for eshu-hq/eshu#11493 (arbiter ruling
// 7178944958, decision 1): the only build-time-known empty dimension, the
// explain literal "”" environment, must not scan fact_records. The pruned
// dimension is an empty stub CTE with identical columns; its
// runtime_filter_candidates leg stays so the downstream CTEs, the $N
// placeholder set, the filter_kind values, and the outer AND semantics are
// untouched. Red on shipped code, which emits the full environment
// dimension scan for the explain "”" environment.
func TestSupplyChainImpactRuntimeFilterPrunesLiteralEmptyEnvironmentDimension(t *testing.T) {
	t.Parallel()

	pruned := supplyChainImpactRuntimeFilterCTE("$9", "$8", "''", "$11", "$12")

	for _, want := range []string{
		"runtime_environment_matches AS MATERIALIZED (",
		"runtime_environment_matches AS MATERIALIZED (\n  SELECT scope_id, generation_id, payload",
		"FROM fact_records\n  WHERE FALSE",
		// The stubbed dimension keeps its candidates leg: granularity is the
		// whole dimension (arbiter decision 2), never an intra-CTE arm, and
		// the leg is never omitted alone.
		"FROM runtime_environment_matches AS runtime_fact",
		"'environment'::text AS filter_kind",
		// The live dimensions are untouched.
		"runtime_workload_identity_matches AS MATERIALIZED (",
		"runtime_workload_catalog_matches AS MATERIALIZED (",
		"runtime_service_matches AS MATERIALIZED (",
		"BTRIM(payload->>'workload_id')",
		"BTRIM(payload->>'service_id')",
		// The guards that make removal unsafe stay exactly as shipped.
		"NOT COALESCE(",
	} {
		if !strings.Contains(pruned, want) {
			t.Fatalf("pruned explain filter CTE missing %q:\n%s", want, pruned)
		}
	}
	for _, forbidden := range []string{
		"payload->>'environment'",
		"reducer_ci_cd_run_correlation",
	} {
		if strings.Contains(pruned, forbidden) {
			t.Fatalf("pruned explain filter CTE still scans the empty environment dimension (%q):\n%s", forbidden, pruned)
		}
	}

	// Every explain entry point builds its filter with the literal "''"
	// environment, so every one must carry the stub.
	for name, query := range map[string]string{
		"legacy explain":        ExplainFindingQuery,
		"public-id explain":     ExplainFindingByPublicIDQuery,
		"supports explain":      ExplainFindingFromSupportsQuery,
		"supports public-id":    ExplainFindingByPublicIDFromSupportsQuery,
		"supports image":        ExplainFindingFromImageQuery,
		"decide-first cve":      ExplainFindingUngrantedByKeyQuery[explainKeyCVE],
		"decide-first advisory": ExplainFindingUngrantedByKeyQuery[explainKeyAdvisory],
	} {
		if !strings.Contains(query, "WHERE FALSE") {
			t.Fatalf("%s does not carry the pruned empty environment dimension", name)
		}
		if strings.Contains(query, "payload->>'environment'") {
			t.Fatalf("%s still scans the empty environment dimension", name)
		}
	}
}

// TestSupplyChainImpactRuntimeFilterKeepsBindDimensionsByteIdentical pins
// the other half of the ruling bar: a dimension whose parameter is a bind
// expression ($N, emptiness unknowable at SQL-build time) must emit exactly
// the shipped text. Per-request value-aware pruning is blocked (arbiter
// decision 1); this test fails any attempt to thread request values into
// the builder.
func TestSupplyChainImpactRuntimeFilterKeepsBindDimensionsByteIdentical(t *testing.T) {
	t.Parallel()

	for _, params := range [][5]string{
		{"$9", "$10", "$11", "$22", "$23"},
		{"$8", "$9", "$10", "$18", "$19"},
		{"$9", "$8", "''", "$11", "$12"},
	} {
		// The "''" row is covered by the pruning test above; here it only
		// proves the non-pruned dimensions around it are untouched.
		got := supplyChainImpactRuntimeFilterCTE(params[0], params[1], params[2], params[3], params[4])
		if params[2] != "''" {
			want := sprintfRuntimeFilterCTE(params[0], params[1], params[2], params[3], params[4])
			if got != want {
				t.Fatalf("bind-parameter filter CTE differs from shipped text:\n--- got ---\n%s\n--- want ---\n%s", got, want)
			}
		}
		assertRuntimeFilterPlaceholdersEqual(t, got, sprintfRuntimeFilterCTE(params[0], params[1], params[2], params[3], params[4]))
	}
}

var runtimeFilterPlaceholderPattern = regexp.MustCompile(`\$(\d+)`)

// assertRuntimeFilterPlaceholdersEqual pins placeholder-set identity between
// two filter CTE texts: pruning must add or remove no $N, so the arg slices
// every store binds stay valid.
func assertRuntimeFilterPlaceholdersEqual(t *testing.T, got, want string) {
	t.Helper()
	set := func(text string) []string {
		matches := runtimeFilterPlaceholderPattern.FindAllString(text, -1)
		seen := map[string]bool{}
		var out []string
		for _, match := range matches {
			if !seen[match] {
				seen[match] = true
				out = append(out, match)
			}
		}
		sort.Strings(out)
		return out
	}
	gotSet, wantSet := set(got), set(want)
	if strings.Join(gotSet, ",") != strings.Join(wantSet, ",") {
		t.Fatalf("placeholder sets differ: got [%s], want [%s]", strings.Join(gotSet, ","), strings.Join(wantSet, ","))
	}
}
