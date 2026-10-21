// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package runtimebind_test

import (
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/collector/awscloud/awsruntime"
	"github.com/eshu-hq/eshu/go/internal/collector/awscloud/catalog"
	_ "github.com/eshu-hq/eshu/go/internal/collector/awscloud/service/route53recoverycontrolconfig/runtimebind"
)

// TestRoute53RecoveryControlConfigRuntimeBindRegisters confirms importing the
// binding installs the recovery-control scanner builder.
func TestRoute53RecoveryControlConfigRuntimeBindRegisters(t *testing.T) {
	build, ok := awsruntime.LookupBuilder(catalog.ServiceRoute53RecoveryControlConfig)
	if !ok {
		t.Fatalf("LookupBuilder(%q) ok = false, want true", catalog.ServiceRoute53RecoveryControlConfig)
	}
	scanner, err := build(awsruntime.ScannerDeps{
		AWSConfig: aws.Config{Region: "us-west-2"},
		Boundary: awscloud.Boundary{
			AccountID:   "123456789012",
			Region:      "us-west-2",
			ServiceKind: catalog.ServiceRoute53RecoveryControlConfig,
		},
	})
	if err != nil {
		t.Fatalf("build() error = %v", err)
	}
	if scanner == nil {
		t.Fatalf("build() returned nil scanner")
	}
}
