// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package runtimebind_test

import (
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/collector/awscloud/awsruntime"
	_ "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/cloudhsmv2/runtimebind"
)

// TestCloudHSMV2RuntimeBindRegisters confirms importing the binding installs the
// CloudHSM v2 scanner builder.
func TestCloudHSMV2RuntimeBindRegisters(t *testing.T) {
	build, ok := awsruntime.LookupBuilder(awscloud.ServiceCloudHSMV2)
	if !ok {
		t.Fatalf("LookupBuilder(%q) ok = false, want true", awscloud.ServiceCloudHSMV2)
	}
	scanner, err := build(awsruntime.ScannerDeps{
		AWSConfig: aws.Config{Region: "us-east-1"},
		Boundary: awscloud.Boundary{
			AccountID:   "123456789012",
			Region:      "us-east-1",
			ServiceKind: awscloud.ServiceCloudHSMV2,
		},
	})
	if err != nil {
		t.Fatalf("build() error = %v", err)
	}
	if scanner == nil {
		t.Fatalf("build() returned nil scanner")
	}
}
