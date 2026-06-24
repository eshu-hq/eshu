// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package runtimebind_test

import (
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/collector/awscloud/awsruntime"
	_ "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/amplify/runtimebind"
)

// TestAmplifyRuntimeBindRegisters confirms importing the binding installs the
// Amplify scanner builder and that the builder resolves without a redaction key
// because the SDK adapter drops every secret-bearing field at the boundary.
func TestAmplifyRuntimeBindRegisters(t *testing.T) {
	build, ok := awsruntime.LookupBuilder(awscloud.ServiceAmplify)
	if !ok {
		t.Fatalf("LookupBuilder(%q) ok = false, want true", awscloud.ServiceAmplify)
	}

	scanner, err := build(awsruntime.ScannerDeps{
		AWSConfig: aws.Config{Region: "us-east-1"},
		Boundary:  awscloud.Boundary{AccountID: "123456789012", Region: "us-east-1", ServiceKind: awscloud.ServiceAmplify},
	})
	if err != nil {
		t.Fatalf("build() error = %v", err)
	}
	if scanner == nil {
		t.Fatalf("build() returned nil scanner")
	}
}
