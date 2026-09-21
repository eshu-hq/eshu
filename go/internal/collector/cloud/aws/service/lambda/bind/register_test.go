// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package bind_test

import (
	"strings"
	"testing"

	awsv2 "github.com/aws/aws-sdk-go-v2/aws"

	"github.com/eshu-hq/eshu/go/internal/collector/cloud/aws"
	"github.com/eshu-hq/eshu/go/internal/collector/cloud/aws/runtime"
	_ "github.com/eshu-hq/eshu/go/internal/collector/cloud/aws/service/lambda/bind"
	"github.com/eshu-hq/eshu/go/internal/redact"
)

// TestLambdaRuntimeBindRegisters confirms importing the binding
// installs the Lambda scanner builder.
func TestLambdaRuntimeBindRegisters(t *testing.T) {
	build, ok := runtime.LookupBuilder(aws.ServiceLambda)
	if !ok {
		t.Fatalf("LookupBuilder(%q) ok = false, want true", aws.ServiceLambda)
	}
	key, err := redact.NewKey([]byte("aws-redaction-key"))
	if err != nil {
		t.Fatalf("NewKey() error = %v", err)
	}
	scanner, err := build(runtime.ScannerDeps{
		AWSConfig:    awsv2.Config{Region: "us-east-1"},
		Boundary:     aws.Boundary{AccountID: "123456789012", Region: "us-east-1", ServiceKind: aws.ServiceLambda},
		RedactionKey: key,
	})
	if err != nil {
		t.Fatalf("build() error = %v", err)
	}
	if scanner == nil {
		t.Fatalf("build() returned nil scanner")
	}
}

// TestLambdaRuntimeBindRequiresRedactionKey covers the guard the
// binding inherits from the legacy switch.
func TestLambdaRuntimeBindRequiresRedactionKey(t *testing.T) {
	build, ok := runtime.LookupBuilder(aws.ServiceLambda)
	if !ok {
		t.Fatalf("LookupBuilder(%q) ok = false, want true", aws.ServiceLambda)
	}
	_, err := build(runtime.ScannerDeps{
		AWSConfig: awsv2.Config{Region: "us-east-1"},
		Boundary:  aws.Boundary{AccountID: "123456789012", Region: "us-east-1", ServiceKind: aws.ServiceLambda},
	})
	if err == nil {
		t.Fatalf("build() error = nil, want missing redaction key")
	}
	if !strings.Contains(err.Error(), "redaction key") {
		t.Fatalf("build() error = %q, want redaction key", err)
	}
}
