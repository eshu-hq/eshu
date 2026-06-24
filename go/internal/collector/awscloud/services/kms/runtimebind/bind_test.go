// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package runtimebind_test

import (
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/collector/awscloud/awsruntime"
	_ "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/kms/runtimebind"
)

// TestKMSRuntimeBindRegisters confirms importing the binding installs the
// KMS scanner builder so DefaultScannerFactory can resolve service_kind
// "kms" without a central switch.
func TestKMSRuntimeBindRegisters(t *testing.T) {
	build, ok := awsruntime.LookupBuilder(awscloud.ServiceKMS)
	if !ok {
		t.Fatalf("LookupBuilder(%q) ok = false, want true", awscloud.ServiceKMS)
	}
	scanner, err := build(awsruntime.ScannerDeps{
		AWSConfig: aws.Config{Region: "us-east-1"},
		Boundary:  awscloud.Boundary{AccountID: "123456789012", Region: "us-east-1", ServiceKind: awscloud.ServiceKMS},
	})
	if err != nil {
		t.Fatalf("build() error = %v", err)
	}
	if scanner == nil {
		t.Fatalf("build() returned nil scanner")
	}
}
