// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package runtimebind_test

import (
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/collector/awscloud/awsruntime"
	_ "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/autoscaling/runtimebind"
)

// TestAutoScalingRuntimeBindRegisters confirms importing the binding installs
// the Auto Scaling scanner builder.
func TestAutoScalingRuntimeBindRegisters(t *testing.T) {
	build, ok := awsruntime.LookupBuilder(awscloud.ServiceAutoScaling)
	if !ok {
		t.Fatalf("LookupBuilder(%q) ok = false, want true", awscloud.ServiceAutoScaling)
	}
	scanner, err := build(awsruntime.ScannerDeps{
		AWSConfig: aws.Config{Region: "us-east-1"},
		Boundary:  awscloud.Boundary{AccountID: "123456789012", Region: "us-east-1", ServiceKind: awscloud.ServiceAutoScaling},
	})
	if err != nil {
		t.Fatalf("build() error = %v", err)
	}
	if scanner == nil {
		t.Fatalf("build() returned nil scanner")
	}
}

// TestAutoScalingRuntimeBindDoesNotRequireRedactionKey confirms the Auto
// Scaling scanner registers without a redaction-key requirement, because it
// drops launch configuration and launch template UserData by never mapping it.
func TestAutoScalingRuntimeBindDoesNotRequireRedactionKey(t *testing.T) {
	if awsruntime.ServiceRequiresRedactionKey(awscloud.ServiceAutoScaling) {
		t.Fatalf("ServiceRequiresRedactionKey(%q) = true, want false", awscloud.ServiceAutoScaling)
	}
}
