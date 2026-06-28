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

// TestParseTrustPolicyFederatedPrincipalType proves that a Federated-keyed
// principal in a trust policy produces a TrustPrincipal with Type="Federated",
// exercising the map-branch of trustPrincipalEntries.
func TestParseTrustPolicyFederatedPrincipalType(t *testing.T) {
	raw := `{"Statement":[{"Effect":"Allow","Principal":{"Federated":"arn:aws:iam::123456789012:oidc-provider/token.actions.githubusercontent.com"},"Action":"sts:AssumeRoleWithWebIdentity"}]}`
	_, principals, err := parseTrustPolicy(raw)
	if err != nil {
		t.Fatalf("parseTrustPolicy() error = %v", err)
	}
	if len(principals) != 1 {
		t.Fatalf("principals count = %d, want 1: %#v", len(principals), principals)
	}
	if principals[0].Type != "Federated" {
		t.Fatalf("Type = %q, want Federated", principals[0].Type)
	}
	if principals[0].Identifier != "arn:aws:iam::123456789012:oidc-provider/token.actions.githubusercontent.com" {
		t.Fatalf("Identifier = %q", principals[0].Identifier)
	}
}

// TestParseTrustPolicyArrayPrincipalFlattened proves that a bare JSON array as
// the Principal field of a trust statement flattens all elements into
// TrustPrincipal entries with Type="AWS" via trustPrincipalEntries([]any).
func TestParseTrustPolicyArrayPrincipalFlattened(t *testing.T) {
	raw := `{"Statement":[{"Effect":"Allow","Principal":["arn:aws:iam::111122223333:root","arn:aws:iam::444455556666:role/cicd"],"Action":"sts:AssumeRole"}]}`
	_, principals, err := parseTrustPolicy(raw)
	if err != nil {
		t.Fatalf("parseTrustPolicy() error = %v", err)
	}
	if len(principals) != 2 {
		t.Fatalf("principals count = %d, want 2: %#v", len(principals), principals)
	}
	wantSet := map[string]bool{
		"arn:aws:iam::111122223333:root":      true,
		"arn:aws:iam::444455556666:role/cicd": true,
	}
	for _, p := range principals {
		if !wantSet[p.Identifier] {
			t.Fatalf("unexpected principal identifier %q", p.Identifier)
		}
	}
}

// TestParseTrustPolicyRejectsMalformedDocument proves that parseTrustPolicy
// propagates a JSON-parse error rather than returning a zero-value document.
func TestParseTrustPolicyRejectsMalformedDocument(t *testing.T) {
	_, _, err := parseTrustPolicy("{not json")
	if err == nil {
		t.Fatal("parseTrustPolicy() error = nil, want parse error for malformed document")
	}
}

// TestParseTrustPolicyEmptyDocumentReturnsNil proves that a blank or empty
// trust policy string produces nil document and nil principals without error.
func TestParseTrustPolicyEmptyDocumentReturnsNil(t *testing.T) {
	document, principals, err := parseTrustPolicy("   ")
	if err != nil {
		t.Fatalf("parseTrustPolicy() error = %v, want nil for blank doc", err)
	}
	if document != nil {
		t.Fatalf("document = %#v, want nil for blank doc", document)
	}
	if principals != nil {
		t.Fatalf("principals = %#v, want nil for blank doc", principals)
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
