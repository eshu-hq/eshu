// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"github.com/eshu-hq/eshu/go/internal/query/supplychain"
	"github.com/eshu-hq/eshu/go/internal/query/supplychain/advisory"
)

func init() {
	capabilityMatrix[vulnerabilityScannerReadContractCapability] = supplychain.LightweightExactSupport()
	capabilityMatrix[sbomAttestationAttachmentsCapability] = supplychain.AuthoritativeExactSupport()
	capabilityMatrix[advisory.AdvisoryEvidenceCapability] = capabilitySupport{
		LocalLightweightMax:   nil,
		LocalAuthoritativeMax: &truthExact,
		LocalFullStackMax:     &truthExact,
		ProductionMax:         &truthExact,
		RequiredProfile:       ProfileLocalAuthoritative,
	}
	capabilityMatrix[advisory.AdvisoryCatalogCapability] = capabilitySupport{
		LocalLightweightMax:   nil,
		LocalAuthoritativeMax: &truthExact,
		LocalFullStackMax:     &truthExact,
		ProductionMax:         &truthExact,
		RequiredProfile:       ProfileLocalAuthoritative,
	}
	capabilityMatrix[supplyChainImpactFindingsCapability] = supplychain.AuthoritativeExactSupport()
	capabilityMatrix[supplyChainImpactExplanationCapability] = supplychain.AuthoritativeExactSupport()
	capabilityMatrix[containerImageIdentitiesCapability] = supplychain.AuthoritativeExactSupport()
	capabilityMatrix[securityAlertReconciliationsCapability] = supplychain.AuthoritativeExactSupport()
	capabilityMatrix[supplyChainImpactAggregateCapability] = supplychain.AuthoritativeExactSupport()
	capabilityMatrix[securityAlertReconciliationAggregateCapability] = supplychain.AuthoritativeExactSupport()
	capabilityMatrix[containerImageIdentityAggregateCapability] = supplychain.AuthoritativeExactSupport()
	capabilityMatrix[sbomAttestationAttachmentAggregateCapability] = supplychain.AuthoritativeExactSupport()
}
