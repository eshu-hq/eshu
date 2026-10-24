// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	producerreadiness "github.com/eshu-hq/eshu/go/internal/storage/postgres/readiness/producer"
)

// TestBuildReducerSupplyChainSecurityHandlersWiresTheImpactProducerReadiness
// guards the #8827 wire. A nil securityalert.ImpactProducerReadiness means "no
// fence": every handler logic test stays green while production silently
// goes back to spending a counting attempt on a wait for supply_chain_impact.
func TestBuildReducerSupplyChainSecurityHandlersWiresTheImpactProducerReadiness(t *testing.T) {
	t.Parallel()

	handlers := buildReducerSupplyChainSecurityHandlers(nil, postgres.FactStore{}, nil, endpointPresenceWirings{})
	if handlers.SecurityAlertImpactProducerReadiness == nil {
		t.Fatal("SecurityAlertImpactProducerReadiness is nil: the reconciliation impact fence would ship inert")
	}
	if _, ok := handlers.SecurityAlertImpactProducerReadiness.(producerreadiness.ImpactStore); !ok {
		t.Fatalf("SecurityAlertImpactProducerReadiness = %T, want producerreadiness.ImpactStore", handlers.SecurityAlertImpactProducerReadiness)
	}
}
