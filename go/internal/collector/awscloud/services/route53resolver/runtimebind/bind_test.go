// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package runtimebind_test

import (
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/collector/awscloud/awsruntime"
	_ "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/route53resolver/runtimebind"
)

// TestRoute53ResolverRuntimeBindRegisters confirms importing the binding
// installs the Route 53 Resolver scanner builder.
func TestRoute53ResolverRuntimeBindRegisters(t *testing.T) {
	build, ok := awsruntime.LookupBuilder(awscloud.ServiceRoute53Resolver)
	if !ok {
		t.Fatalf("LookupBuilder(%q) ok = false, want true", awscloud.ServiceRoute53Resolver)
	}
	scanner, err := build(awsruntime.ScannerDeps{
		AWSConfig: aws.Config{Region: "us-east-1"},
		Boundary: awscloud.Boundary{
			AccountID:   "123456789012",
			Region:      "us-east-1",
			ServiceKind: awscloud.ServiceRoute53Resolver,
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
	if awsruntime.ServiceRequiresRedactionKey(awscloud.ServiceRoute53Resolver) {
		t.Fatalf("ServiceRequiresRedactionKey(%q) = true, want false", awscloud.ServiceRoute53Resolver)
	}
}
