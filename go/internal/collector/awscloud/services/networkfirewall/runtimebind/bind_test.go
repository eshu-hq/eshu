// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package runtimebind_test

import (
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/collector/awscloud/awsruntime"
	_ "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/networkfirewall/runtimebind"
)

// TestNetworkFirewallRuntimeBindRegisters confirms importing the binding
// installs the Network Firewall scanner builder.
func TestNetworkFirewallRuntimeBindRegisters(t *testing.T) {
	build, ok := awsruntime.LookupBuilder(awscloud.ServiceNetworkFirewall)
	if !ok {
		t.Fatalf("LookupBuilder(%q) ok = false, want true", awscloud.ServiceNetworkFirewall)
	}
	scanner, err := build(awsruntime.ScannerDeps{
		AWSConfig: aws.Config{Region: "us-east-1"},
		Boundary:  awscloud.Boundary{AccountID: "123456789012", Region: "us-east-1", ServiceKind: awscloud.ServiceNetworkFirewall},
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
	if awsruntime.ServiceRequiresRedactionKey(awscloud.ServiceNetworkFirewall) {
		t.Fatalf("ServiceRequiresRedactionKey(%q) = true, want false", awscloud.ServiceNetworkFirewall)
	}
}
