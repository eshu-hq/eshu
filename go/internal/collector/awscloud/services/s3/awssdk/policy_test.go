// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import "testing"

func TestDeriveBucketPolicyFlags(t *testing.T) {
	cases := []struct {
		name             string
		ownerAccountID   string
		document         string
		wantPublic       *bool
		wantCrossAccount *bool
	}{
		{
			name:           "wildcard principal is public",
			ownerAccountID: "123456789012",
			document: `{"Version":"2012-10-17","Statement":[
				{"Effect":"Allow","Principal":"*","Action":"s3:GetObject","Resource":"arn:aws:s3:::b/*"}]}`,
			wantPublic:       boolPtr(true),
			wantCrossAccount: boolPtr(false),
		},
		{
			name:           "aws wildcard principal is public",
			ownerAccountID: "123456789012",
			document: `{"Statement":[
				{"Effect":"Allow","Principal":{"AWS":"*"},"Action":"s3:GetObject","Resource":"arn:aws:s3:::b/*"}]}`,
			wantPublic:       boolPtr(true),
			wantCrossAccount: boolPtr(false),
		},
		{
			name:           "cross-account principal",
			ownerAccountID: "123456789012",
			document: `{"Statement":[
				{"Effect":"Allow","Principal":{"AWS":"arn:aws:iam::999988887777:root"},"Action":"s3:GetObject","Resource":"arn:aws:s3:::b/*"}]}`,
			wantPublic:       boolPtr(false),
			wantCrossAccount: boolPtr(true),
		},
		{
			name:           "same-account principal is neither public nor cross-account",
			ownerAccountID: "123456789012",
			document: `{"Statement":[
				{"Effect":"Allow","Principal":{"AWS":"arn:aws:iam::123456789012:role/app"},"Action":"s3:GetObject","Resource":"arn:aws:s3:::b/*"}]}`,
			wantPublic:       boolPtr(false),
			wantCrossAccount: boolPtr(false),
		},
		{
			name:           "deny wildcard is not a public grant",
			ownerAccountID: "123456789012",
			document: `{"Statement":[
				{"Effect":"Deny","Principal":"*","Action":"s3:GetObject","Resource":"arn:aws:s3:::b/*"}]}`,
			wantPublic:       boolPtr(false),
			wantCrossAccount: boolPtr(false),
		},
		{
			name:           "bare account id principal cross-account",
			ownerAccountID: "123456789012",
			document: `{"Statement":[
				{"Effect":"Allow","Principal":{"AWS":["999988887777","123456789012"]},"Action":"s3:GetObject","Resource":"arn:aws:s3:::b/*"}]}`,
			wantPublic:       boolPtr(false),
			wantCrossAccount: boolPtr(true),
		},
		{
			name:           "bare principal array cross-account",
			ownerAccountID: "123456789012",
			document: `{"Statement":[
				{"Effect":"Allow","Principal":["999988887777","123456789012"],"Action":"s3:GetObject","Resource":"arn:aws:s3:::b/*"}]}`,
			wantPublic:       boolPtr(false),
			wantCrossAccount: boolPtr(true),
		},
		{
			name:           "service principal is neither public nor cross-account",
			ownerAccountID: "123456789012",
			document: `{"Statement":[
				{"Effect":"Allow","Principal":{"Service":"cloudtrail.amazonaws.com"},"Action":"s3:PutObject","Resource":"arn:aws:s3:::b/*"}]}`,
			wantPublic:       boolPtr(false),
			wantCrossAccount: boolPtr(false),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			public, crossAccount, err := deriveBucketPolicyFlags(tc.document, tc.ownerAccountID)
			if err != nil {
				t.Fatalf("deriveBucketPolicyFlags() error = %v, want nil", err)
			}
			assertBoolPtr(t, "public", public, tc.wantPublic)
			assertBoolPtr(t, "crossAccount", crossAccount, tc.wantCrossAccount)
		})
	}
}

func TestDeriveBucketPolicyFlagsHandlesURLEncodedDocument(t *testing.T) {
	encoded := "%7B%22Statement%22%3A%5B%7B%22Effect%22%3A%22Allow%22%2C%22Principal%22%3A%22*%22%2C%22Action%22%3A%22s3%3AGetObject%22%7D%5D%7D"
	public, crossAccount, err := deriveBucketPolicyFlags(encoded, "123456789012")
	if err != nil {
		t.Fatalf("deriveBucketPolicyFlags() error = %v, want nil", err)
	}
	assertBoolPtr(t, "public", public, boolPtr(true))
	assertBoolPtr(t, "crossAccount", crossAccount, boolPtr(false))
}

func TestDeriveBucketPolicyFlagsRejectsMalformedDocument(t *testing.T) {
	_, _, err := deriveBucketPolicyFlags("{not json", "123456789012")
	if err == nil {
		t.Fatalf("deriveBucketPolicyFlags() error = nil, want parse error for malformed document")
	}
}

func TestDeriveBucketPolicyExternalPrincipalGrants(t *testing.T) {
	document := `{"Version":"2012-10-17","Statement":[
		{"Sid":"PublicRead","Effect":"Allow","Principal":"*","Action":"s3:GetObject","Resource":"arn:aws:s3:::b/*"},
		{"Sid":"CrossAccount","Effect":"Allow","Principal":{"AWS":["999988887777","arn:aws:iam::888877776666:role/consumer","arn:aws:iam::123456789012:role/internal"]},"Action":"s3:GetObject","Resource":"arn:aws:s3:::b/*"},
		{"Sid":"Service","Effect":"Allow","Principal":{"Service":"cloudtrail.amazonaws.com"},"Action":"s3:PutObject","Resource":"arn:aws:s3:::b/*"},
		{"Sid":"Federated","Effect":"Allow","Principal":{"Federated":"arn:aws:iam::999988887777:saml-provider/Example"},"Action":"s3:GetObject","Resource":"arn:aws:s3:::b/*"},
		{"Sid":"Denied","Effect":"Deny","Principal":{"AWS":"111122223333"},"Action":"s3:GetObject","Resource":"arn:aws:s3:::b/*"}]}`

	grants, err := deriveBucketPolicyExternalPrincipalGrants(document, "123456789012")
	if err != nil {
		t.Fatalf("deriveBucketPolicyExternalPrincipalGrants() error = %v, want nil", err)
	}
	want := []principalGrant{
		{
			Kind:           "public",
			Value:          "*",
			Outcome:        "public",
			Public:         true,
			StatementSID:   "PublicRead",
			Unsupported:    false,
			UnsupportedKey: "",
		},
		{
			Kind:             "aws_account",
			Value:            "999988887777",
			AccountID:        "999988887777",
			Outcome:          "cross_account",
			CrossAccount:     true,
			StatementSID:     "CrossAccount",
			PrincipalIsExact: true,
		},
		{
			Kind:             "aws_arn",
			Value:            "arn:aws:iam::888877776666:role/consumer",
			AccountID:        "888877776666",
			Partition:        "aws",
			Outcome:          "cross_account",
			CrossAccount:     true,
			StatementSID:     "CrossAccount",
			PrincipalIsExact: true,
		},
		{
			Kind:             "aws_service",
			Value:            "cloudtrail.amazonaws.com",
			Service:          "cloudtrail.amazonaws.com",
			Outcome:          "aws_service",
			ServicePrincipal: true,
			StatementSID:     "Service",
			PrincipalIsExact: true,
		},
		{
			Kind:           "unsupported",
			Value:          "Federated",
			Outcome:        "unsupported_principal",
			StatementSID:   "Federated",
			Unsupported:    true,
			UnsupportedKey: "Federated",
		},
	}
	if len(grants) != len(want) {
		t.Fatalf("grant count = %d, want %d: %#v", len(grants), len(want), grants)
	}
	for i := range want {
		if grants[i] != want[i] {
			t.Fatalf("grant[%d] = %#v, want %#v", i, grants[i], want[i])
		}
	}
}

func TestDeriveBucketPolicyExternalPrincipalGrantsHandlesURLEncodedDocument(t *testing.T) {
	encoded := "%7B%22Statement%22%3A%5B%7B%22Effect%22%3A%22Allow%22%2C%22Principal%22%3A%22%2A%22%2C%22Action%22%3A%22s3%3AGetObject%22%7D%5D%7D"
	grants, err := deriveBucketPolicyExternalPrincipalGrants(encoded, "123456789012")
	if err != nil {
		t.Fatalf("deriveBucketPolicyExternalPrincipalGrants() error = %v, want nil", err)
	}
	if len(grants) != 1 || grants[0].Kind != "public" || grants[0].Value != "*" {
		t.Fatalf("grants = %#v, want one public wildcard grant", grants)
	}
}

func TestDeriveBucketPolicyExternalPrincipalGrantsHandlesBarePrincipalArray(t *testing.T) {
	document := `{"Statement":[
		{"Effect":"Allow","Principal":["999988887777","arn:aws:iam::123456789012:role/internal"],"Action":"s3:GetObject"}]}`
	grants, err := deriveBucketPolicyExternalPrincipalGrants(document, "123456789012")
	if err != nil {
		t.Fatalf("deriveBucketPolicyExternalPrincipalGrants() error = %v, want nil", err)
	}
	want := []principalGrant{{
		Kind:             "aws_account",
		Value:            "999988887777",
		AccountID:        "999988887777",
		Outcome:          "cross_account",
		CrossAccount:     true,
		PrincipalIsExact: true,
	}}
	if len(grants) != len(want) {
		t.Fatalf("grant count = %d, want %d: %#v", len(grants), len(want), grants)
	}
	for i := range want {
		if grants[i] != want[i] {
			t.Fatalf("grant[%d] = %#v, want %#v", i, grants[i], want[i])
		}
	}
}

func TestDeriveBucketPolicyExternalPrincipalGrantsRejectsMalformedDocument(t *testing.T) {
	_, err := deriveBucketPolicyExternalPrincipalGrants("{not json", "123456789012")
	if err == nil {
		t.Fatalf("deriveBucketPolicyExternalPrincipalGrants() error = nil, want parse error")
	}
}

func assertBoolPtr(t *testing.T, label string, got, want *bool) {
	t.Helper()
	switch {
	case got == nil && want == nil:
		return
	case got == nil || want == nil:
		t.Fatalf("%s = %v, want %v", label, ptrStr(got), ptrStr(want))
	case *got != *want:
		t.Fatalf("%s = %v, want %v", label, *got, *want)
	}
}

func ptrStr(value *bool) string {
	if value == nil {
		return "nil"
	}
	if *value {
		return "true"
	}
	return "false"
}

func boolPtr(value bool) *bool {
	return &value
}
