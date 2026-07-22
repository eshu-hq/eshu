// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Command schemagen regenerates the checked-in JSON Schema artifacts under
// sdk/go/factschema/schema/. Run it via `go generate ./...` from the
// sdk/go/factschema module root (see the //go:generate directive in
// decode.go), or directly with:
//
//	go run ./internal/schemagen/cmd
//
// The command is deterministic: running it twice in a row against an
// unchanged struct produces byte-identical output, which is what
// schema_gen_test.go's drift test relies on.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/eshu-hq/eshu/sdk/go/factschema/internal/schemagen"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	moduleRoot, err := moduleRootDir()
	if err != nil {
		return err
	}

	targets := []struct {
		name     string
		generate func() ([]byte, error)
	}{
		{name: "aws_resource.v1.schema.json", generate: schemagen.AWSResourceSchema},
		{name: "aws_relationship.v1.schema.json", generate: schemagen.AWSRelationshipSchema},
		{name: "aws_dns_record.v1.schema.json", generate: schemagen.AWSDNSRecordSchema},
		{name: "aws_image_reference.v1.schema.json", generate: schemagen.AWSImageReferenceSchema},
		{name: "aws_security_group_rule.v1.schema.json", generate: schemagen.AWSSecurityGroupRuleSchema},
		{name: "aws_warning.v1.schema.json", generate: schemagen.AWSWarningSchema},
		{name: "ec2_instance_posture.v1.schema.json", generate: schemagen.EC2InstancePostureSchema},
		{name: "rds_instance_posture.v1.schema.json", generate: schemagen.RDSInstancePostureSchema},
		{name: "s3_bucket_posture.v1.schema.json", generate: schemagen.S3BucketPostureSchema},
		{name: "s3_external_principal_grant.v1.schema.json", generate: schemagen.S3ExternalPrincipalGrantSchema},
		{name: "aws_iam_permission.v1.schema.json", generate: schemagen.AWSIAMPermissionSchema},
		{name: "aws_resource_policy_permission.v1.schema.json", generate: schemagen.AWSResourcePolicyPermissionSchema},
		{name: "aws_iam_principal.v1.schema.json", generate: schemagen.AWSIAMPrincipalSchema},
		// The incident family fact kinds are DOTTED (Wave 4a, first dotted
		// family). The schema filename is the dotted kind plus the version
		// suffix; a dot in a filename is valid and needs no transform.
		{name: "incident.record.v1.schema.json", generate: schemagen.IncidentRecordSchema},
		{name: "incident.lifecycle_event.v1.schema.json", generate: schemagen.IncidentLifecycleEventSchema},
		{name: "change.record.v1.schema.json", generate: schemagen.ChangeRecordSchema},
		{name: "incident_routing.applied_pagerduty_resource.v1.schema.json", generate: schemagen.IncidentRoutingAppliedPagerDutyResourceSchema},
		{name: "incident_routing.applied_alert_route.v1.schema.json", generate: schemagen.IncidentRoutingAppliedAlertRouteSchema},
		{name: "incident_routing.observed_pagerduty_service.v1.schema.json", generate: schemagen.IncidentRoutingObservedPagerDutyServiceSchema},
		{name: "incident_routing.observed_pagerduty_integration.v1.schema.json", generate: schemagen.IncidentRoutingObservedPagerDutyIntegrationSchema},
		{name: "incident_routing.coverage_warning.v1.schema.json", generate: schemagen.IncidentRoutingCoverageWarningSchema},
		{name: "gcp_cloud_resource.v1.schema.json", generate: schemagen.GCPCloudResourceSchema},
		{name: "gcp_cloud_relationship.v1.schema.json", generate: schemagen.GCPCloudRelationshipSchema},
		{name: "gcp_collection_warning.v1.schema.json", generate: schemagen.GCPCollectionWarningSchema},
		{name: "gcp_dns_record.v1.schema.json", generate: schemagen.GCPDNSRecordSchema},
		{name: "gcp_iam_policy_observation.v1.schema.json", generate: schemagen.GCPIAMPolicyObservationSchema},
		{name: "gcp_tag_observation.v1.schema.json", generate: schemagen.GCPTagObservationSchema},
		{name: "gcp_image_reference.v1.schema.json", generate: schemagen.GCPImageReferenceSchema},
		{name: "azure_cloud_resource.v1.schema.json", generate: schemagen.AzureCloudResourceSchema},
		{name: "azure_cloud_relationship.v1.schema.json", generate: schemagen.AzureCloudRelationshipSchema},
		{name: "azure_dns_record.v1.schema.json", generate: schemagen.AzureDNSRecordSchema},
		{name: "azure_collection_warning.v1.schema.json", generate: schemagen.AzureCollectionWarningSchema},
		{name: "azure_tag_observation.v1.schema.json", generate: schemagen.AzureTagObservationSchema},
		{name: "azure_identity_observation.v1.schema.json", generate: schemagen.AzureIdentityObservationSchema},
		{name: "azure_resource_change.v1.schema.json", generate: schemagen.AzureResourceChangeSchema},
		{name: "azure_image_reference.v1.schema.json", generate: schemagen.AzureImageReferenceSchema},
		// The kubernetes_live family fact kinds are DOTTED, matching the
		// incident family's convention above.
		{name: "kubernetes_live.pod_template.v1.schema.json", generate: schemagen.KubernetesLivePodTemplateSchema},
		{name: "kubernetes_live.relationship.v1.schema.json", generate: schemagen.KubernetesLiveRelationshipSchema},
		{name: "kubernetes_live.warning.v1.schema.json", generate: schemagen.KubernetesLiveWarningSchema},
		{name: "kubernetes_live.namespace.v1.schema.json", generate: schemagen.KubernetesLiveNamespaceSchema},
		// The oci_registry family fact kinds are DOTTED (like the incident
		// family). The schema filename is the dotted kind plus the version
		// suffix; a dot in a filename is valid and needs no transform.
		{name: "oci_registry.repository.v1.schema.json", generate: schemagen.OCIRegistryRepositorySchema},
		{name: "oci_registry.image_manifest.v1.schema.json", generate: schemagen.OCIImageManifestSchema},
		{name: "oci_registry.image_index.v1.schema.json", generate: schemagen.OCIImageIndexSchema},
		{name: "oci_registry.image_descriptor.v1.schema.json", generate: schemagen.OCIImageDescriptorSchema},
		{name: "oci_registry.image_tag_observation.v1.schema.json", generate: schemagen.OCIImageTagObservationSchema},
		{name: "oci_registry.image_referrer.v1.schema.json", generate: schemagen.OCIImageReferrerSchema},
		{name: "oci_registry.warning.v1.schema.json", generate: schemagen.OCIRegistryWarningSchema},
		// The terraform_state family fact kinds are UNDERSCORE-separated. Five
		// are consumed by the projector's source-local canonical extractor; three
		// (candidate, provider_binding, warning) are typed-but-not-yet-consumed
		// (terraformstate/v1/doc.go) but still ship a checked-in schema.
		{name: "terraform_state_snapshot.v1.schema.json", generate: schemagen.TerraformStateSnapshotSchema},
		{name: "terraform_state_resource.v1.schema.json", generate: schemagen.TerraformStateResourceSchema},
		{name: "terraform_state_module.v1.schema.json", generate: schemagen.TerraformStateModuleSchema},
		{name: "terraform_state_output.v1.schema.json", generate: schemagen.TerraformStateOutputSchema},
		{name: "terraform_state_tag_observation.v1.schema.json", generate: schemagen.TerraformStateTagObservationSchema},
		{name: "terraform_state_candidate.v1.schema.json", generate: schemagen.TerraformStateCandidateSchema},
		{name: "terraform_state_provider_binding.v1.schema.json", generate: schemagen.TerraformStateProviderBindingSchema},
		{name: "terraform_state_warning.v1.schema.json", generate: schemagen.TerraformStateWarningSchema},
		// The package_registry family fact kinds are DOTTED (like the incident
		// and oci_registry families). Three are consumed by the projector's
		// source-local canonical extractor; six (source_hint, package_artifact,
		// vulnerability_hint, registry_event, repository_hosting, warning) are
		// typed-but-not-yet-consumed through the decode seam
		// (packageregistry/v1/doc.go) but still ship a checked-in schema.
		{name: "package_registry.package.v1.schema.json", generate: schemagen.PackageRegistryPackageSchema},
		{name: "package_registry.package_version.v1.schema.json", generate: schemagen.PackageRegistryPackageVersionSchema},
		{name: "package_registry.package_dependency.v1.schema.json", generate: schemagen.PackageRegistryPackageDependencySchema},
		{name: "package_registry.source_hint.v1.schema.json", generate: schemagen.PackageRegistrySourceHintSchema},
		{name: "package_registry.package_artifact.v1.schema.json", generate: schemagen.PackageRegistryPackageArtifactSchema},
		{name: "package_registry.vulnerability_hint.v1.schema.json", generate: schemagen.PackageRegistryVulnerabilityHintSchema},
		{name: "package_registry.registry_event.v1.schema.json", generate: schemagen.PackageRegistryRegistryEventSchema},
		{name: "package_registry.repository_hosting.v1.schema.json", generate: schemagen.PackageRegistryRepositoryHostingSchema},
		{name: "package_registry.warning.v1.schema.json", generate: schemagen.PackageRegistryWarningSchema},
		// The sbom_attestation family fact kinds are DOTTED (like the incident
		// family). document/component/warning and statement/signature_verification
		// are consumed by the reducer's sbom_attestation_attachment domain;
		// dependency_relationship/external_reference/slsa_provenance are
		// typed-but-not-yet-consumed (sbom/v1/doc.go) but still ship a checked-in
		// schema.
		{name: "sbom.document.v1.schema.json", generate: schemagen.SBOMDocumentSchema},
		{name: "sbom.component.v1.schema.json", generate: schemagen.SBOMComponentSchema},
		{name: "sbom.dependency_relationship.v1.schema.json", generate: schemagen.SBOMDependencyRelationshipSchema},
		{name: "sbom.external_reference.v1.schema.json", generate: schemagen.SBOMExternalReferenceSchema},
		{name: "sbom.warning.v1.schema.json", generate: schemagen.SBOMWarningSchema},
		{name: "attestation.statement.v1.schema.json", generate: schemagen.AttestationStatementSchema},
		{name: "attestation.signature_verification.v1.schema.json", generate: schemagen.AttestationSignatureVerificationSchema},
		{name: "attestation.slsa_provenance.v1.schema.json", generate: schemagen.AttestationSLSAProvenanceSchema},
		// The scanner_worker family fact kinds are DOTTED and emitted by the
		// image analyzer as supply-chain coverage/warning evidence.
		{name: "scanner_worker.analysis.v1.schema.json", generate: schemagen.ScannerWorkerAnalysisSchema},
		{name: "scanner_worker.warning.v1.schema.json", generate: schemagen.ScannerWorkerWarningSchema},
		// The vulnerability family fact kinds are DOTTED (like the incident
		// family). The schema filename is the dotted kind plus the version
		// suffix; a dot in a filename is valid and needs no transform.
		// vulnerability.suppression belongs to the separate
		// vulnerability_suppression registry family and is not listed here.
		{name: "vulnerability.cve.v1.schema.json", generate: schemagen.VulnerabilityCVESchema},
		{name: "vulnerability.affected_package.v1.schema.json", generate: schemagen.VulnerabilityAffectedPackageSchema},
		{name: "vulnerability.affected_product.v1.schema.json", generate: schemagen.VulnerabilityAffectedProductSchema},
		{name: "vulnerability.os_package.v1.schema.json", generate: schemagen.VulnerabilityOSPackageSchema},
		{name: "vulnerability.epss_score.v1.schema.json", generate: schemagen.VulnerabilityEPSSScoreSchema},
		{name: "vulnerability.known_exploited.v1.schema.json", generate: schemagen.VulnerabilityKnownExploitedSchema},
		{name: "vulnerability.go_module_evidence.v1.schema.json", generate: schemagen.VulnerabilityGoModuleEvidenceSchema},
		{name: "vulnerability.go_call_reachability.v1.schema.json", generate: schemagen.VulnerabilityGoCallReachabilitySchema},
		// vulnerability.reference and vulnerability.source_snapshot have no
		// reducer decode call; they are typed anyway (issue #4717) so the
		// go/internal/query SQL-schema lockstep test can lock their raw-SQL
		// reads to a committed schema (vulnerability/v1/doc.go).
		// vulnerability.warning stays untyped — it has neither a reducer
		// decode call nor a raw-SQL read-model consumer.
		{name: "vulnerability.reference.v1.schema.json", generate: schemagen.VulnerabilityReferenceSchema},
		{name: "vulnerability.source_snapshot.v1.schema.json", generate: schemagen.VulnerabilitySourceSnapshotSchema},
		// The code family fact kinds are BARE (no family prefix), unlike every
		// other family here: they are the git collector's original,
		// pre-Contract-System literal kinds ("file", "repository"). Only the
		// outer envelope identity is typed; parsed_file_data stays an opaque
		// map[string]any pass-through (issue #4750 defers the inner-AST
		// typing).
		{name: "file.v1.schema.json", generate: schemagen.CodegraphFileSchema},
		{name: "repository.v1.schema.json", generate: schemagen.CodegraphRepositorySchema},
		// The codedataflow family fact kinds are also BARE (no family prefix),
		// the git collector's original, pre-Contract-System literal kinds.
		// code_dataflow_function has no reducer decode call today (query-layer
		// only consumer); it is still typed for family completeness (see
		// codedataflow/v1/doc.go).
		{name: "code_dataflow_scanned.v1.schema.json", generate: schemagen.CodeDataflowScannedSchema},
		{name: "code_dataflow_function.v1.schema.json", generate: schemagen.CodeDataflowFunctionSchema},
		{name: "code_function_summary.v1.schema.json", generate: schemagen.CodeFunctionSummarySchema},
		{name: "code_function_source.v1.schema.json", generate: schemagen.CodeFunctionSourceSchema},
		{name: "code_taint_evidence.v1.schema.json", generate: schemagen.CodeTaintEvidenceSchema},
		{name: "code_interproc_evidence.v1.schema.json", generate: schemagen.CodeInterprocEvidenceSchema},
		// The ci_cd_run family fact kinds are DOTTED (like the incident
		// family). The schema filename is the dotted kind plus the version
		// suffix; a dot in a filename is valid and needs no transform.
		// ci.job, ci.pipeline_definition, and ci.warning are emitted but have
		// no reducer decode call today, so they are NOT typed (cicdrun/v1
		// AGENTS.md).
		{name: "ci.run.v1.schema.json", generate: schemagen.CICDRunSchema},
		{name: "ci.artifact.v1.schema.json", generate: schemagen.CICDArtifactSchema},
		{name: "ci.environment_observation.v1.schema.json", generate: schemagen.CICDEnvironmentObservationSchema},
		{name: "ci.trigger_edge.v1.schema.json", generate: schemagen.CICDTriggerEdgeSchema},
		{name: "ci.step.v1.schema.json", generate: schemagen.CICDStepSchema},
		{name: "ci.workflow_image_evidence.v1.schema.json", generate: schemagen.CICDWorkflowImageEvidenceSchema},
		// The secrets_iam family fact kinds are UNDERSCORE-separated, like the
		// aws/gcp/azure kinds.
		{name: "aws_iam_trust_policy.v1.schema.json", generate: schemagen.AWSIAMTrustPolicySchema},
		{name: "aws_iam_permission_policy.v1.schema.json", generate: schemagen.AWSIAMPermissionPolicySchema},
		{name: "aws_iam_policy_attachment.v1.schema.json", generate: schemagen.AWSIAMPolicyAttachmentSchema},
		{name: "aws_iam_permission_boundary.v1.schema.json", generate: schemagen.AWSIAMPermissionBoundarySchema},
		{name: "aws_iam_instance_profile.v1.schema.json", generate: schemagen.AWSIAMInstanceProfileSchema},
		{name: "aws_iam_access_analyzer_finding.v1.schema.json", generate: schemagen.AWSIAMAccessAnalyzerFindingSchema},
		{name: "gcp_iam_principal.v1.schema.json", generate: schemagen.GCPIAMPrincipalSchema},
		{name: "gcp_iam_trust_policy.v1.schema.json", generate: schemagen.GCPIAMTrustPolicySchema},
		{name: "gcp_iam_permission_policy.v1.schema.json", generate: schemagen.GCPIAMPermissionPolicySchema},
		{name: "k8s_rbac_role.v1.schema.json", generate: schemagen.KubernetesRBACRoleSchema},
		{name: "k8s_rbac_binding.v1.schema.json", generate: schemagen.KubernetesRBACBindingSchema},
		{name: "k8s_service_account_token_posture.v1.schema.json", generate: schemagen.KubernetesServiceAccountTokenPostureSchema},
		{name: "vault_auth_role.v1.schema.json", generate: schemagen.VaultAuthRoleSchema},
		{name: "vault_acl_policy.v1.schema.json", generate: schemagen.VaultACLPolicySchema},
		{name: "vault_kv_metadata.v1.schema.json", generate: schemagen.VaultKVMetadataSchema},
		{name: "k8s_service_account.v1.schema.json", generate: schemagen.KubernetesServiceAccountSchema},
		{name: "k8s_workload_identity_use.v1.schema.json", generate: schemagen.KubernetesWorkloadIdentityUseSchema},
		{name: "eks_irsa_annotation.v1.schema.json", generate: schemagen.EKSIRSAAnnotationSchema},
		{name: "eks_pod_identity_association.v1.schema.json", generate: schemagen.EKSPodIdentityAssociationSchema},
		{name: "k8s_gcp_workload_identity_binding.v1.schema.json", generate: schemagen.KubernetesGCPWorkloadIdentityBindingSchema},
		{name: "vault_auth_mount.v1.schema.json", generate: schemagen.VaultAuthMountSchema},
		{name: "vault_identity_entity.v1.schema.json", generate: schemagen.VaultIdentityEntitySchema},
		{name: "vault_identity_alias.v1.schema.json", generate: schemagen.VaultIdentityAliasSchema},
		{name: "vault_secret_engine_mount.v1.schema.json", generate: schemagen.VaultSecretEngineMountSchema},
		{name: "secrets_iam_coverage_warning.v1.schema.json", generate: schemagen.SecretsIAMCoverageWarningSchema},
		// The work_item family fact kinds are DOTTED (like the incident
		// family). All nine are emitted by the Jira collector; the decode site
		// is the query read-model layer, not the reducer (workitem/v1/README.md).
		{name: "work_item.record.v1.schema.json", generate: schemagen.WorkItemRecordSchema},
		{name: "work_item.transition.v1.schema.json", generate: schemagen.WorkItemTransitionSchema},
		{name: "work_item.external_link.v1.schema.json", generate: schemagen.WorkItemExternalLinkSchema},
		{name: "work_item.project_metadata.v1.schema.json", generate: schemagen.WorkItemProjectMetadataSchema},
		{name: "work_item.issue_type_metadata.v1.schema.json", generate: schemagen.WorkItemIssueTypeMetadataSchema},
		{name: "work_item.status_metadata.v1.schema.json", generate: schemagen.WorkItemStatusMetadataSchema},
		{name: "work_item.workflow_metadata.v1.schema.json", generate: schemagen.WorkItemWorkflowMetadataSchema},
		{name: "work_item.field_metadata.v1.schema.json", generate: schemagen.WorkItemFieldMetadataSchema},
		{name: "work_item.metadata_warning.v1.schema.json", generate: schemagen.WorkItemMetadataWarningSchema},
		// The security_alert family has one DOTTED fact kind (like the incident
		// family). Its single decode site (extractProviderSecurityAlerts) feeds
		// both the reconciliation read surface and the supply-chain-impact
		// seeder, so the typed struct mirrors the existing payload exactly
		// (Contract System v1 Wave 4e, #4566/#4582).
		{name: "security_alert.repository_alert.v1.schema.json", generate: schemagen.SecurityAlertRepositoryAlertSchema},
		// The reducer_derived family contains reducer-owned durable findings.
		// These kinds are written by reducer domains after source evidence has
		// been admitted, so their schemas describe read-model payloads rather
		// than collector input.
		{name: "reducer_supply_chain_impact_finding.v1.schema.json", generate: schemagen.ReducerSupplyChainImpactFindingSchema},
		{name: "reducer_aws_cloud_runtime_drift_finding.v1.schema.json", generate: schemagen.ReducerAWSCloudRuntimeDriftFindingSchema},
		{name: "reducer_multi_cloud_runtime_drift_finding.v1.schema.json", generate: schemagen.ReducerMultiCloudRuntimeDriftFindingSchema},
		{name: "reducer_terraform_config_state_drift_finding.v1.schema.json", generate: schemagen.ReducerTerraformConfigStateDriftFindingSchema},
		{name: "reducer_package_ownership_correlation.v1.schema.json", generate: schemagen.ReducerPackageOwnershipCorrelationSchema},
		{name: "reducer_package_consumption_correlation.v1.schema.json", generate: schemagen.ReducerPackageConsumptionCorrelationSchema},
		{name: "reducer_package_publication_correlation.v1.schema.json", generate: schemagen.ReducerPackagePublicationCorrelationSchema},
		// The observability family fact kinds are DOTTED (like the incident
		// family). All eighteen are consumed by the reducer's
		// observability_coverage_correlation domain (observability/v1/doc.go).
		{name: "observability.declared_folder.v1.schema.json", generate: schemagen.ObservabilityDeclaredFolderSchema},
		{name: "observability.declared_dashboard.v1.schema.json", generate: schemagen.ObservabilityDeclaredDashboardSchema},
		{name: "observability.declared_datasource.v1.schema.json", generate: schemagen.ObservabilityDeclaredDatasourceSchema},
		{name: "observability.declared_alert_rule.v1.schema.json", generate: schemagen.ObservabilityDeclaredAlertRuleSchema},
		{name: "observability.declared_scrape_config.v1.schema.json", generate: schemagen.ObservabilityDeclaredScrapeConfigSchema},
		{name: "observability.declared_metric_rule.v1.schema.json", generate: schemagen.ObservabilityDeclaredMetricRuleSchema},
		{name: "observability.declared_metric_route.v1.schema.json", generate: schemagen.ObservabilityDeclaredMetricRouteSchema},
		{name: "observability.declared_log_route.v1.schema.json", generate: schemagen.ObservabilityDeclaredLogRouteSchema},
		{name: "observability.declared_trace_route.v1.schema.json", generate: schemagen.ObservabilityDeclaredTraceRouteSchema},
		{name: "observability.applied_resource.v1.schema.json", generate: schemagen.ObservabilityAppliedResourceSchema},
		{name: "observability.applied_sync_state.v1.schema.json", generate: schemagen.ObservabilityAppliedSyncStateSchema},
		{name: "observability.observed_dashboard.v1.schema.json", generate: schemagen.ObservabilityObservedDashboardSchema},
		{name: "observability.observed_target.v1.schema.json", generate: schemagen.ObservabilityObservedTargetSchema},
		{name: "observability.observed_rule.v1.schema.json", generate: schemagen.ObservabilityObservedRuleSchema},
		{name: "observability.observed_log_signal.v1.schema.json", generate: schemagen.ObservabilityObservedLogSignalSchema},
		{name: "observability.observed_trace_signal.v1.schema.json", generate: schemagen.ObservabilityObservedTraceSignalSchema},
		{name: "observability.coverage_warning.v1.schema.json", generate: schemagen.ObservabilityCoverageWarningSchema},
		{name: "observability.source_instance.v1.schema.json", generate: schemagen.ObservabilitySourceInstanceSchema},
		// The documentation family fact kinds are UNDERSCORE-separated (not
		// dotted, unlike work_item/incident/sbom). Only documentation_document
		// and documentation_entity_mention have a reducer decode site today;
		// the rest are typed-but-deferred (documentation/v1/README.md).
		// documentation_section carries its own schema-minor version (1.1),
		// reflected in its schema description, not its filename (the file
		// name convention is kind+".v1.schema.json" across every major-1
		// kind regardless of minor).
		{name: "documentation_source.v1.schema.json", generate: schemagen.DocumentationSourceSchema},
		{name: "documentation_document.v1.schema.json", generate: schemagen.DocumentationDocumentSchema},
		{name: "documentation_section.v1.schema.json", generate: schemagen.DocumentationSectionSchema},
		{name: "documentation_link.v1.schema.json", generate: schemagen.DocumentationLinkSchema},
		{name: "documentation_entity_mention.v1.schema.json", generate: schemagen.DocumentationEntityMentionSchema},
		{name: "documentation_claim_candidate.v1.schema.json", generate: schemagen.DocumentationClaimCandidateSchema},
		{name: "documentation_finding.v1.schema.json", generate: schemagen.DocumentationFindingSchema},
		{name: "documentation_evidence_packet.v1.schema.json", generate: schemagen.DocumentationEvidencePacketSchema},
		{name: "semantic.documentation_observation.v1.schema.json", generate: schemagen.SemanticDocumentationObservationSchema},
		{name: "semantic.code_hint.v1.schema.json", generate: schemagen.SemanticCodeHintSchema},
		// The service_catalog family is ALREADY registered and
		// schema-version-admitted; this wave only fills payload_schema_overrides
		// for the four kinds a real consumer decodes (three via the reducer
		// correlation index, one via a raw-SQL JSONB loader in
		// go/internal/query — servicecatalog/v1/README.md). The other five
		// kinds have no decode-side consumer and are intentionally untyped.
		{name: "service_catalog.entity.v1.schema.json", generate: schemagen.ServiceCatalogEntitySchema},
		{name: "service_catalog.ownership.v1.schema.json", generate: schemagen.ServiceCatalogOwnershipSchema},
		{name: "service_catalog.repository_link.v1.schema.json", generate: schemagen.ServiceCatalogRepositoryLinkSchema},
		{name: "service_catalog.operational_link.v1.schema.json", generate: schemagen.ServiceCatalogOperationalLinkSchema},
		// The submodule family fact kind is DOTTED, matching the incident/
		// service_catalog convention above. The git collector emits it and
		// the reducer decodes and projects it into PINS_SUBMODULE graph
		// edges (issue #5420).
		{name: "submodule.pin.v1.schema.json", generate: schemagen.SubmodulePinSchema},
		// The codeowners family is Phase 1 of issue #5419 (branch-aware
		// CODEOWNERS ingestion, epic #5415): the contract only. No collector
		// or reducer/query consumer exists yet for this kind.
		{name: "codeowners.ownership.v1.schema.json", generate: schemagen.CodeownersOwnershipSchema},
	}

	for _, target := range targets {
		raw, err := target.generate()
		if err != nil {
			return fmt.Errorf("schemagen: generate %s: %w", target.name, err)
		}

		dest := filepath.Join(moduleRoot, "schema", target.name)
		if err := os.WriteFile(dest, raw, 0o644); err != nil {
			return fmt.Errorf("schemagen: write %s: %w", dest, err)
		}
	}

	return nil
}

// modulePath is the module path declared in this module's go.mod. It anchors
// the upward search in moduleRootDir so the command finds this module's root
// rather than a parent module's go.mod higher up the tree.
const modulePath = "module github.com/eshu-hq/eshu/sdk/go/factschema"

// byteOrderMark is a UTF-8 BOM that a go.mod file may carry as its first
// bytes; declaresModule trims it before comparing the module line.
const byteOrderMark = "\ufeff"

// moduleRootDir returns the directory holding this module's go.mod by walking
// up from the working directory. It matches on the module path declared in
// go.mod, not merely the presence of a go.mod, so it never mistakes a parent
// module's root for this one. It fails fast with a clear error rather than
// writing the schema files to a wrong path.
func moduleRootDir() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("schemagen: getwd: %w", err)
	}

	for {
		goMod := filepath.Join(dir, "go.mod")
		if raw, readErr := os.ReadFile(goMod); readErr == nil {
			if declaresModule(raw, modulePath) {
				return dir, nil
			}
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf(
				"schemagen: could not locate the %q module root above the working directory; run via `go generate ./...` from the module root",
				modulePath,
			)
		}
		dir = parent
	}
}

// declaresModule reports whether a go.mod file's contents declare
// wantModuleLine as the module path, tolerating a leading UTF-8 byte-order
// mark and surrounding whitespace on the module line.
func declaresModule(goMod []byte, wantModuleLine string) bool {
	for line := range strings.SplitSeq(string(goMod), "\n") {
		if strings.TrimSpace(strings.TrimPrefix(line, byteOrderMark)) == wantModuleLine {
			return true
		}
	}
	return false
}
