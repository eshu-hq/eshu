// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"strings"
	"testing"
)

func TestBootstrapDefinitionsIncludeAdvisoryEvidenceReadIndexes(t *testing.T) {
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
		"fact_records_vulnerability_active_cve_lookup_v2_idx",
		"fact_records_vulnerability_active_advisory_lookup_v2_idx",
		"fact_records_vulnerability_active_ghsa_lookup_v2_idx",
		"fact_records_vulnerability_package_purl_lookup_idx",
		"(payload->>'cve_id'), scope_id, generation_id",
		"fact_kind IN (",
		"'vulnerability.cve'",
		"'vulnerability.reference'",
	} {
		if !strings.Contains(facts.SQL, want) {
			t.Fatalf("Bootstrap SQL missing advisory evidence index fragment %q", want)
		}
	}
}
