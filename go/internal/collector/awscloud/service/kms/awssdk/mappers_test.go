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

// TestPrincipalAndTypeEmptyFields proves that principalAndType returns empty
// strings for both the principal and the type when neither the ARN nor the
// service principal field is set. The ARN-only and service-only paths are
// exercised by TestMapGrantPrefersARNPrincipal and
// TestMapGrantClassifiesServicePrincipal respectively.
func TestPrincipalAndTypeEmptyFields(t *testing.T) {
	principal, principalType := principalAndType("", "")
	if principal != "" || principalType != "" {
		t.Fatalf("principalAndType(%q,%q) = (%q,%q), want both empty", "", "", principal, principalType)
	}
}

// TestRotationCheckSupportedRejectsNonCustomerKey proves rotationCheckSupported
// returns false for AWS-managed keys so the adapter never issues a
// GetKeyRotationStatus call for keys it cannot control. AWS returns
// UnsupportedOperationException for those keys; suppressing the call keeps
// noise out of throttle and error counters.
func TestRotationCheckSupportedRejectsNonCustomerKey(t *testing.T) {
	awsManaged := &kmstypes.KeyMetadata{
		KeyManager: kmstypes.KeyManagerTypeAws,
		KeyUsage:   kmstypes.KeyUsageTypeEncryptDecrypt,
		KeySpec:    kmstypes.KeySpecSymmetricDefault,
		KeyState:   kmstypes.KeyStateEnabled,
	}
	if rotationCheckSupported(awsManaged) {
		t.Fatalf("rotationCheckSupported = true for AWS-managed key, want false")
	}
}

// TestRotationCheckSupportedRejectsPendingDeletion proves rotationCheckSupported
// returns false when the key is in a pending-deletion state (line 202 in
// mappers.go). AWS's GetKeyRotationStatus returns UnsupportedOperation for
// pending-deletion keys.
func TestRotationCheckSupportedRejectsPendingDeletion(t *testing.T) {
	pendingDeletion := &kmstypes.KeyMetadata{
		KeyManager: kmstypes.KeyManagerTypeCustomer,
		KeyUsage:   kmstypes.KeyUsageTypeEncryptDecrypt,
		KeySpec:    kmstypes.KeySpecSymmetricDefault,
		KeyState:   kmstypes.KeyStatePendingDeletion,
	}
	if rotationCheckSupported(pendingDeletion) {
		t.Fatalf("rotationCheckSupported = true for pending-deletion key, want false")
	}
}

// TestRotationCheckSupportedAcceptsEligibleSymmetricKey proves
// rotationCheckSupported returns true only for customer-managed, symmetric,
// encrypt-decrypt, non-pending keys — the exact set for which
// GetKeyRotationStatus is meaningful.
func TestRotationCheckSupportedAcceptsEligibleSymmetricKey(t *testing.T) {
	eligible := &kmstypes.KeyMetadata{
		KeyManager: kmstypes.KeyManagerTypeCustomer,
		KeyUsage:   kmstypes.KeyUsageTypeEncryptDecrypt,
		KeySpec:    kmstypes.KeySpecSymmetricDefault,
		KeyState:   kmstypes.KeyStateEnabled,
	}
	if !rotationCheckSupported(eligible) {
		t.Fatalf("rotationCheckSupported = false for eligible symmetric key, want true")
	}
}
