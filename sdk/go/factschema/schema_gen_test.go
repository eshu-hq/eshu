// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package factschema

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/sdk/go/factschema/internal/schemagen"
)

// TestSchemasHaveNoDrift regenerates every fact kind's JSON Schema in memory and
// asserts it is byte-identical to the checked-in artifact under schema/. This
// makes schema drift a `go test` failure rather than something only the
// schema-diff CI gate would catch: if a typed payload struct changes without
// re-running `go generate ./...`, this test fails until the committed schema is
// regenerated. Every new fact kind MUST add a row here so its schema is
// drift-locked to its struct like the others.
func TestSchemasHaveNoDrift(t *testing.T) {
	t.Parallel()

	cases := []struct {
		file     string
		generate func() ([]byte, error)
	}{
		{file: "aws_resource.v1.schema.json", generate: schemagen.AWSResourceSchema},
		{file: "aws_relationship.v1.schema.json", generate: schemagen.AWSRelationshipSchema},
		{file: "aws_dns_record.v1.schema.json", generate: schemagen.AWSDNSRecordSchema},
		{file: "aws_image_reference.v1.schema.json", generate: schemagen.AWSImageReferenceSchema},
		{file: "aws_security_group_rule.v1.schema.json", generate: schemagen.AWSSecurityGroupRuleSchema},
		{file: "aws_warning.v1.schema.json", generate: schemagen.AWSWarningSchema},
		{file: "ec2_instance_posture.v1.schema.json", generate: schemagen.EC2InstancePostureSchema},
		{file: "rds_instance_posture.v1.schema.json", generate: schemagen.RDSInstancePostureSchema},
		{file: "s3_bucket_posture.v1.schema.json", generate: schemagen.S3BucketPostureSchema},
		{file: "s3_external_principal_grant.v1.schema.json", generate: schemagen.S3ExternalPrincipalGrantSchema},
		{file: "aws_iam_permission.v1.schema.json", generate: schemagen.AWSIAMPermissionSchema},
		{file: "aws_resource_policy_permission.v1.schema.json", generate: schemagen.AWSResourcePolicyPermissionSchema},
		{file: "aws_iam_principal.v1.schema.json", generate: schemagen.AWSIAMPrincipalSchema},
		{file: "aws_iam_trust_policy.v1.schema.json", generate: schemagen.AWSIAMTrustPolicySchema},
		{file: "aws_iam_permission_policy.v1.schema.json", generate: schemagen.AWSIAMPermissionPolicySchema},
		{file: "aws_iam_policy_attachment.v1.schema.json", generate: schemagen.AWSIAMPolicyAttachmentSchema},
		{file: "aws_iam_permission_boundary.v1.schema.json", generate: schemagen.AWSIAMPermissionBoundarySchema},
		{file: "aws_iam_instance_profile.v1.schema.json", generate: schemagen.AWSIAMInstanceProfileSchema},
		{file: "aws_iam_access_analyzer_finding.v1.schema.json", generate: schemagen.AWSIAMAccessAnalyzerFindingSchema},
		{file: "gcp_cloud_resource.v1.schema.json", generate: schemagen.GCPCloudResourceSchema},
		{file: "gcp_cloud_relationship.v1.schema.json", generate: schemagen.GCPCloudRelationshipSchema},
		{file: "gcp_collection_warning.v1.schema.json", generate: schemagen.GCPCollectionWarningSchema},
		{file: "gcp_dns_record.v1.schema.json", generate: schemagen.GCPDNSRecordSchema},
		{file: "gcp_iam_policy_observation.v1.schema.json", generate: schemagen.GCPIAMPolicyObservationSchema},
		{file: "gcp_tag_observation.v1.schema.json", generate: schemagen.GCPTagObservationSchema},
		{file: "gcp_image_reference.v1.schema.json", generate: schemagen.GCPImageReferenceSchema},
		{file: "gcp_iam_principal.v1.schema.json", generate: schemagen.GCPIAMPrincipalSchema},
		{file: "gcp_iam_trust_policy.v1.schema.json", generate: schemagen.GCPIAMTrustPolicySchema},
		{file: "gcp_iam_permission_policy.v1.schema.json", generate: schemagen.GCPIAMPermissionPolicySchema},
		{file: "azure_cloud_resource.v1.schema.json", generate: schemagen.AzureCloudResourceSchema},
		{file: "azure_cloud_relationship.v1.schema.json", generate: schemagen.AzureCloudRelationshipSchema},
		{file: "azure_dns_record.v1.schema.json", generate: schemagen.AzureDNSRecordSchema},
		{file: "azure_collection_warning.v1.schema.json", generate: schemagen.AzureCollectionWarningSchema},
		{file: "azure_tag_observation.v1.schema.json", generate: schemagen.AzureTagObservationSchema},
		{file: "azure_identity_observation.v1.schema.json", generate: schemagen.AzureIdentityObservationSchema},
		{file: "azure_resource_change.v1.schema.json", generate: schemagen.AzureResourceChangeSchema},
		{file: "azure_image_reference.v1.schema.json", generate: schemagen.AzureImageReferenceSchema},
		{file: "incident.record.v1.schema.json", generate: schemagen.IncidentRecordSchema},
		{file: "incident.lifecycle_event.v1.schema.json", generate: schemagen.IncidentLifecycleEventSchema},
		{file: "change.record.v1.schema.json", generate: schemagen.ChangeRecordSchema},
		{file: "incident_routing.applied_pagerduty_resource.v1.schema.json", generate: schemagen.IncidentRoutingAppliedPagerDutyResourceSchema},
		{file: "incident_routing.applied_alert_route.v1.schema.json", generate: schemagen.IncidentRoutingAppliedAlertRouteSchema},
		{file: "incident_routing.observed_pagerduty_service.v1.schema.json", generate: schemagen.IncidentRoutingObservedPagerDutyServiceSchema},
		{file: "incident_routing.observed_pagerduty_integration.v1.schema.json", generate: schemagen.IncidentRoutingObservedPagerDutyIntegrationSchema},
		{file: "incident_routing.coverage_warning.v1.schema.json", generate: schemagen.IncidentRoutingCoverageWarningSchema},
		{file: "kubernetes_live.pod_template.v1.schema.json", generate: schemagen.KubernetesLivePodTemplateSchema},
		{file: "kubernetes_live.relationship.v1.schema.json", generate: schemagen.KubernetesLiveRelationshipSchema},
		{file: "kubernetes_live.warning.v1.schema.json", generate: schemagen.KubernetesLiveWarningSchema},
		{file: "oci_registry.repository.v1.schema.json", generate: schemagen.OCIRegistryRepositorySchema},
		{file: "oci_registry.image_manifest.v1.schema.json", generate: schemagen.OCIImageManifestSchema},
		{file: "oci_registry.image_index.v1.schema.json", generate: schemagen.OCIImageIndexSchema},
		{file: "oci_registry.image_descriptor.v1.schema.json", generate: schemagen.OCIImageDescriptorSchema},
		{file: "oci_registry.image_tag_observation.v1.schema.json", generate: schemagen.OCIImageTagObservationSchema},
		{file: "oci_registry.image_referrer.v1.schema.json", generate: schemagen.OCIImageReferrerSchema},
		{file: "oci_registry.warning.v1.schema.json", generate: schemagen.OCIRegistryWarningSchema},
		{file: "terraform_state_snapshot.v1.schema.json", generate: schemagen.TerraformStateSnapshotSchema},
		{file: "terraform_state_resource.v1.schema.json", generate: schemagen.TerraformStateResourceSchema},
		{file: "terraform_state_module.v1.schema.json", generate: schemagen.TerraformStateModuleSchema},
		{file: "terraform_state_output.v1.schema.json", generate: schemagen.TerraformStateOutputSchema},
		{file: "terraform_state_tag_observation.v1.schema.json", generate: schemagen.TerraformStateTagObservationSchema},
		{file: "terraform_state_candidate.v1.schema.json", generate: schemagen.TerraformStateCandidateSchema},
		{file: "terraform_state_provider_binding.v1.schema.json", generate: schemagen.TerraformStateProviderBindingSchema},
		{file: "terraform_state_warning.v1.schema.json", generate: schemagen.TerraformStateWarningSchema},
		{file: "package_registry.package.v1.schema.json", generate: schemagen.PackageRegistryPackageSchema},
		{file: "package_registry.package_version.v1.schema.json", generate: schemagen.PackageRegistryPackageVersionSchema},
		{file: "package_registry.package_dependency.v1.schema.json", generate: schemagen.PackageRegistryPackageDependencySchema},
		{file: "package_registry.source_hint.v1.schema.json", generate: schemagen.PackageRegistrySourceHintSchema},
		{file: "package_registry.package_artifact.v1.schema.json", generate: schemagen.PackageRegistryPackageArtifactSchema},
		{file: "package_registry.vulnerability_hint.v1.schema.json", generate: schemagen.PackageRegistryVulnerabilityHintSchema},
		{file: "package_registry.registry_event.v1.schema.json", generate: schemagen.PackageRegistryRegistryEventSchema},
		{file: "package_registry.repository_hosting.v1.schema.json", generate: schemagen.PackageRegistryRepositoryHostingSchema},
		{file: "package_registry.warning.v1.schema.json", generate: schemagen.PackageRegistryWarningSchema},
		{file: "sbom.document.v1.schema.json", generate: schemagen.SBOMDocumentSchema},
		{file: "sbom.component.v1.schema.json", generate: schemagen.SBOMComponentSchema},
		{file: "sbom.dependency_relationship.v1.schema.json", generate: schemagen.SBOMDependencyRelationshipSchema},
		{file: "sbom.external_reference.v1.schema.json", generate: schemagen.SBOMExternalReferenceSchema},
		{file: "sbom.warning.v1.schema.json", generate: schemagen.SBOMWarningSchema},
		{file: "attestation.statement.v1.schema.json", generate: schemagen.AttestationStatementSchema},
		{file: "attestation.signature_verification.v1.schema.json", generate: schemagen.AttestationSignatureVerificationSchema},
		{file: "attestation.slsa_provenance.v1.schema.json", generate: schemagen.AttestationSLSAProvenanceSchema},
		{file: "scanner_worker.analysis.v1.schema.json", generate: schemagen.ScannerWorkerAnalysisSchema},
		{file: "scanner_worker.warning.v1.schema.json", generate: schemagen.ScannerWorkerWarningSchema},
		{file: "vulnerability.cve.v1.schema.json", generate: schemagen.VulnerabilityCVESchema},
		{file: "vulnerability.affected_package.v1.schema.json", generate: schemagen.VulnerabilityAffectedPackageSchema},
		{file: "vulnerability.affected_product.v1.schema.json", generate: schemagen.VulnerabilityAffectedProductSchema},
		{file: "vulnerability.os_package.v1.schema.json", generate: schemagen.VulnerabilityOSPackageSchema},
		{file: "vulnerability.epss_score.v1.schema.json", generate: schemagen.VulnerabilityEPSSScoreSchema},
		{file: "vulnerability.known_exploited.v1.schema.json", generate: schemagen.VulnerabilityKnownExploitedSchema},
		{file: "vulnerability.go_module_evidence.v1.schema.json", generate: schemagen.VulnerabilityGoModuleEvidenceSchema},
		{file: "vulnerability.go_call_reachability.v1.schema.json", generate: schemagen.VulnerabilityGoCallReachabilitySchema},
		{file: "vulnerability.reference.v1.schema.json", generate: schemagen.VulnerabilityReferenceSchema},
		{file: "vulnerability.source_snapshot.v1.schema.json", generate: schemagen.VulnerabilitySourceSnapshotSchema},
		{file: "file.v1.schema.json", generate: schemagen.CodegraphFileSchema},
		{file: "repository.v1.schema.json", generate: schemagen.CodegraphRepositorySchema},
		{file: "code_dataflow_scanned.v1.schema.json", generate: schemagen.CodeDataflowScannedSchema},
		{file: "code_dataflow_function.v1.schema.json", generate: schemagen.CodeDataflowFunctionSchema},
		{file: "code_function_summary.v1.schema.json", generate: schemagen.CodeFunctionSummarySchema},
		{file: "code_function_source.v1.schema.json", generate: schemagen.CodeFunctionSourceSchema},
		{file: "code_taint_evidence.v1.schema.json", generate: schemagen.CodeTaintEvidenceSchema},
		{file: "code_interproc_evidence.v1.schema.json", generate: schemagen.CodeInterprocEvidenceSchema},
		{file: "ci.run.v1.schema.json", generate: schemagen.CICDRunSchema},
		{file: "ci.artifact.v1.schema.json", generate: schemagen.CICDArtifactSchema},
		{file: "ci.environment_observation.v1.schema.json", generate: schemagen.CICDEnvironmentObservationSchema},
		{file: "ci.trigger_edge.v1.schema.json", generate: schemagen.CICDTriggerEdgeSchema},
		{file: "ci.step.v1.schema.json", generate: schemagen.CICDStepSchema},
		{file: "ci.workflow_image_evidence.v1.schema.json", generate: schemagen.CICDWorkflowImageEvidenceSchema},
		{file: "vault_auth_role.v1.schema.json", generate: schemagen.VaultAuthRoleSchema},
		{file: "vault_acl_policy.v1.schema.json", generate: schemagen.VaultACLPolicySchema},
		{file: "vault_kv_metadata.v1.schema.json", generate: schemagen.VaultKVMetadataSchema},
		{file: "vault_auth_mount.v1.schema.json", generate: schemagen.VaultAuthMountSchema},
		{file: "vault_identity_entity.v1.schema.json", generate: schemagen.VaultIdentityEntitySchema},
		{file: "vault_identity_alias.v1.schema.json", generate: schemagen.VaultIdentityAliasSchema},
		{file: "vault_secret_engine_mount.v1.schema.json", generate: schemagen.VaultSecretEngineMountSchema},
		{file: "k8s_service_account.v1.schema.json", generate: schemagen.KubernetesServiceAccountSchema},
		{file: "k8s_workload_identity_use.v1.schema.json", generate: schemagen.KubernetesWorkloadIdentityUseSchema},
		{file: "eks_irsa_annotation.v1.schema.json", generate: schemagen.EKSIRSAAnnotationSchema},
		{file: "eks_pod_identity_association.v1.schema.json", generate: schemagen.EKSPodIdentityAssociationSchema},
		{file: "k8s_gcp_workload_identity_binding.v1.schema.json", generate: schemagen.KubernetesGCPWorkloadIdentityBindingSchema},
		{file: "k8s_rbac_role.v1.schema.json", generate: schemagen.KubernetesRBACRoleSchema},
		{file: "k8s_rbac_binding.v1.schema.json", generate: schemagen.KubernetesRBACBindingSchema},
		{file: "k8s_service_account_token_posture.v1.schema.json", generate: schemagen.KubernetesServiceAccountTokenPostureSchema},
		{file: "secrets_iam_coverage_warning.v1.schema.json", generate: schemagen.SecretsIAMCoverageWarningSchema},
		{file: "work_item.record.v1.schema.json", generate: schemagen.WorkItemRecordSchema},
		{file: "work_item.transition.v1.schema.json", generate: schemagen.WorkItemTransitionSchema},
		{file: "work_item.external_link.v1.schema.json", generate: schemagen.WorkItemExternalLinkSchema},
		{file: "work_item.project_metadata.v1.schema.json", generate: schemagen.WorkItemProjectMetadataSchema},
		{file: "work_item.issue_type_metadata.v1.schema.json", generate: schemagen.WorkItemIssueTypeMetadataSchema},
		{file: "work_item.status_metadata.v1.schema.json", generate: schemagen.WorkItemStatusMetadataSchema},
		{file: "work_item.workflow_metadata.v1.schema.json", generate: schemagen.WorkItemWorkflowMetadataSchema},
		{file: "work_item.field_metadata.v1.schema.json", generate: schemagen.WorkItemFieldMetadataSchema},
		{file: "work_item.metadata_warning.v1.schema.json", generate: schemagen.WorkItemMetadataWarningSchema},
		{file: "reducer_supply_chain_impact_finding.v1.schema.json", generate: schemagen.ReducerSupplyChainImpactFindingSchema},
		{file: "reducer_aws_cloud_runtime_drift_finding.v1.schema.json", generate: schemagen.ReducerAWSCloudRuntimeDriftFindingSchema},
		{file: "reducer_multi_cloud_runtime_drift_finding.v1.schema.json", generate: schemagen.ReducerMultiCloudRuntimeDriftFindingSchema},
		{file: "reducer_terraform_config_state_drift_finding.v1.schema.json", generate: schemagen.ReducerTerraformConfigStateDriftFindingSchema},
		{file: "reducer_package_ownership_correlation.v1.schema.json", generate: schemagen.ReducerPackageOwnershipCorrelationSchema},
		{file: "reducer_package_consumption_correlation.v1.schema.json", generate: schemagen.ReducerPackageConsumptionCorrelationSchema},
		{file: "reducer_package_publication_correlation.v1.schema.json", generate: schemagen.ReducerPackagePublicationCorrelationSchema},
		{file: "service_catalog.entity.v1.schema.json", generate: schemagen.ServiceCatalogEntitySchema},
		{file: "service_catalog.ownership.v1.schema.json", generate: schemagen.ServiceCatalogOwnershipSchema},
		{file: "service_catalog.repository_link.v1.schema.json", generate: schemagen.ServiceCatalogRepositoryLinkSchema},
		{file: "service_catalog.operational_link.v1.schema.json", generate: schemagen.ServiceCatalogOperationalLinkSchema},
		{file: "submodule.pin.v1.schema.json", generate: schemagen.SubmodulePinSchema},
		{file: "codeowners.ownership.v1.schema.json", generate: schemagen.CodeownersOwnershipSchema},
		{file: "documentation_claim_candidate.v1.schema.json", generate: schemagen.DocumentationClaimCandidateSchema},
		{file: "documentation_document.v1.schema.json", generate: schemagen.DocumentationDocumentSchema},
		{file: "documentation_entity_mention.v1.schema.json", generate: schemagen.DocumentationEntityMentionSchema},
		{file: "documentation_evidence_packet.v1.schema.json", generate: schemagen.DocumentationEvidencePacketSchema},
		{file: "documentation_finding.v1.schema.json", generate: schemagen.DocumentationFindingSchema},
		{file: "documentation_link.v1.schema.json", generate: schemagen.DocumentationLinkSchema},
		{file: "documentation_section.v1.schema.json", generate: schemagen.DocumentationSectionSchema},
		{file: "documentation_source.v1.schema.json", generate: schemagen.DocumentationSourceSchema},
		{file: "observability.applied_resource.v1.schema.json", generate: schemagen.ObservabilityAppliedResourceSchema},
		{file: "observability.applied_sync_state.v1.schema.json", generate: schemagen.ObservabilityAppliedSyncStateSchema},
		{file: "observability.coverage_warning.v1.schema.json", generate: schemagen.ObservabilityCoverageWarningSchema},
		{file: "observability.declared_alert_rule.v1.schema.json", generate: schemagen.ObservabilityDeclaredAlertRuleSchema},
		{file: "observability.declared_dashboard.v1.schema.json", generate: schemagen.ObservabilityDeclaredDashboardSchema},
		{file: "observability.declared_datasource.v1.schema.json", generate: schemagen.ObservabilityDeclaredDatasourceSchema},
		{file: "observability.declared_folder.v1.schema.json", generate: schemagen.ObservabilityDeclaredFolderSchema},
		{file: "observability.declared_log_route.v1.schema.json", generate: schemagen.ObservabilityDeclaredLogRouteSchema},
		{file: "observability.declared_metric_route.v1.schema.json", generate: schemagen.ObservabilityDeclaredMetricRouteSchema},
		{file: "observability.declared_metric_rule.v1.schema.json", generate: schemagen.ObservabilityDeclaredMetricRuleSchema},
		{file: "observability.declared_scrape_config.v1.schema.json", generate: schemagen.ObservabilityDeclaredScrapeConfigSchema},
		{file: "observability.declared_trace_route.v1.schema.json", generate: schemagen.ObservabilityDeclaredTraceRouteSchema},
		{file: "observability.observed_dashboard.v1.schema.json", generate: schemagen.ObservabilityObservedDashboardSchema},
		{file: "observability.observed_log_signal.v1.schema.json", generate: schemagen.ObservabilityObservedLogSignalSchema},
		{file: "observability.observed_rule.v1.schema.json", generate: schemagen.ObservabilityObservedRuleSchema},
		{file: "observability.observed_target.v1.schema.json", generate: schemagen.ObservabilityObservedTargetSchema},
		{file: "observability.observed_trace_signal.v1.schema.json", generate: schemagen.ObservabilityObservedTraceSignalSchema},
		{file: "observability.source_instance.v1.schema.json", generate: schemagen.ObservabilitySourceInstanceSchema},
		{file: "security_alert.repository_alert.v1.schema.json", generate: schemagen.SecurityAlertRepositoryAlertSchema},
		{file: "semantic.code_hint.v1.schema.json", generate: schemagen.SemanticCodeHintSchema},
		{file: "semantic.documentation_observation.v1.schema.json", generate: schemagen.SemanticDocumentationObservationSchema},
	}

	for _, tc := range cases {
		t.Run(tc.file, func(t *testing.T) {
			t.Parallel()

			got, err := tc.generate()
			if err != nil {
				t.Fatalf("generate %s error = %v, want nil", tc.file, err)
			}

			want, err := os.ReadFile(filepath.Join("schema", tc.file))
			if err != nil {
				t.Fatalf("os.ReadFile(schema/%s) error = %v, want nil", tc.file, err)
			}

			if !bytes.Equal(got, want) {
				t.Fatalf("generated %s drifted from committed artifact; run `go generate ./...` in sdk/go/factschema and commit the result\n\ngenerated:\n%s\n\ncommitted:\n%s", tc.file, got, want)
			}
		})
	}
}

// TestSchemasMatchCollectorPayloadShape locks the openness and nullability
// contract every fact-kind schema must hold so it validates the REAL collector
// payload, not just the reducer-consumed typed subset. The collectors emit extra
// context/service keys the reducer ignores (collector_instance_id, service_kind,
// service-specific fields, the nested attributes object) and explicit JSON null
// for absent optionals (boolOrNil / int32OrNil / a nil pointer). So for every
// kind:
//   - the top-level object MUST be open (additionalProperties: true), and
//   - every optional property (not in "required") MUST accept null.
//
// A schema that is additionalProperties:false or types an optional non-nullable
// would reject a real emitted payload — the wrong committed contract this test
// prevents (it fails if the generator's post-processing is dropped).
func TestSchemasMatchCollectorPayloadShape(t *testing.T) {
	t.Parallel()

	files := []string{
		"aws_resource.v1.schema.json",
		"aws_relationship.v1.schema.json",
		"aws_dns_record.v1.schema.json",
		"aws_image_reference.v1.schema.json",
		"aws_security_group_rule.v1.schema.json",
		"aws_warning.v1.schema.json",
		"ec2_instance_posture.v1.schema.json",
		"rds_instance_posture.v1.schema.json",
		"s3_bucket_posture.v1.schema.json",
		"s3_external_principal_grant.v1.schema.json",
		"aws_iam_permission.v1.schema.json",
		"aws_resource_policy_permission.v1.schema.json",
		"aws_iam_principal.v1.schema.json",
		"aws_iam_trust_policy.v1.schema.json",
		"aws_iam_permission_policy.v1.schema.json",
		"aws_iam_policy_attachment.v1.schema.json",
		"aws_iam_permission_boundary.v1.schema.json",
		"aws_iam_instance_profile.v1.schema.json",
		"aws_iam_access_analyzer_finding.v1.schema.json",
		"gcp_cloud_resource.v1.schema.json",
		"gcp_cloud_relationship.v1.schema.json",
		"gcp_collection_warning.v1.schema.json",
		"gcp_dns_record.v1.schema.json",
		"gcp_iam_policy_observation.v1.schema.json",
		"gcp_tag_observation.v1.schema.json",
		"gcp_image_reference.v1.schema.json",
		"gcp_iam_principal.v1.schema.json",
		"gcp_iam_trust_policy.v1.schema.json",
		"gcp_iam_permission_policy.v1.schema.json",
		"azure_cloud_resource.v1.schema.json",
		"azure_cloud_relationship.v1.schema.json",
		"azure_dns_record.v1.schema.json",
		"azure_collection_warning.v1.schema.json",
		"azure_tag_observation.v1.schema.json",
		"azure_identity_observation.v1.schema.json",
		"azure_resource_change.v1.schema.json",
		"azure_image_reference.v1.schema.json",
		"incident.record.v1.schema.json",
		"incident.lifecycle_event.v1.schema.json",
		"change.record.v1.schema.json",
		"incident_routing.applied_pagerduty_resource.v1.schema.json",
		"incident_routing.applied_alert_route.v1.schema.json",
		"incident_routing.observed_pagerduty_service.v1.schema.json",
		"incident_routing.observed_pagerduty_integration.v1.schema.json",
		"incident_routing.coverage_warning.v1.schema.json",
		"kubernetes_live.pod_template.v1.schema.json",
		"kubernetes_live.relationship.v1.schema.json",
		"kubernetes_live.warning.v1.schema.json",
		"oci_registry.repository.v1.schema.json",
		"oci_registry.image_manifest.v1.schema.json",
		"oci_registry.image_index.v1.schema.json",
		"oci_registry.image_descriptor.v1.schema.json",
		"oci_registry.image_tag_observation.v1.schema.json",
		"oci_registry.image_referrer.v1.schema.json",
		"oci_registry.warning.v1.schema.json",
		"terraform_state_snapshot.v1.schema.json",
		"terraform_state_resource.v1.schema.json",
		"terraform_state_module.v1.schema.json",
		"terraform_state_output.v1.schema.json",
		"terraform_state_tag_observation.v1.schema.json",
		"terraform_state_candidate.v1.schema.json",
		"terraform_state_provider_binding.v1.schema.json",
		"terraform_state_warning.v1.schema.json",
		"package_registry.package.v1.schema.json",
		"package_registry.package_version.v1.schema.json",
		"package_registry.package_dependency.v1.schema.json",
		"package_registry.source_hint.v1.schema.json",
		"package_registry.package_artifact.v1.schema.json",
		"package_registry.vulnerability_hint.v1.schema.json",
		"package_registry.registry_event.v1.schema.json",
		"package_registry.repository_hosting.v1.schema.json",
		"package_registry.warning.v1.schema.json",
		"sbom.document.v1.schema.json",
		"sbom.component.v1.schema.json",
		"sbom.dependency_relationship.v1.schema.json",
		"sbom.external_reference.v1.schema.json",
		"sbom.warning.v1.schema.json",
		"attestation.statement.v1.schema.json",
		"attestation.signature_verification.v1.schema.json",
		"attestation.slsa_provenance.v1.schema.json",
		"scanner_worker.analysis.v1.schema.json",
		"scanner_worker.warning.v1.schema.json",
		"vulnerability.cve.v1.schema.json",
		"vulnerability.affected_package.v1.schema.json",
		"vulnerability.affected_product.v1.schema.json",
		"vulnerability.os_package.v1.schema.json",
		"vulnerability.epss_score.v1.schema.json",
		"vulnerability.known_exploited.v1.schema.json",
		"vulnerability.go_module_evidence.v1.schema.json",
		"vulnerability.go_call_reachability.v1.schema.json",
		"vulnerability.reference.v1.schema.json",
		"vulnerability.source_snapshot.v1.schema.json",
		"file.v1.schema.json",
		"repository.v1.schema.json",
		"ci.run.v1.schema.json",
		"ci.artifact.v1.schema.json",
		"ci.environment_observation.v1.schema.json",
		"ci.trigger_edge.v1.schema.json",
		"ci.step.v1.schema.json",
		"ci.workflow_image_evidence.v1.schema.json",
		"vault_auth_role.v1.schema.json",
		"vault_acl_policy.v1.schema.json",
		"vault_kv_metadata.v1.schema.json",
		"vault_auth_mount.v1.schema.json",
		"vault_identity_entity.v1.schema.json",
		"vault_identity_alias.v1.schema.json",
		"vault_secret_engine_mount.v1.schema.json",
		"k8s_service_account.v1.schema.json",
		"k8s_workload_identity_use.v1.schema.json",
		"eks_irsa_annotation.v1.schema.json",
		"eks_pod_identity_association.v1.schema.json",
		"k8s_gcp_workload_identity_binding.v1.schema.json",
		"k8s_rbac_role.v1.schema.json",
		"k8s_rbac_binding.v1.schema.json",
		"k8s_service_account_token_posture.v1.schema.json",
		"secrets_iam_coverage_warning.v1.schema.json",
		"service_catalog.entity.v1.schema.json",
		"service_catalog.ownership.v1.schema.json",
		"service_catalog.repository_link.v1.schema.json",
		"service_catalog.operational_link.v1.schema.json",
		"codeowners.ownership.v1.schema.json",
		"documentation_claim_candidate.v1.schema.json",
		"documentation_document.v1.schema.json",
		"documentation_entity_mention.v1.schema.json",
		"documentation_evidence_packet.v1.schema.json",
		"documentation_finding.v1.schema.json",
		"documentation_link.v1.schema.json",
		"documentation_section.v1.schema.json",
		"documentation_source.v1.schema.json",
		"observability.applied_resource.v1.schema.json",
		"observability.applied_sync_state.v1.schema.json",
		"observability.coverage_warning.v1.schema.json",
		"observability.declared_alert_rule.v1.schema.json",
		"observability.declared_dashboard.v1.schema.json",
		"observability.declared_datasource.v1.schema.json",
		"observability.declared_folder.v1.schema.json",
		"observability.declared_log_route.v1.schema.json",
		"observability.declared_metric_route.v1.schema.json",
		"observability.declared_metric_rule.v1.schema.json",
		"observability.declared_scrape_config.v1.schema.json",
		"observability.declared_trace_route.v1.schema.json",
		"observability.observed_dashboard.v1.schema.json",
		"observability.observed_log_signal.v1.schema.json",
		"observability.observed_rule.v1.schema.json",
		"observability.observed_target.v1.schema.json",
		"observability.observed_trace_signal.v1.schema.json",
		"observability.source_instance.v1.schema.json",
		"security_alert.repository_alert.v1.schema.json",
		"semantic.code_hint.v1.schema.json",
		"semantic.documentation_observation.v1.schema.json",
	}

	for _, file := range files {
		t.Run(file, func(t *testing.T) {
			t.Parallel()

			raw, err := os.ReadFile(filepath.Join("schema", file))
			if err != nil {
				t.Fatalf("os.ReadFile(schema/%s) error = %v, want nil", file, err)
			}
			var schema map[string]any
			if err := json.Unmarshal(raw, &schema); err != nil {
				t.Fatalf("unmarshal schema/%s error = %v", file, err)
			}

			if open, _ := schema["additionalProperties"].(bool); !open {
				t.Fatalf("schema/%s top-level additionalProperties = %v, want true; the collector payload carries context/service keys the reducer does not consume", file, schema["additionalProperties"])
			}

			required := map[string]struct{}{}
			if rawRequired, ok := schema["required"].([]any); ok {
				for _, r := range rawRequired {
					if name, isString := r.(string); isString {
						required[name] = struct{}{}
					}
				}
			}

			props, ok := schema["properties"].(map[string]any)
			if !ok {
				t.Fatalf("schema/%s has no properties object", file)
			}
			for name, rawProp := range props {
				if _, isRequired := required[name]; isRequired {
					continue
				}
				prop, ok := rawProp.(map[string]any)
				if !ok {
					continue
				}
				if !typeAcceptsNull(prop["type"]) {
					t.Fatalf("schema/%s optional property %q type = %v, want it to accept null; the collector emits explicit null for an absent optional", file, name, prop["type"])
				}
			}
		})
	}
}

// typeAcceptsNull reports whether a JSON Schema "type" value permits an explicit
// null: a bare "null", a union array containing "null", or an absent type (an
// untyped open object already accepts null).
func typeAcceptsNull(t any) bool {
	switch typed := t.(type) {
	case nil:
		return true
	case string:
		return typed == "null"
	case []any:
		for _, v := range typed {
			if v == "null" {
				return true
			}
		}
		return false
	default:
		return false
	}
}
