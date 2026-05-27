package postgres

import (
	"strings"
	"testing"
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
		"'reducer_service_catalog_correlation'",
		"'reducer_workload_identity'",
		"fact.payload->>'package_id' = ANY($1::text[])",
		"fact.payload->>'purl' = ANY($2::text[])",
		"fact.payload->>'cve_id' = ANY($3::text[])",
		"fact.payload->>'subject_digest' = ANY($4::text[])",
		"fact.payload->>'artifact_digest' = ANY($4::text[])",
		"fact.payload->>'cpe' = ANY($5::text[])",
		"fact.payload->>'criteria' = ANY($5::text[])",
		"fact.payload->>'document_id' = ANY($6::text[])",
		"fact.payload->>'repository_id' = ANY($7::text[])",
		"fact.payload->>'image_ref' = ANY($8::text[])",
		"fact.fact_id > $9",
		"LIMIT $10",
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
	for _, want := range []string{
		"'vulnerability.suppression'",
		"fact.payload->'scope'->>'package_id' = ANY($1::text[])",
		"fact.payload->'scope'->>'purl' = ANY($2::text[])",
		"fact.payload->'scope'->>'cve_id' = ANY($3::text[])",
		"fact.payload->'scope'->>'subject_digest' = ANY($4::text[])",
	} {
		if !strings.Contains(listActiveSupplyChainImpactFactsQuery, want) {
			t.Fatalf("listActiveSupplyChainImpactFactsQuery missing %q:\n%s", want, listActiveSupplyChainImpactFactsQuery)
		}
	}
}

func TestListActiveSupplyChainImpactFactsQueryBoundsRepositoryFollowUp(t *testing.T) {
	t.Parallel()

	for _, want := range []string{
		"OR (\n          fact.fact_kind IN (",
		"'vulnerability.suppression',\n              'reducer_container_image_identity'",
		"'reducer_ci_cd_run_correlation',\n              'reducer_service_catalog_correlation'",
		"'reducer_workload_identity'",
		"fact.payload->>'repository_id' = ANY($7::text[])",
		"fact.payload->'scope'->>'repository_id' = ANY($7::text[])",
		"fact.scope_id = ANY($7::text[])",
		"fact.payload->>'scope_id' = ANY($7::text[])",
		"scope.payload->>'repo_id' = ANY($7::text[])",
		"scope.payload->>'id' = ANY($7::text[])",
	} {
		if !strings.Contains(listActiveSupplyChainImpactFactsQuery, want) {
			t.Fatalf("listActiveSupplyChainImpactFactsQuery missing %q:\n%s", want, listActiveSupplyChainImpactFactsQuery)
		}
	}
	if strings.Contains(listActiveSupplyChainImpactFactsQuery, "OR fact.payload->>'repository_id' = ANY($7::text[])") {
		t.Fatalf("repository_id follow-up must be fact-kind gated:\n%s", listActiveSupplyChainImpactFactsQuery)
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
		"fact_records_security_alert_reconciliation_provider_idx",
		"fact_records_security_alert_reconciliation_cve_ids_idx",
		"fact_records_security_alert_reconciliation_ghsa_ids_idx",
		"'security_alert.repository_alert'",
		"'reducer_security_alert_reconciliation'",
		"(payload->>'repository_id')",
		"(payload->>'provider')",
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
