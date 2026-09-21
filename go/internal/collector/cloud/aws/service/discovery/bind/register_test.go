// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package bind_test

import (
	"testing"

	awsv2 "github.com/aws/aws-sdk-go-v2/aws"

	"github.com/eshu-hq/eshu/go/internal/collector/cloud/aws"
	"github.com/eshu-hq/eshu/go/internal/collector/cloud/aws/runtime"
	_ "github.com/eshu-hq/eshu/go/internal/collector/cloud/aws/service/discovery/bind"
)

// TestServiceDiscoveryRuntimeBindRegisters confirms importing the binding
// installs the Cloud Map (Service Discovery) scanner builder and that the
// scanner needs no redaction key.
func TestServiceDiscoveryRuntimeBindRegisters(t *testing.T) {
	build, ok := runtime.LookupBuilder(aws.ServiceServiceDiscovery)
	if !ok {
		t.Fatalf("LookupBuilder(%q) ok = false, want true", aws.ServiceServiceDiscovery)
	}
	scanner, err := build(runtime.ScannerDeps{
		AWSConfig: awsv2.Config{Region: "us-east-1"},
		Boundary:  aws.Boundary{AccountID: "123456789012", Region: "us-east-1", ServiceKind: aws.ServiceServiceDiscovery},
	})
	if err != nil {
		t.Fatalf("build() error = %v", err)
	}
	if scanner == nil {
		t.Fatalf("build() returned nil scanner")
	}
	if runtime.ServiceRequiresRedactionKey(aws.ServiceServiceDiscovery) {
		t.Fatalf("ServiceRequiresRedactionKey(%q) = true, want false", aws.ServiceServiceDiscovery)
	}
}
