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

func TestListActiveSupplyChainImpactFactsQueryIsPackageBoundedAndPaged(t *testing.T) {
	t.Parallel()

	for _, want := range []string{
		"scope.active_generation_id = fact.generation_id",
		"generation.status = 'active'",
		"fact.is_tombstone = FALSE",
		"fact.fact_kind IN (",
		"'vulnerability.cve'",
		"'vulnerability.affected_package'",
		"'vulnerability.affected_product'",
		"'security_alert.repository_alert'",
		"'reducer_ci_cd_run_correlation'",
		"'reducer_platform_materialization'",
		"'reducer_service_catalog_correlation'",
		"'reducer_workload_identity'",
		"'oci_registry.image_manifest'",
		"'oci_registry.image_index'",
		"'oci_registry.image_tag_observation'",
		"'oci_registry.image_referrer'",
		"fact.payload->>'package_id' = ANY($1::text[])",
		"fact.payload->>'purl' = ANY($2::text[])",
		"fact.payload->>'cve_id' = ANY($3::text[])",
		"cardinality($4::text[]) > 0",
		"fact.payload->>'advisory_id' = ANY($4::text[])",
		"fact.payload->>'subject_digest' = ANY($5::text[])",
		"fact.payload->>'artifact_digest' = ANY($5::text[])",
		"fact.payload->>'referrer_digest' = ANY($5::text[])",
		"fact.payload->>'resolved_digest' = ANY($5::text[])",
		"fact.payload->>'cpe' = ANY($6::text[])",
		"fact.payload->>'criteria' = ANY($6::text[])",
		"fact.payload->>'document_id' = ANY($7::text[])",
		"fact.payload->>'repository_id' = ANY($8::text[])",
		"fact.payload->>'image_ref' = ANY($9::text[])",
		"fact.fact_id > $11",
		"LIMIT $12",
	} {
		if !strings.Contains(listActiveSupplyChainImpactFactsQuery, want) {
			t.Fatalf("listActiveSupplyChainImpactFactsQuery missing %q:\n%s", want, listActiveSupplyChainImpactFactsQuery)
		}
	}
}

func TestListActiveSupplyChainImpactFactsQueryIncludesVulnerabilitySuppression(t *testing.T) {
	t.Parallel()

	// vulnerability.suppression facts must be expandable through the same
	// bounded active-evidence walk as the rest of the supply-chain impact
	// kinds; otherwise suppressions outside the initially loaded
	// scope/generation never reach the reducer and operator-authored
	// suppressions silently miss findings.
	//
	// The scope-nested predicates below are the round-3 review F-6
	// lower(btrim(...)) normalized shape ($16-$19), which SUPERSEDES (not
	// supplements) the original exact-match ->'scope'->>'X' = ANY($1-$5)
	// shape: $16-$19 bind lower(btrim(...)) of the SAME filter values
	// bound to $1/$2/$3/$5, so every row the old exact-match predicate
	// could select, the normalized predicate also selects, plus payloads
	// whose case/whitespace differs from the filter (see
	// TestListActiveSupplyChainImpactFactsQueryNormalizesSuppressionScopeSiblings,
	// facts_active_supply_chain_impact_scope_normalize_test.go, for the
	// full F-6 predicate-shape and unchanged-sibling assertions). $4
	// (AdvisoryIDs, #5465) stays exact-match -- round-3 predates
	// advisory_id support on this branch.
	for _, want := range []string{
		"'vulnerability.suppression'",
		"fact.payload->'scope'->>'advisory_id' = ANY($4::text[])",
		"lower(btrim(fact.payload->'scope'->>'package_id', E' \\t\\n\\v\\f\\r')) = ANY($16::text[])",
		"lower(btrim(fact.payload->'scope'->>'purl', E' \\t\\n\\v\\f\\r')) = ANY($17::text[])",
		"lower(btrim(fact.payload->'scope'->>'cve_id', E' \\t\\n\\v\\f\\r')) = ANY($18::text[])",
		"lower(btrim(fact.payload->'scope'->>'subject_digest', E' \\t\\n\\v\\f\\r')) = ANY($19::text[])",
	} {
		if !strings.Contains(listActiveSupplyChainImpactFactsQuery, want) {
			t.Fatalf("listActiveSupplyChainImpactFactsQuery missing %q:\n%s", want, listActiveSupplyChainImpactFactsQuery)
		}
	}
}

func TestListActiveSupplyChainImpactFactsQueryMatchesSuppressionByEnvironmentWorkloadService(t *testing.T) {
	t.Parallel()

	// #5466 P0 follow-up: a vulnerability.suppression fact scoped ONLY by
	// environment/workload_id/service_id (no cve_id/advisory_id/package_id/
	// purl/subject_digest/repository_id at all) must still be selectable by
	// this query. Before this predicate existed, such a suppression could
	// never enter the reducer's working set: the scope struct and matcher
	// (go/internal/reducer/supply_chain_suppression*.go) would accept it, but
	// nothing in this WHERE clause could ever match it.
	for _, want := range []string{
		"fact.fact_kind = 'vulnerability.suppression'",
		"lower(btrim(fact.payload->'scope'->>'workload_id', E' \\t\\n\\v\\f\\r')) = ANY($13::text[])",
		"lower(btrim(fact.payload->'scope'->>'service_id', E' \\t\\n\\v\\f\\r')) = ANY($14::text[])",
		"lower(btrim(fact.payload->'scope'->>'environment', E' \\t\\n\\v\\f\\r')) = ANY($15::text[])",
	} {
		if !strings.Contains(listActiveSupplyChainImpactFactsQuery, want) {
			t.Fatalf("listActiveSupplyChainImpactFactsQuery missing %q:\n%s", want, listActiveSupplyChainImpactFactsQuery)
		}
	}
}

func TestListActiveSupplyChainImpactFactsBindsWorkloadAndServiceIDsToDistinctPlaceholders(t *testing.T) {
	t.Parallel()

	// #5466 P2-1: $13 (WorkloadIDs) and $14 (ServiceIDs) have no other
	// coverage that can catch a bind-order swap in
	// listActiveSupplyChainImpactFactsPage --
	// TestListActiveSupplyChainImpactFactsQueryMatchesSuppressionByEnvironmentWorkloadService
	// only proves the SQL string mentions $13/$14, not that Go binds the
	// right filter field to each placeholder. Swapping
	// filter.WorkloadIDs/filter.ServiceIDs in that call would pass every
	// other test in the repo and silently make both anchors inert.
	db := &fakeExecQueryer{queryResponses: []queueFakeRows{{rows: nil}}}
	store := NewFactStore(db)

	_, _, err := store.ListActiveSupplyChainImpactFacts(context.Background(), reducer.SupplyChainImpactFactFilter{
		WorkloadIDs: []string{"Workload:X"},
		ServiceIDs:  []string{"Service:Y"},
	})
	if err != nil {
		t.Fatalf("ListActiveSupplyChainImpactFacts() error = %v, want nil", err)
	}
	if got, want := len(db.queries), 1; got != want {
		t.Fatalf("query count = %d, want %d", got, want)
	}

	// args are 0-indexed; $13 is args[12] and $14 is args[13].
	workloadArg, ok := db.queries[0].args[12].([]string)
	if !ok {
		t.Fatalf("$13 (index 12) arg type = %T, want []string", db.queries[0].args[12])
	}
	if got, want := workloadArg, []string{"workload:x"}; !slices.Equal(got, want) {
		t.Fatalf("$13 (WorkloadIDs) arg = %v, want %v", got, want)
	}

	serviceArg, ok := db.queries[0].args[13].([]string)
	if !ok {
		t.Fatalf("$14 (index 13) arg type = %T, want []string", db.queries[0].args[13])
	}
	if got, want := serviceArg, []string{"service:y"}; !slices.Equal(got, want) {
		t.Fatalf("$14 (ServiceIDs) arg = %v, want %v", got, want)
	}
}

func TestExpandEnvironmentAliasFilterValues(t *testing.T) {
	t.Parallel()

	got := expandEnvironmentAliasFilterValues([]string{"prod", "qa", "unknown-env"})
	want := []string{"prod", "production", "qa", "unknown-env"}
	if !slices.Equal(got, want) {
		t.Fatalf("expandEnvironmentAliasFilterValues() = %v, want %v", got, want)
	}
}

// TestExpandEnvironmentAliasFilterValuesCanonicalizesNonCanonicalInput is
// the #5466 round-2 review F-2 fix proof: the function must not assume its
// input already went through environment.Canonical. A mixed-case value
// ("Prod") or an alias spelling passed in directly ("production", not the
// canonical "prod") must both canonicalize before the aliasesByCanonical
// lookup and expand to the identical alias set -- otherwise either input
// would miss the lookup, pass through unexpanded, and could then never
// equal the lower(trim(payload)) side of the SQL predicate.
func TestExpandEnvironmentAliasFilterValuesCanonicalizesNonCanonicalInput(t *testing.T) {
	t.Parallel()

	want := []string{"prod", "production"}
	for _, in := range [][]string{
		{"Prod"},
		{"production"},
		{" PRODUCTION "},
	} {
		got := expandEnvironmentAliasFilterValues(in)
		if !slices.Equal(got, want) {
			t.Fatalf("expandEnvironmentAliasFilterValues(%v) = %v, want %v", in, got, want)
		}
	}
}

func TestLowerCleanedStringFilterValues(t *testing.T) {
	t.Parallel()

	got := lowerCleanedStringFilterValues([]string{" Workload:X ", "workload:x", "SERVICE:Y"})
	want := []string{"service:y", "workload:x"}
	if !slices.Equal(got, want) {
		t.Fatalf("lowerCleanedStringFilterValues() = %v, want %v", got, want)
	}
}

func TestListActiveSupplyChainImpactFactsQueryBoundsRepositoryFollowUp(t *testing.T) {
	t.Parallel()

	for _, want := range []string{
		"OR (\n          fact.fact_kind IN (",
		"'vulnerability.suppression'",
		"'reducer_container_image_identity'",
		"'reducer_ci_cd_run_correlation'",
		"'reducer_platform_materialization'",
		"'reducer_service_catalog_correlation'",
		"'reducer_workload_identity'",
		"fact.payload->>'repository_id' = ANY($8::text[])",
		"fact.payload->>'repo_id' = ANY($8::text[])",
		// Round-3 review F-6: this was ->'scope'->>'repository_id' = ANY($8)
		// (exact match); $20 supersedes it with lower(btrim(...)) -- see
		// TestListActiveSupplyChainImpactFactsQueryNormalizesSuppressionScopeSiblings
		// (facts_active_supply_chain_impact_scope_normalize_test.go).
		"lower(btrim(fact.payload->'scope'->>'repository_id', E' \\t\\n\\v\\f\\r')) = ANY($20::text[])",
		"fact.scope_id = ANY($8::text[])",
		"fact.payload->>'scope_id' = ANY($8::text[])",
		"scope.source_key = ANY($8::text[])",
		"scope.payload->>'repo_id' = ANY($8::text[])",
		"scope.payload->>'id' = ANY($8::text[])",
	} {
		if !strings.Contains(listActiveSupplyChainImpactFactsQuery, want) {
			t.Fatalf("listActiveSupplyChainImpactFactsQuery missing %q:\n%s", want, listActiveSupplyChainImpactFactsQuery)
		}
	}
	if strings.Contains(listActiveSupplyChainImpactFactsQuery, "OR fact.payload->>'repository_id' = ANY($8::text[])") {
		t.Fatalf("repository_id follow-up must be fact-kind gated:\n%s", listActiveSupplyChainImpactFactsQuery)
	}
}

func TestListActiveSupplyChainImpactFactsQueryLoadsPackageConsumptionByRepository(t *testing.T) {
	t.Parallel()

	repositoryBranchStart := strings.Index(listActiveSupplyChainImpactFactsQuery, "fact.fact_kind IN (\n              'vulnerability.suppression'")
	if repositoryBranchStart < 0 {
		t.Fatalf("repository follow-up branch missing:\n%s", listActiveSupplyChainImpactFactsQuery)
	}
	repositoryBranchEnd := strings.Index(listActiveSupplyChainImpactFactsQuery[repositoryBranchStart:], "OR (\n          fact.fact_kind = 'file'")
	if repositoryBranchEnd < 0 {
		t.Fatalf("repository follow-up branch end missing:\n%s", listActiveSupplyChainImpactFactsQuery)
	}
	repositoryBranch := listActiveSupplyChainImpactFactsQuery[repositoryBranchStart : repositoryBranchStart+repositoryBranchEnd]
	for _, want := range []string{
		"'reducer_package_consumption_correlation'",
		"'reducer_container_image_identity'",
	} {
		if !strings.Contains(repositoryBranch, want) {
			t.Fatalf("repository follow-up branch must load %s rows:\n%s", want, repositoryBranch)
		}
	}
}

func TestListActiveSupplyChainImpactFactsQuerySeparatesParserFileFollowUp(t *testing.T) {
	t.Parallel()

	repositoryBranchStart := strings.Index(listActiveSupplyChainImpactFactsQuery, "fact.fact_kind IN (\n              'vulnerability.suppression'")
	if repositoryBranchStart < 0 {
		t.Fatalf("repository follow-up branch missing:\n%s", listActiveSupplyChainImpactFactsQuery)
	}
	repositoryBranchEnd := strings.Index(listActiveSupplyChainImpactFactsQuery[repositoryBranchStart:], "OR (\n          fact.fact_kind = 'file'")
	if repositoryBranchEnd < 0 {
		t.Fatalf("repository follow-up branch end missing:\n%s", listActiveSupplyChainImpactFactsQuery)
	}
	repositoryBranch := listActiveSupplyChainImpactFactsQuery[repositoryBranchStart : repositoryBranchStart+repositoryBranchEnd]
	if strings.Contains(repositoryBranch, "'file'") {
		t.Fatalf("repository follow-up branch must not load parser file facts:\n%s", repositoryBranch)
	}

	for _, want := range []string{
		"fact.fact_kind = 'file'",
		"ANY($10::text[])",
		"fact.payload->'parsed_file_data'->>'language'",
		"'javascript', 'jsx', 'typescript', 'tsx'",
		"fact.fact_id > $11",
		"LIMIT $12",
	} {
		if !strings.Contains(listActiveSupplyChainImpactFactsQuery, want) {
			t.Fatalf("listActiveSupplyChainImpactFactsQuery missing %q:\n%s", want, listActiveSupplyChainImpactFactsQuery)
		}
	}
}

func TestListActiveSecurityAlertReconciliationFactsQueryIsScopedAndPaged(t *testing.T) {
	t.Parallel()

	for _, want := range []string{
		"scope.active_generation_id = fact.generation_id",
		"generation.status = 'active'",
		"fact.is_tombstone = FALSE",
		"fact.fact_kind IN (",
		"'security_alert.repository_alert'",
		"'reducer_package_consumption_correlation'",
		"'reducer_supply_chain_impact_finding'",
		"fact.payload->>'repository_id' = ANY($1::text[])",
		"fact.payload->>'package_id' = ANY($2::text[])",
		"fact.payload->'cve_ids' ?| $3::text[]",
		"fact.payload->'ghsa_ids' ?| $4::text[]",
		"fact.payload->>'cve_id' = ANY($3::text[])",
		"fact.payload->>'advisory_id' = ANY($4::text[])",
		"fact.fact_id > $5",
		"LIMIT $6",
	} {
		if !strings.Contains(listActiveSecurityAlertReconciliationFactsQuery, want) {
			t.Fatalf("listActiveSecurityAlertReconciliationFactsQuery missing %q:\n%s", want, listActiveSecurityAlertReconciliationFactsQuery)
		}
	}
}

func TestBootstrapDefinitionsIncludeSupplyChainImpactFactIndexes(t *testing.T) {
	t.Parallel()

	var facts Definition
	for _, def := range BootstrapDefinitions() {
		if def.Name == "fact_records" {
			facts = def
			break
		}
	}
	if facts.Name == "" {
		t.Fatal("fact_records definition missing")
	}
	for _, want := range []string{
		"fact_records_supply_chain_impact_lookup_idx",
		"fact_records_supply_chain_impact_status_lookup_idx",
		"fact_records_supply_chain_impact_package_lookup_idx",
		"fact_records_supply_chain_impact_priority_lookup_idx",
		"fact_records_supply_chain_impact_repository_lookup_idx",
		"fact_records_vulnerability_affected_package_lookup_idx",
		"fact_records_vulnerability_affected_product_lookup_idx",
		"fact_records_sbom_component_purl_idx",
		"fact_records_sbom_component_cpe_idx",
		"fact_records_ci_cd_run_correlations_image_ref_idx",
	} {
		if !strings.Contains(facts.SQL, want) {
			t.Fatalf("Bootstrap SQL missing %q", want)
		}
	}

	// #3389: the supply-chain impact aggregate (GET
	// /api/v0/supply-chain/impact/findings/count) enumerates every active
	// reducer_supply_chain_impact_finding fact with no payload anchor in the
	// common "count everything" case, so it needs a partial index whose
	// leading key columns are the (scope_id, generation_id) join keys -- the
	// existing impact indexes all lead with a payload column and so cannot
	// bound the no-anchor enumeration to the fact_kind, forcing a whole-table
	// scan at collector scale.
	scopeBoundIdx := "CREATE INDEX IF NOT EXISTS fact_records_supply_chain_impact_active_scan_idx"
	if !strings.Contains(facts.SQL, scopeBoundIdx) {
		t.Fatalf("Bootstrap SQL missing scope-bound impact active-scan index %q:\n%s", scopeBoundIdx, facts.SQL)
	}
	impactScanSQL := facts.SQL[strings.Index(facts.SQL, scopeBoundIdx):] //nolint:gocritic // offBy1: the previous t.Fatalf guards the Index() != -1 case for scopeBoundIdx.
	for _, want := range []string{
		"(\n        scope_id,\n        generation_id,\n        fact_id ASC\n    )",
		"WHERE fact_kind = 'reducer_supply_chain_impact_finding'",
		"AND is_tombstone = FALSE",
	} {
		if !strings.Contains(impactScanSQL, want) {
			t.Fatalf("impact active-scan index missing %q:\n%s", want, impactScanSQL[:min(len(impactScanSQL), 400)])
		}
	}
	statusIndexStart := strings.Index(facts.SQL, "CREATE INDEX IF NOT EXISTS fact_records_supply_chain_impact_status_lookup_idx")
	if statusIndexStart < 0 {
		t.Fatal("supply-chain impact status index missing")
	}
	statusIndexSQL := facts.SQL[statusIndexStart:]
	statusColumn := strings.Index(statusIndexSQL, "(payload->>'impact_status')")
	cveColumn := strings.Index(statusIndexSQL, "(payload->>'cve_id')")
	if statusColumn < 0 {
		t.Fatalf("status index missing impact_status leading column: %s", statusIndexSQL)
	}
	if cveColumn >= 0 && cveColumn < statusColumn {
		t.Fatalf("status index should lead with impact_status, not cve_id: %s", statusIndexSQL)
	}
}

func TestBootstrapDefinitionsIncludeSecurityAlertReconciliationIndexes(t *testing.T) {
	t.Parallel()

	var facts Definition
	for _, def := range BootstrapDefinitions() {
		if def.Name == "fact_records" {
			facts = def
			break
		}
	}
	if facts.Name == "" {
		t.Fatal("fact_records definition missing")
	}
	for _, want := range []string{
		"fact_records_security_alert_repository_lookup_idx",
		"fact_records_security_alert_cve_ids_idx",
		"fact_records_security_alert_ghsa_ids_idx",
		"fact_records_security_alert_reconciliation_lookup_idx",
		"fact_records_security_alert_reconciliation_provider_repository_idx",
		"fact_records_security_alert_reconciliation_scope_idx",
		"fact_records_security_alert_reconciliation_provider_idx",
		"fact_records_security_alert_reconciliation_cve_ids_idx",
		"fact_records_security_alert_reconciliation_ghsa_ids_idx",
		"'security_alert.repository_alert'",
		"'reducer_security_alert_reconciliation'",
		"(payload->>'repository_id')",
		"(payload->>'provider')",
		"(payload->>'provider_repository_id')",
		"(payload->>'scope_id')",
		"(payload->>'package_id')",
		"(payload->>'provider_state')",
		"(payload->>'reconciliation_status')",
		"USING GIN ((payload->'cve_ids'))",
		"USING GIN ((payload->'ghsa_ids'))",
	} {
		if !strings.Contains(facts.SQL, want) {
			t.Fatalf("Bootstrap SQL missing %q", want)
		}
	}
}

func TestBootstrapDefinitionsIncludeAdvisoryCatalogActiveScanIndexes(t *testing.T) {
	t.Parallel()

	var facts Definition
	for _, def := range BootstrapDefinitions() {
		if def.Name == "fact_records" {
			facts = def
			break
		}
	}
	if facts.Name == "" {
		t.Fatal("fact_records definition missing")
	}

	// #3389: the advisory catalog (GET /api/v0/supply-chain/advisories)
	// enumerates every active vulnerability.cve fact (the catalog spine) and
	// every active vulnerability.known_exploited fact (the KEV CTE) with no
	// cve_id anchor, so it needs per-fact-kind partial indexes whose leading
	// key columns are the (scope_id, generation_id) join keys. The existing
	// fact_records_vulnerability_active_*_v2_idx indexes lead with a payload
	// column and span six fact kinds, so they cannot bound the single-kind
	// no-anchor enumeration to one kind's active tuples.
	checks := []struct {
		index   string
		factKnd string
	}{
		{"fact_records_vulnerability_cve_active_scan_idx", "WHERE fact_kind = 'vulnerability.cve'"},
		{"fact_records_vulnerability_known_exploited_active_scan_idx", "WHERE fact_kind = 'vulnerability.known_exploited'"},
		{"fact_records_vulnerability_affected_package_active_scan_idx", "WHERE fact_kind = 'vulnerability.affected_package'"},
	}
	for _, c := range checks {
		create := "CREATE INDEX IF NOT EXISTS " + c.index
		if !strings.Contains(facts.SQL, create) {
			t.Fatalf("Bootstrap SQL missing advisory catalog active-scan index %q", c.index)
		}
		idxSQL := facts.SQL[strings.Index(facts.SQL, create):] //nolint:gocritic // offBy1: the previous t.Fatalf guards the Index() != -1 case for `create`.
		for _, want := range []string{
			"(\n        scope_id,\n        generation_id,\n        fact_id ASC\n    )",
			c.factKnd,
			"AND is_tombstone = FALSE",
		} {
			if !strings.Contains(idxSQL, want) {
				t.Fatalf("index %q missing %q:\n%s", c.index, want, idxSQL[:min(len(idxSQL), 400)])
			}
		}
	}
}
