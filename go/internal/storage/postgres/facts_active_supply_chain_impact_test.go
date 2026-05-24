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
		"fact.payload->>'package_id' = ANY($1::text[])",
		"fact.payload->>'purl' = ANY($2::text[])",
		"fact.payload->>'cve_id' = ANY($3::text[])",
		"fact.payload->>'subject_digest' = ANY($4::text[])",
		"fact.payload->>'cpe' = ANY($5::text[])",
		"fact.payload->>'criteria' = ANY($5::text[])",
		"fact.payload->>'document_id' = ANY($6::text[])",
		"fact.fact_id > $7",
		"LIMIT $8",
	} {
		if !strings.Contains(listActiveSupplyChainImpactFactsQuery, want) {
			t.Fatalf("listActiveSupplyChainImpactFactsQuery missing %q:\n%s", want, listActiveSupplyChainImpactFactsQuery)
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
		"fact_records_vulnerability_affected_package_lookup_idx",
		"fact_records_vulnerability_affected_product_lookup_idx",
		"fact_records_sbom_component_purl_idx",
		"fact_records_sbom_component_cpe_idx",
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
