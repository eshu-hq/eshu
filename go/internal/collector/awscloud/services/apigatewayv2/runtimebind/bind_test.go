// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package runtimebind_test

import (
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/collector/awscloud/awsruntime"
	_ "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/apigatewayv2/runtimebind"
)

// TestAPIGatewayV2RuntimeBindRegisters confirms importing the binding installs
// the API Gateway v2 scanner builder.
func TestAPIGatewayV2RuntimeBindRegisters(t *testing.T) {
	build, ok := awsruntime.LookupBuilder(awscloud.ServiceAPIGatewayV2)
	if !ok {
		t.Fatalf("LookupBuilder(%q) ok = false, want true", awscloud.ServiceAPIGatewayV2)
	}
	scanner, err := build(awsruntime.ScannerDeps{
		AWSConfig: aws.Config{Region: "us-east-1"},
		Boundary:  awscloud.Boundary{AccountID: "123456789012", Region: "us-east-1", ServiceKind: awscloud.ServiceAPIGatewayV2},
	})
	if err != nil {
		t.Fatalf("build() error = %v", err)
	}
	if scanner == nil {
		t.Fatalf("build() returned nil scanner")
	}
}

// TestAPIGatewayV2RuntimeBindDoesNotRequireRedactionKey proves the v2 scanner
// drops secrets and templates by not mapping them, so it carries no redaction
// requirement.
func TestAPIGatewayV2RuntimeBindDoesNotRequireRedactionKey(t *testing.T) {
	if awsruntime.ServiceRequiresRedactionKey(awscloud.ServiceAPIGatewayV2) {
		t.Fatalf("ServiceRequiresRedactionKey(%q) = true, want false", awscloud.ServiceAPIGatewayV2)
	}
}
