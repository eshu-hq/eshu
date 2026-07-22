// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package projector

import (
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

// fanOutParityScopeAndGeneration returns the single scope/generation every
// fact in fanOutParityFixture shares. appendScopeGenerationReducerIntents
// does not validate a fact's own ScopeID/GenerationID against the caller's
// scopeValue/generation (that is validateFactBoundary's job, one layer up in
// buildProjection), so every builder call in this file's tests can safely
// reuse one scope for a mixed-domain fixture.
func fanOutParityScopeAndGeneration() (scope.IngestionScope, scope.ScopeGeneration) {
	scopeValue := scope.IngestionScope{ScopeID: "mixed:fanout:demo", SourceSystem: "mixed"}
	generation := scope.ScopeGeneration{
		ScopeID:      scopeValue.ScopeID,
		GenerationID: "gen-fanout-1",
		ObservedAt:   time.Date(2026, time.July, 1, 12, 0, 0, 0, time.UTC),
	}
	return scopeValue, generation
}

// fanOutParityFixture builds one inputFacts slice spanning most of the 40
// build*ReducerIntent probes appendScopeGenerationReducerIntents fans out to
// (issue #4875). It exists to prove the shared reducerIntentFactIndex
// refactor is behavior-preserving: TestAppendScopeGenerationReducerIntentsFanOutParity
// pins the exact emitted intent set this fixture produces, run and confirmed
// against the pre-#4875 full-scan implementation, and must still pass
// byte-for-byte after the builders are rewired onto the shared index.
//
// The ordering is deliberate, not incidental. Several probes choose their
// anchor fact as "whichever accepted-kind fact appears earliest in
// inputFacts", not "earliest fact of the first kind checked in source order"
// (see reducer_intent_fact_index.go's firstAcrossKinds doc). This fixture
// interleaves facts so that, for every multi-kind probe exercised here, the
// correct anchor is NOT the first kind literally named in that probe's
// switch/list — a same-file regression (e.g. a refactor that accidentally
// iterates candidate kinds instead of candidate facts) would pick the wrong
// anchor and fail the parity test, even though every individual kind lookup
// still "works".
func fanOutParityFixture(scopeValue scope.IngestionScope, generation scope.ScopeGeneration) []facts.Envelope {
	scopeID, generationID := scopeValue.ScopeID, generation.GenerationID

	return []facts.Envelope{
		// Irrelevant decoys: no build*ReducerIntent probe matches this kind.
		// Their only job is to widen inputFacts so a full O(N) scan (the
		// pre-#4875 behavior) is not accidentally free on this fixture.
		{FactID: "decoy-0", FactKind: "code_symbol_reference"},

		// Earliest gcp_cloud_resource fact: cloud_inventory_admission's
		// candidate kinds are {aws_resource, gcp_cloud_resource,
		// azure_cloud_resource}; this must win over the aws_resource and
		// azure_cloud_resource facts placed later below.
		{
			FactID: "gcp-resource-1", ScopeID: scopeID, GenerationID: generationID,
			FactKind: facts.GCPCloudResourceFactKind, SchemaVersion: facts.GCPCloudResourceSchemaVersion,
			CollectorKind: "gcp_cloud", SourceRef: facts.Ref{SourceSystem: "gcp"},
			Payload: map[string]any{
				"project_id": "demo", "asset_type": "run.googleapis.com/Service",
				"full_resource_name": "//run.googleapis.com/projects/demo/locations/us-central1/services/api",
			},
		},

		// Earliest gcp_image_reference fact: container_image_identity's
		// candidate kinds include this and oci_registry.image_manifest /
		// oci_registry.image_referrer placed later; this must win.
		gcpImageReferenceEnvelope("gcp-image-1", scopeID, generationID),

		{FactID: "decoy-1", FactKind: "code_symbol_reference"},

		// Earliest incident-routing candidate kind (observed PagerDuty
		// service): incident_routing_materialization's candidate kinds are
		// {incident.record} + facts.IncidentRoutingFactKinds(); this must win
		// over the incident.record fact placed later.
		incidentRoutingObservedServiceEnvelope("incident-routing-observed-1", scopeID, generationID),

		// Earliest package_registry.package fact. This single fact anchors
		// THREE different multi-kind probes below because it is the earliest
		// present fact of a kind each of them accepts:
		//   - package_source_correlation (kind-priority fallback: no
		//     package_registry.source_hint fact is present, so this wins)
		//   - security_alert_reconciliation (candidate kinds {security_alert.
		//     repository_alert, package_registry.package}; this precedes the
		//     security_alert.repository_alert fact placed later)
		//   - supply_chain_impact (11-kind candidate set; this precedes every
		//     other candidate-kind fact placed later)
		packageIdentityEnvelope("package-identity-1", scopeID, generationID),

		{FactID: "decoy-2", FactKind: "code_symbol_reference"},

		// aws_resource (generic, non-observability, non-instance-profile
		// resource_type): anchors aws_cloud_runtime_drift,
		// aws_resource_materialization, and workload_cloud_relationship_
		// materialization, none of which filter by resource_type.
		awsResourceEnvelope("aws-resource-generic-1", scopeID, generationID),

		{
			FactID: "azure-resource-1", ScopeID: scopeID, GenerationID: generationID,
			FactKind: facts.AzureCloudResourceFactKind, SchemaVersion: facts.AzureCloudResourceSchemaVersion,
			CollectorKind: "azure", SourceRef: facts.Ref{SourceSystem: "azure"},
			Payload: map[string]any{
				"arm_resource_id": "/subscriptions/sub-1/resourceGroups/rg/providers/Microsoft.Compute/virtualMachines/vm",
				"resource_type":   "microsoft.compute/virtualmachines",
			},
		},
		{
			FactID: "azure-relationship-1", ScopeID: scopeID, GenerationID: generationID,
			FactKind: facts.AzureCloudRelationshipFactKind, SchemaVersion: facts.AzureCloudRelationshipSchemaVersion,
			CollectorKind: "azure", SourceRef: facts.Ref{SourceSystem: "azure"},
			Payload: map[string]any{
				"source_arm_resource_id": "/subscriptions/sub-1/resourceGroups/rg/providers/Microsoft.Compute/virtualMachines/vm",
				"target_arm_resource_id": "/subscriptions/sub-1/resourceGroups/rg/providers/Microsoft.Network/networkInterfaces/nic",
				"relationship_type":      "managed_by",
				"support_state":          "supported",
			},
		},

		// aws_relationship with a non-container-image target_type so it does
		// NOT also trigger container_image_identity.
		{
			FactID: "aws-relationship-1", ScopeID: scopeID, GenerationID: generationID,
			FactKind: facts.AWSRelationshipFactKind, SchemaVersion: facts.AWSRelationshipSchemaVersion,
			CollectorKind: "aws_cloud", SourceRef: facts.Ref{SourceSystem: "aws"},
			Payload: map[string]any{
				"account_id": "123456789012", "region": "us-east-1",
				"relationship_type": "ATTACHED_TO", "target_type": "ec2_instance",
				"source_resource_id": "arn:aws:ec2:us-east-1:123456789012:volume/vol-1",
				"target_resource_id": "arn:aws:ec2:us-east-1:123456789012:instance/i-1",
				"source_arn":         "arn:aws:ec2:us-east-1:123456789012:volume/vol-1",
				"target_arn":         "arn:aws:ec2:us-east-1:123456789012:instance/i-1",
			},
		},

		{
			FactID: "gcp-relationship-1", ScopeID: scopeID, GenerationID: generationID,
			FactKind: facts.GCPCloudRelationshipFactKind, SchemaVersion: facts.GCPCloudRelationshipSchemaVersion,
			CollectorKind: "gcp_cloud", SourceRef: facts.Ref{SourceSystem: "gcp"},
			Payload: map[string]any{
				"source_full_resource_name": "//run.googleapis.com/projects/demo/locations/us-central1/services/api",
				"target_full_resource_name": "//iam.googleapis.com/projects/demo/serviceAccounts/api@demo.iam.gserviceaccount.com",
				"relationship_type":         "runs_as",
			},
		},

		{FactID: "decoy-3", FactKind: "code_symbol_reference"},

		// EC2 posture without an instance-profile ARN: anchors the three
		// unconditional EC2-posture probes (node materialization, internet
		// exposure, block-device KMS posture). It is deliberately first among
		// the two EC2 posture facts so ec2_uses_profile_materialization (which
		// filters on a non-blank instance_profile_arn) must skip it and pick
		// the second one below.
		ec2UsesProfileIntentEnvelope("ec2-posture-no-profile-1", scopeID, generationID, "i-aaa", ""),
		ec2UsesProfileIntentEnvelope("ec2-posture-with-profile-1", scopeID, generationID, "i-bbb",
			"arn:aws:iam::111122223333:instance-profile/app"),

		// IAM permission that is NOT a trust statement: iam_can_assume_
		// materialization must skip it and pick the trust statement below.
		iamTrustPermissionEnvelope("iam-permission-identity-1", scopeID, generationID, "identity"),
		iamTrustPermissionEnvelope("iam-permission-trust-1", scopeID, generationID, "trust"),

		// S3 posture without a logging target: anchors the unconditional
		// s3_internet_exposure_materialization probe. s3_logs_to_
		// materialization filters on a non-blank logging_target_bucket, so it
		// must skip this one and pick the second S3 posture fact below.
		s3PostureIntentEnvelope("s3-posture-no-logging-1", scopeID, generationID, ""),
		s3PostureIntentEnvelope("s3-posture-with-logging-1", scopeID, generationID, "central-logs"),

		s3ExternalPrincipalGrantIntentEnvelope("s3-external-grant-1", scopeID, generationID,
			"account", "999988887777", "granted"),

		rdsPostureIntentEnvelope("rds-posture-1", scopeID, generationID),

		// aws_resource typed as an IAM instance profile: iam_instance_profile_
		// role_materialization filters on resource_type ==
		// "aws_iam_instance_profile", so this (not aws-resource-generic-1
		// above) must be its anchor.
		iamInstanceProfileResourceFact("aws-resource-iam-profile-1", scopeID, generationID,
			"arn:aws:iam::123456789012:role/app-role"),

		// aws_resource typed as a CloudWatch alarm: observability_coverage_
		// materialization and the AWS branch of observability_coverage_
		// correlation both filter to the observabilityResourceTypes set, so
		// this (not aws-resource-generic-1 or aws-resource-iam-profile-1)
		// must be their anchor.
		observabilityAWSResourceEnvelope("aws-resource-observability-1", scopeID, generationID, "aws_cloudwatch_alarm"),

		sgRuleFactEnvelope(),

		{FactID: "decoy-4", FactKind: "code_symbol_reference"},

		kubernetesPodTemplateEnvelope("k8s-pod-template-1", scopeID, generationID),
		kubernetesNamespaceEnvelope("k8s-namespace-1", scopeID, generationID),

		// oci_registry.image_manifest: a supply_chain_impact candidate kind,
		// but container_image_identity's earliest candidate is still
		// gcp-image-1 above.
		ociRegistryManifestEnvelope("oci-manifest-1", scopeID, generationID),

		// oci_registry.image_referrer: candidate for BOTH
		// sbom_attestation_attachment and supply_chain_impact. It precedes
		// the sbom.document fact below, so sbom_attestation_attachment must
		// anchor here, not on the SBOM document — proving the probe does not
		// take "first case in its switch" as a shortcut for "first fact in
		// original order".
		ociRegistryReferrerEnvelope("oci-referrer-1", scopeID, generationID),

		{
			FactID: "sbom-document-1", ScopeID: scopeID, GenerationID: generationID,
			FactKind: facts.SBOMDocumentFactKind, SchemaVersion: facts.SBOMAttestationSchemaVersionV1,
			SourceRef: facts.Ref{SourceSystem: "sbom_attestation"},
			Payload: map[string]any{
				"document_id":     "doc-team-api",
				"document_digest": "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
				"subject_digest":  "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			},
		},

		// security_alert.repository_alert: security_alert_reconciliation's
		// other candidate kind, but package-identity-1 above already won that
		// probe. supply_chain_impact also accepts this kind but likewise
		// already anchored on package-identity-1.
		{
			FactID: "security-alert-1", ScopeID: scopeID, GenerationID: generationID,
			FactKind: facts.SecurityAlertRepositoryAlertFactKind, SchemaVersion: facts.SecurityAlertSchemaVersionV1,
			SourceRef: facts.Ref{SourceSystem: "security_alert"},
			Payload: map[string]any{
				"provider": "github_dependabot", "provider_alert_number": int64(42),
				"repository_id": scopeID, "package_id": "npm://registry.npmjs.org/left-pad",
			},
		},

		// incident.record: incident_routing_materialization's other candidate
		// kind, but incident-routing-observed-1 above already won that probe.
		incidentRoutingIncidentEnvelope("incident-record-1", scopeID, generationID),

		{
			FactID: "secrets-iam-principal-1", ScopeID: scopeID, GenerationID: generationID,
			FactKind: facts.AWSIAMPrincipalFactKind, SchemaVersion: mustSecretsIAMSchemaVersion(facts.AWSIAMPrincipalFactKind),
			CollectorKind: "secrets_iam_posture", SourceRef: facts.Ref{SourceSystem: "secrets_iam_posture"},
			Payload: map[string]any{
				"scope_id": scopeID, "generation_id": generationID, "provider": "aws",
			},
		},

		{
			FactID: "service-catalog-entity-1", ScopeID: scopeID, GenerationID: generationID,
			FactKind: facts.ServiceCatalogEntityFactKind, SchemaVersion: facts.ServiceCatalogSchemaVersionV1,
			SourceRef: facts.Ref{SourceSystem: "service_catalog"},
			Payload:   map[string]any{"entity_ref": "component:default/checkout"},
		},

		// code_dataflow_scanned marker only (no code_taint_evidence /
		// code_interproc_evidence findings present): both probes must fall
		// back to this marker as their trigger.
		{
			FactID: "code-dataflow-marker-1", ScopeID: scopeID, GenerationID: generationID,
			FactKind: facts.CodeDataflowScannedFactKind, CollectorKind: "git",
			Payload: map[string]any{"repo_id": "repo-fanout"},
		},

		// code_function_summary finding: buildCodeFunctionSummaryReducerIntent
		// prefers a finding fact over the marker above when both are present.
		{
			FactID: "code-function-summary-1", ScopeID: scopeID, GenerationID: generationID,
			FactKind: facts.CodeFunctionSummaryFactKind, CollectorKind: "git",
			Payload: map[string]any{"function_id": "repo-fanout\x1fpkg\x1f\x1fHandle"},
		},

		{FactID: "decoy-5", FactKind: "code_symbol_reference"},
	}
}

// mustSecretsIAMSchemaVersion resolves the registered schema version for a
// secrets/IAM source fact kind used by fanOutParityFixture. It panics on an
// unregistered kind so a typo in the fixture fails loudly at test-init time
// instead of silently emitting an unversioned fact.
func mustSecretsIAMSchemaVersion(kind string) string {
	version, ok := facts.SecretsIAMSchemaVersion(kind)
	if !ok {
		panic("fanOutParityFixture: kind not registered in SecretsIAMSchemaVersion: " + kind)
	}
	return version
}
