// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package bind_test

import (
	"testing"

	awsv2 "github.com/aws/aws-sdk-go-v2/aws"

	"github.com/eshu-hq/eshu/go/internal/collector/cloud/aws"
	"github.com/eshu-hq/eshu/go/internal/collector/cloud/aws/runtime"
	_ "github.com/eshu-hq/eshu/go/internal/collector/cloud/aws/service/route53recoverycontrolconfig/bind"
)

// TestRoute53RecoveryControlConfigRuntimeBindRegisters confirms importing the
// binding installs the recovery-control scanner builder.
func TestRoute53RecoveryControlConfigRuntimeBindRegisters(t *testing.T) {
	build, ok := runtime.LookupBuilder(aws.ServiceRoute53RecoveryControlConfig)
	if !ok {
		t.Fatalf("LookupBuilder(%q) ok = false, want true", aws.ServiceRoute53RecoveryControlConfig)
	}
	scanner, err := build(runtime.ScannerDeps{
		AWSConfig: awsv2.Config{Region: "us-west-2"},
		Boundary: aws.Boundary{
			AccountID:   "123456789012",
			Region:      "us-west-2",
			ServiceKind: aws.ServiceRoute53RecoveryControlConfig,
		},
	})
	if err != nil {
		t.Fatalf("build() error = %v", err)
	}
	if scanner == nil {
		t.Fatalf("build() returned nil scanner")
	}
}
