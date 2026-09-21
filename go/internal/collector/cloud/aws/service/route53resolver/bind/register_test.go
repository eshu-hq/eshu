// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package bind_test

import (
	"testing"

	awsv2 "github.com/aws/aws-sdk-go-v2/aws"

	"github.com/eshu-hq/eshu/go/internal/collector/cloud/aws"
	"github.com/eshu-hq/eshu/go/internal/collector/cloud/aws/runtime"
	_ "github.com/eshu-hq/eshu/go/internal/collector/cloud/aws/service/route53resolver/bind"
)

// TestRoute53ResolverRuntimeBindRegisters confirms importing the binding
// installs the Route 53 Resolver scanner builder.
func TestRoute53ResolverRuntimeBindRegisters(t *testing.T) {
	build, ok := runtime.LookupBuilder(aws.ServiceRoute53Resolver)
	if !ok {
		t.Fatalf("LookupBuilder(%q) ok = false, want true", aws.ServiceRoute53Resolver)
	}
	scanner, err := build(runtime.ScannerDeps{
		AWSConfig: awsv2.Config{Region: "us-east-1"},
		Boundary: aws.Boundary{
			AccountID:   "123456789012",
			Region:      "us-east-1",
			ServiceKind: aws.ServiceRoute53Resolver,
		},
	})
	if err != nil {
		t.Fatalf("build() error = %v", err)
	}
	if scanner == nil {
		t.Fatalf("build() returned nil scanner")
	}
}

// TestRoute53ResolverDoesNotRequireRedactionKey confirms the binding does not
// declare a redaction-key requirement: domain-list contents are dropped by not
// mapping them, so no HMAC redaction is needed.
func TestRoute53ResolverDoesNotRequireRedactionKey(t *testing.T) {
	if runtime.ServiceRequiresRedactionKey(aws.ServiceRoute53Resolver) {
		t.Fatalf("ServiceRequiresRedactionKey(%q) = true, want false", aws.ServiceRoute53Resolver)
	}
}
