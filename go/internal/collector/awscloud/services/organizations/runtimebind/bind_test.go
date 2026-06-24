// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package runtimebind_test

import (
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/collector/awscloud/awsruntime"
	_ "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/organizations/runtimebind"
	"github.com/eshu-hq/eshu/go/internal/redact"
)

// TestOrganizationsRuntimeBindRegisters confirms importing the binding
// installs the Organizations scanner builder.
func TestOrganizationsRuntimeBindRegisters(t *testing.T) {
	build, ok := awsruntime.LookupBuilder(awscloud.ServiceOrganizations)
	if !ok {
		t.Fatalf("LookupBuilder(%q) ok = false, want true", awscloud.ServiceOrganizations)
	}
	key, err := redact.NewKey([]byte("aws-redaction-key"))
	if err != nil {
		t.Fatalf("NewKey() error = %v", err)
	}
	scanner, err := build(awsruntime.ScannerDeps{
		AWSConfig:    aws.Config{Region: "us-east-1"},
		Boundary:     awscloud.Boundary{AccountID: "123456789012", Region: "us-east-1", ServiceKind: awscloud.ServiceOrganizations},
		RedactionKey: key,
	})
	if err != nil {
		t.Fatalf("build() error = %v", err)
	}
	if scanner == nil {
		t.Fatalf("build() returned nil scanner")
	}
}

// TestOrganizationsRuntimeBindRequiresRedactionKey covers the guard the
// binding inherits from the legacy switch.
func TestOrganizationsRuntimeBindRequiresRedactionKey(t *testing.T) {
	build, ok := awsruntime.LookupBuilder(awscloud.ServiceOrganizations)
	if !ok {
		t.Fatalf("LookupBuilder(%q) ok = false, want true", awscloud.ServiceOrganizations)
	}
	_, err := build(awsruntime.ScannerDeps{
		AWSConfig: aws.Config{Region: "us-east-1"},
		Boundary:  awscloud.Boundary{AccountID: "123456789012", Region: "us-east-1", ServiceKind: awscloud.ServiceOrganizations},
	})
	if err == nil {
		t.Fatalf("build() error = nil, want missing redaction key")
	}
	if !strings.Contains(err.Error(), "redaction key") {
		t.Fatalf("build() error = %q, want redaction key", err)
	}
}
