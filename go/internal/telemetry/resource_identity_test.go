// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package telemetry

import (
	"log/slog"
	"strings"
	"testing"
)

func TestSafeResourceLogIdentityRedactsAWSARNs(t *testing.T) {
	t.Parallel()

	rawARN := "arn:aws:lambda:us-east-1:123456789012:function:prod-payments-secret-sync"

	identity := SafeResourceLogIdentity(rawARN)
	if identity.IdentityKind != "aws_arn" {
		t.Fatalf("IdentityKind = %q, want aws_arn", identity.IdentityKind)
	}
	if identity.ResourceType != "lambda:function" {
		t.Fatalf("ResourceType = %q, want lambda:function", identity.ResourceType)
	}
	if !strings.HasPrefix(identity.Fingerprint, "sha256:") {
		t.Fatalf("Fingerprint = %q, want sha256 prefix", identity.Fingerprint)
	}
	if strings.Contains(identity.Fingerprint, "prod-payments-secret-sync") {
		t.Fatalf("Fingerprint leaked raw resource name: %q", identity.Fingerprint)
	}
}

func TestSafeResourceLogIdentityClassifiesAWSARNResourcePrefixes(t *testing.T) {
	t.Parallel()

	identity := SafeResourceLogIdentity("arn:aws:states:us-east-1:123456789012:stateMachine:order-fulfillment")
	if identity.IdentityKind != "aws_arn" {
		t.Fatalf("IdentityKind = %q, want aws_arn", identity.IdentityKind)
	}
	if identity.ResourceType != "states:statemachine" {
		t.Fatalf("ResourceType = %q, want states:statemachine", identity.ResourceType)
	}
	if strings.Contains(identity.Fingerprint, "order-fulfillment") {
		t.Fatalf("Fingerprint leaked raw resource name: %q", identity.Fingerprint)
	}
}

func TestSafeResourceLogIdentityClassifiesTerraformAddresses(t *testing.T) {
	t.Parallel()

	identity := SafeResourceLogIdentity("aws_lambda_function.prod_payments_secret_sync")
	if identity.IdentityKind != "terraform_address" {
		t.Fatalf("IdentityKind = %q, want terraform_address", identity.IdentityKind)
	}
	if identity.ResourceType != "aws_lambda_function" {
		t.Fatalf("ResourceType = %q, want aws_lambda_function", identity.ResourceType)
	}
}

func TestSafeResourceLogIdentityClassifiesModuleQualifiedTerraformAddresses(t *testing.T) {
	t.Parallel()

	for _, input := range []string{
		"module.vpc.aws_lambda_function.prod_payments_secret_sync",
		`module.vpc["prod"].module.workers.aws_lambda_function.prod_payments_secret_sync[0]`,
	} {
		identity := SafeResourceLogIdentity(input)
		if identity.IdentityKind != "terraform_address" {
			t.Fatalf("IdentityKind for %q = %q, want terraform_address", input, identity.IdentityKind)
		}
		if identity.ResourceType != "aws_lambda_function" {
			t.Fatalf("ResourceType for %q = %q, want aws_lambda_function", input, identity.ResourceType)
		}
		if strings.Contains(identity.Fingerprint, "prod_payments_secret_sync") {
			t.Fatalf("Fingerprint leaked raw resource name for %q: %q", input, identity.Fingerprint)
		}
	}
}

func TestSafeResourceLogIdentityLeavesIncompleteModuleAddressUnknown(t *testing.T) {
	t.Parallel()

	identity := SafeResourceLogIdentity("module.vpc")
	if identity.IdentityKind != "resource_identifier" {
		t.Fatalf("IdentityKind = %q, want resource_identifier", identity.IdentityKind)
	}
	if identity.ResourceType != "unknown" {
		t.Fatalf("ResourceType = %q, want unknown", identity.ResourceType)
	}
}

func TestSafeResourceLogAttrsUseFrozenKeys(t *testing.T) {
	t.Parallel()

	attrs := SafeResourceLogAttrs("arn:aws:iam::123456789012:role/prod-secret-role")
	keys := make(map[string]bool, len(attrs))
	for _, attr := range attrs {
		keys[attr.Key] = true
		if attr.Value.Kind() != slog.KindString {
			t.Fatalf("attr %q kind = %v, want string", attr.Key, attr.Value.Kind())
		}
	}
	for _, want := range []string{LogKeyResourceFingerprint, LogKeyResourceIdentityKind, LogKeyResourceType} {
		if !keys[want] {
			t.Fatalf("SafeResourceLogAttrs() missing key %q in %#v", want, attrs)
		}
	}
}
