// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package contract

import (
	"fmt"
	"sort"
	"strings"
)

// Domain identifies a reducer-owned truth or projection domain.
type Domain string

const (
	// DomainAWSCloudImageMaterialization owns AWS image relationship materialization.
	DomainAWSCloudImageMaterialization Domain = "aws_cloud_image_materialization"
	// DomainAWSCloudRuntimeDrift owns AWS runtime drift correlation.
	DomainAWSCloudRuntimeDrift Domain = "aws_cloud_runtime_drift"
	// DomainAWSRelationshipMaterialization owns AWS relationship materialization.
	DomainAWSRelationshipMaterialization Domain = "aws_relationship_materialization"
	// DomainAWSResourceMaterialization owns AWS resource materialization.
	DomainAWSResourceMaterialization Domain = "aws_resource_materialization"
	// DomainAzureRelationshipMaterialization owns Azure relationship materialization.
	DomainAzureRelationshipMaterialization Domain = "azure_relationship_materialization"
	// DomainAzureResourceMaterialization owns Azure resource materialization.
	DomainAzureResourceMaterialization Domain = "azure_resource_materialization"
	// DomainCICDRunCorrelation owns CI/CD run correlation.
	DomainCICDRunCorrelation Domain = "ci_cd_run_correlation"
	// DomainCloudAssetResolution owns cloud asset resolution.
	DomainCloudAssetResolution Domain = "cloud_asset_resolution"
	// DomainCloudInventoryAdmission owns cloud inventory admission.
	DomainCloudInventoryAdmission Domain = "cloud_inventory_admission"
	// DomainCodeCallMaterialization owns code-call materialization.
	DomainCodeCallMaterialization Domain = "code_call_materialization"
	// DomainCodeFunctionSummary owns code function summaries.
	DomainCodeFunctionSummary Domain = "code_function_summary"
	// DomainCodeImportRepoEdge owns code-import repository edges.
	DomainCodeImportRepoEdge Domain = "code_import_repo_edge"
	// DomainCodeInterprocEvidence owns interprocedural evidence.
	DomainCodeInterprocEvidence Domain = "code_interproc_evidence"
	// DomainCodeTaintEvidence owns code taint evidence.
	DomainCodeTaintEvidence Domain = "code_taint_evidence"
	// DomainCodeownersOwnership owns CODEOWNERS materialization.
	DomainCodeownersOwnership Domain = "codeowners_ownership"
	// DomainConfigStateDrift owns configuration-state drift correlation.
	DomainConfigStateDrift Domain = "config_state_drift"
	// DomainContainerImageIdentity owns container image identity.
	DomainContainerImageIdentity Domain = "container_image_identity"
	// DomainCrossplaneSatisfiedByMaterialization owns Crossplane claim edges.
	DomainCrossplaneSatisfiedByMaterialization Domain = "crossplane_satisfied_by_materialization"
	// DomainDataLineage reserves the data-lineage domain identifier.
	DomainDataLineage Domain = "data_lineage"
	// DomainDeployableUnitCorrelation owns deployable-unit correlation.
	DomainDeployableUnitCorrelation Domain = "deployable_unit_correlation"
	// DomainDeploymentMapping owns deployment mapping.
	DomainDeploymentMapping Domain = "deployment_mapping"
	// DomainDocumentationMaterialization owns documentation edge materialization.
	DomainDocumentationMaterialization Domain = "documentation_materialization"
	// DomainEC2BlockDeviceKMSPostureMaterialization owns EC2 block-device posture.
	DomainEC2BlockDeviceKMSPostureMaterialization Domain = "ec2_block_device_kms_posture_materialization"
	// DomainEC2InstanceIdentityMaterialization owns EC2 instance identity augmentation.
	DomainEC2InstanceIdentityMaterialization Domain = "ec2_instance_identity_materialization"
	// DomainEC2InstanceNodeMaterialization owns EC2 instance node materialization.
	DomainEC2InstanceNodeMaterialization Domain = "ec2_instance_node_materialization"
	// DomainEC2InternetExposureMaterialization owns EC2 exposure posture.
	DomainEC2InternetExposureMaterialization Domain = "ec2_internet_exposure_materialization"
	// DomainEC2UsesProfileMaterialization owns EC2 instance-profile edges.
	DomainEC2UsesProfileMaterialization Domain = "ec2_uses_profile_materialization"
	// DomainEshuSearchDocument owns curated search-document projection.
	DomainEshuSearchDocument Domain = "eshu_search_document"
	// DomainGCPRelationshipMaterialization owns GCP relationship materialization.
	DomainGCPRelationshipMaterialization Domain = "gcp_relationship_materialization"
	// DomainGCPResourceMaterialization owns GCP resource materialization.
	DomainGCPResourceMaterialization Domain = "gcp_resource_materialization"
	// DomainGovernance reserves the governance domain identifier.
	DomainGovernance Domain = "governance"
	// DomainIAMCanAssumeMaterialization owns IAM CAN_ASSUME edges.
	DomainIAMCanAssumeMaterialization Domain = "iam_can_assume_materialization"
	// DomainIAMCanPerformMaterialization owns IAM CAN_PERFORM edges.
	DomainIAMCanPerformMaterialization Domain = "iam_can_perform_materialization"
	// DomainIAMEscalationMaterialization owns IAM escalation edges.
	DomainIAMEscalationMaterialization Domain = "iam_escalation_materialization"
	// DomainIAMInstanceProfileRoleMaterialization owns instance-profile role edges.
	DomainIAMInstanceProfileRoleMaterialization Domain = "iam_instance_profile_role_materialization"
	// DomainIncidentRepositoryCorrelation owns incident-to-repository correlation.
	DomainIncidentRepositoryCorrelation Domain = "incident_repository_correlation"
	// DomainIncidentRoutingMaterialization owns incident-routing materialization.
	DomainIncidentRoutingMaterialization Domain = "incident_routing_materialization"
	// DomainInheritanceMaterialization owns inheritance edge materialization.
	DomainInheritanceMaterialization Domain = "inheritance_materialization"
	// DomainKubernetesCorrelation owns Kubernetes correlation.
	DomainKubernetesCorrelation Domain = "kubernetes_correlation"
	// DomainKubernetesCorrelationMaterialization owns Kubernetes correlation edges.
	DomainKubernetesCorrelationMaterialization Domain = "kubernetes_correlation_materialization"
	// DomainKubernetesNamespaceMaterialization owns Kubernetes namespace nodes.
	DomainKubernetesNamespaceMaterialization Domain = "kubernetes_namespace_materialization"
	// DomainKubernetesWorkloadMaterialization owns Kubernetes workload nodes.
	DomainKubernetesWorkloadMaterialization Domain = "kubernetes_workload_materialization"
	// DomainMultiCloudRuntimeDrift owns provider-neutral runtime drift.
	DomainMultiCloudRuntimeDrift Domain = "multi_cloud_runtime_drift"
	// DomainObservabilityCoverageCorrelation owns observability correlation.
	DomainObservabilityCoverageCorrelation Domain = "observability_coverage_correlation"
	// DomainObservabilityCoverageMaterialization owns observability coverage edges.
	DomainObservabilityCoverageMaterialization Domain = "observability_coverage_materialization"
	// DomainOwnership reserves the ownership domain identifier.
	DomainOwnership Domain = "ownership"
	// DomainPackageSourceCorrelation owns package-source correlation.
	DomainPackageSourceCorrelation Domain = "package_source_correlation"
	// DomainPlatformInfraMaterialization owns platform-infrastructure materialization.
	DomainPlatformInfraMaterialization Domain = "platform_infra_materialization"
	// DomainRDSPostureMaterialization owns RDS posture materialization.
	DomainRDSPostureMaterialization Domain = "rds_posture_materialization"
	// DomainRationaleMaterialization owns rationale edge materialization.
	DomainRationaleMaterialization Domain = "rationale_materialization"
	// DomainS3ExternalPrincipalGrantMaterialization owns S3 external-principal grants.
	DomainS3ExternalPrincipalGrantMaterialization Domain = "s3_external_principal_grant_materialization"
	// DomainS3InternetExposureMaterialization owns S3 exposure posture.
	DomainS3InternetExposureMaterialization Domain = "s3_internet_exposure_materialization"
	// DomainS3LogsToMaterialization owns S3 logging edges.
	DomainS3LogsToMaterialization Domain = "s3_logs_to_materialization"
	// DomainSBOMAttestationAttachment owns SBOM attestation attachment.
	DomainSBOMAttestationAttachment Domain = "sbom_attestation_attachment"
	// DomainSQLRelationshipMaterialization owns SQL relationship materialization.
	DomainSQLRelationshipMaterialization Domain = "sql_relationship_materialization"
	// DomainSecretsIAMGraphProjection owns secrets/IAM graph projection.
	DomainSecretsIAMGraphProjection Domain = "secrets_iam_graph_projection"
	// DomainSecretsIAMTrustChain owns secrets/IAM trust correlation.
	DomainSecretsIAMTrustChain Domain = "secrets_iam_trust_chain" // #nosec G101 -- domain identifier, not a credential
	// DomainSecurityAlertReconciliation owns security-alert reconciliation.
	DomainSecurityAlertReconciliation Domain = "security_alert_reconciliation"
	// DomainSecurityGroupCidrMaterialization owns security-group endpoint nodes.
	DomainSecurityGroupCidrMaterialization Domain = "security_group_cidr_materialization"
	// DomainSecurityGroupReachabilityMaterialization owns security-group reachability edges.
	DomainSecurityGroupReachabilityMaterialization Domain = "security_group_reachability_materialization"
	// DomainSecurityGroupRuleMaterialization owns security-group rule nodes.
	DomainSecurityGroupRuleMaterialization Domain = "security_group_rule_materialization"
	// DomainSemanticEntityMaterialization owns semantic entity materialization.
	DomainSemanticEntityMaterialization Domain = "semantic_entity_materialization"
	// DomainServiceCatalogCorrelation owns service-catalog correlation.
	DomainServiceCatalogCorrelation Domain = "service_catalog_correlation"
	// DomainShellExecMaterialization owns shell-exec materialization.
	DomainShellExecMaterialization Domain = "shell_exec_materialization"
	// DomainSubmodulePin owns submodule pin materialization.
	DomainSubmodulePin Domain = "submodule_pin"
	// DomainSupplyChainImpact owns supply-chain impact correlation.
	DomainSupplyChainImpact Domain = "supply_chain_impact"
	// DomainWorkloadCloudRelationshipMaterialization owns workload-cloud edges.
	DomainWorkloadCloudRelationshipMaterialization Domain = "workload_cloud_relationship_materialization"
	// DomainWorkloadIdentity owns workload identity resolution.
	DomainWorkloadIdentity Domain = "workload_identity"
	// DomainWorkloadMaterialization owns workload materialization.
	DomainWorkloadMaterialization Domain = "workload_materialization"
)

const (
	// DomainRepoDependency identifies repository dependency projection work.
	DomainRepoDependency = "repo_dependency"
	// DomainWorkloadDependency identifies workload dependency projection work.
	DomainWorkloadDependency = "workload_dependency"
	// DomainCodeCalls identifies code-call projection work.
	DomainCodeCalls = "code_calls"
	// DomainSQLRelationships identifies SQL relationship projection work.
	DomainSQLRelationships = "sql_relationships"
	// DomainShellExec identifies shell-exec projection work.
	DomainShellExec = "shell_exec"
	// DomainInheritanceEdges identifies inheritance projection work.
	DomainInheritanceEdges = "inheritance_edges"
	// DomainDocumentationEdges identifies documentation projection work.
	DomainDocumentationEdges = "documentation_edges"
	// DomainRationaleEdges identifies rationale projection work.
	DomainRationaleEdges = "rationale_edges"
	// DomainDeployableUnitEdges identifies deployable-unit projection work.
	DomainDeployableUnitEdges = "deployable_unit_edges"
	// DomainHandlesRoute identifies route-handler projection work.
	DomainHandlesRoute = "handles_route"
	// DomainRunsIn identifies runtime-placement projection work.
	DomainRunsIn = "runs_in"
	// DomainInvokesCloudAction identifies cloud-action projection work.
	DomainInvokesCloudAction = "invokes_cloud_action"
	// DomainCodeownersOwnershipEdges identifies CODEOWNERS projection work.
	DomainCodeownersOwnershipEdges = "codeowners_ownership_edges"
	// DomainSubmodulePinEdges identifies submodule-pin projection work.
	DomainSubmodulePinEdges = "submodule_pin_edges"
	// DomainSearchVectorBuild identifies search-vector build work.
	DomainSearchVectorBuild Domain = "search_vector_build"
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
	DomainEC2InstanceIdentityMaterialization:       {},
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

// KnownDomains returns the validation set in deterministic order.
func KnownDomains() []Domain {
	domains := make([]Domain, 0, len(knownDomains))
	for domain := range knownDomains {
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
