// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package bind_test

import (
	"testing"

	awsv2 "github.com/aws/aws-sdk-go-v2/aws"

	"github.com/eshu-hq/eshu/go/internal/collector/cloud/aws"
	"github.com/eshu-hq/eshu/go/internal/collector/cloud/aws/runtime"
	_ "github.com/eshu-hq/eshu/go/internal/collector/cloud/aws/service/vpclattice/bind"
)

// TestVPCLatticeRuntimeBindRegisters confirms importing the binding installs
// the VPC Lattice scanner builder.
func TestVPCLatticeRuntimeBindRegisters(t *testing.T) {
	build, ok := runtime.LookupBuilder(aws.ServiceVPCLattice)
	if !ok {
		t.Fatalf("LookupBuilder(%q) ok = false, want true", aws.ServiceVPCLattice)
	}
	scanner, err := build(runtime.ScannerDeps{
		AWSConfig: awsv2.Config{Region: "us-east-1"},
		Boundary: aws.Boundary{
			AccountID:   "123456789012",
			Region:      "us-east-1",
			ServiceKind: aws.ServiceVPCLattice,
		},
	})
	if err != nil {
		t.Fatalf("build() error = %v", err)
	}
	if scanner == nil {
		t.Fatalf("build() returned nil scanner")
	}
}
