// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"strings"
	"testing"
)

func TestBootstrapDefinitionsIncludeAdvisoryEvidenceReadIndexes(t *testing.T) {
	t.Parallel()

	byName := map[string]string{}
	for _, def := range BootstrapDefinitions() {
		byName[def.Name] = def.SQL
	}
	facts, ok := byName["fact_records"]
	if !ok {
		t.Fatal("fact_records definition missing")
	}
	for _, want := range []string{
		"fact_records_vulnerability_active_cve_lookup_v2_idx",
		"fact_records_vulnerability_active_advisory_lookup_v2_idx",
		"fact_records_vulnerability_active_ghsa_lookup_v2_idx",
		"fact_records_vulnerability_package_purl_lookup_idx",
		"(payload->>'cve_id'), scope_id, generation_id",
		"fact_kind IN (",
		"'vulnerability.cve'",
		"'vulnerability.reference'",
	} {
		if !strings.Contains(facts, want) {
			t.Fatalf("Bootstrap SQL missing advisory evidence index fragment %q", want)
		}
	}
	// #9474: the five-kind package/purl slices ship in migrations 240-241
	// (the fact_records base is an immutable shipped migration), so pin
	// each five-kind partial index in its own migration definition: the
	// non-blocking build, the v2 leader shape, and the five-kind
	// predicate the new legs rely on.
	for definition, leg := range map[string]string{
		"fact_records_vulnerability_active_package_lookup_v2_idx": "(payload->>'package_id'), scope_id, generation_id, fact_kind, fact_id",
		"fact_records_vulnerability_active_purl_lookup_v2_idx":    "(payload->>'purl'), scope_id, generation_id, fact_kind, fact_id",
	} {
		migration, ok := byName[definition]
		if !ok {
			t.Fatalf("migration definition %q missing", definition)
		}
		for _, want := range []string{
			"CREATE INDEX CONCURRENTLY IF NOT EXISTS " + definition,
			leg,
			"fact_kind IN ('vulnerability.cve', 'vulnerability.affected_product', 'vulnerability.epss_score', 'vulnerability.known_exploited', 'vulnerability.reference')",
		} {
			if !strings.Contains(migration, want) {
				t.Fatalf("migration %q missing advisory evidence index fragment %q", definition, want)
			}
		}
	}
}
