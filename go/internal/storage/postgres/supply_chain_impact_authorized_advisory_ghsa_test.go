// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/facts/supply/chain"
)

// TestAuthorizedImpactHasNoGHSAAuthority pins that a direct ghsa source cannot
// hold an authority in the protected scanner read: it has no expected kinds, so
// authorizedImpactAllowedKinds refuses every allowed-kind list, including the
// one osv holds (#10037).
func TestAuthorizedImpactHasNoGHSAAuthority(t *testing.T) {
	t.Parallel()

	if got := authorizedImpactExpectedKinds("ghsa"); got != nil {
		t.Fatalf("authorizedImpactExpectedKinds(ghsa) = %v, want nil", got)
	}
	for _, raw := range []string{
		`["vulnerability.cve","vulnerability.affected_package"]`,
		`["vulnerability.cve"]`,
		`[]`,
	} {
		if authorizedImpactAllowedKinds("ghsa", raw) {
			t.Errorf("authorizedImpactAllowedKinds(ghsa, %s) = true, want false", raw)
		}
	}
}

// TestAuthorizedImpactKeepsOSVCarriedGHSAIDs pins both sides of that exclusion
// for one advisory id: a fact a direct ghsa source declares matches no
// registrable authority, and the OSV copy of the same GHSA id stays an osv fact,
// so it keeps flowing through a registered osv scope.
func TestAuthorizedImpactKeepsOSVCarriedGHSAIDs(t *testing.T) {
	t.Parallel()

	envelope := func(kind, source string) facts.Envelope {
		return facts.Envelope{
			FactKind:      kind,
			SchemaVersion: chain.VulnerabilityIntelligenceSchemaVersionV1,
			Payload:       map[string]any{"advisory_id": "GHSA-xxxx-xxxx-xxxx", "source": source},
		}
	}
	for _, kind := range authorizedImpactExpectedKinds("osv") {
		for _, class := range []string{"osv", "nvd", "cisa_kev", "first_epss"} {
			if advisorySourceClassMatches(envelope(kind, "ghsa"), class) {
				t.Errorf("%s declared by source ghsa matches the %s authority", kind, class)
			}
		}
		if !advisorySourceClassMatches(envelope(kind, "osv"), "osv") {
			t.Errorf("%s of a GHSA id declared by source osv does not match the osv authority", kind)
		}
	}
}
