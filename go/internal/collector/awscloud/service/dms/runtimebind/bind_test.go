// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package runtimebind_test

import (
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/collector/awscloud/awsruntime"
	"github.com/eshu-hq/eshu/go/internal/collector/awscloud/catalog"
	_ "github.com/eshu-hq/eshu/go/internal/collector/awscloud/service/dms/runtimebind"
)

// TestDMSRuntimeBindRegisters confirms importing the binding installs the DMS
// scanner builder.
func TestDMSRuntimeBindRegisters(t *testing.T) {
	build, ok := awsruntime.LookupBuilder(catalog.ServiceDMS)
	if !ok {
		t.Fatalf("LookupBuilder(%q) ok = false, want true", catalog.ServiceDMS)
	}
	scanner, err := build(awsruntime.ScannerDeps{
		AWSConfig: aws.Config{Region: "us-east-1"},
		Boundary: awscloud.Boundary{
			AccountID:   "123456789012",
			Region:      "us-east-1",
			ServiceKind: catalog.ServiceDMS,
		},
	})
	if err != nil {
		t.Fatalf("build() error = %v", err)
	}
	if scanner == nil {
		t.Fatalf("build() returned nil scanner")
	}
}
