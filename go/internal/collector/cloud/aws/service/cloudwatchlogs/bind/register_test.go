// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package bind_test

import (
	"testing"

	awsv2 "github.com/aws/aws-sdk-go-v2/aws"

	"github.com/eshu-hq/eshu/go/internal/collector/cloud/aws"
	"github.com/eshu-hq/eshu/go/internal/collector/cloud/aws/runtime"
	_ "github.com/eshu-hq/eshu/go/internal/collector/cloud/aws/service/cloudwatchlogs/bind"
)

// TestCloudWatchLogsRuntimeBindRegisters confirms importing the binding
// installs the CloudWatch Logs scanner builder.
func TestCloudWatchLogsRuntimeBindRegisters(t *testing.T) {
	build, ok := runtime.LookupBuilder(aws.ServiceCloudWatchLogs)
	if !ok {
		t.Fatalf("LookupBuilder(%q) ok = false, want true", aws.ServiceCloudWatchLogs)
	}
	scanner, err := build(runtime.ScannerDeps{
		AWSConfig: awsv2.Config{Region: "us-east-1"},
		Boundary:  aws.Boundary{AccountID: "123456789012", Region: "us-east-1", ServiceKind: aws.ServiceCloudWatchLogs},
	})
	if err != nil {
		t.Fatalf("build() error = %v", err)
	}
	if scanner == nil {
		t.Fatalf("build() returned nil scanner")
	}
}
