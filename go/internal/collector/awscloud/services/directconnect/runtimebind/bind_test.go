// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package runtimebind_test

import (
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/collector/awscloud/awsruntime"
	_ "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/directconnect/runtimebind"
)

// TestDirectConnectRuntimeBindRegisters confirms importing the binding installs
// the Direct Connect scanner builder. Direct Connect does not redact, so the
// builder must succeed with a zero redaction key.
func TestDirectConnectRuntimeBindRegisters(t *testing.T) {
	build, ok := awsruntime.LookupBuilder(awscloud.ServiceDirectConnect)
	if !ok {
		t.Fatalf("LookupBuilder(%q) ok = false, want true", awscloud.ServiceDirectConnect)
	}
	scanner, err := build(awsruntime.ScannerDeps{
		AWSConfig: aws.Config{Region: "us-east-1"},
		Boundary: awscloud.Boundary{
			AccountID:   "123456789012",
			Region:      "us-east-1",
			ServiceKind: awscloud.ServiceDirectConnect,
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
	if awsruntime.ServiceRequiresRedactionKey(awscloud.ServiceDirectConnect) {
		t.Fatalf("ServiceRequiresRedactionKey(%q) = true, want false; Direct Connect drops secrets by exclusion", awscloud.ServiceDirectConnect)
	}
}
