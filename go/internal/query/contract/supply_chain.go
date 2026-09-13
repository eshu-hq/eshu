// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package contract

import (
	supplychain "github.com/eshu-hq/eshu/go/internal/query/supply/chain"
	"github.com/eshu-hq/eshu/go/internal/query/supply/chain/advisory"
)

func init() {
	register(vulnerabilityScannerReadContractCapability, supplychain.LightweightExactSupport())
	register(sbomAttestationAttachmentsCapability, supplychain.AuthoritativeExactSupport())
	register(advisory.EvidenceCapability, capabilitySupport{
		LocalLightweightMax:   nil,
		LocalAuthoritativeMax: &truthExact,
		LocalFullStackMax:     &truthExact,
		ProductionMax:         &truthExact,
		RequiredProfile:       ProfileLocalAuthoritative,
	})
	register(advisory.CatalogCapability, capabilitySupport{
		LocalLightweightMax:   nil,
		LocalAuthoritativeMax: &truthExact,
		LocalFullStackMax:     &truthExact,
		ProductionMax:         &truthExact,
		RequiredProfile:       ProfileLocalAuthoritative,
	})
	register(supplyChainImpactFindingsCapability, supplychain.AuthoritativeExactSupport())
	register(supplyChainImpactExplanationCapability, supplychain.AuthoritativeExactSupport())
	register(containerImageIdentitiesCapability, supplychain.AuthoritativeExactSupport())
	register(securityAlertReconciliationsCapability, supplychain.AuthoritativeExactSupport())
	register(supplyChainImpactAggregateCapability, supplychain.AuthoritativeExactSupport())
	register(securityAlertReconciliationAggregateCapability, supplychain.AuthoritativeExactSupport())
	register(containerImageIdentityAggregateCapability, supplychain.AuthoritativeExactSupport())
	register(sbomAttestationAttachmentAggregateCapability, supplychain.AuthoritativeExactSupport())
}
