// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"slices"
	"strings"
	"testing"

	factimpact "github.com/eshu-hq/eshu/go/internal/storage/postgres/facts/impact"
)

// The #8880 branch catalog replaces the single pre-#8880 statement. These
// tests re-derive that statement's shape contracts over every statement the
// loader can issue.

const supplyChainImpactActiveJoin = `JOIN ingestion_scopes AS scope
      ON scope.scope_id = fact.scope_id
     AND scope.active_generation_id = fact.generation_id
    JOIN scope_generations AS generation
      ON generation.scope_id = fact.scope_id
     AND generation.generation_id = fact.generation_id
    WHERE generation.status = 'active'`

// supplyChainImpactCatalogSQL joins every branch statement for text checks.
func supplyChainImpactCatalogSQL() string {
	var parts []string
	for _, branch := range factimpact.Branches() {
		parts = append(parts, branch.SQL)
	}
	return strings.Join(parts, "\n;\n")
}

func supplyChainImpactBranch(t *testing.T, name string) factimpact.Branch {
	t.Helper()
	for _, branch := range factimpact.Branches() {
		if branch.Name == name {
			return branch
		}
	}
	t.Fatalf("catalog has no branch %s", name)
	return factimpact.Branch{}
}

func TestListActiveSupplyChainImpactFactsQueryIsPackageBoundedAndPaged(t *testing.T) {
	t.Parallel()

	var kinds []string
	for _, branch := range factimpact.Branches() {
		for _, want := range []string{supplyChainImpactActiveJoin, "fact.is_tombstone = FALSE", "WITH candidate AS MATERIALIZED (", "active AS MATERIALIZED ("} {
			if !strings.Contains(branch.SQL, want) {
				t.Fatalf("branch %s missing %q:\n%s", branch.Name, want, branch.SQL)
			}
		}
		for _, kind := range branch.Kinds {
			if !slices.Contains(kinds, kind) {
				kinds = append(kinds, kind)
			}
		}
	}
	slices.Sort(kinds)
	retired := factimpact.KindsInRetiredOrder()
	slices.Sort(retired)
	if !slices.Equal(kinds, retired) {
		t.Fatalf("catalog kinds = %v, want the retired scanned set %v", kinds, retired)
	}
	all := sqlWithoutLineComments(supplyChainImpactCatalogSQL())
	for _, key := range []string{"package_id", "purl", "cve_id", "advisory_id", "subject_digest", "digest", "artifact_digest", "referrer_digest", "resolved_digest", "cpe", "criteria", "document_id", "repository_id", "image_ref"} {
		if !strings.Contains(all, "fact.payload->>'"+key+"' = ANY($") {
			t.Fatalf("no branch compares payload key %s", key)
		}
	}
	if !strings.Contains(factimpact.PayloadPageSQL, "WHERE fact.fact_id = ANY($1::text[])") {
		t.Fatalf("payload pages must fetch by primary key:\n%s", factimpact.PayloadPageSQL)
	}
	if supplyChainImpactPayloadPageSize != 500 {
		t.Fatalf("payload page size = %d, want 500", supplyChainImpactPayloadPageSize)
	}
}

func TestListActiveSupplyChainImpactFactsQuerySeparatesCanonicalIdentities(t *testing.T) {
	t.Parallel()

	for _, forbidden := range []string{"container_image_identity_current_support_facts_for(", "'reducer_container_image_identity'", "UNION ALL"} {
		if strings.Contains(supplyChainImpactCatalogSQL(), forbidden) {
			t.Fatalf("a legacy branch retains mixed identity operation %q", forbidden)
		}
	}
	if !strings.Contains(listCurrentContainerImageIdentitySupportFactsQuery, "container_image_identity_current_support_facts_for(") {
		t.Fatalf("separate supply-chain identity stream must use the support-grain function:\n%s", listCurrentContainerImageIdentitySupportFactsQuery)
	}
	if !strings.Contains(factimpact.PayloadPageSQL, "COALESCE(fact.source_uri, '') AS source_uri") ||
		!strings.Contains(factimpact.PayloadPageSQL, "COALESCE(fact.source_record_id, '') AS source_record_id") {
		t.Fatalf("payload page must keep the source_uri/source_record_id COALESCE")
	}
}

func TestListActiveSupplyChainImpactFactsQueryIncludesVulnerabilitySuppression(t *testing.T) {
	t.Parallel()

	// vulnerability.suppression facts must be expandable through the same
	// bounded active-evidence walk as the rest of the kinds, through the
	// normalized lower(btrim(...)) scope arms (#5466 round-3 F-6), which
	// SUPERSEDE the original exact ->'scope'->>'X' arms.
	suppression := supplyChainImpactBranch(t, "vulnerability_suppression")
	for _, key := range []string{"package_id", "purl", "cve_id", "subject_digest", "repository_id", "advisory_id"} {
		want := "lower(btrim(fact.payload->'scope'->>'" + key + "', " + suppressionScopeTrimCharactersSQL + ")) = ANY($"
		if !strings.Contains(suppression.SQL, want) {
			t.Fatalf("suppression branch missing normalized %s arm:\n%s", key, suppression.SQL)
		}
	}
	if regexpScopeExact := "fact.payload->'scope'->>'advisory_id' = ANY("; strings.Contains(supplyChainImpactCatalogSQL(), regexpScopeExact) {
		t.Fatalf("scope-nested advisory_id exact match must stay superseded by the normalized arm")
	}
}

func TestListActiveSupplyChainImpactFactsQueryDoesNotDiscoverSuppressionByDeploymentContext(t *testing.T) {
	t.Parallel()

	// Deployment context narrows a suppression selected through a
	// vulnerability identity. It must never become a discovery predicate in
	// any statement, the guard included.
	for _, forbidden := range []string{
		"fact.payload->'scope'->>'workload_id'",
		"fact.payload->'scope'->>'service_id'",
		"fact.payload->'scope'->>'environment'",
		"payload ? 'environment'", "payload ? 'workload_id'", "payload ? 'service_id'",
	} {
		if strings.Contains(supplyChainImpactCatalogSQL(), forbidden) {
			t.Fatalf("catalog contains deployment-only discovery predicate %q", forbidden)
		}
	}
}

func TestListActiveSupplyChainImpactFactsQueryBoundsRepositoryFollowUp(t *testing.T) {
	t.Parallel()

	// Repository arms (RepositoryIDs and its resolved scopes) may only select
	// the six repository kinds; in the guard they stay behind the kind gate.
	for _, branch := range factimpact.Branches() {
		usesRepository := slices.Contains(branch.Params, factimpact.RepositoryIDs) || slices.Contains(branch.Params, factimpact.RepositoryScopeIDs)
		if !usesRepository || branch.Shape == factimpact.ShapeGuard {
			continue
		}
		for _, kind := range branch.Kinds {
			if !slices.Contains(factimpact.RepositoryKinds(), kind) {
				t.Fatalf("branch %s binds repository arms for non-repository kind %s", branch.Name, kind)
			}
		}
	}
	guard := supplyChainImpactBranch(t, "foreign_key_guard")
	if !strings.Contains(guard.SQL, "(fact.fact_kind IN ('vulnerability.suppression', 'reducer_package_consumption_correlation', 'reducer_ci_cd_run_correlation', 'reducer_platform_materialization', 'reducer_service_catalog_correlation', 'reducer_workload_identity')\n      AND (fact.payload->>'repository_id' = ANY(") {
		t.Fatalf("guard repository arms must stay kind-gated:\n%s", guard.SQL)
	}
}

func TestListActiveSupplyChainImpactFactsQueryLoadsPackageConsumptionByRepository(t *testing.T) {
	t.Parallel()

	consumption := []string{"package_consumption_repository_id", "repository_payload_scope_id", "repository_scope"}
	for _, name := range consumption {
		if !slices.Contains(supplyChainImpactBranch(t, name).Kinds, factimpact.KindPackageConsumption) {
			t.Fatalf("branch %s must load package-consumption rows by repository", name)
		}
	}
	for _, want := range []string{"FROM container_image_identity_current_support_facts_for(", "$3::text[]", "$4::text[]", "$5::text[]"} {
		if !strings.Contains(listCurrentContainerImageIdentitySupportFactsQuery, want) {
			t.Fatalf("canonical repository follow-up missing %q", want)
		}
	}
}

func TestListActiveSupplyChainImpactFactsQuerySeparatesParserFileFollowUp(t *testing.T) {
	t.Parallel()

	for _, branch := range factimpact.Branches() {
		if !slices.Contains(branch.Kinds, factimpact.KindFile) {
			continue
		}
		switch branch.Name {
		case "file_repo_id", "file_scope":
			if !strings.Contains(branch.SQL, factimpact.JSTSLanguagePredicate) || len(branch.RunWhen) != 1 ||
				(branch.RunWhen[0] != factimpact.FileRepositoryIDs && branch.RunWhen[0] != factimpact.FileRepositoryScopeIDs) {
				t.Fatalf("file branch %s must be JS/TS-filtered and gated on file repositories only", branch.Name)
			}
		case "foreign_key_guard":
		default:
			t.Fatalf("branch %s may load parser file facts", branch.Name)
		}
	}
}

func sqlWithoutLineComments(query string) string {
	lines := strings.Split(query, "\n")
	for i, line := range lines {
		if commentStart := strings.Index(line, "--"); commentStart >= 0 {
			lines[i] = line[:commentStart]
		}
	}
	return strings.Join(lines, "\n")
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
