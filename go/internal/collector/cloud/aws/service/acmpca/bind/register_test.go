// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package bind_test

import (
	"testing"

	awsv2 "github.com/aws/aws-sdk-go-v2/aws"

	"github.com/eshu-hq/eshu/go/internal/collector/cloud/aws"
	"github.com/eshu-hq/eshu/go/internal/collector/cloud/aws/runtime"
	_ "github.com/eshu-hq/eshu/go/internal/collector/cloud/aws/service/acmpca/bind"
)

// TestACMPCARuntimeBindRegisters confirms importing the binding installs the
// ACM Private CA scanner builder and that the scanner requires no redaction key.
func TestACMPCARuntimeBindRegisters(t *testing.T) {
	build, ok := runtime.LookupBuilder(aws.ServiceACMPCA)
	if !ok {
		t.Fatalf("LookupBuilder(%q) ok = false, want true", aws.ServiceACMPCA)
	}
	scanner, err := build(runtime.ScannerDeps{
		AWSConfig: awsv2.Config{Region: "us-east-1"},
		Boundary:  aws.Boundary{AccountID: "123456789012", Region: "us-east-1", ServiceKind: aws.ServiceACMPCA},
	})
	if err != nil {
		t.Fatalf("build() error = %v", err)
	}
	if scanner == nil {
		t.Fatalf("build() returned nil scanner")
	}
	if runtime.ServiceRequiresRedactionKey(aws.ServiceACMPCA) {
		t.Fatalf("ServiceRequiresRedactionKey(%q) = true, want false (metadata-only scanner)", aws.ServiceACMPCA)
	}
}
