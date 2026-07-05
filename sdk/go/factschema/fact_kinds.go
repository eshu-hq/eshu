// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package factschema

// Fact kind identifiers this module knows how to decode. A fact kind string
// is namespaced and stable across schema-version majors; only the payload
// shape changes between majors, handled by the switch inside each
// kind-specific Decode function (Contract System v1 §3.2).
//
// Every value is the exact wire fact-kind string the collector emits and the
// reducer loads (go/internal/facts.*FactKind). The contracts module cannot
// import go/internal/facts, so the values are duplicated here; the reducer-side
// drift lock TestFactSchemaKindsMatchWireFactKinds asserts each stays byte-equal
// to its facts.*FactKind counterpart so a constant can never silently diverge
// from its wire kind.
const (
	// FactKindAWSResource is the "aws_resource" fact kind.
	FactKindAWSResource = "aws_resource"
	// FactKindAWSRelationship is the "aws_relationship" fact kind.
	FactKindAWSRelationship = "aws_relationship"
	// FactKindAWSSecurityGroupRule is the "aws_security_group_rule" fact kind.
	FactKindAWSSecurityGroupRule = "aws_security_group_rule"
	// FactKindEC2InstancePosture is the "ec2_instance_posture" fact kind.
	FactKindEC2InstancePosture = "ec2_instance_posture"
	// FactKindS3BucketPosture is the "s3_bucket_posture" fact kind.
	FactKindS3BucketPosture = "s3_bucket_posture"
	// FactKindAWSIAMPermission is the "aws_iam_permission" fact kind.
	FactKindAWSIAMPermission = "aws_iam_permission"
	// FactKindAWSResourcePolicyPermission is the
	// "aws_resource_policy_permission" fact kind.
	FactKindAWSResourcePolicyPermission = "aws_resource_policy_permission"
	// FactKindAWSIAMPrincipal is the "aws_iam_principal" fact kind.
	FactKindAWSIAMPrincipal = "aws_iam_principal"

	// The incident family fact-kind strings are DOTTED, unlike the underscore
	// aws/iam kinds above. The dots are part of the wire kind the collector
	// already emits (go/internal/facts.IncidentRecordFactKind and siblings); the
	// values here MATCH those wire strings byte-for-byte and never invent or
	// rename the namespace. TestFactSchemaKindsMatchWireFactKinds (reducer side)
	// asserts each stays byte-equal to its facts.*FactKind counterpart.

	// FactKindIncidentRecord is the "incident.record" fact kind.
	FactKindIncidentRecord = "incident.record"
	// FactKindIncidentLifecycleEvent is the "incident.lifecycle_event" fact kind.
	FactKindIncidentLifecycleEvent = "incident.lifecycle_event"
	// FactKindChangeRecord is the "change.record" fact kind.
	FactKindChangeRecord = "change.record"
	// FactKindIncidentRoutingAppliedPagerDutyResource is the
	// "incident_routing.applied_pagerduty_resource" fact kind.
	FactKindIncidentRoutingAppliedPagerDutyResource = "incident_routing.applied_pagerduty_resource"
	// FactKindIncidentRoutingAppliedAlertRoute is the
	// "incident_routing.applied_alert_route" fact kind.
	FactKindIncidentRoutingAppliedAlertRoute = "incident_routing.applied_alert_route"
	// FactKindIncidentRoutingObservedPagerDutyService is the
	// "incident_routing.observed_pagerduty_service" fact kind.
	FactKindIncidentRoutingObservedPagerDutyService = "incident_routing.observed_pagerduty_service"
	// FactKindIncidentRoutingObservedPagerDutyIntegration is the
	// "incident_routing.observed_pagerduty_integration" fact kind.
	FactKindIncidentRoutingObservedPagerDutyIntegration = "incident_routing.observed_pagerduty_integration"
	// FactKindIncidentRoutingCoverageWarning is the
	// "incident_routing.coverage_warning" fact kind.
	FactKindIncidentRoutingCoverageWarning = "incident_routing.coverage_warning"
	// FactKindGCPCloudResource is the "gcp_cloud_resource" fact kind.
	FactKindGCPCloudResource = "gcp_cloud_resource"
	// FactKindGCPCloudRelationship is the "gcp_cloud_relationship" fact kind.
	FactKindGCPCloudRelationship = "gcp_cloud_relationship"
	// FactKindGCPCollectionWarning is the "gcp_collection_warning" fact kind.
	FactKindGCPCollectionWarning = "gcp_collection_warning"
	// FactKindGCPDNSRecord is the "gcp_dns_record" fact kind.
	FactKindGCPDNSRecord = "gcp_dns_record"
	// FactKindGCPIAMPolicyObservation is the "gcp_iam_policy_observation"
	// fact kind.
	FactKindGCPIAMPolicyObservation = "gcp_iam_policy_observation"
	// FactKindAzureCloudResource is the "azure_cloud_resource" fact kind.
	FactKindAzureCloudResource = "azure_cloud_resource"
	// FactKindAzureCloudRelationship is the "azure_cloud_relationship" fact
	// kind.
	FactKindAzureCloudRelationship = "azure_cloud_relationship"
	// FactKindAzureDNSRecord is the "azure_dns_record" fact kind.
	FactKindAzureDNSRecord = "azure_dns_record"
	// FactKindAzureCollectionWarning is the "azure_collection_warning" fact
	// kind.
	FactKindAzureCollectionWarning = "azure_collection_warning"

	// The kubernetes_live family fact-kind strings are DOTTED, matching the
	// incident family's convention above. The dots are part of the wire kind
	// the collector already emits (go/internal/facts.KubernetesPodTemplateFactKind
	// and siblings); the values here MATCH those wire strings byte-for-byte and
	// never invent or rename the namespace. TestFactSchemaKindsMatchWireFactKinds
	// (reducer side) asserts each stays byte-equal to its facts.*FactKind
	// counterpart.

	// FactKindKubernetesLivePodTemplate is the "kubernetes_live.pod_template"
	// fact kind.
	FactKindKubernetesLivePodTemplate = "kubernetes_live.pod_template"
	// FactKindKubernetesLiveRelationship is the "kubernetes_live.relationship"
	// fact kind.
	FactKindKubernetesLiveRelationship = "kubernetes_live.relationship"
	// FactKindKubernetesLiveWarning is the "kubernetes_live.warning" fact kind.
	FactKindKubernetesLiveWarning = "kubernetes_live.warning"

	// The oci_registry family fact-kind strings are DOTTED, like the incident
	// family. The dots are part of the wire kind the collector already emits
	// (go/internal/facts.OCIRegistryRepositoryFactKind and siblings); the
	// values here MATCH those wire strings byte-for-byte and never invent or
	// rename the namespace. TestFactSchemaKindsMatchWireFactKinds (reducer side)
	// asserts each stays byte-equal to its facts.*FactKind counterpart.

	// FactKindOCIRegistryRepository is the "oci_registry.repository" fact kind.
	FactKindOCIRegistryRepository = "oci_registry.repository"
	// FactKindOCIImageTagObservation is the
	// "oci_registry.image_tag_observation" fact kind.
	FactKindOCIImageTagObservation = "oci_registry.image_tag_observation"
	// FactKindOCIImageManifest is the "oci_registry.image_manifest" fact kind.
	FactKindOCIImageManifest = "oci_registry.image_manifest"
	// FactKindOCIImageIndex is the "oci_registry.image_index" fact kind.
	FactKindOCIImageIndex = "oci_registry.image_index"
	// FactKindOCIImageDescriptor is the "oci_registry.image_descriptor" fact
	// kind.
	FactKindOCIImageDescriptor = "oci_registry.image_descriptor"
	// FactKindOCIImageReferrer is the "oci_registry.image_referrer" fact kind.
	FactKindOCIImageReferrer = "oci_registry.image_referrer"
	// FactKindOCIRegistryWarning is the "oci_registry.warning" fact kind.
	FactKindOCIRegistryWarning = "oci_registry.warning"
	// The terraform_state family fact-kind strings are UNDERSCORE-separated,
	// like the aws/gcp/azure kinds. The values here MATCH the wire strings the
	// terraform-state collector emits (go/internal/facts.TerraformState*FactKind)
	// byte-for-byte; the reducer-side drift lock
	// TestFactSchemaKindsMatchWireFactKinds asserts each stays byte-equal to its
	// facts.*FactKind counterpart.

	// FactKindTerraformStateSnapshot is the "terraform_state_snapshot" fact kind.
	FactKindTerraformStateSnapshot = "terraform_state_snapshot"
	// FactKindTerraformStateResource is the "terraform_state_resource" fact kind.
	FactKindTerraformStateResource = "terraform_state_resource"
	// FactKindTerraformStateModule is the "terraform_state_module" fact kind.
	FactKindTerraformStateModule = "terraform_state_module"
	// FactKindTerraformStateOutput is the "terraform_state_output" fact kind.
	FactKindTerraformStateOutput = "terraform_state_output"
	// FactKindTerraformStateTagObservation is the
	// "terraform_state_tag_observation" fact kind.
	FactKindTerraformStateTagObservation = "terraform_state_tag_observation"
	// FactKindTerraformStateCandidate is the "terraform_state_candidate" fact
	// kind. Typed but not yet consumed (terraformstate/v1/doc.go).
	FactKindTerraformStateCandidate = "terraform_state_candidate"
	// FactKindTerraformStateProviderBinding is the
	// "terraform_state_provider_binding" fact kind. Typed but not yet consumed.
	FactKindTerraformStateProviderBinding = "terraform_state_provider_binding"
	// FactKindTerraformStateWarning is the "terraform_state_warning" fact kind.
	// Typed but not yet consumed (terraformstate/v1/doc.go).
	FactKindTerraformStateWarning = "terraform_state_warning"

	// The package_registry family fact-kind strings are DOTTED, like the
	// incident/oci_registry families. The dots are part of the wire kind the
	// collector already emits (go/internal/facts.PackageRegistry*FactKind and
	// siblings); the values here MATCH those wire strings byte-for-byte and
	// never invent or rename the namespace. TestFactSchemaKindsMatchWireFactKinds
	// (reducer side) asserts each stays byte-equal to its facts.*FactKind
	// counterpart.

	// FactKindPackageRegistryPackage is the "package_registry.package" fact
	// kind.
	FactKindPackageRegistryPackage = "package_registry.package"
	// FactKindPackageRegistryPackageVersion is the
	// "package_registry.package_version" fact kind.
	FactKindPackageRegistryPackageVersion = "package_registry.package_version"
	// FactKindPackageRegistryPackageDependency is the
	// "package_registry.package_dependency" fact kind.
	FactKindPackageRegistryPackageDependency = "package_registry.package_dependency"
	// FactKindPackageRegistrySourceHint is the "package_registry.source_hint"
	// fact kind. Typed but not yet consumed through this module's decode seam
	// (packageregistry/v1/doc.go); read today only by the reducer's
	// package_source_correlation domain via raw payload access.
	FactKindPackageRegistrySourceHint = "package_registry.source_hint"
	// FactKindPackageRegistryPackageArtifact is the
	// "package_registry.package_artifact" fact kind. Typed but not yet
	// consumed.
	FactKindPackageRegistryPackageArtifact = "package_registry.package_artifact"
	// FactKindPackageRegistryVulnerabilityHint is the
	// "package_registry.vulnerability_hint" fact kind. Typed but not yet
	// consumed through the decode seam; its package_id field is read by a
	// raw-SQL-JSONB loader (packageregistry/v1/doc.go).
	FactKindPackageRegistryVulnerabilityHint = "package_registry.vulnerability_hint"
	// FactKindPackageRegistryRegistryEvent is the
	// "package_registry.registry_event" fact kind. Typed but not yet consumed.
	FactKindPackageRegistryRegistryEvent = "package_registry.registry_event"
	// FactKindPackageRegistryRepositoryHosting is the
	// "package_registry.repository_hosting" fact kind. Typed but not yet
	// consumed.
	FactKindPackageRegistryRepositoryHosting = "package_registry.repository_hosting"
	// FactKindPackageRegistryWarning is the "package_registry.warning" fact
	// kind. Typed but not yet consumed through the decode seam; its ecosystem
	// and warning_code fields are read by a raw-SQL-JSONB loader
	// (packageregistry/v1/doc.go).
	FactKindPackageRegistryWarning = "package_registry.warning"
	// The sbom_attestation family fact-kind strings are DOTTED, matching the
	// incident/kubernetes_live/oci_registry convention above. The dots are
	// part of the wire kind the collector already emits
	// (go/internal/facts.SBOMDocumentFactKind and siblings); the values here
	// MATCH those wire strings byte-for-byte and never invent or rename the
	// namespace. TestFactSchemaKindsMatchWireFactKinds (reducer side) asserts
	// each stays byte-equal to its facts.*FactKind counterpart.

	// FactKindSBOMDocument is the "sbom.document" fact kind.
	FactKindSBOMDocument = "sbom.document"
	// FactKindSBOMComponent is the "sbom.component" fact kind.
	FactKindSBOMComponent = "sbom.component"
	// FactKindSBOMDependencyRelationship is the "sbom.dependency_relationship"
	// fact kind. Typed but not yet consumed (sbom/v1/doc.go).
	FactKindSBOMDependencyRelationship = "sbom.dependency_relationship"
	// FactKindSBOMExternalReference is the "sbom.external_reference" fact
	// kind. Typed but not yet consumed (sbom/v1/doc.go).
	FactKindSBOMExternalReference = "sbom.external_reference"
	// FactKindSBOMWarning is the "sbom.warning" fact kind.
	FactKindSBOMWarning = "sbom.warning"
	// FactKindAttestationStatement is the "attestation.statement" fact kind.
	FactKindAttestationStatement = "attestation.statement"
	// FactKindAttestationSignatureVerification is the
	// "attestation.signature_verification" fact kind.
	FactKindAttestationSignatureVerification = "attestation.signature_verification"
	// FactKindAttestationSLSAProvenance is the "attestation.slsa_provenance"
	// fact kind. Typed but not yet consumed or emitted (sbom/v1/doc.go).
	FactKindAttestationSLSAProvenance = "attestation.slsa_provenance"
	// The vulnerability family fact-kind strings are DOTTED, like the incident
	// family. The dots are part of the wire kind the collector already emits
	// (go/internal/facts.Vulnerability*FactKind); the values here MATCH those
	// wire strings byte-for-byte and never invent or rename the namespace.
	// TestFactSchemaKindsMatchWireFactKinds (reducer side) asserts each stays
	// byte-equal to its facts.*FactKind counterpart. vulnerability.suppression
	// belongs to the SEPARATE vulnerability_suppression registry family and is
	// not declared here.

	// FactKindVulnerabilityCVE is the "vulnerability.cve" fact kind.
	FactKindVulnerabilityCVE = "vulnerability.cve"
	// FactKindVulnerabilityAffectedPackage is the
	// "vulnerability.affected_package" fact kind.
	FactKindVulnerabilityAffectedPackage = "vulnerability.affected_package"
	// FactKindVulnerabilityAffectedProduct is the
	// "vulnerability.affected_product" fact kind.
	FactKindVulnerabilityAffectedProduct = "vulnerability.affected_product"
	// FactKindVulnerabilityOSPackage is the "vulnerability.os_package" fact
	// kind.
	FactKindVulnerabilityOSPackage = "vulnerability.os_package"
	// FactKindVulnerabilityEPSSScore is the "vulnerability.epss_score" fact
	// kind.
	FactKindVulnerabilityEPSSScore = "vulnerability.epss_score"
	// FactKindVulnerabilityKnownExploited is the
	// "vulnerability.known_exploited" fact kind.
	FactKindVulnerabilityKnownExploited = "vulnerability.known_exploited"
	// FactKindVulnerabilityGoModuleEvidence is the
	// "vulnerability.go_module_evidence" fact kind.
	FactKindVulnerabilityGoModuleEvidence = "vulnerability.go_module_evidence"
	// FactKindVulnerabilityGoCallReachability is the
	// "vulnerability.go_call_reachability" fact kind.
	FactKindVulnerabilityGoCallReachability = "vulnerability.go_call_reachability"

	// The ci_cd_run family fact-kind strings are DOTTED, like the incident
	// family. The dots are part of the wire kind the collector already emits
	// (go/internal/facts.CICDRunFactKind and siblings); the values here MATCH
	// those wire strings byte-for-byte and never invent or rename the
	// namespace. TestFactSchemaKindsMatchWireFactKinds (reducer side) asserts
	// each stays byte-equal to its facts.*FactKind counterpart.
	// ci.job, ci.pipeline_definition, and ci.warning are emitted but have no
	// reducer decode call today, so they are NOT declared here (cicdrun/v1
	// AGENTS.md).

	// FactKindCICDRun is the "ci.run" fact kind.
	FactKindCICDRun = "ci.run"
	// FactKindCICDArtifact is the "ci.artifact" fact kind.
	FactKindCICDArtifact = "ci.artifact"
	// FactKindCICDEnvironmentObservation is the "ci.environment_observation"
	// fact kind.
	FactKindCICDEnvironmentObservation = "ci.environment_observation"
	// FactKindCICDTriggerEdge is the "ci.trigger_edge" fact kind.
	FactKindCICDTriggerEdge = "ci.trigger_edge"
	// FactKindCICDStep is the "ci.step" fact kind.
	FactKindCICDStep = "ci.step"
	// FactKindCICDWorkflowImageEvidence is the "ci.workflow_image_evidence"
	// fact kind.
	FactKindCICDWorkflowImageEvidence = "ci.workflow_image_evidence"
	// The secrets_iam family fact-kind strings are UNDERSCORE-separated, like
	// the aws/gcp/azure kinds. The values here MATCH the wire strings the
	// secrets_iam posture collector emits (go/internal/facts.VaultAuthRoleFactKind
	// and siblings) byte-for-byte; the reducer-side drift lock
	// TestFactSchemaKindsMatchWireFactKinds asserts each stays byte-equal to
	// its facts.*FactKind counterpart. Only the VAULT and K8S lane kinds are
	// listed here (Contract System v1 Wave 4d, #4566/#4582): the AWS IAM lane
	// (aws_iam_principal and siblings) was already migrated in #4568 and its
	// constants live above; the GCP IAM lane (gcp_iam_principal,
	// gcp_iam_trust_policy, gcp_iam_permission_policy) is deferred to a future
	// wave and has no constant here.

	// FactKindVaultAuthRole is the "vault_auth_role" fact kind.
	FactKindVaultAuthRole = "vault_auth_role"
	// FactKindVaultACLPolicy is the "vault_acl_policy" fact kind.
	FactKindVaultACLPolicy = "vault_acl_policy"
	// FactKindVaultKVMetadata is the "vault_kv_metadata" fact kind.
	FactKindVaultKVMetadata = "vault_kv_metadata"
	// FactKindKubernetesServiceAccount is the "k8s_service_account" fact kind.
	FactKindKubernetesServiceAccount = "k8s_service_account"
	// FactKindKubernetesWorkloadIdentityUse is the
	// "k8s_workload_identity_use" fact kind.
	FactKindKubernetesWorkloadIdentityUse = "k8s_workload_identity_use"
	// FactKindEKSIRSAAnnotation is the "eks_irsa_annotation" fact kind.
	FactKindEKSIRSAAnnotation = "eks_irsa_annotation"
	// FactKindEKSPodIdentityAssociation is the "eks_pod_identity_association"
	// fact kind.
	FactKindEKSPodIdentityAssociation = "eks_pod_identity_association"
	// FactKindKubernetesGCPWorkloadIdentityBinding is the
	// "k8s_gcp_workload_identity_binding" fact kind.
	FactKindKubernetesGCPWorkloadIdentityBinding = "k8s_gcp_workload_identity_binding" // #nosec G101 -- fact-kind identifier string, not a credential
	// The work_item family fact-kind strings are DOTTED, matching the
	// incident/kubernetes_live/oci_registry/package_registry/sbom_attestation
	// convention above. The dots are part of the wire kind the collector
	// already emits (go/internal/facts.WorkItemRecordFactKind and siblings);
	// the values here MATCH those wire strings byte-for-byte and never invent
	// or rename the namespace. TestFactSchemaKindsMatchWireFactKinds (reducer
	// side) asserts each stays byte-equal to its facts.*FactKind counterpart.
	// Unlike every other family in this module, the decode site for this
	// family is the QUERY read-model layer (go/internal/query), not the
	// reducer or projector — see workitem/v1/README.md.

	// FactKindWorkItemRecord is the "work_item.record" fact kind.
	FactKindWorkItemRecord = "work_item.record"
	// FactKindWorkItemTransition is the "work_item.transition" fact kind.
	FactKindWorkItemTransition = "work_item.transition"
	// FactKindWorkItemExternalLink is the "work_item.external_link" fact kind.
	FactKindWorkItemExternalLink = "work_item.external_link"
	// FactKindWorkItemProjectMetadata is the "work_item.project_metadata" fact
	// kind.
	FactKindWorkItemProjectMetadata = "work_item.project_metadata"
	// FactKindWorkItemIssueTypeMetadata is the
	// "work_item.issue_type_metadata" fact kind.
	FactKindWorkItemIssueTypeMetadata = "work_item.issue_type_metadata"
	// FactKindWorkItemStatusMetadata is the "work_item.status_metadata" fact
	// kind.
	FactKindWorkItemStatusMetadata = "work_item.status_metadata"
	// FactKindWorkItemWorkflowMetadata is the "work_item.workflow_metadata"
	// fact kind.
	FactKindWorkItemWorkflowMetadata = "work_item.workflow_metadata"
	// FactKindWorkItemFieldMetadata is the "work_item.field_metadata" fact
	// kind.
	FactKindWorkItemFieldMetadata = "work_item.field_metadata"
	// FactKindWorkItemMetadataWarning is the "work_item.metadata_warning"
	// fact kind.
	FactKindWorkItemMetadataWarning = "work_item.metadata_warning"
	// FactKindSecurityAlertRepositoryAlert is the
	// "security_alert.repository_alert" fact kind.
	FactKindSecurityAlertRepositoryAlert = "security_alert.repository_alert"

	// The observability family fact-kind strings are DOTTED, matching the
	// incident/kubernetes_live/oci_registry/package_registry/sbom_attestation/
	// work_item convention above. The dots are part of the wire kind the
	// collectors already emit (go/internal/facts.Observability*FactKind and
	// siblings); the values here MATCH those wire strings byte-for-byte and never
	// invent or rename the namespace. TestFactSchemaKindsMatchWireFactKinds
	// (reducer side) asserts each stays byte-equal to its facts.*FactKind
	// counterpart. All eighteen kinds are consumed by the reducer's
	// observability_coverage_correlation domain (observability/v1/doc.go).

	// FactKindObservabilityDeclaredFolder is the "observability.declared_folder"
	// fact kind.
	FactKindObservabilityDeclaredFolder = "observability.declared_folder"
	// FactKindObservabilityDeclaredDashboard is the
	// "observability.declared_dashboard" fact kind.
	FactKindObservabilityDeclaredDashboard = "observability.declared_dashboard"
	// FactKindObservabilityDeclaredDatasource is the
	// "observability.declared_datasource" fact kind.
	FactKindObservabilityDeclaredDatasource = "observability.declared_datasource"
	// FactKindObservabilityDeclaredAlertRule is the
	// "observability.declared_alert_rule" fact kind.
	FactKindObservabilityDeclaredAlertRule = "observability.declared_alert_rule"
	// FactKindObservabilityDeclaredScrapeConfig is the
	// "observability.declared_scrape_config" fact kind.
	FactKindObservabilityDeclaredScrapeConfig = "observability.declared_scrape_config"
	// FactKindObservabilityDeclaredMetricRule is the
	// "observability.declared_metric_rule" fact kind.
	FactKindObservabilityDeclaredMetricRule = "observability.declared_metric_rule"
	// FactKindObservabilityDeclaredMetricRoute is the
	// "observability.declared_metric_route" fact kind.
	FactKindObservabilityDeclaredMetricRoute = "observability.declared_metric_route"
	// FactKindObservabilityDeclaredLogRoute is the
	// "observability.declared_log_route" fact kind.
	FactKindObservabilityDeclaredLogRoute = "observability.declared_log_route"
	// FactKindObservabilityDeclaredTraceRoute is the
	// "observability.declared_trace_route" fact kind.
	FactKindObservabilityDeclaredTraceRoute = "observability.declared_trace_route"
	// FactKindObservabilityAppliedResource is the
	// "observability.applied_resource" fact kind.
	FactKindObservabilityAppliedResource = "observability.applied_resource"
	// FactKindObservabilityAppliedSyncState is the
	// "observability.applied_sync_state" fact kind.
	FactKindObservabilityAppliedSyncState = "observability.applied_sync_state"
	// FactKindObservabilityObservedDashboard is the
	// "observability.observed_dashboard" fact kind.
	FactKindObservabilityObservedDashboard = "observability.observed_dashboard"
	// FactKindObservabilityObservedTarget is the
	// "observability.observed_target" fact kind.
	FactKindObservabilityObservedTarget = "observability.observed_target"
	// FactKindObservabilityObservedRule is the "observability.observed_rule"
	// fact kind.
	FactKindObservabilityObservedRule = "observability.observed_rule"
	// FactKindObservabilityObservedLogSignal is the
	// "observability.observed_log_signal" fact kind.
	FactKindObservabilityObservedLogSignal = "observability.observed_log_signal"
	// FactKindObservabilityObservedTraceSignal is the
	// "observability.observed_trace_signal" fact kind.
	FactKindObservabilityObservedTraceSignal = "observability.observed_trace_signal"
	// FactKindObservabilityCoverageWarning is the
	// "observability.coverage_warning" fact kind.
	FactKindObservabilityCoverageWarning = "observability.coverage_warning"
	// FactKindObservabilitySourceInstance is the
	// "observability.source_instance" fact kind.
	FactKindObservabilitySourceInstance = "observability.source_instance"
)
