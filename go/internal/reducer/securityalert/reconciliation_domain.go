// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package securityalert

import (
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/truth"
)

// SecurityAlertReconciliationDomainDefinition returns the additive definition
// for provider alert reconciliation. The domain writes durable reducer facts
// for comparison state only; provider alert state is never impact truth.
// Exported so the reducer root's registration forwarder
// (defaults_additive_domains_supply_chain.go) can call it (issue #6061).
func SecurityAlertReconciliationDomainDefinition() reducercontract.DomainDefinition {
	return reducercontract.DomainDefinition{
		Domain:  reducercontract.DomainSecurityAlertReconciliation,
		Summary: "compare provider repository security alerts with Eshu-owned dependency and impact evidence",
		Ownership: reducercontract.OwnershipShape{
			CrossSource:    true,
			CrossScope:     true,
			CanonicalWrite: true,
		},
		TruthContract: truth.Contract{
			CanonicalKind: "security_alert_reconciliation",
			SourceLayers: []truth.Layer{
				truth.LayerSourceDeclaration,
				truth.LayerObservedResource,
			},
		},
	}
}
