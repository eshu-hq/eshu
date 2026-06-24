// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package runtimebind_test

import (
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/collector/awscloud/awsruntime"
	_ "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/globalaccelerator/runtimebind"
)

// TestGlobalAcceleratorRuntimeBindRegisters confirms importing the binding
// installs the Global Accelerator scanner builder.
func TestGlobalAcceleratorRuntimeBindRegisters(t *testing.T) {
	build, ok := awsruntime.LookupBuilder(awscloud.ServiceGlobalAccelerator)
	if !ok {
		t.Fatalf("LookupBuilder(%q) ok = false, want true", awscloud.ServiceGlobalAccelerator)
	}
	scanner, err := build(awsruntime.ScannerDeps{
		AWSConfig: aws.Config{Region: "us-west-2"},
		Boundary: awscloud.Boundary{
			AccountID:   "123456789012",
			Region:      "us-west-2",
			ServiceKind: awscloud.ServiceGlobalAccelerator,
		},
	})
	if err != nil {
		t.Fatalf("build() error = %v", err)
	}
	if scanner == nil {
		t.Fatalf("build() returned nil scanner")
	}
}
