// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import "github.com/eshu-hq/eshu/go/internal/truth"

// securityAlertReconciliationDomainDefinition returns the additive definition
// for provider alert reconciliation. The domain writes durable reducer facts
// for comparison state only; provider alert state is never impact truth.
func securityAlertReconciliationDomainDefinition() DomainDefinition {
	return DomainDefinition{
		Domain:  DomainSecurityAlertReconciliation,
		Summary: "compare provider repository security alerts with Eshu-owned dependency and impact evidence",
		Ownership: OwnershipShape{
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
