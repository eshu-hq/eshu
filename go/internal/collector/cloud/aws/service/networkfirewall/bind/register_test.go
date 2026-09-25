// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package bind_test

import (
	"testing"

	awsv2 "github.com/aws/aws-sdk-go-v2/aws"

	"github.com/eshu-hq/eshu/go/internal/collector/cloud/aws"
	"github.com/eshu-hq/eshu/go/internal/collector/cloud/aws/runtime"
	_ "github.com/eshu-hq/eshu/go/internal/collector/cloud/aws/service/networkfirewall/bind"
)

// TestNetworkFirewallRuntimeBindRegisters confirms importing the binding
// installs the Network Firewall scanner builder.
func TestNetworkFirewallRuntimeBindRegisters(t *testing.T) {
	build, ok := runtime.LookupBuilder(aws.ServiceNetworkFirewall)
	if !ok {
		t.Fatalf("LookupBuilder(%q) ok = false, want true", aws.ServiceNetworkFirewall)
	}
	scanner, err := build(runtime.ScannerDeps{
		AWSConfig: awsv2.Config{Region: "us-east-1"},
		Boundary:  aws.Boundary{AccountID: "123456789012", Region: "us-east-1", ServiceKind: aws.ServiceNetworkFirewall},
	})
	if err != nil {
		t.Fatalf("build() error = %v", err)
	}
	if scanner == nil {
		t.Fatalf("build() returned nil scanner")
	}
}

// TestNetworkFirewallDoesNotRequireRedactionKey documents that the scanner
// drops sensitive bodies by never mapping them, so it needs no redaction key.
func TestNetworkFirewallDoesNotRequireRedactionKey(t *testing.T) {
	if runtime.ServiceRequiresRedactionKey(aws.ServiceNetworkFirewall) {
		t.Fatalf("ServiceRequiresRedactionKey(%q) = true, want false", aws.ServiceNetworkFirewall)
	}
}
