// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package freshness

import (
	"slices"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud/awsruntime"
	_ "github.com/eshu-hq/eshu/go/internal/collector/awscloud/awsruntime/bindings"
)

// TestSupportedServiceKindsMatchScannerRegistry pins the host-owned
// scheduling vocabulary to the scanner registry as sets. Trigger validation
// and target-scope authorization accept exactly the service kinds the runtime
// can scan: a kind the registry drops must fail here before a trigger for it
// can be planned, and a kind the host invents must fail before the planner
// schedules unscannable work.
//
// Transition pin: the blank bindings import above populates the registry for
// this test binary only. Retire this test with the registry when the scanner
// tree is deleted (Lane 2); the host list then stands alone beside the
// satellite Contract.
func TestSupportedServiceKindsMatchScannerRegistry(t *testing.T) {
	t.Parallel()

	host := append([]string(nil), SupportedServiceKinds()...)
	if !slices.IsSorted(host) {
		t.Fatal("SupportedServiceKinds() is not sorted")
	}
	registry := append([]string(nil), awsruntime.SupportedServiceKinds()...)
	slices.Sort(registry)
	if !slices.Equal(host, registry) {
		t.Fatalf("host vocabulary differs from scanner registry:\nhost-only: %q\nregistry-only: %q", onlyIn(host, registry), onlyIn(registry, host))
	}
	for _, kind := range host {
		if !SupportsServiceKind(kind) {
			t.Fatalf("SupportsServiceKind(%q) = false, want true", kind)
		}
	}
	if SupportsServiceKind("nonexistent-service-kind") {
		t.Fatal("SupportsServiceKind(nonexistent-service-kind) = true, want false")
	}
}

// TestGlobalServiceKindsAreSupported pins the aws-global scheduling boundary:
// the global-only kinds are schedulable vocabulary, and the global region
// spelling matches the scope IDs the planners mint.
func TestGlobalServiceKindsAreSupported(t *testing.T) {
	t.Parallel()

	if GlobalRegion != "aws-global" {
		t.Fatalf("GlobalRegion = %q, want %q", GlobalRegion, "aws-global")
	}
	for _, kind := range []string{"iam", "route53", "cloudfront"} {
		if !SupportsServiceKind(kind) {
			t.Fatalf("SupportsServiceKind(%q) = false, want true (global scheduling boundary)", kind)
		}
		if !IsGlobalServiceKind(kind) {
			t.Fatalf("IsGlobalServiceKind(%q) = false, want true", kind)
		}
	}
	for _, kind := range []string{"ec2", "lambda", "s3"} {
		if IsGlobalServiceKind(kind) {
			t.Fatalf("IsGlobalServiceKind(%q) = true, want false", kind)
		}
	}
}

// TestNormalizationKindsAreSupported pins every service kind the EventBridge
// normalization maps can emit to the supported vocabulary, so a mapped kind
// can never fail the Validate call normalization itself performs.
func TestNormalizationKindsAreSupported(t *testing.T) {
	t.Parallel()

	for _, kind := range []string{
		"apigateway", "apigatewayv2", "cloudfront", "cloudwatchlogs", "dynamodb",
		"ec2", "ecr", "ecs", "eks", "elbv2", "eventbridge", "glue", "iam",
		"lambda", "rds", "redshift", "route53", "s3", "secretsmanager",
		"securityhub", "sns", "sqs", "ssm",
	} {
		if !SupportsServiceKind(kind) {
			t.Fatalf("SupportsServiceKind(%q) = false, want true (emitted by EventBridge normalization)", kind)
		}
	}
}

func onlyIn(have, want []string) []string {
	var out []string
	for _, kind := range have {
		if !slices.Contains(want, kind) {
			out = append(out, kind)
		}
	}
	return out
}
