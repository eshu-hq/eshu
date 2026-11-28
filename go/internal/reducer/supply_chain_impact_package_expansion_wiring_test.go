// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"testing"

	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
)

// TestSupplyChainImpactRegistrationEnablesPackageExpansionBound pins the
// #11097 production wiring: the registered handler carries the provenance
// bound on package-to-CVE transitive expansion independently of
// CriteriaExpansionBound. Drop the line and every logic test stays green while
// production reverts to unbounded package fan-out, so the wiring needs its own
// test. Rollback is the same one line in reverse: nil restores the prior
// semantics.
func TestSupplyChainImpactRegistrationEnablesPackageExpansionBound(t *testing.T) {
	t.Parallel()

	definitions := appendSupplyChainCorrelationAdditiveDomains(nil, DefaultHandlers{
		FactLoader: &stubSupplyChainImpactFactLoader{},
		SupplyChainSecurityHandlers: SupplyChainSecurityHandlers{
			SupplyChainImpactWriter:             &recordingSupplyChainImpactWriter{},
			SupplyChainImpactFencingTokenIssuer: fixedSupplyChainImpactFencingTokenIssuer{token: 1},
		},
	})

	for _, definition := range definitions {
		if definition.Domain != DomainSupplyChainImpact {
			continue
		}
		handler, ok := definition.Handler.(supplychaincore.SupplyChainImpactHandler)
		if !ok {
			t.Fatalf("handler for %s = %T, want supplychaincore.SupplyChainImpactHandler", definition.Domain, definition.Handler)
		}
		if handler.PackageExpansionBound == nil {
			t.Fatal("PackageExpansionBound is nil on the registered handler: production would run the unbounded package fan-out")
		}
		return
	}
	t.Fatalf("no %s registration found", DomainSupplyChainImpact)
}
