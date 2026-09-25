// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package bind_test

import (
	"testing"

	awsv2 "github.com/aws/aws-sdk-go-v2/aws"

	"github.com/eshu-hq/eshu/go/internal/collector/cloud/aws"
	"github.com/eshu-hq/eshu/go/internal/collector/cloud/aws/runtime"
	_ "github.com/eshu-hq/eshu/go/internal/collector/cloud/aws/service/codedeploy/bind"
	"github.com/eshu-hq/eshu/go/internal/redact"
)

// TestCodeDeployRuntimeBindRegisters confirms importing the binding installs
// the CodeDeploy scanner builder and that the builder requires a redaction key
// because on-premises tag values are redacted.
func TestCodeDeployRuntimeBindRegisters(t *testing.T) {
	build, ok := runtime.LookupBuilder(aws.ServiceCodeDeploy)
	if !ok {
		t.Fatalf("LookupBuilder(%q) ok = false, want true", aws.ServiceCodeDeploy)
	}

	if _, err := build(runtime.ScannerDeps{
		AWSConfig: awsv2.Config{Region: "us-east-1"},
		Boundary:  aws.Boundary{AccountID: "123456789012", Region: "us-east-1", ServiceKind: aws.ServiceCodeDeploy},
	}); err == nil {
		t.Fatalf("build() error = nil, want redaction-key-required error")
	}

	key, err := redact.NewKey([]byte("codedeploy-redaction-key"))
	if err != nil {
		t.Fatalf("NewKey() error = %v", err)
	}
	scanner, err := build(runtime.ScannerDeps{
		AWSConfig:    awsv2.Config{Region: "us-east-1"},
		Boundary:     aws.Boundary{AccountID: "123456789012", Region: "us-east-1", ServiceKind: aws.ServiceCodeDeploy},
		RedactionKey: key,
	})
	if err != nil {
		t.Fatalf("build() error = %v", err)
	}
	if scanner == nil {
		t.Fatalf("build() returned nil scanner")
	}
}
