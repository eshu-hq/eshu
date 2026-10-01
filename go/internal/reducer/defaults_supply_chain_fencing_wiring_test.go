// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"testing"

	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
)

// fixedSupplyChainImpactFencingTokenIssuer issues one constant token.
type fixedSupplyChainImpactFencingTokenIssuer struct{ token int64 }

func (i fixedSupplyChainImpactFencingTokenIssuer) NextSupplyChainImpactFencingToken(context.Context) (int64, error) {
	return i.token, nil
}

func supplyChainImpactRegistration(definitions []DomainDefinition) (DomainDefinition, bool) {
	for _, definition := range definitions {
		if definition.Domain == DomainSupplyChainImpact {
			return definition, true
		}
	}
	return DomainDefinition{}, false
}

// TestSupplyChainImpactRegistrationRequiresTheFencingTokenIssuer pins #7142:
// a writer wired without the issuer must leave supply_chain_impact
// unregistered. Registering it would either fail every pass or invite a
// token-less write that leaves the stale-pass fence inert.
func TestSupplyChainImpactRegistrationRequiresTheFencingTokenIssuer(t *testing.T) {
	t.Parallel()

	definitions := appendSupplyChainCorrelationAdditiveDomains(nil, DefaultHandlers{
		FactLoader: &stubSupplyChainImpactFactLoader{},
		SupplyChainSecurityHandlers: SupplyChainSecurityHandlers{
			SupplyChainImpactWriter: &recordingSupplyChainImpactWriter{},
		},
	})
	if _, found := supplyChainImpactRegistration(definitions); found {
		t.Fatal("supply_chain_impact registered with a writer but no fencing-token issuer")
	}
}

// TestSupplyChainImpactRegistrationCarriesTheFencingTokenIssuer pins that the
// registered handler holds the issuer the registry was given, not a nil or a
// different one.
func TestSupplyChainImpactRegistrationCarriesTheFencingTokenIssuer(t *testing.T) {
	t.Parallel()

	issuer := fixedSupplyChainImpactFencingTokenIssuer{token: 77}
	definitions := appendSupplyChainCorrelationAdditiveDomains(nil, DefaultHandlers{
		FactLoader: &stubSupplyChainImpactFactLoader{},
		SupplyChainSecurityHandlers: SupplyChainSecurityHandlers{
			SupplyChainImpactWriter:             &recordingSupplyChainImpactWriter{},
			SupplyChainImpactFencingTokenIssuer: issuer,
		},
	})
	definition, found := supplyChainImpactRegistration(definitions)
	if !found {
		t.Fatal("supply_chain_impact not registered with a writer and an issuer")
	}
	handler, ok := definition.Handler.(supplychaincore.SupplyChainImpactHandler)
	if !ok {
		t.Fatalf("handler = %T, want supplychaincore.SupplyChainImpactHandler", definition.Handler)
	}
	if handler.FencingTokenIssuer != supplychaincore.SupplyChainImpactFencingTokenIssuer(issuer) {
		t.Fatal("registered handler does not carry the issuer passed to DefaultHandlers")
	}
	token, err := handler.FencingTokenIssuer.NextSupplyChainImpactFencingToken(context.Background())
	if err != nil || token != 77 {
		t.Fatalf("issuer token = %d, %v; want 77", token, err)
	}
}
