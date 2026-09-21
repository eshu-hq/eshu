// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package bind_test

import (
	"testing"

	awsv2 "github.com/aws/aws-sdk-go-v2/aws"

	"github.com/eshu-hq/eshu/go/internal/collector/cloud/aws"
	"github.com/eshu-hq/eshu/go/internal/collector/cloud/aws/runtime"
	_ "github.com/eshu-hq/eshu/go/internal/collector/cloud/aws/service/appmesh/bind"
	"github.com/eshu-hq/eshu/go/internal/redact"
)

// TestAppMeshRuntimeBindRegisters confirms importing the binding installs the
// App Mesh scanner builder.
func TestAppMeshRuntimeBindRegisters(t *testing.T) {
	build, ok := runtime.LookupBuilder(aws.ServiceAppMesh)
	if !ok {
		t.Fatalf("LookupBuilder(%q) ok = false, want true", aws.ServiceAppMesh)
	}
	key, err := redact.NewKey([]byte("appmesh-redaction-key"))
	if err != nil {
		t.Fatalf("NewKey() error = %v", err)
	}
	scanner, err := build(runtime.ScannerDeps{
		AWSConfig:    awsv2.Config{Region: "us-east-1"},
		Boundary:     aws.Boundary{AccountID: "123456789012", Region: "us-east-1", ServiceKind: aws.ServiceAppMesh},
		RedactionKey: key,
	})
	if err != nil {
		t.Fatalf("build() error = %v", err)
	}
	if scanner == nil {
		t.Fatalf("build() returned nil scanner")
	}
}

// TestAppMeshRuntimeBindRequiresRedactionKey confirms the builder fails closed
// when the redaction key is zero, because App Mesh redacts sensitive HTTP
// header match values.
func TestAppMeshRuntimeBindRequiresRedactionKey(t *testing.T) {
	build, ok := runtime.LookupBuilder(aws.ServiceAppMesh)
	if !ok {
		t.Fatalf("LookupBuilder(%q) ok = false, want true", aws.ServiceAppMesh)
	}
	_, err := build(runtime.ScannerDeps{
		AWSConfig: awsv2.Config{Region: "us-east-1"},
		Boundary:  aws.Boundary{AccountID: "123456789012", Region: "us-east-1", ServiceKind: aws.ServiceAppMesh},
	})
	if err == nil {
		t.Fatalf("build() error = nil, want redaction-key-required rejection")
	}
}
