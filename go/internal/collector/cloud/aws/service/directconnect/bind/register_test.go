// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package bind_test

import (
	"testing"

	awsv2 "github.com/aws/aws-sdk-go-v2/aws"

	"github.com/eshu-hq/eshu/go/internal/collector/cloud/aws"
	"github.com/eshu-hq/eshu/go/internal/collector/cloud/aws/runtime"
	_ "github.com/eshu-hq/eshu/go/internal/collector/cloud/aws/service/directconnect/bind"
)

// TestDirectConnectRuntimeBindRegisters confirms importing the binding installs
// the Direct Connect scanner builder. Direct Connect does not redact, so the
// builder must succeed with a zero redaction key.
func TestDirectConnectRuntimeBindRegisters(t *testing.T) {
	build, ok := runtime.LookupBuilder(aws.ServiceDirectConnect)
	if !ok {
		t.Fatalf("LookupBuilder(%q) ok = false, want true", aws.ServiceDirectConnect)
	}
	scanner, err := build(runtime.ScannerDeps{
		AWSConfig: awsv2.Config{Region: "us-east-1"},
		Boundary: aws.Boundary{
			AccountID:   "123456789012",
			Region:      "us-east-1",
			ServiceKind: aws.ServiceDirectConnect,
		},
	})
	if err != nil {
		t.Fatalf("build() error = %v", err)
	}
	if scanner == nil {
		t.Fatalf("build() returned nil scanner")
	}
}

// TestDirectConnectRuntimeBindDoesNotRequireRedactionKey pins that the binding
// does not declare RequiresRedactionKey: Direct Connect drops the BGP auth key
// and MACsec key material by never mapping them, so it never needs a key.
func TestDirectConnectRuntimeBindDoesNotRequireRedactionKey(t *testing.T) {
	if runtime.ServiceRequiresRedactionKey(aws.ServiceDirectConnect) {
		t.Fatalf("ServiceRequiresRedactionKey(%q) = true, want false; Direct Connect drops secrets by exclusion", aws.ServiceDirectConnect)
	}
}
