// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package runtimebind_test

import (
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/collector/awscloud/awsruntime"
	"github.com/eshu-hq/eshu/go/internal/collector/awscloud/catalog"
	_ "github.com/eshu-hq/eshu/go/internal/collector/awscloud/service/codedeploy/runtimebind"
	"github.com/eshu-hq/eshu/go/internal/redact"
)

// TestCodeDeployRuntimeBindRegisters confirms importing the binding installs
// the CodeDeploy scanner builder and that the builder requires a redaction key
// because on-premises tag values are redacted.
func TestCodeDeployRuntimeBindRegisters(t *testing.T) {
	build, ok := awsruntime.LookupBuilder(catalog.ServiceCodeDeploy)
	if !ok {
		t.Fatalf("LookupBuilder(%q) ok = false, want true", catalog.ServiceCodeDeploy)
	}

	if _, err := build(awsruntime.ScannerDeps{
		AWSConfig: aws.Config{Region: "us-east-1"},
		Boundary:  awscloud.Boundary{AccountID: "123456789012", Region: "us-east-1", ServiceKind: catalog.ServiceCodeDeploy},
	}); err == nil {
		t.Fatalf("build() error = nil, want redaction-key-required error")
	}

	key, err := redact.NewKey([]byte("codedeploy-redaction-key"))
	if err != nil {
		t.Fatalf("NewKey() error = %v", err)
	}
	scanner, err := build(awsruntime.ScannerDeps{
		AWSConfig:    aws.Config{Region: "us-east-1"},
		Boundary:     awscloud.Boundary{AccountID: "123456789012", Region: "us-east-1", ServiceKind: catalog.ServiceCodeDeploy},
		RedactionKey: key,
	})
	if err != nil {
		t.Fatalf("build() error = %v", err)
	}
	if scanner == nil {
		t.Fatalf("build() returned nil scanner")
	}
}
