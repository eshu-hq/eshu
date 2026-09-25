// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package bind_test

import (
	"testing"

	awsv2 "github.com/aws/aws-sdk-go-v2/aws"

	"github.com/eshu-hq/eshu/go/internal/collector/cloud/aws"
	"github.com/eshu-hq/eshu/go/internal/collector/cloud/aws/runtime"
	_ "github.com/eshu-hq/eshu/go/internal/collector/cloud/aws/service/apigatewayv2/bind"
)

// TestAPIGatewayV2RuntimeBindRegisters confirms importing the binding installs
// the API Gateway v2 scanner builder.
func TestAPIGatewayV2RuntimeBindRegisters(t *testing.T) {
	build, ok := runtime.LookupBuilder(aws.ServiceAPIGatewayV2)
	if !ok {
		t.Fatalf("LookupBuilder(%q) ok = false, want true", aws.ServiceAPIGatewayV2)
	}
	scanner, err := build(runtime.ScannerDeps{
		AWSConfig: awsv2.Config{Region: "us-east-1"},
		Boundary:  aws.Boundary{AccountID: "123456789012", Region: "us-east-1", ServiceKind: aws.ServiceAPIGatewayV2},
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
	if runtime.ServiceRequiresRedactionKey(aws.ServiceAPIGatewayV2) {
		t.Fatalf("ServiceRequiresRedactionKey(%q) = true, want false", aws.ServiceAPIGatewayV2)
	}
}
