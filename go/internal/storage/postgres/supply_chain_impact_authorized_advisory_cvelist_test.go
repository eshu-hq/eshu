// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"slices"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/facts/supply/chain"
)

// The CVE List v5 speaks for its own registry class (eshu-hq/eshu#9880): its
// vulnerability.cve facts attribute to cve_list through the payload source,
// and the authority admits exactly that kind. References ride along as
// evidence but are not authorized-impact kinds, the same way NVD references
// are not.
func TestAuthorizedImpactAdmitsCVEListCVE(t *testing.T) {
	t.Parallel()

	if got := authorizedImpactExpectedKinds("cve_list"); !slices.Equal(got, []string{"vulnerability.cve"}) {
		t.Fatalf("authorizedImpactExpectedKinds(cve_list) = %v, want [vulnerability.cve]", got)
	}
	if !authorizedImpactAllowedKinds("cve_list", `["vulnerability.cve"]`) {
		t.Fatal("cve_list authority must allow exactly [vulnerability.cve]")
	}
	envelope := facts.Envelope{
		FactKind:      "vulnerability.cve",
		SchemaVersion: chain.VulnerabilityIntelligenceSchemaVersionV1,
		Payload:       map[string]any{"advisory_id": "CVE-2026-9101", "source": "cve_list"},
	}
	if !advisorySourceClassMatches(envelope, "cve_list") {
		t.Fatal("a cve_list cve fact must match the cve_list authority")
	}
	for _, other := range []string{"nvd", "osv", "cisa_kev", "first_epss"} {
		if advisorySourceClassMatches(envelope, other) {
			t.Fatalf("a cve_list cve fact must not match the foreign %s authority", other)
		}
	}
}
