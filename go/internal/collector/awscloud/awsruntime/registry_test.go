// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awsruntime_test

import (
	"context"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/collector/awscloud/awsruntime"
	_ "github.com/eshu-hq/eshu/go/internal/collector/awscloud/awsruntime/bindings"
	"github.com/eshu-hq/eshu/go/internal/redact"
)

// TestDefaultScannerFactoryBuildsIAMScanner is the sanity check that a basic
// plain-builder service resolves through the registry-backed factory. The
// exhaustive per-service coverage lives in
// registry_supported_services_test.go and in each service's runtimebind tests.
func TestDefaultScannerFactoryBuildsIAMScanner(t *testing.T) {
	factory := awsruntime.DefaultScannerFactory{}
	lease := staticAWSConfigLease{config: aws.Config{Region: "us-east-1"}}
	scanner, err := factory.Scanner(context.Background(), awsruntime.Target{
		AccountID:   "123456789012",
		Region:      "us-east-1",
		ServiceKind: awscloud.ServiceIAM,
	}, awscloud.Boundary{
		AccountID:   "123456789012",
		Region:      "us-east-1",
		ServiceKind: awscloud.ServiceIAM,
	}, lease)
	if err != nil {
		t.Fatalf("Scanner() error = %v", err)
	}
	if scanner == nil {
		t.Fatalf("Scanner() = nil, want IAM scanner")
	}
}

// TestDefaultScannerFactoryRequiresRedactionKeyForECS guards the ECS builder
// redaction-key precondition through the runtime entry point.
func TestDefaultScannerFactoryRequiresRedactionKeyForECS(t *testing.T) {
	factory := awsruntime.DefaultScannerFactory{}
	_, err := factory.Scanner(context.Background(), awsruntime.Target{
		AccountID:   "123456789012",
		Region:      "us-east-1",
		ServiceKind: awscloud.ServiceECS,
	}, awscloud.Boundary{
		AccountID:   "123456789012",
		Region:      "us-east-1",
		ServiceKind: awscloud.ServiceECS,
	}, staticAWSConfigLease{config: aws.Config{Region: "us-east-1"}})
	if err == nil {
		t.Fatalf("Scanner() error = nil, want missing ECS redaction key")
	}
	if !strings.Contains(err.Error(), "redaction key") {
		t.Fatalf("Scanner() error = %q, want redaction key", err)
	}
}

// TestDefaultScannerFactoryRequiresRedactionKeyForLambda guards the Lambda
// builder redaction-key precondition through the runtime entry point.
func TestDefaultScannerFactoryRequiresRedactionKeyForLambda(t *testing.T) {
	factory := awsruntime.DefaultScannerFactory{}
	_, err := factory.Scanner(context.Background(), awsruntime.Target{
		AccountID:   "123456789012",
		Region:      "us-east-1",
		ServiceKind: awscloud.ServiceLambda,
	}, awscloud.Boundary{
		AccountID:   "123456789012",
		Region:      "us-east-1",
		ServiceKind: awscloud.ServiceLambda,
	}, staticAWSConfigLease{config: aws.Config{Region: "us-east-1"}})
	if err == nil {
		t.Fatalf("Scanner() error = nil, want missing Lambda redaction key")
	}
	if !strings.Contains(err.Error(), "redaction key") {
		t.Fatalf("Scanner() error = %q, want redaction key", err)
	}
}

// TestDefaultScannerFactoryRequiresRedactionKeyForOrganizations guards the
// Organizations builder redaction-key precondition.
func TestDefaultScannerFactoryRequiresRedactionKeyForOrganizations(t *testing.T) {
	factory := awsruntime.DefaultScannerFactory{}
	_, err := factory.Scanner(context.Background(), awsruntime.Target{
		AccountID:   "123456789012",
		Region:      "us-east-1",
		ServiceKind: awscloud.ServiceOrganizations,
	}, awscloud.Boundary{
		AccountID:   "123456789012",
		Region:      "us-east-1",
		ServiceKind: awscloud.ServiceOrganizations,
	}, staticAWSConfigLease{config: aws.Config{Region: "us-east-1"}})
	if err == nil {
		t.Fatalf("Scanner() error = nil, want missing Organizations redaction key")
	}
	if !strings.Contains(err.Error(), "redaction key") {
		t.Fatalf("Scanner() error = %q, want redaction key", err)
	}
}

// TestDefaultScannerFactoryRequiresRedactionKeyForSecurityHub guards the
// SecurityHub builder redaction-key precondition.
func TestDefaultScannerFactoryRequiresRedactionKeyForSecurityHub(t *testing.T) {
	factory := awsruntime.DefaultScannerFactory{}
	_, err := factory.Scanner(context.Background(), awsruntime.Target{
		AccountID:   "123456789012",
		Region:      "us-east-1",
		ServiceKind: awscloud.ServiceSecurityHub,
	}, awscloud.Boundary{
		AccountID:   "123456789012",
		Region:      "us-east-1",
		ServiceKind: awscloud.ServiceSecurityHub,
	}, staticAWSConfigLease{config: aws.Config{Region: "us-east-1"}})
	if err == nil {
		t.Fatalf("Scanner() error = nil, want missing Security Hub redaction key")
	}
	if !strings.Contains(err.Error(), "redaction key") {
		t.Fatalf("Scanner() error = %q, want redaction key", err)
	}
}

// TestDefaultScannerFactoryBuildsECSWithRedactionKey covers the positive
// redaction-key path through the runtime entry point.
func TestDefaultScannerFactoryBuildsECSWithRedactionKey(t *testing.T) {
	key, err := redact.NewKey([]byte("aws-redaction-key"))
	if err != nil {
		t.Fatalf("NewKey() error = %v", err)
	}
	factory := awsruntime.DefaultScannerFactory{RedactionKey: key}
	scanner, err := factory.Scanner(context.Background(), awsruntime.Target{
		AccountID:   "123456789012",
		Region:      "us-east-1",
		ServiceKind: awscloud.ServiceECS,
	}, awscloud.Boundary{
		AccountID:   "123456789012",
		Region:      "us-east-1",
		ServiceKind: awscloud.ServiceECS,
	}, staticAWSConfigLease{config: aws.Config{Region: "us-east-1"}})
	if err != nil {
		t.Fatalf("Scanner() error = %v", err)
	}
	if scanner == nil {
		t.Fatalf("Scanner() = nil, want ECS scanner")
	}
}

// TestDefaultScannerFactoryRejectsUnsupportedService confirms the registry
// miss case still surfaces the documented error.
func TestDefaultScannerFactoryRejectsUnsupportedService(t *testing.T) {
	factory := awsruntime.DefaultScannerFactory{}
	_, err := factory.Scanner(context.Background(), awsruntime.Target{
		AccountID:   "123456789012",
		Region:      "us-east-1",
		ServiceKind: "unknown-service",
	}, awscloud.Boundary{
		AccountID:   "123456789012",
		Region:      "us-east-1",
		ServiceKind: "unknown-service",
	}, staticAWSConfigLease{config: aws.Config{Region: "us-east-1"}})
	if err == nil {
		t.Fatalf("Scanner() error = nil, want unsupported service error")
	}
	if !strings.Contains(err.Error(), `unsupported AWS service_kind "unknown-service"`) {
		t.Fatalf("Scanner() error = %q, want unsupported service_kind", err)
	}
}

// TestDefaultScannerFactoryRequiresAWSConfigLease confirms the lease type
// guard runs before registry lookup, so a wrong lease type cannot reach a
// builder.
func TestDefaultScannerFactoryRequiresAWSConfigLease(t *testing.T) {
	factory := awsruntime.DefaultScannerFactory{}
	_, err := factory.Scanner(context.Background(), awsruntime.Target{
		AccountID:   "123456789012",
		Region:      "us-east-1",
		ServiceKind: awscloud.ServiceIAM,
	}, awscloud.Boundary{
		AccountID:   "123456789012",
		Region:      "us-east-1",
		ServiceKind: awscloud.ServiceIAM,
	}, releaseOnlyLease{})
	if err == nil {
		t.Fatalf("Scanner() error = nil, want unsupported lease error")
	}
	if !strings.Contains(err.Error(), "unsupported AWS credential lease") {
		t.Fatalf("Scanner() error = %q, want unsupported lease", err)
	}
}
