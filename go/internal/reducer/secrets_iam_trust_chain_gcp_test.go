// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func gcpPrincipalFact(fingerprint string) facts.Envelope {
	return facts.Envelope{
		FactID:   "principal-" + fingerprint,
		FactKind: facts.GCPIAMPrincipalFactKind,
		Payload: map[string]any{
			"principal_fingerprint": fingerprint,
			"principal_type":        "gcp_service_account",
			"project_id":            "demo-proj",
		},
	}
}

func gcpPermissionFact(factID, fingerprint, role string, isSecret, broad bool) facts.Envelope {
	return facts.Envelope{
		FactID:   factID,
		FactKind: facts.GCPIAMPermissionPolicyFactKind,
		Payload: map[string]any{
			"principal_fingerprint":       fingerprint,
			"role":                        role,
			"resource_full_resource_name": "//res/" + role,
			"resource_is_secret":          isSecret,
			"broad_role":                  broad,
		},
	}
}

func findGCPObservation(models SecretsIAMTrustChainReadModels, riskType string) (SecretsIAMPrivilegePostureObservation, bool) {
	for _, obs := range models.PrivilegePostureObservations {
		if obs.RiskType == riskType {
			return obs, true
		}
	}
	return SecretsIAMPrivilegePostureObservation{}, false
}

func TestGCPSecretAccessGrantProducesPostureObservation(t *testing.T) {
	t.Parallel()

	const fp = "sha256:svc-a"
	models, err := BuildSecretsIAMTrustChainReadModels([]facts.Envelope{
		gcpPrincipalFact(fp),
		gcpPermissionFact("perm-1", fp, "roles/secretmanager.secretAccessor", true, false),
	})
	if err != nil {
		t.Fatalf("BuildSecretsIAMTrustChainReadModels() error = %v, want nil", err)
	}

	obs, ok := findGCPObservation(models, gcpRiskSecretAccessGrant)
	if !ok {
		t.Fatalf("expected a %s posture observation, got %+v", gcpRiskSecretAccessGrant, models.PrivilegePostureObservations)
	}
	if obs.State != SecretsIAMTrustChainStateExact {
		t.Fatalf("state = %q, want exact", obs.State)
	}
	if obs.SubjectFingerprint != fp {
		t.Fatalf("subject = %q, want %q", obs.SubjectFingerprint, fp)
	}
	if len(obs.EvidenceFactIDs) != 2 {
		t.Fatalf("evidence fact ids = %v, want principal + permission", obs.EvidenceFactIDs)
	}
}

func TestGCPBroadRoleGrantProducesPostureObservation(t *testing.T) {
	t.Parallel()

	const fp = "sha256:svc-b"
	models, err := BuildSecretsIAMTrustChainReadModels([]facts.Envelope{
		gcpPrincipalFact(fp),
		gcpPermissionFact("perm-2", fp, "roles/owner", false, true),
	})
	if err != nil {
		t.Fatalf("BuildSecretsIAMTrustChainReadModels() error = %v, want nil", err)
	}

	if _, ok := findGCPObservation(models, gcpRiskBroadRoleGrant); !ok {
		t.Fatalf("expected a %s posture observation, got %+v", gcpRiskBroadRoleGrant, models.PrivilegePostureObservations)
	}
}

func TestGCPNarrowNonSecretGrantProducesNoObservation(t *testing.T) {
	t.Parallel()

	const fp = "sha256:svc-c"
	models, err := BuildSecretsIAMTrustChainReadModels([]facts.Envelope{
		gcpPrincipalFact(fp),
		gcpPermissionFact("perm-3", fp, "roles/storage.objectViewer", false, false),
	})
	if err != nil {
		t.Fatalf("BuildSecretsIAMTrustChainReadModels() error = %v, want nil", err)
	}

	if len(models.PrivilegePostureObservations) != 0 {
		t.Fatalf("narrow non-secret grant must not produce posture, got %+v", models.PrivilegePostureObservations)
	}
}

func TestGCPWorkloadIdentityTrustAdmitsExactWorkloadToSecretAccessPath(t *testing.T) {
	t.Parallel()

	serviceAccountKey := "sha256:k8s-service-account"
	targetFingerprint := "sha256:gcp-service-account"
	targetEmailDigest := "sha256:gcp-service-account-email"
	subjectFingerprint := "sha256:gke-subject"
	secretResource := "//secretmanager.googleapis.com/projects/demo-proj/secrets/db"
	models, err := BuildSecretsIAMTrustChainReadModels([]facts.Envelope{
		secretsIAMReducerFact("sa", facts.KubernetesServiceAccountFactKind, "k8s-scope", "k8s-gen", map[string]any{
			"provider":                 "kubernetes",
			"service_account_join_key": serviceAccountKey,
		}),
		secretsIAMReducerFact("workload", facts.KubernetesWorkloadIdentityUseFactKind, "k8s-scope", "k8s-gen", map[string]any{
			"provider":                 "kubernetes",
			"service_account_join_key": serviceAccountKey,
			"workload_object_id":       "workload-stable-id",
			"workload_kind":            "deployments",
		}),
		secretsIAMReducerFact("k8s-gcp-binding", facts.KubernetesGCPWorkloadIdentityBindingFactKind, "k8s-scope", "k8s-gen", map[string]any{
			"provider":                                  "kubernetes",
			"service_account_join_key":                  serviceAccountKey,
			"gcp_service_account_email_digest":          targetEmailDigest,
			"gcp_workload_identity_subject_fingerprint": subjectFingerprint,
		}),
		gcpPrincipalFact(targetFingerprint),
		secretsIAMReducerFact("gcp-trust", facts.GCPIAMTrustPolicyFactKind, "gcp-scope", "gcp-gen", map[string]any{
			"provider":                                  "gcp_iam",
			"target_principal_fingerprint":              targetFingerprint,
			"target_service_account_email_digest":       targetEmailDigest,
			"target_service_account_cloud_resource_uid": "gcp-cloud-resource-sa",
			"gcp_workload_identity_subject_fingerprint": subjectFingerprint,
			"impersonation_mode":                        "workload_identity",
			"role":                                      "roles/iam.workloadIdentityUser",
		}),
		secretsIAMReducerFact("gcp-perm", facts.GCPIAMPermissionPolicyFactKind, "gcp-scope", "gcp-gen", map[string]any{
			"provider":                    "gcp_iam",
			"principal_fingerprint":       targetFingerprint,
			"role":                        "roles/secretmanager.secretAccessor",
			"resource_full_resource_name": secretResource,
			"resource_asset_type":         "secretmanager.googleapis.com/Secret",
			"resource_is_secret":          true,
		}),
	})
	if err != nil {
		t.Fatalf("BuildSecretsIAMTrustChainReadModels() error = %v, want nil", err)
	}

	if got, want := len(models.IdentityTrustChains), 1; got != want {
		t.Fatalf("IdentityTrustChains len = %d, want %d: %#v", got, want, models.IdentityTrustChains)
	}
	chain := models.IdentityTrustChains[0]
	if got, want := chain.State, SecretsIAMTrustChainStateExact; got != want {
		t.Fatalf("chain.State = %q, want %q", got, want)
	}
	if got, want := chain.WorkloadObjectID, "workload-stable-id"; got != want {
		t.Fatalf("WorkloadObjectID = %q, want %q", got, want)
	}
	if got, want := chain.GCPServiceAccountFingerprint, targetFingerprint; got != want {
		t.Fatalf("GCPServiceAccountFingerprint = %q, want %q", got, want)
	}
	if got, want := chain.GCPServiceAccountCloudResourceUID, "gcp-cloud-resource-sa"; got != want {
		t.Fatalf("GCPServiceAccountCloudResourceUID = %q, want %q", got, want)
	}
	if got, want := chain.GCPServiceAccountAssumeMode, "workload_identity"; got != want {
		t.Fatalf("GCPServiceAccountAssumeMode = %q, want %q", got, want)
	}
	for _, gap := range models.PostureGaps {
		if gap.GapType == "missing_identity_provider_hop" {
			t.Fatalf("unexpected missing identity-provider gap for exact GCP path: %#v", gap)
		}
	}

	if got, want := len(models.SecretAccessPaths), 1; got != want {
		t.Fatalf("SecretAccessPaths len = %d, want %d: %#v", got, want, models.SecretAccessPaths)
	}
	path := models.SecretAccessPaths[0]
	if got, want := path.State, SecretsIAMTrustChainStateExact; got != want {
		t.Fatalf("path.State = %q, want %q", got, want)
	}
	if got, want := path.CloudProvider, "gcp"; got != want {
		t.Fatalf("CloudProvider = %q, want %q", got, want)
	}
	if path.CloudSecretResourceFingerprint == "" || path.CloudSecretResourceFingerprint == secretResource {
		t.Fatalf("CloudSecretResourceFingerprint = %q, want redacted non-empty fingerprint", path.CloudSecretResourceFingerprint)
	}
}

func TestGCPWorkloadIdentityTrustDoesNotAdmitMetadataOnlySecretRole(t *testing.T) {
	t.Parallel()

	serviceAccountKey := "sha256:k8s-service-account"
	targetFingerprint := "sha256:gcp-service-account"
	targetEmailDigest := "sha256:gcp-service-account-email"
	subjectFingerprint := "sha256:gke-subject"
	models, err := BuildSecretsIAMTrustChainReadModels([]facts.Envelope{
		secretsIAMReducerFact("sa", facts.KubernetesServiceAccountFactKind, "k8s-scope", "k8s-gen", map[string]any{
			"provider":                 "kubernetes",
			"service_account_join_key": serviceAccountKey,
		}),
		secretsIAMReducerFact("workload", facts.KubernetesWorkloadIdentityUseFactKind, "k8s-scope", "k8s-gen", map[string]any{
			"provider":                 "kubernetes",
			"service_account_join_key": serviceAccountKey,
			"workload_object_id":       "workload-stable-id",
			"workload_kind":            "deployments",
		}),
		secretsIAMReducerFact("k8s-gcp-binding", facts.KubernetesGCPWorkloadIdentityBindingFactKind, "k8s-scope", "k8s-gen", map[string]any{
			"provider":                                  "kubernetes",
			"service_account_join_key":                  serviceAccountKey,
			"gcp_service_account_email_digest":          targetEmailDigest,
			"gcp_workload_identity_subject_fingerprint": subjectFingerprint,
		}),
		gcpPrincipalFact(targetFingerprint),
		secretsIAMReducerFact("gcp-trust", facts.GCPIAMTrustPolicyFactKind, "gcp-scope", "gcp-gen", map[string]any{
			"provider":                                  "gcp_iam",
			"target_principal_fingerprint":              targetFingerprint,
			"target_service_account_email_digest":       targetEmailDigest,
			"gcp_workload_identity_subject_fingerprint": subjectFingerprint,
			"impersonation_mode":                        "workload_identity",
			"role":                                      "roles/iam.workloadIdentityUser",
		}),
		secretsIAMReducerFact("gcp-perm-viewer", facts.GCPIAMPermissionPolicyFactKind, "gcp-scope", "gcp-gen", map[string]any{
			"provider":                    "gcp_iam",
			"principal_fingerprint":       targetFingerprint,
			"role":                        "roles/secretmanager.viewer",
			"resource_full_resource_name": "//secretmanager.googleapis.com/projects/demo-proj/secrets/db",
			"resource_asset_type":         "secretmanager.googleapis.com/Secret",
			"resource_is_secret":          true,
		}),
	})
	if err != nil {
		t.Fatalf("BuildSecretsIAMTrustChainReadModels() error = %v, want nil", err)
	}

	if got, want := len(models.IdentityTrustChains), 1; got != want {
		t.Fatalf("IdentityTrustChains len = %d, want %d: %#v", got, want, models.IdentityTrustChains)
	}
	if len(models.SecretAccessPaths) != 0 {
		t.Fatalf("metadata-only Secret Manager role produced secret access paths: %#v", models.SecretAccessPaths)
	}
}

func TestGCPWorkloadIdentityBindingMissingTrustDoesNotEmitGenericGap(t *testing.T) {
	t.Parallel()

	serviceAccountKey := "sha256:k8s-service-account"
	models, err := BuildSecretsIAMTrustChainReadModels([]facts.Envelope{
		secretsIAMReducerFact("sa", facts.KubernetesServiceAccountFactKind, "k8s-scope", "k8s-gen", map[string]any{
			"provider":                 "kubernetes",
			"service_account_join_key": serviceAccountKey,
		}),
		secretsIAMReducerFact("workload", facts.KubernetesWorkloadIdentityUseFactKind, "k8s-scope", "k8s-gen", map[string]any{
			"provider":                 "kubernetes",
			"service_account_join_key": serviceAccountKey,
			"workload_object_id":       "workload-stable-id",
			"workload_kind":            "deployments",
		}),
		secretsIAMReducerFact("k8s-gcp-binding", facts.KubernetesGCPWorkloadIdentityBindingFactKind, "k8s-scope", "k8s-gen", map[string]any{
			"provider":                                  "kubernetes",
			"service_account_join_key":                  serviceAccountKey,
			"gcp_service_account_email_digest":          "sha256:gcp-service-account-email",
			"gcp_workload_identity_subject_fingerprint": "sha256:gke-subject",
		}),
	})
	if err != nil {
		t.Fatalf("BuildSecretsIAMTrustChainReadModels() error = %v, want nil", err)
	}

	seenGCPGap := false
	for _, gap := range models.PostureGaps {
		switch gap.GapType {
		case "missing_gcp_workload_identity_trust":
			seenGCPGap = true
		case "missing_identity_provider_hop":
			t.Fatalf("unexpected generic identity-provider gap for GCP binding: %#v", gap)
		}
	}
	if !seenGCPGap {
		t.Fatalf("missing_gcp_workload_identity_trust gap not emitted: %#v", models.PostureGaps)
	}
}

func TestGCPGrantWithoutPrincipalIsNotFabricated(t *testing.T) {
	t.Parallel()

	// A permission fact with no matching principal fact must not fabricate an
	// identity-grant observation.
	models, err := BuildSecretsIAMTrustChainReadModels([]facts.Envelope{
		gcpPermissionFact("perm-4", "sha256:orphan", "roles/secretmanager.secretAccessor", true, false),
	})
	if err != nil {
		t.Fatalf("BuildSecretsIAMTrustChainReadModels() error = %v, want nil", err)
	}

	if len(models.PrivilegePostureObservations) != 0 {
		t.Fatalf("orphan grant must not produce posture, got %+v", models.PrivilegePostureObservations)
	}
}
