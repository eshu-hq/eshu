// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"fmt"
	"sort"
	"strings"
)

var knownDomains = map[Domain]struct{}{
	DomainWorkloadIdentity:                         {},
	DomainDeployableUnitCorrelation:                {},
	DomainCloudAssetResolution:                     {},
	DomainDeploymentMapping:                        {},
	DomainDataLineage:                              {},
	DomainCodeInterprocEvidence:                    {},
	DomainCodeTaintEvidence:                        {},
	DomainCodeFunctionSummary:                      {},
	DomainOwnership:                                {},
	DomainGovernance:                               {},
	DomainWorkloadMaterialization:                  {},
	DomainCodeCallMaterialization:                  {},
	DomainPlatformInfraMaterialization:             {},
	DomainSemanticEntityMaterialization:            {},
	DomainSQLRelationshipMaterialization:           {},
	DomainShellExecMaterialization:                 {},
	DomainInheritanceMaterialization:               {},
	DomainDocumentationMaterialization:             {},
	DomainRationaleMaterialization:                 {},
	DomainCodeownersOwnership:                      {},
	DomainSubmodulePin:                             {},
	DomainConfigStateDrift:                         {},
	DomainPackageSourceCorrelation:                 {},
	DomainCodeImportRepoEdge:                       {},
	DomainContainerImageIdentity:                   {},
	DomainCICDRunCorrelation:                       {},
	DomainServiceCatalogCorrelation:                {},
	DomainSBOMAttestationAttachment:                {},
	DomainSupplyChainImpact:                        {},
	DomainSecurityAlertReconciliation:              {},
	DomainSecretsIAMTrustChain:                     {},
	DomainAWSCloudRuntimeDrift:                     {},
	DomainMultiCloudRuntimeDrift:                   {},
	DomainAWSResourceMaterialization:               {},
	DomainGCPResourceMaterialization:               {},
	DomainAzureResourceMaterialization:             {},
	DomainGCPRelationshipMaterialization:           {},
	DomainAzureRelationshipMaterialization:         {},
	DomainWorkloadCloudRelationshipMaterialization: {},
	DomainEC2InstanceNodeMaterialization:           {},
	DomainAWSRelationshipMaterialization:           {},
	DomainAWSCloudImageMaterialization:             {},
	DomainObservabilityCoverageCorrelation:         {},
	DomainObservabilityCoverageMaterialization:     {},
	DomainKubernetesCorrelation:                    {},
	DomainKubernetesWorkloadMaterialization:        {},
	DomainKubernetesCorrelationMaterialization:     {},
	DomainKubernetesNamespaceMaterialization:       {},
	DomainCrossplaneSatisfiedByMaterialization:     {},
	DomainSecurityGroupCidrMaterialization:         {},
	DomainSecurityGroupRuleMaterialization:         {},
	DomainSecurityGroupReachabilityMaterialization: {},
	DomainIAMCanAssumeMaterialization:              {},
	DomainS3LogsToMaterialization:                  {},
	DomainS3ExternalPrincipalGrantMaterialization:  {},
	DomainRDSPostureMaterialization:                {},
	DomainEC2UsesProfileMaterialization:            {},
	DomainIAMInstanceProfileRoleMaterialization:    {},
	DomainEC2InternetExposureMaterialization:       {},
	DomainEC2BlockDeviceKMSPostureMaterialization:  {},
	DomainS3InternetExposureMaterialization:        {},
	DomainIAMEscalationMaterialization:             {},
	DomainIAMCanPerformMaterialization:             {},
	DomainIncidentRoutingMaterialization:           {},
	DomainIncidentRepositoryCorrelation:            {},
	DomainSecretsIAMGraphProjection:                {},
	DomainCloudInventoryAdmission:                  {},
	DomainEshuSearchDocument:                       {},
}

// AllDomains returns every reducer-owned domain sorted lexicographically: the
// claim/materialization domains in knownDomains plus the shared/edge projection
// domains in allProjectionDomains. It is the single source of truth for tooling
// that must enumerate the full domain set (the capability surface inventory and
// its drift gate), so a domain added to either registry automatically appears in
// the inventory and cannot drain truth without being tracked. Duplicates across
// the two registries collapse to one entry.
func AllDomains() []Domain {
	set := make(map[Domain]struct{}, len(knownDomains)+len(allProjectionDomains))
	for domain := range knownDomains {
		set[domain] = struct{}{}
	}
	for _, domain := range allProjectionDomains {
		set[domain] = struct{}{}
	}
	domains := make([]Domain, 0, len(set))
	for domain := range set {
		domains = append(domains, domain)
	}
	sort.Slice(domains, func(i, j int) bool { return domains[i] < domains[j] })
	return domains
}

// ParseDomain converts one raw string into a known reducer domain.
func ParseDomain(raw string) (Domain, error) {
	domain := Domain(strings.TrimSpace(raw))
	if err := domain.Validate(); err != nil {
		return "", err
	}
	return domain, nil
}

// Validate checks that the reducer domain is explicit and known.
func (domain Domain) Validate() error {
	if strings.TrimSpace(string(domain)) == "" {
		return fmt.Errorf("domain must not be blank")
	}
	if _, ok := knownDomains[domain]; !ok {
		return fmt.Errorf("unknown reducer domain %q", domain)
	}
	return nil
}
