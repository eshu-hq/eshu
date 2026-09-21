// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package bind_test

import (
	"strings"
	"testing"

	awsv2 "github.com/aws/aws-sdk-go-v2/aws"

	"github.com/eshu-hq/eshu/go/internal/collector/cloud/aws"
	"github.com/eshu-hq/eshu/go/internal/collector/cloud/aws/runtime"
	_ "github.com/eshu-hq/eshu/go/internal/collector/cloud/aws/service/cognito/bind"
	"github.com/eshu-hq/eshu/go/internal/redact"
)

// TestCognitoRuntimeBindRegisters confirms importing the binding installs the
// Cognito scanner builder.
func TestCognitoRuntimeBindRegisters(t *testing.T) {
	build, ok := runtime.LookupBuilder(aws.ServiceCognito)
	if !ok {
		t.Fatalf("LookupBuilder(%q) ok = false, want true", aws.ServiceCognito)
	}
	key, err := redact.NewKey([]byte("aws-redaction-key"))
	if err != nil {
		t.Fatalf("NewKey() error = %v", err)
	}
	scanner, err := build(runtime.ScannerDeps{
		AWSConfig:    awsv2.Config{Region: "us-east-1"},
		Boundary:     aws.Boundary{AccountID: "123456789012", Region: "us-east-1", ServiceKind: aws.ServiceCognito},
		RedactionKey: key,
	})
	if err != nil {
		t.Fatalf("build() error = %v", err)
	}
	if scanner == nil {
		t.Fatalf("build() returned nil scanner")
	}
}

// TestCognitoRuntimeBindRequiresRedactionKey covers the redaction-key guard the
// Cognito builder enforces, mirroring the ECS and Lambda scanners.
func TestCognitoRuntimeBindRequiresRedactionKey(t *testing.T) {
	build, ok := runtime.LookupBuilder(aws.ServiceCognito)
	if !ok {
		t.Fatalf("LookupBuilder(%q) ok = false, want true", aws.ServiceCognito)
	}
	_, err := build(runtime.ScannerDeps{
		AWSConfig: awsv2.Config{Region: "us-east-1"},
		Boundary:  aws.Boundary{AccountID: "123456789012", Region: "us-east-1", ServiceKind: aws.ServiceCognito},
	})
	if err == nil {
		t.Fatalf("build() error = nil, want missing redaction key")
	}
	if !strings.Contains(err.Error(), "redaction key") {
		t.Fatalf("build() error = %q, want redaction key", err)
	}
}
