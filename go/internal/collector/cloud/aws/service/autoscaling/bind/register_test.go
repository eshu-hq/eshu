// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package bind_test

import (
	"testing"

	awsv2 "github.com/aws/aws-sdk-go-v2/aws"

	"github.com/eshu-hq/eshu/go/internal/collector/cloud/aws"
	"github.com/eshu-hq/eshu/go/internal/collector/cloud/aws/runtime"
	_ "github.com/eshu-hq/eshu/go/internal/collector/cloud/aws/service/autoscaling/bind"
)

// TestAutoScalingRuntimeBindRegisters confirms importing the binding installs
// the Auto Scaling scanner builder.
func TestAutoScalingRuntimeBindRegisters(t *testing.T) {
	build, ok := runtime.LookupBuilder(aws.ServiceAutoScaling)
	if !ok {
		t.Fatalf("LookupBuilder(%q) ok = false, want true", aws.ServiceAutoScaling)
	}
	scanner, err := build(runtime.ScannerDeps{
		AWSConfig: awsv2.Config{Region: "us-east-1"},
		Boundary:  aws.Boundary{AccountID: "123456789012", Region: "us-east-1", ServiceKind: aws.ServiceAutoScaling},
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
	if runtime.ServiceRequiresRedactionKey(aws.ServiceAutoScaling) {
		t.Fatalf("ServiceRequiresRedactionKey(%q) = true, want false", aws.ServiceAutoScaling)
	}
}
