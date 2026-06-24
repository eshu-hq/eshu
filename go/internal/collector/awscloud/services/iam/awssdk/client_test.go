// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"testing"

	awsiamtypes "github.com/aws/aws-sdk-go-v2/service/iam/types"

	iamservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/iam"
)

func TestParseTrustPolicyIncludesWildcardStringPrincipal(t *testing.T) {
	_, principals, err := parseTrustPolicy(`{"Version":"2012-10-17","Statement":{"Effect":"Allow","Principal":"*","Action":"sts:AssumeRole"}}`)
	if err != nil {
		t.Fatalf("parseTrustPolicy() error = %v", err)
	}
	want := iamservice.TrustPrincipal{Type: "AWS", Identifier: "*"}
	if len(principals) != 1 || principals[0] != want {
		t.Fatalf("principals = %#v, want %#v", principals, []iamservice.TrustPrincipal{want})
	}
}

func TestParseTrustPolicyFallsBackToAlreadyDecodedJSON(t *testing.T) {
	raw := `{"Version":"2012-10-17","Statement":{"Effect":"Allow","Principal":{"AWS":"arn:aws:iam::123456789012:root"},"Action":"sts:AssumeRole","Condition":{"StringLike":{"aws:PrincipalTag/team":"platform%ops"}}}}`
	_, principals, err := parseTrustPolicy(raw)
	if err != nil {
		t.Fatalf("parseTrustPolicy() error = %v", err)
	}
	want := iamservice.TrustPrincipal{Type: "AWS", Identifier: "arn:aws:iam::123456789012:root"}
	if len(principals) != 1 || principals[0] != want {
		t.Fatalf("principals = %#v, want %#v", principals, []iamservice.TrustPrincipal{want})
	}
}

func TestPermissionBoundaryMapsPolicyIdentity(t *testing.T) {
	policyARN := "arn:aws:iam::123456789012:policy/developer-boundary"
	got := permissionBoundary(&awsiamtypes.AttachedPermissionsBoundary{
		PermissionsBoundaryArn:  &policyARN,
		PermissionsBoundaryType: awsiamtypes.PermissionsBoundaryAttachmentTypePolicy,
	})
	if got.PolicyARN != policyARN {
		t.Fatalf("PolicyARN = %q, want %q", got.PolicyARN, policyARN)
	}
	if got.Type != string(awsiamtypes.PermissionsBoundaryAttachmentTypePolicy) {
		t.Fatalf("Type = %q, want %q", got.Type, awsiamtypes.PermissionsBoundaryAttachmentTypePolicy)
	}
}

func TestFingerprintStringRedactsOIDCProviderURL(t *testing.T) {
	raw := "https://token.actions.githubusercontent.com"
	got := fingerprintString(raw)
	if got == "" {
		t.Fatal("fingerprintString returned empty fingerprint")
	}
	if got == raw {
		t.Fatal("fingerprintString returned the raw provider URL")
	}
	if got != fingerprintString(" "+raw+" ") {
		t.Fatal("fingerprintString changed when surrounding whitespace changed")
	}
}
