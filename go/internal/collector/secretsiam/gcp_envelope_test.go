// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package secretsiam

import (
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func gcpTestContext() GCPEnvelopeContext {
	return GCPEnvelopeContext{
		ProjectID:           "demo-proj",
		LocationBucket:      "global",
		ScopeID:             "scope-1",
		GenerationID:        "gen-1",
		CollectorInstanceID: "collector-1",
		FencingToken:        7,
		ObservedAt:          time.Unix(1_700_000_000, 0).UTC(),
		SourceURI:           "cai://demo-proj",
	}
}

func TestNewGCPPrincipalEnvelope(t *testing.T) {
	t.Parallel()

	env, err := NewGCPPrincipalEnvelope(GCPPrincipalObservation{
		Context:              gcpTestContext(),
		PrincipalFingerprint: "sha256:abc",
		MemberClass:          "serviceAccount",
	})
	if err != nil {
		t.Fatalf("NewGCPPrincipalEnvelope error = %v", err)
	}
	if env.FactKind != facts.GCPIAMPrincipalFactKind {
		t.Fatalf("FactKind = %q, want %q", env.FactKind, facts.GCPIAMPrincipalFactKind)
	}
	if got := env.Payload["provider"]; got != ProviderGCPIAM {
		t.Fatalf("provider = %v, want %q", got, ProviderGCPIAM)
	}
	if got := env.Payload["principal_fingerprint"]; got != "sha256:abc" {
		t.Fatalf("principal_fingerprint = %v", got)
	}
	if got := env.Payload["principal_type"]; got != PrincipalTypeGCPServiceAccount {
		t.Fatalf("principal_type = %v, want %q", got, PrincipalTypeGCPServiceAccount)
	}
	if got := env.Payload["project_id"]; got != "demo-proj" {
		t.Fatalf("project_id = %v", got)
	}
	if env.ScopeID != "scope-1" || env.GenerationID != "gen-1" {
		t.Fatalf("scope/gen = %q/%q", env.ScopeID, env.GenerationID)
	}
	// No raw account_id/region leakage from the AWS payload shape.
	if _, ok := env.Payload["account_id"]; ok {
		t.Fatal("gcp principal payload must not carry an account_id key")
	}
}

func TestNewGCPPrincipalEnvelopeRequiresFingerprint(t *testing.T) {
	t.Parallel()

	if _, err := NewGCPPrincipalEnvelope(GCPPrincipalObservation{Context: gcpTestContext()}); err == nil {
		t.Fatal("expected error when principal_fingerprint is empty")
	}
}

func TestNewGCPPermissionPolicyEnvelope(t *testing.T) {
	t.Parallel()

	env, err := NewGCPPermissionPolicyEnvelope(GCPPermissionPolicyObservation{
		Context:              gcpTestContext(),
		PrincipalFingerprint: "sha256:abc",
		Role:                 "roles/secretmanager.secretAccessor",
		ResourceFullName:     "//secretmanager.googleapis.com/projects/demo-proj/secrets/db",
		ResourceAssetType:    "secretmanager.googleapis.com/Secret",
		ResourceIsSecret:     true,
		ConditionPresent:     false,
	})
	if err != nil {
		t.Fatalf("NewGCPPermissionPolicyEnvelope error = %v", err)
	}
	if env.FactKind != facts.GCPIAMPermissionPolicyFactKind {
		t.Fatalf("FactKind = %q, want %q", env.FactKind, facts.GCPIAMPermissionPolicyFactKind)
	}
	if got := env.Payload["principal_fingerprint"]; got != "sha256:abc" {
		t.Fatalf("principal_fingerprint = %v (must match the principal fact join key)", got)
	}
	if got := env.Payload["role"]; got != "roles/secretmanager.secretAccessor" {
		t.Fatalf("role = %v", got)
	}
	if got := env.Payload["resource_is_secret"]; got != true {
		t.Fatalf("resource_is_secret = %v, want true", got)
	}
	if got := env.Payload["resource_full_resource_name"]; got != "//secretmanager.googleapis.com/projects/demo-proj/secrets/db" {
		t.Fatalf("resource_full_resource_name = %v", got)
	}
}

func TestNewGCPPermissionPolicyEnvelopeRequiresRoleAndResource(t *testing.T) {
	t.Parallel()

	base := GCPPermissionPolicyObservation{
		Context:              gcpTestContext(),
		PrincipalFingerprint: "sha256:abc",
		Role:                 "roles/x",
		ResourceFullName:     "//r",
	}
	missingRole := base
	missingRole.Role = ""
	if _, err := NewGCPPermissionPolicyEnvelope(missingRole); err == nil {
		t.Fatal("expected error when role is empty")
	}
	missingResource := base
	missingResource.ResourceFullName = ""
	if _, err := NewGCPPermissionPolicyEnvelope(missingResource); err == nil {
		t.Fatal("expected error when resource is empty")
	}
}

func TestNewGCPTrustPolicyEnvelopeJoinsTargetAndWorkloadIdentitySubject(t *testing.T) {
	t.Parallel()

	targetEmail := "app@demo-proj.iam.gserviceaccount.com"
	targetEmailDigest := GCPServiceAccountEmailDigest(targetEmail)
	subjectFingerprint := GCPWorkloadIdentitySubjectFingerprint("demo-proj.svc.id.goog", "payments", "checkout-sa")
	env, err := NewGCPTrustPolicyEnvelope(GCPTrustPolicyObservation{
		Context:                               gcpTestContext(),
		TargetPrincipalFingerprint:            "sha256:gcp-service-account",
		TargetServiceAccountEmailDigest:       targetEmailDigest,
		TargetServiceAccountCloudResourceUID:  "gcp://cloud-resource/service-account",
		TrustedMemberFingerprint:              "sha256:gke-member",
		TrustedMemberClass:                    GCPMemberClassServiceAccount,
		Role:                                  "roles/iam.workloadIdentityUser",
		ImpersonationMode:                     GCPImpersonationModeWorkloadIdentity,
		GCPWorkloadIdentitySubjectFingerprint: subjectFingerprint,
		GCPWorkloadIdentityMemberClass:        GCPWorkloadIdentityMemberClassServiceAccount,
	})
	if err != nil {
		t.Fatalf("NewGCPTrustPolicyEnvelope error = %v", err)
	}
	if env.FactKind != facts.GCPIAMTrustPolicyFactKind {
		t.Fatalf("FactKind = %q, want %q", env.FactKind, facts.GCPIAMTrustPolicyFactKind)
	}
	if got := env.Payload["target_principal_fingerprint"]; got != "sha256:gcp-service-account" {
		t.Fatalf("target_principal_fingerprint = %v", got)
	}
	if got := env.Payload["target_service_account_email_digest"]; got != targetEmailDigest {
		t.Fatalf("target_service_account_email_digest = %v, want %q", got, targetEmailDigest)
	}
	if got := env.Payload["gcp_workload_identity_subject_fingerprint"]; got != subjectFingerprint {
		t.Fatalf("gcp_workload_identity_subject_fingerprint = %v, want %q", got, subjectFingerprint)
	}
	if got := env.Payload["impersonation_mode"]; got != GCPImpersonationModeWorkloadIdentity {
		t.Fatalf("impersonation_mode = %v, want %q", got, GCPImpersonationModeWorkloadIdentity)
	}
	forbidden := []string{
		targetEmail,
		"demo-proj.svc.id.goog",
		"payments",
		"checkout-sa",
	}
	for _, value := range forbidden {
		if payloadValueContains(env.Payload, value) {
			t.Fatalf("gcp trust payload leaked raw identity %q: %#v", value, env.Payload)
		}
	}
}

func TestNewGCPTrustPolicyEnvelopeRequiresTargetAndTrustedIdentity(t *testing.T) {
	t.Parallel()

	base := GCPTrustPolicyObservation{
		Context:                         gcpTestContext(),
		TargetPrincipalFingerprint:      "sha256:gcp-service-account",
		TargetServiceAccountEmailDigest: "sha256:email",
		TrustedMemberFingerprint:        "sha256:member",
		Role:                            "roles/iam.serviceAccountTokenCreator",
		ImpersonationMode:               GCPImpersonationModeTokenCreator,
	}
	missingTarget := base
	missingTarget.TargetPrincipalFingerprint = ""
	if _, err := NewGCPTrustPolicyEnvelope(missingTarget); err == nil {
		t.Fatal("expected error when target_principal_fingerprint is empty")
	}
	missingTrusted := base
	missingTrusted.TrustedMemberFingerprint = ""
	if _, err := NewGCPTrustPolicyEnvelope(missingTrusted); err == nil {
		t.Fatal("expected error when trusted_member_fingerprint is empty")
	}
}

func TestGCPContextAllowsEmptyProjectID(t *testing.T) {
	t.Parallel()

	// Organization- and folder-level IAM bindings have no project segment;
	// project_id is descriptive provenance, not identity, so a blank project must
	// still produce a valid principal fact keyed on the service-account
	// fingerprint.
	ctx := gcpTestContext()
	ctx.ProjectID = ""
	env, err := NewGCPPrincipalEnvelope(GCPPrincipalObservation{Context: ctx, PrincipalFingerprint: "sha256:abc"})
	if err != nil {
		t.Fatalf("blank project_id must be allowed: %v", err)
	}
	if env.StableFactKey == "" {
		t.Fatal("principal fact must have a stable key independent of project_id")
	}
}

func TestGCPContextRequiresScopeID(t *testing.T) {
	t.Parallel()

	ctx := gcpTestContext()
	ctx.ScopeID = ""
	if _, err := NewGCPPrincipalEnvelope(GCPPrincipalObservation{Context: ctx, PrincipalFingerprint: "sha256:abc"}); err == nil {
		t.Fatal("expected error when scope_id is empty")
	}
}

func TestGCPPrincipalStableKeyIsProjectIndependent(t *testing.T) {
	t.Parallel()

	// The same service-account fingerprint observed in two different project
	// contexts must produce the same principal identity (stable key), so one
	// identity is never split into several non-idempotent principal facts.
	ctxA := gcpTestContext()
	ctxA.ProjectID = "proj-a"
	ctxB := gcpTestContext()
	ctxB.ProjectID = "proj-b"
	envA, err := NewGCPPrincipalEnvelope(GCPPrincipalObservation{Context: ctxA, PrincipalFingerprint: "sha256:abc"})
	if err != nil {
		t.Fatalf("envA error: %v", err)
	}
	envB, err := NewGCPPrincipalEnvelope(GCPPrincipalObservation{Context: ctxB, PrincipalFingerprint: "sha256:abc"})
	if err != nil {
		t.Fatalf("envB error: %v", err)
	}
	if envA.StableFactKey != envB.StableFactKey {
		t.Fatalf("stable keys differ across projects (%q vs %q); identity must be project-independent", envA.StableFactKey, envB.StableFactKey)
	}
}
