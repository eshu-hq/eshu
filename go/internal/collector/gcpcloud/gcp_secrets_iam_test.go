// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpcloud

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/secretsiam"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/redact"
)

func testSecretIAMResourceObservation() ResourceObservation {
	return ResourceObservation{
		Name:       "//secretmanager.googleapis.com/projects/demo-proj/secrets/db-password",
		AssetType:  secretManagerSecretAssetType,
		Location:   "global",
		Ancestors:  []string{"projects/123456789"},
		UpdateTime: time.Date(2026, 6, 9, 12, 1, 0, 0, time.UTC),
		IAMPolicyBindings: []IAMPolicyBindingObservation{
			{
				Role:    "roles/secretmanager.secretAccessor",
				Members: []string{"serviceAccount:app@demo-proj.iam.gserviceaccount.com", "group:secops", "allUsers"},
			},
			{
				Role:    "roles/owner",
				Members: []string{"serviceAccount:app@demo-proj.iam.gserviceaccount.com"},
			},
		},
	}
}

func testServiceAccountTrustObservation() ResourceObservation {
	return ResourceObservation{
		Name:                "//iam.googleapis.com/projects/demo-proj/serviceAccounts/app@demo-proj.iam.gserviceaccount.com",
		AssetType:           serviceAccountAssetType,
		Location:            "global",
		ServiceAccountEmail: "app@demo-proj.iam.gserviceaccount.com",
		Ancestors:           []string{"projects/123456789"},
		UpdateTime:          time.Date(2026, 6, 9, 12, 2, 0, 0, time.UTC),
		IAMPolicyBindings: []IAMPolicyBindingObservation{
			{
				Role:    "roles/iam.workloadIdentityUser",
				Members: []string{"serviceAccount:demo-proj.svc.id.goog[payments/checkout-sa]"},
			},
		},
	}
}

func TestGenerationBuildEmitsGCPSecretsIAMFacts(t *testing.T) {
	key := testRedactionKey(t)
	gen := NewGeneration(testGenerationBoundary(), key)
	gen.ObserveReadTime(time.Date(2026, 6, 9, 12, 5, 0, 0, time.UTC))

	if err := gen.AddPage([]ResourceObservation{testSecretIAMResourceObservation()}); err != nil {
		t.Fatalf("AddPage: %v", err)
	}
	envelopes, err := gen.Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	// One distinct service-account principal across both bindings; group/public
	// members never become principals.
	if got := countKind(envelopes, facts.GCPIAMPrincipalFactKind); got != 1 {
		t.Fatalf("gcp principal fact count = %d, want 1", got)
	}
	// Two permission grants for the service account (secretAccessor + owner).
	if got := countKind(envelopes, facts.GCPIAMPermissionPolicyFactKind); got != 2 {
		t.Fatalf("gcp permission fact count = %d, want 2", got)
	}

	// No raw member identity leaks.
	for _, env := range envelopes {
		payload := fmt.Sprintf("%#v", env.Payload)
		for _, forbidden := range []string{"app@demo-proj.iam.gserviceaccount.com", "group:secops"} {
			if strings.Contains(payload, forbidden) {
				t.Fatalf("raw member leaked in %s payload: %s", env.FactKind, payload)
			}
		}
	}

	// The principal fingerprint must match across principal, permission, and the
	// gcp_iam_policy_observation member fingerprint, so the reducer joins them.
	var principalFP string
	for _, env := range envelopes {
		if env.FactKind == facts.GCPIAMPrincipalFactKind {
			principalFP = env.Payload["principal_fingerprint"].(string)
		}
	}
	if principalFP == "" {
		t.Fatal("principal fingerprint missing")
	}
	wantFP := FingerprintMember("serviceAccount:app@demo-proj.iam.gserviceaccount.com", key)
	if principalFP != wantFP {
		t.Fatalf("principal fingerprint = %q, want member fingerprint %q", principalFP, wantFP)
	}

	var secretGrant, ownerGrant facts.Envelope
	for _, env := range envelopes {
		if env.FactKind != facts.GCPIAMPermissionPolicyFactKind {
			continue
		}
		if env.Payload["principal_fingerprint"] != wantFP {
			t.Fatalf("permission principal fingerprint = %v, want %q", env.Payload["principal_fingerprint"], wantFP)
		}
		switch env.Payload["role"] {
		case "roles/secretmanager.secretAccessor":
			secretGrant = env
		case "roles/owner":
			ownerGrant = env
		}
	}
	if secretGrant.FactKind == "" || ownerGrant.FactKind == "" {
		t.Fatal("expected both secretAccessor and owner permission grants")
	}
	if secretGrant.Payload["resource_is_secret"] != true {
		t.Fatalf("secret grant resource_is_secret = %#v, want true", secretGrant.Payload["resource_is_secret"])
	}
	if ownerGrant.Payload["broad_role"] != true {
		t.Fatalf("owner grant broad_role = %#v, want true", ownerGrant.Payload["broad_role"])
	}
}

func TestGenerationBuildEmitsGCPTrustPolicyForServiceAccountImpersonation(t *testing.T) {
	key := testRedactionKey(t)
	gen := NewGeneration(testGenerationBoundary(), key)
	gen.ObserveReadTime(time.Date(2026, 6, 9, 12, 5, 0, 0, time.UTC))

	if err := gen.AddPage([]ResourceObservation{testServiceAccountTrustObservation()}); err != nil {
		t.Fatalf("AddPage: %v", err)
	}
	envelopes, err := gen.Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	trusts := envelopesOfKind(envelopes, facts.GCPIAMTrustPolicyFactKind)
	if len(trusts) != 1 {
		t.Fatalf("gcp trust policy fact count = %d, want 1: %#v", len(trusts), envelopes)
	}

	trust := trusts[0]
	targetMember := "serviceAccount:app@demo-proj.iam.gserviceaccount.com"
	targetFingerprint := FingerprintMember(targetMember, key)
	if got := trust.Payload["target_principal_fingerprint"]; got != targetFingerprint {
		t.Fatalf("target_principal_fingerprint = %v, want %q", got, targetFingerprint)
	}
	if got := trust.Payload["target_service_account_email_digest"]; got != secretsiam.GCPServiceAccountEmailDigest("app@demo-proj.iam.gserviceaccount.com") {
		t.Fatalf("target_service_account_email_digest = %v", got)
	}
	if got := trust.Payload["gcp_workload_identity_subject_fingerprint"]; got != secretsiam.GCPWorkloadIdentitySubjectFingerprint("demo-proj.svc.id.goog", "payments", "checkout-sa") {
		t.Fatalf("gcp_workload_identity_subject_fingerprint = %v", got)
	}
	if got := trust.Payload["impersonation_mode"]; got != secretsiam.GCPImpersonationModeWorkloadIdentity {
		t.Fatalf("impersonation_mode = %v", got)
	}
	for _, forbidden := range []string{
		"app@demo-proj.iam.gserviceaccount.com",
		"demo-proj.svc.id.goog",
		"payments",
		"checkout-sa",
	} {
		if strings.Contains(fmt.Sprintf("%#v", trust.Payload), forbidden) {
			t.Fatalf("trust payload leaked raw identity %q: %#v", forbidden, trust.Payload)
		}
	}
}

func TestGenerationBuildGCPTrustPolicyCloudResourceUIDMatchesResourceLocation(t *testing.T) {
	key := testRedactionKey(t)
	boundary := testGenerationBoundary()
	if boundary.LocationBucket == "" {
		t.Fatal("test boundary must carry a non-empty location bucket")
	}
	gen := NewGeneration(boundary, key)
	obs := testServiceAccountTrustObservation()
	obs.Location = ""

	if err := gen.AddPage([]ResourceObservation{obs}); err != nil {
		t.Fatalf("AddPage: %v", err)
	}
	envelopes, err := gen.Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	trusts := envelopesOfKind(envelopes, facts.GCPIAMTrustPolicyFactKind)
	if len(trusts) != 1 {
		t.Fatalf("gcp trust policy fact count = %d, want 1: %#v", len(trusts), envelopes)
	}
	want := gcpCloudResourceUID(
		ProjectIDFromFullName(obs.Name),
		"",
		obs.AssetType,
		obs.Name,
	)
	if got := trusts[0].Payload["target_service_account_cloud_resource_uid"]; got != want {
		t.Fatalf("target_service_account_cloud_resource_uid = %v, want materialized resource uid %q", got, want)
	}
}

func TestGenerationBuildSkipsGCPSecretsIAMWithoutRedactionKey(t *testing.T) {
	gen := NewGeneration(testGenerationBoundary(), redact.Key{})
	if err := gen.AddPage([]ResourceObservation{testSecretIAMResourceObservation()}); err != nil {
		t.Fatalf("AddPage: %v", err)
	}
	envelopes, err := gen.Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if got := countKind(envelopes, facts.GCPIAMPrincipalFactKind); got != 0 {
		t.Fatalf("gcp principal fact count = %d, want 0 without a redaction key", got)
	}
	if got := countKind(envelopes, facts.GCPIAMPermissionPolicyFactKind); got != 0 {
		t.Fatalf("gcp permission fact count = %d, want 0 without a redaction key", got)
	}
}

func envelopesOfKind(envelopes []facts.Envelope, factKind string) []facts.Envelope {
	out := make([]facts.Envelope, 0)
	for _, envelope := range envelopes {
		if envelope.FactKind == factKind {
			out = append(out, envelope)
		}
	}
	return out
}
