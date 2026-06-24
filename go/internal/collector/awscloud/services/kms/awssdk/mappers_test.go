// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	kmstypes "github.com/aws/aws-sdk-go-v2/service/kms/types"
)

// TestMapGrantClassifiesServicePrincipal proves a grant whose grantee arrives
// in GranteeServicePrincipal (a service principal such as "s3.amazonaws.com",
// not an ARN) is recorded with GranteePrincipalType "Service" so the scanner
// never emits it as an ARN. The ARN case is exercised end-to-end by
// TestClientListKeysEmitsMetadataAndDropsEncryptionContext.
func TestMapGrantClassifiesServicePrincipal(t *testing.T) {
	grant := mapGrant(kmstypes.GrantListEntry{
		GrantId:                 aws.String("service-grant"),
		GranteeServicePrincipal: aws.String("s3.amazonaws.com"),
		Operations:              []kmstypes.GrantOperation{kmstypes.GrantOperationDecrypt},
	})
	if grant.GranteePrincipal != "s3.amazonaws.com" {
		t.Fatalf("GranteePrincipal = %q, want %q", grant.GranteePrincipal, "s3.amazonaws.com")
	}
	if grant.GranteePrincipalType != "Service" {
		t.Fatalf("GranteePrincipalType = %q, want %q for a service principal", grant.GranteePrincipalType, "Service")
	}
}

// TestMapGrantPrefersARNPrincipal proves that when both the ARN and service
// principal fields are set, the ARN wins and is classified as "AWS".
func TestMapGrantPrefersARNPrincipal(t *testing.T) {
	grant := mapGrant(kmstypes.GrantListEntry{
		GrantId:                 aws.String("dual-grant"),
		GranteePrincipal:        aws.String("arn:aws:iam::123456789012:role/eshu-app"),
		GranteeServicePrincipal: aws.String("s3.amazonaws.com"),
	})
	if grant.GranteePrincipal != "arn:aws:iam::123456789012:role/eshu-app" {
		t.Fatalf("GranteePrincipal = %q, want the ARN", grant.GranteePrincipal)
	}
	if grant.GranteePrincipalType != "AWS" {
		t.Fatalf("GranteePrincipalType = %q, want %q", grant.GranteePrincipalType, "AWS")
	}
}
