// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package projector

import (
	"sort"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// fanOutParityExpectation pins one build*ReducerIntent probe's exact
// emission decision (anchor fact, entity key, reason, source system, and
// payload) against fanOutParityFixture.
type fanOutParityExpectation struct {
	factID       string
	entityKey    string
	reason       string
	sourceSystem string
	payload      map[string]any
}

// fanOutParityExpectations is the golden set this file pins: it was captured
// by running appendScopeGenerationReducerIntents (the pre-#4875 full-scan
// implementation, unmodified) against fanOutParityFixture, before the shared
// reducerIntentFactIndex refactor touched any of the then-39
// build*ReducerIntent probes. New probes extend the same fixture and expectation
// map. TestAppendScopeGenerationReducerIntentsFanOutParity asserts the current
// implementation produces this exact set. A change that
// intentionally alters a probe's emission decision must update this map in
// the same change, with the new expectation re-derived from the intended
// behavior (never from "whatever the new code happens to output").
var fanOutParityExpectations = map[reducer.Domain]fanOutParityExpectation{
	reducer.DomainAWSCloudImageMaterialization: {
		// Trigger is aws_resource fact presence (issue #5450 retraction-safety
		// fix), the SAME persistent signal DomainAWSResourceMaterialization
		// uses -- not lambda_function_uses_image relationship presence, so
		// AWSCloudImageMaterializationHandler.Handle's retract-first logic
		// still runs (and correctly retracts to zero) in a generation whose
		// Lambda no longer carries an image relationship at all.
		factID: "aws-resource-generic-1", entityKey: "aws_resource_materialization:mixed:fanout:demo",
		reason: "aws runtime resource facts observed", sourceSystem: "aws",
	},
	reducer.DomainAWSCloudRuntimeDrift: {
		factID: "aws-resource-generic-1", entityKey: "aws_cloud_runtime_drift:mixed:fanout:demo",
		reason: "aws runtime resource facts observed", sourceSystem: "aws",
	},
	reducer.DomainAWSRelationshipMaterialization: {
		factID: "aws-relationship-1", entityKey: "aws_resource_materialization:mixed:fanout:demo",
		reason: "aws runtime relationship facts observed", sourceSystem: "aws",
	},
	reducer.DomainAWSResourceMaterialization: {
		factID: "aws-resource-generic-1", entityKey: "aws_resource_materialization:mixed:fanout:demo",
		reason: "aws runtime resource facts observed", sourceSystem: "aws",
	},
	reducer.DomainAzureRelationshipMaterialization: {
		factID: "azure-relationship-1", entityKey: "azure_resource_materialization:mixed:fanout:demo",
		reason: "azure runtime relationship facts observed", sourceSystem: "azure",
	},
	reducer.DomainAzureResourceMaterialization: {
		factID: "azure-resource-1", entityKey: "azure_resource_materialization:mixed:fanout:demo",
		reason: "azure runtime resource facts observed", sourceSystem: "azure",
	},
	reducer.DomainCloudInventoryAdmission: {
		// Earliest across {aws_resource, gcp_cloud_resource,
		// azure_cloud_resource} is the gcp_cloud_resource fact, even though
		// it is not the first-listed kind in the source set.
		factID: "gcp-resource-1", entityKey: "cloud_inventory_admission:mixed:fanout:demo",
		reason: "provider cloud-inventory source facts observed", sourceSystem: "gcp",
	},
	reducer.DomainCodeFunctionSummary: {
		// A code_function_summary finding is preferred over the
		// code_dataflow_scanned marker when both are present.
		factID: "code-function-summary-1", entityKey: "code_function_summary:mixed:fanout:demo",
		reason: "value-flow function summaries observed", sourceSystem: "git",
		payload: map[string]any{"repo_id": "repo-fanout", "full_snapshot": true},
	},
	reducer.DomainCodeInterprocEvidence: {
		factID: "code-dataflow-marker-1", entityKey: "code_interproc_evidence:mixed:fanout:demo",
		reason: "value-flow gate scanned; reconcile cross-function evidence", sourceSystem: "git",
	},
	reducer.DomainCodeTaintEvidence: {
		factID: "code-dataflow-marker-1", entityKey: "code_taint_evidence:mixed:fanout:demo",
		reason: "value-flow gate scanned; reconcile taint evidence", sourceSystem: "git",
	},
	reducer.DomainContainerImageIdentity: {
		// Earliest across container_image_identity's ~9 candidate kinds is
		// the gcp_image_reference fact, ahead of the oci_registry.image_manifest
		// fact placed later.
		factID: "gcp-image-1", entityKey: "container_image_identity:mixed:fanout:demo",
		reason: "container image identity evidence observed", sourceSystem: "gcp",
	},
	reducer.DomainEC2BlockDeviceKMSPostureMaterialization: {
		factID: "ec2-posture-no-profile-1", entityKey: "ec2_block_device_kms_posture_materialization:mixed:fanout:demo",
		reason: "ec2 block-device posture observed", sourceSystem: "aws",
	},
	reducer.DomainEC2InstanceIdentityMaterialization: {
		// Same trigger fact and reason-shape as DomainAWSResourceMaterialization
		// (firstOfKind(EC2InstancePostureFactKind), the SAME fact the node it
		// augments triggers on, #5743 residual fix), with the EC2 instance node
		// phase entity key: it must resolve against that node, not the generic
		// aws_resource_materialization phase (see the builder's doc comment).
		factID: "ec2-posture-no-profile-1", entityKey: "ec2_instance_node_materialization:mixed:fanout:demo",
		reason: "ec2 instance posture observed for ec2 instance identity projection", sourceSystem: "aws",
	},
	reducer.DomainEC2InstanceNodeMaterialization: {
		factID: "ec2-posture-no-profile-1", entityKey: "ec2_instance_node_materialization:mixed:fanout:demo",
		reason: "ec2 instance posture facts observed", sourceSystem: "aws",
	},
	reducer.DomainEC2InternetExposureMaterialization: {
		factID: "ec2-posture-no-profile-1", entityKey: "ec2_instance_node_materialization:mixed:fanout:demo",
		reason: "ec2 instance posture observed", sourceSystem: "aws",
	},
	reducer.DomainEC2UsesProfileMaterialization: {
		// Skips ec2-posture-no-profile-1 (blank instance_profile_arn) and
		// anchors the second EC2 posture fact.
		factID: "ec2-posture-with-profile-1", entityKey: "ec2_uses_profile_materialization:mixed:fanout:demo",
		reason: "ec2 instance profile usage observed", sourceSystem: "aws",
	},
	reducer.DomainGCPRelationshipMaterialization: {
		factID: "gcp-relationship-1", entityKey: "gcp_resource_materialization:mixed:fanout:demo",
		reason: "gcp runtime relationship facts observed", sourceSystem: "gcp",
	},
	reducer.DomainGCPResourceMaterialization: {
		factID: "gcp-resource-1", entityKey: "gcp_resource_materialization:mixed:fanout:demo",
		reason: "gcp cloud resource facts observed", sourceSystem: "gcp",
	},
	reducer.DomainIAMCanAssumeMaterialization: {
		// Skips the non-trust IAM permission and anchors the trust statement.
		factID: "iam-permission-trust-1", entityKey: "aws_resource_materialization:mixed:fanout:demo",
		reason: "aws iam trust statements observed", sourceSystem: "aws",
	},
	reducer.DomainIAMInstanceProfileRoleMaterialization: {
		factID: "aws-resource-iam-profile-1", entityKey: "aws_resource_materialization:mixed:fanout:demo",
		reason: "iam instance profiles observed", sourceSystem: "aws",
	},
	reducer.DomainIncidentRoutingMaterialization: {
		// Earliest across {incident.record} + IncidentRoutingFactKinds() is
		// the observed-PagerDuty-service fact, ahead of incident.record.
		factID: "incident-routing-observed-1", entityKey: "incident_routing_materialization:mixed:fanout:demo",
		reason: "pagerduty incident-routing evidence observed", sourceSystem: "pagerduty",
	},
	reducer.DomainKubernetesCorrelation: {
		factID: "k8s-pod-template-1", entityKey: "kubernetes_correlation:mixed:fanout:demo",
		reason: "kubernetes live workload evidence observed", sourceSystem: "kubernetes_live",
	},
	reducer.DomainKubernetesCorrelationMaterialization: {
		factID: "k8s-pod-template-1", entityKey: "kubernetes_workload_materialization:mixed:fanout:demo",
		reason: "kubernetes live workload pod-template facts observed", sourceSystem: "kubernetes_live",
	},
	reducer.DomainKubernetesNamespaceMaterialization: {
		factID: "k8s-namespace-1", entityKey: "kubernetes_namespace_materialization:mixed:fanout:demo",
		reason: "kubernetes live namespace facts observed", sourceSystem: "kubernetes_live",
	},
	reducer.DomainKubernetesWorkloadMaterialization: {
		factID: "k8s-pod-template-1", entityKey: "kubernetes_workload_materialization:mixed:fanout:demo",
		reason: "kubernetes live workload pod-template facts observed", sourceSystem: "kubernetes_live",
	},
	reducer.DomainObservabilityCoverageCorrelation: {
		factID: "aws-resource-observability-1", entityKey: "observability_coverage_correlation:mixed:fanout:demo",
		reason: "aws observability resource facts observed", sourceSystem: "aws",
	},
	reducer.DomainObservabilityCoverageMaterialization: {
		factID: "aws-resource-observability-1", entityKey: "aws_resource_materialization:mixed:fanout:demo",
		reason: "aws observability resource facts observed", sourceSystem: "aws",
	},
	reducer.DomainPackageSourceCorrelation: {
		factID: "package-identity-1", entityKey: "package_source_correlation:mixed:fanout:demo",
		reason: "package registry identity observed", sourceSystem: "package_registry",
	},
	reducer.DomainRDSPostureMaterialization: {
		factID: "rds-posture-1", entityKey: "aws_resource_materialization:mixed:fanout:demo",
		reason: "rds posture facts observed", sourceSystem: "aws",
	},
	reducer.DomainS3ExternalPrincipalGrantMaterialization: {
		factID: "s3-external-grant-1", entityKey: "aws_resource_materialization:mixed:fanout:demo",
		reason: "s3 external principal grant observed", sourceSystem: "aws",
	},
	reducer.DomainS3InternetExposureMaterialization: {
		factID: "s3-posture-no-logging-1", entityKey: "aws_resource_materialization:mixed:fanout:demo",
		reason: "s3 bucket posture observed", sourceSystem: "aws",
	},
	reducer.DomainS3LogsToMaterialization: {
		// Skips s3-posture-no-logging-1 (blank logging_target_bucket) and
		// anchors the second S3 posture fact.
		factID: "s3-posture-with-logging-1", entityKey: "aws_resource_materialization:mixed:fanout:demo",
		reason: "s3 bucket access logging observed", sourceSystem: "aws",
	},
	reducer.DomainSBOMAttestationAttachment: {
		// Earliest across {sbom.document, attestation.statement,
		// oci_registry.image_referrer} is the OCI referrer fact, ahead of the
		// sbom.document fact placed later — proving the probe does not take
		// "first case in its switch" as a shortcut for "first fact in
		// original order".
		factID: "oci-referrer-1", entityKey: "sbom_attestation_attachment:mixed:fanout:demo",
		reason: "sbom or attestation subject evidence observed", sourceSystem: "oci_registry",
	},
	reducer.DomainSecretsIAMTrustChain: {
		factID: "secrets-iam-principal-1", entityKey: "secrets_iam_trust_chain:mixed:fanout:demo",
		reason: "secrets/IAM source facts observed", sourceSystem: "secrets_iam_posture",
	},
	reducer.DomainSecurityAlertReconciliation: {
		// Earliest across {security_alert.repository_alert,
		// package_registry.package} is the package identity fact, ahead of
		// the security_alert.repository_alert fact placed later.
		factID: "package-identity-1", entityKey: "security_alert_reconciliation:mixed:fanout:demo",
		reason: "package registry identity observed", sourceSystem: "package_registry",
	},
	reducer.DomainSecurityGroupCidrMaterialization: {
		factID: "fact-sg-rule-1", entityKey: "aws_resource_materialization:mixed:fanout:demo",
		reason: "aws security group rule facts observed (endpoint nodes)", sourceSystem: "",
	},
	reducer.DomainSecurityGroupReachabilityMaterialization: {
		factID: "fact-sg-rule-1", entityKey: "aws_resource_materialization:mixed:fanout:demo",
		reason: "aws security group rule facts observed (reachability edges)", sourceSystem: "",
	},
	reducer.DomainSecurityGroupRuleMaterialization: {
		factID: "fact-sg-rule-1", entityKey: "aws_resource_materialization:mixed:fanout:demo",
		reason: "aws security group rule facts observed (rule nodes)", sourceSystem: "",
	},
	reducer.DomainServiceCatalogCorrelation: {
		factID: "service-catalog-entity-1", entityKey: "service_catalog_correlation:mixed:fanout:demo",
		reason: "service catalog facts observed", sourceSystem: "service_catalog",
	},
	reducer.DomainSupplyChainImpact: {
		// Earliest across supply_chain_impact's 11 candidate kinds is the
		// package identity fact, ahead of the security_alert, oci-manifest,
		// and oci-referrer facts placed later.
		factID: "package-identity-1", entityKey: "supply_chain_impact:mixed:fanout:demo",
		reason: "package registry identity observed", sourceSystem: "package_registry",
	},
	reducer.DomainWorkloadCloudRelationshipMaterialization: {
		factID: "aws-resource-generic-1", entityKey: "aws_resource_materialization:mixed:fanout:demo",
		reason: "aws resource workload anchors observed", sourceSystem: "aws",
	},
}

// TestAppendScopeGenerationReducerIntentsFanOutParity is the #4875 accuracy
// gate: it proves appendScopeGenerationReducerIntents (and, after the shared
// reducerIntentFactIndex lands, the 40 build*ReducerIntent probes it fans out
// to) emits byte-identical intents — same anchor fact, entity key, reason,
// source system, and payload for every domain — before and after the index
// refactor. fanOutParityExpectations was captured from the pre-refactor
// full-scan implementation; this test must pass unchanged on both sides of
// the refactor.
func TestAppendScopeGenerationReducerIntentsFanOutParity(t *testing.T) {
	t.Parallel()

	scopeValue, generation := fanOutParityScopeAndGeneration()
	inputFacts := fanOutParityFixture(scopeValue, generation)

	intents := appendScopeGenerationReducerIntents(nil, scopeValue, generation, inputFacts)

	byDomain := make(map[reducer.Domain]ReducerIntent, len(intents))
	for _, intent := range intents {
		if _, dup := byDomain[intent.Domain]; dup {
			t.Fatalf("domain %s emitted more than one intent; fixture must be scope-keyed 1:1 per domain", intent.Domain)
		}
		byDomain[intent.Domain] = intent
	}

	if got, want := len(byDomain), len(fanOutParityExpectations); got != want {
		gotDomains := domainNames(byDomain)
		wantDomains := make([]string, 0, len(fanOutParityExpectations))
		for domain := range fanOutParityExpectations {
			wantDomains = append(wantDomains, string(domain))
		}
		sort.Strings(gotDomains)
		sort.Strings(wantDomains)
		t.Fatalf("emitted domain count = %d, want %d\ngot domains:  %v\nwant domains: %v", got, want, gotDomains, wantDomains)
	}

	for domain, want := range fanOutParityExpectations {
		got, ok := byDomain[domain]
		if !ok {
			t.Errorf("domain %s: no intent emitted, want one", domain)
			continue
		}
		if got.ScopeID != scopeValue.ScopeID {
			t.Errorf("domain %s: ScopeID = %q, want %q", domain, got.ScopeID, scopeValue.ScopeID)
		}
		if got.GenerationID != generation.GenerationID {
			t.Errorf("domain %s: GenerationID = %q, want %q", domain, got.GenerationID, generation.GenerationID)
		}
		if got.FactID != want.factID {
			t.Errorf("domain %s: FactID = %q, want %q", domain, got.FactID, want.factID)
		}
		if got.EntityKey != want.entityKey {
			t.Errorf("domain %s: EntityKey = %q, want %q", domain, got.EntityKey, want.entityKey)
		}
		if got.Reason != want.reason {
			t.Errorf("domain %s: Reason = %q, want %q", domain, got.Reason, want.reason)
		}
		if got.SourceSystem != want.sourceSystem {
			t.Errorf("domain %s: SourceSystem = %q, want %q", domain, got.SourceSystem, want.sourceSystem)
		}
		if !payloadsEqual(got.Payload, want.payload) {
			t.Errorf("domain %s: Payload = %#v, want %#v", domain, got.Payload, want.payload)
		}
	}

	for domain := range byDomain {
		if _, expected := fanOutParityExpectations[domain]; !expected {
			t.Errorf("domain %s: unexpected intent emitted, not in fanOutParityExpectations", domain)
		}
	}
}

func domainNames(byDomain map[reducer.Domain]ReducerIntent) []string {
	names := make([]string, 0, len(byDomain))
	for domain := range byDomain {
		names = append(names, string(domain))
	}
	return names
}

func payloadsEqual(got, want map[string]any) bool {
	if len(got) != len(want) {
		return false
	}
	for key, wantValue := range want {
		gotValue, ok := got[key]
		if !ok || gotValue != wantValue {
			return false
		}
	}
	return true
}
