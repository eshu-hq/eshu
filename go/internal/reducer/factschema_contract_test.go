// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"errors"
	"fmt"
	"slices"
	"testing"

	"github.com/eshu-hq/eshu-sdk/factschema"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/facts/cloud"
	factsidentity "github.com/eshu-hq/eshu/go/internal/facts/identity"
	"github.com/eshu-hq/eshu/go/internal/facts/semantic"
	"github.com/eshu-hq/eshu/go/internal/facts/supply/chain"
	"github.com/eshu-hq/eshu/go/internal/reducer/factdecode"
)

var factSchemaWireKindCases = []struct {
	name     string
	contract string
	wireKind string
}{
	{"aws_resource", factschema.FactKindAWSResource, cloud.AWSResourceFactKind},
	{"aws_relationship", factschema.FactKindAWSRelationship, cloud.AWSRelationshipFactKind},
	{"aws_security_group_rule", factschema.FactKindAWSSecurityGroupRule, cloud.AWSSecurityGroupRuleFactKind},
	{"ec2_instance_posture", factschema.FactKindEC2InstancePosture, cloud.EC2InstancePostureFactKind},
	{"s3_bucket_posture", factschema.FactKindS3BucketPosture, cloud.S3BucketPostureFactKind},
	// NOTE (eshu-hq/eshu#9556): aws_ssm_host_application/platform rows land
	// here with the eshu-sdk release carrying these kinds (core links the
	// published eshu-sdk/factschema module, re-synced from sdk/go at
	// release time): the pinned v0.1.0-rc.10 predates them, so referencing
	// its FactKind constants cannot compile yet. The local wire-string pins
	// live in go/internal/facts/cloud/ssm_host_test.go until then.
	{"aws_iam_permission", factschema.FactKindAWSIAMPermission, cloud.AWSIAMPermissionFactKind},
	{"aws_resource_policy_permission", factschema.FactKindAWSResourcePolicyPermission, cloud.AWSResourcePolicyPermissionFactKind},
	{"aws_iam_principal", factschema.FactKindAWSIAMPrincipal, factsidentity.AWSIAMPrincipalFactKind},
	{"aws_iam_trust_policy", factschema.FactKindAWSIAMTrustPolicy, factsidentity.AWSIAMTrustPolicyFactKind},
	{"aws_iam_permission_policy", factschema.FactKindAWSIAMPermissionPolicy, factsidentity.AWSIAMPermissionPolicyFactKind},
	{"aws_iam_policy_attachment", factschema.FactKindAWSIAMPolicyAttachment, factsidentity.AWSIAMPolicyAttachmentFactKind},
	{"aws_iam_permission_boundary", factschema.FactKindAWSIAMPermissionBoundary, factsidentity.AWSIAMPermissionBoundaryFactKind},
	{"aws_iam_instance_profile", factschema.FactKindAWSIAMInstanceProfile, factsidentity.AWSIAMInstanceProfileFactKind},
	{"aws_iam_access_analyzer_finding", factschema.FactKindAWSIAMAccessAnalyzerFinding, factsidentity.AWSIAMAccessAnalyzerFindingFactKind},
	{"gcp_cloud_resource", factschema.FactKindGCPCloudResource, cloud.GCPCloudResourceFactKind},
	{"gcp_cloud_relationship", factschema.FactKindGCPCloudRelationship, cloud.GCPCloudRelationshipFactKind},
	{"gcp_collection_warning", factschema.FactKindGCPCollectionWarning, cloud.GCPCollectionWarningFactKind},
	{"gcp_dns_record", factschema.FactKindGCPDNSRecord, cloud.GCPDNSRecordFactKind},
	{"gcp_iam_policy_observation", factschema.FactKindGCPIAMPolicyObservation, cloud.GCPIAMPolicyObservationFactKind},
	{"gcp_iam_principal", factschema.FactKindGCPIAMPrincipal, factsidentity.GCPIAMPrincipalFactKind},
	{"gcp_iam_trust_policy", factschema.FactKindGCPIAMTrustPolicy, factsidentity.GCPIAMTrustPolicyFactKind},
	{"gcp_iam_permission_policy", factschema.FactKindGCPIAMPermissionPolicy, factsidentity.GCPIAMPermissionPolicyFactKind},
	{"kubernetes_live.pod_template", factschema.FactKindKubernetesLivePodTemplate, cloud.KubernetesPodTemplateFactKind},
	{"kubernetes_live.relationship", factschema.FactKindKubernetesLiveRelationship, cloud.KubernetesRelationshipFactKind},
	{"kubernetes_live.warning", factschema.FactKindKubernetesLiveWarning, cloud.KubernetesWarningFactKind},
	{"kubernetes_live.namespace", factschema.FactKindKubernetesLiveNamespace, cloud.KubernetesNamespaceFactKind},
	{"oci_registry.repository", factschema.FactKindOCIRegistryRepository, chain.OCIRegistryRepositoryFactKind},
	{"oci_registry.image_manifest", factschema.FactKindOCIImageManifest, chain.OCIImageManifestFactKind},
	{"oci_registry.image_index", factschema.FactKindOCIImageIndex, chain.OCIImageIndexFactKind},
	{"oci_registry.image_descriptor", factschema.FactKindOCIImageDescriptor, chain.OCIImageDescriptorFactKind},
	{"oci_registry.image_tag_observation", factschema.FactKindOCIImageTagObservation, chain.OCIImageTagObservationFactKind},
	{"oci_registry.image_referrer", factschema.FactKindOCIImageReferrer, chain.OCIImageReferrerFactKind},
	{"oci_registry.warning", factschema.FactKindOCIRegistryWarning, chain.OCIRegistryWarningFactKind},
	{"sbom.document", factschema.FactKindSBOMDocument, chain.SBOMDocumentFactKind},
	{"sbom.component", factschema.FactKindSBOMComponent, chain.SBOMComponentFactKind},
	{"sbom.dependency_relationship", factschema.FactKindSBOMDependencyRelationship, chain.SBOMDependencyRelationshipFactKind},
	{"sbom.external_reference", factschema.FactKindSBOMExternalReference, chain.SBOMExternalReferenceFactKind},
	{"sbom.warning", factschema.FactKindSBOMWarning, chain.SBOMWarningFactKind},
	{"attestation.statement", factschema.FactKindAttestationStatement, chain.AttestationStatementFactKind},
	{"attestation.signature_verification", factschema.FactKindAttestationSignatureVerification, chain.AttestationSignatureVerificationFactKind},
	{"attestation.slsa_provenance", factschema.FactKindAttestationSLSAProvenance, chain.AttestationSLSAProvenanceFactKind},
	{"scanner_worker.analysis", factschema.FactKindScannerWorkerAnalysis, facts.ScannerWorkerAnalysisFactKind},
	{"scanner_worker.warning", factschema.FactKindScannerWorkerWarning, facts.ScannerWorkerWarningFactKind},
	{"ci.run", factschema.FactKindCICDRun, facts.CICDRunFactKind},
	{"ci.artifact", factschema.FactKindCICDArtifact, facts.CICDArtifactFactKind},
	{"ci.environment_observation", factschema.FactKindCICDEnvironmentObservation, facts.CICDEnvironmentObservationFactKind},
	{"ci.deployment_event", factschema.FactKindCICDDeploymentEvent, facts.CICDDeploymentEventFactKind},
	{"ci.trigger_edge", factschema.FactKindCICDTriggerEdge, facts.CICDTriggerEdgeFactKind},
	{"ci.step", factschema.FactKindCICDStep, facts.CICDStepFactKind},
	{"ci.workflow_image_evidence", factschema.FactKindCICDWorkflowImageEvidence, facts.CICDWorkflowImageEvidenceFactKind},
	{"vault_auth_role", factschema.FactKindVaultAuthRole, factsidentity.VaultAuthRoleFactKind},
	{"vault_acl_policy", factschema.FactKindVaultACLPolicy, factsidentity.VaultACLPolicyFactKind},
	{"vault_kv_metadata", factschema.FactKindVaultKVMetadata, factsidentity.VaultKVMetadataFactKind},
	{"vault_auth_mount", factschema.FactKindVaultAuthMount, factsidentity.VaultAuthMountFactKind},
	{"vault_identity_entity", factschema.FactKindVaultIdentityEntity, factsidentity.VaultIdentityEntityFactKind},
	{"vault_identity_alias", factschema.FactKindVaultIdentityAlias, factsidentity.VaultIdentityAliasFactKind},
	{"vault_secret_engine_mount", factschema.FactKindVaultSecretEngineMount, factsidentity.VaultSecretEngineMountFactKind},
	{"k8s_service_account", factschema.FactKindKubernetesServiceAccount, factsidentity.KubernetesServiceAccountFactKind},
	{"k8s_workload_identity_use", factschema.FactKindKubernetesWorkloadIdentityUse, factsidentity.KubernetesWorkloadIdentityUseFactKind},
	{"eks_irsa_annotation", factschema.FactKindEKSIRSAAnnotation, factsidentity.EKSIRSAAnnotationFactKind},
	{"eks_pod_identity_association", factschema.FactKindEKSPodIdentityAssociation, factsidentity.EKSPodIdentityAssociationFactKind},
	{"k8s_gcp_workload_identity_binding", factschema.FactKindKubernetesGCPWorkloadIdentityBinding, factsidentity.KubernetesGCPWorkloadIdentityBindingFactKind},
	{"k8s_rbac_role", factschema.FactKindKubernetesRBACRole, factsidentity.KubernetesRBACRoleFactKind},
	{"k8s_rbac_binding", factschema.FactKindKubernetesRBACBinding, factsidentity.KubernetesRBACBindingFactKind},
	{"k8s_service_account_token_posture", factschema.FactKindKubernetesServiceAccountTokenPosture, factsidentity.KubernetesServiceAccountTokenPostureFactKind},
	{"secrets_iam_coverage_warning", factschema.FactKindSecretsIAMCoverageWarning, factsidentity.SecretsIAMCoverageWarningFactKind},
	{"security_alert.repository_alert", factschema.FactKindSecurityAlertRepositoryAlert, facts.SecurityAlertRepositoryAlertFactKind},
	{"ref_overlay.branch_observation", factschema.FactKindRefOverlayBranchObservation, facts.RefOverlayBranchObservationFactKind},
	{"runtime_declaration.version", factschema.FactKindRuntimeDeclarationVersion, facts.RuntimeDeclarationVersionFactKind},
	{"config_key_divergence", factschema.FactKindConfigKeyDivergence, facts.ConfigKeyDivergenceFactKind},
	{"observability.declared_folder", factschema.FactKindObservabilityDeclaredFolder, facts.ObservabilityDeclaredFolderFactKind},
	{"observability.declared_dashboard", factschema.FactKindObservabilityDeclaredDashboard, facts.ObservabilityDeclaredDashboardFactKind},
	{"observability.declared_datasource", factschema.FactKindObservabilityDeclaredDatasource, facts.ObservabilityDeclaredDatasourceFactKind},
	{"observability.declared_alert_rule", factschema.FactKindObservabilityDeclaredAlertRule, facts.ObservabilityDeclaredAlertRuleFactKind},
	{"observability.declared_scrape_config", factschema.FactKindObservabilityDeclaredScrapeConfig, facts.ObservabilityDeclaredScrapeConfigFactKind},
	{"observability.declared_metric_rule", factschema.FactKindObservabilityDeclaredMetricRule, facts.ObservabilityDeclaredMetricRuleFactKind},
	{"observability.declared_metric_route", factschema.FactKindObservabilityDeclaredMetricRoute, facts.ObservabilityDeclaredMetricRouteFactKind},
	{"observability.declared_log_route", factschema.FactKindObservabilityDeclaredLogRoute, facts.ObservabilityDeclaredLogRouteFactKind},
	{"observability.declared_trace_route", factschema.FactKindObservabilityDeclaredTraceRoute, facts.ObservabilityDeclaredTraceRouteFactKind},
	{"observability.applied_resource", factschema.FactKindObservabilityAppliedResource, facts.ObservabilityAppliedResourceFactKind},
	{"observability.applied_sync_state", factschema.FactKindObservabilityAppliedSyncState, facts.ObservabilityAppliedSyncStateFactKind},
	{"observability.observed_dashboard", factschema.FactKindObservabilityObservedDashboard, facts.ObservabilityObservedDashboardFactKind},
	{"observability.observed_target", factschema.FactKindObservabilityObservedTarget, facts.ObservabilityObservedTargetFactKind},
	{"observability.observed_rule", factschema.FactKindObservabilityObservedRule, facts.ObservabilityObservedRuleFactKind},
	{"observability.observed_log_signal", factschema.FactKindObservabilityObservedLogSignal, facts.ObservabilityObservedLogSignalFactKind},
	{"observability.observed_trace_signal", factschema.FactKindObservabilityObservedTraceSignal, facts.ObservabilityObservedTraceSignalFactKind},
	{"observability.coverage_warning", factschema.FactKindObservabilityCoverageWarning, facts.ObservabilityCoverageWarningFactKind},
	{"observability.source_instance", factschema.FactKindObservabilitySourceInstance, facts.ObservabilitySourceInstanceFactKind},
	{"semantic.documentation_observation", factschema.FactKindSemanticDocumentationObservation, semantic.SemanticDocumentationObservationFactKind},
	{"semantic.code_hint", factschema.FactKindSemanticCodeHint, semantic.SemanticCodeHintFactKind},
}

// TestFactSchemaKindsMatchWireFactKinds locks each contracts-module fact-kind
// constant (factschema.FactKind*) to the wire fact-kind constant the collector
// emits and the reducer loads (facts.*FactKind). The contracts module is a
// standalone module that cannot import go/internal/facts, so it duplicates the
// wire strings as its own constants; this reducer-side test — which CAN import
// both packages — is the drift lock that keeps the two byte-equal.
//
// Without this lock a typo or a namespaced value (for example the Wave-1
// scaffold's "aws.resource" against the real "aws_resource") would make a Decode
// dispatch silently never match a loaded envelope: no error, no dead letter,
// just a fact kind that is never decoded. Every new decoded kind MUST add a row
// here so the mismatch is a test failure at authoring time.
func TestFactSchemaKindsMatchWireFactKinds(t *testing.T) {
	t.Parallel()

	for _, tc := range factSchemaWireKindCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if tc.contract != tc.wireKind {
				t.Fatalf("factschema constant %q != wire fact kind facts constant %q; the contracts-module fact-kind string has drifted from the reducer's wire kind and Decode dispatch will silently never match", tc.contract, tc.wireKind)
			}
		})
	}
}

func TestFactSchemaWireKindCasesCoverSemanticFacts(t *testing.T) {
	t.Parallel()

	covered := map[string]bool{}
	for _, tc := range factSchemaWireKindCases {
		covered[tc.contract] = true
	}
	for _, kind := range semantic.SemanticFactKinds() {
		if !slices.Contains([]string{
			semantic.SemanticDocumentationObservationFactKind,
			semantic.SemanticCodeHintFactKind,
		}, kind) {
			t.Fatalf("unexpected semantic fact kind %q; update the semantic drift-lock coverage expectation", kind)
		}
		if !covered[kind] {
			t.Fatalf("semantic fact kind %q is missing from factSchemaWireKindCases", kind)
		}
	}
}

// TestPartitionDecodeFailures locks the single fault-isolation boundary every
// batch extractor routes decode errors through. It is the linchpin of the
// per-fact isolation design: get this classifier wrong and either a malformed
// fact aborts the whole intent (regression) or a transient/graph/projection
// error is silently swallowed as a quarantine (the "swallow failures" sin the
// Life Motto forbids). The two branches must stay exactly as asserted here.
func TestPartitionDecodeFailures(t *testing.T) {
	t.Parallel()

	env := facts.Envelope{FactID: "fact-123", FactKind: cloud.AWSResourceFactKind}

	t.Run("input_invalid decode error is quarantined", func(t *testing.T) {
		t.Parallel()

		// A *factdecode.FactDecodeError classified input_invalid (a missing required field)
		// is the ONLY quarantinable error: the extractor skips the one fact and
		// keeps projecting the rest.
		decodeErr := factdecode.NewFactDecodeError(factschema.FactKindAWSResource, &factschema.DecodeError{
			FactKind:       factschema.FactKindAWSResource,
			Classification: factschema.ClassificationInputInvalid,
			Field:          "account_id",
		})

		q, ok, fatal := factdecode.PartitionDecodeFailures(env, decodeErr)
		if !ok {
			t.Fatal("ok = false; an input_invalid *factdecode.FactDecodeError must be quarantinable")
		}
		if fatal != nil {
			t.Fatalf("fatal = %v, want nil for a quarantinable error", fatal)
		}
		if q.FactID != "fact-123" || q.FactKind != cloud.AWSResourceFactKind {
			t.Fatalf("quarantined fact identity = {%q,%q}, want {fact-123, %q}", q.FactID, q.FactKind, cloud.AWSResourceFactKind)
		}
		if q.Field != "account_id" {
			t.Fatalf("quarantined field = %q, want account_id", q.Field)
		}
		if q.Classification != factschema.ClassificationInputInvalid {
			t.Fatalf("quarantined classification = %q, want %q", q.Classification, factschema.ClassificationInputInvalid)
		}
	})

	t.Run("plain non-decode error stays fatal", func(t *testing.T) {
		t.Parallel()

		// A transient fact-load / graph-write / projection error is NOT a
		// *factdecode.FactDecodeError: it must be returned unchanged so the handler fails the
		// whole intent and the durable queue triages it (retry / dependency /
		// projection bug), never silently dropped as a quarantine.
		sentinel := errors.New("transient graph write failure")

		q, ok, fatal := factdecode.PartitionDecodeFailures(env, sentinel)
		if ok {
			t.Fatal("ok = true; a non-decode error must NOT be quarantined")
		}
		if !errors.Is(fatal, sentinel) {
			t.Fatalf("fatal = %v, want the original error returned unchanged", fatal)
		}
		if (q != factdecode.QuarantinedFact{}) {
			t.Fatalf("factdecode.QuarantinedFact = %+v, want zero value for a fatal error", q)
		}
	})

	t.Run("wrapped non-input_invalid decode error stays fatal", func(t *testing.T) {
		t.Parallel()

		// A *factdecode.FactDecodeError whose classification is NOT input_invalid (for
		// example a future unsupported-major or schema-mismatch class) is terminal
		// but not a per-fact quarantine: it must stay fatal so the operator sees
		// the real classification rather than a mislabeled input_invalid skip.
		decodeErr := factdecode.NewFactDecodeError(factschema.FactKindAWSResource, &factschema.DecodeError{
			FactKind:       factschema.FactKindAWSResource,
			Classification: "schema_mismatch",
			Err:            fmt.Errorf("unexpected shape"),
		})

		_, ok, fatal := factdecode.PartitionDecodeFailures(env, decodeErr)
		if ok {
			t.Fatal("ok = true; only ClassificationInputInvalid is quarantinable")
		}
		if fatal == nil {
			t.Fatal("fatal = nil; a non-input_invalid decode error must stay fatal")
		}
	})

	t.Run("unsupported schema major stays fatal even when labeled input_invalid", func(t *testing.T) {
		t.Parallel()

		// The contracts module currently labels an unsupported schema major
		// input_invalid, but it is version skew, not a malformed individual
		// payload. factdecode.PartitionDecodeFailures excludes the ErrUnsupportedSchemaMajor
		// sentinel from the quarantine path so a schema-rollout / version-skew fact
		// fails the whole work item for durable triage (it can succeed once the
		// reducer supports the major) rather than being silently skipped per-fact
		// as if the collector had dropped a required field.
		decodeErr := factdecode.NewFactDecodeError(factschema.FactKindAWSResource, &factschema.DecodeError{
			FactKind:       factschema.FactKindAWSResource,
			Classification: factschema.ClassificationInputInvalid,
			Err:            fmt.Errorf("%w: %q", factschema.ErrUnsupportedSchemaMajor, "2.0.0"),
		})

		q, ok, fatal := factdecode.PartitionDecodeFailures(env, decodeErr)
		if ok {
			t.Fatal("ok = true; an unsupported schema major must NOT be quarantined per-fact")
		}
		if fatal == nil {
			t.Fatal("fatal = nil; an unsupported schema major must stay fatal for durable triage")
		}
		if !errors.Is(fatal, factschema.ErrUnsupportedSchemaMajor) {
			t.Fatalf("fatal = %v, want it to wrap ErrUnsupportedSchemaMajor", fatal)
		}
		if (q != factdecode.QuarantinedFact{}) {
			t.Fatalf("factdecode.QuarantinedFact = %+v, want zero value for a fatal error", q)
		}
	})
}
