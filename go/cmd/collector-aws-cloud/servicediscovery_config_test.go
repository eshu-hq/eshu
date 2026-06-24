// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

// TestLoadRuntimeConfigDoesNotRequireRedactionKeyForServiceDiscovery confirms a
// Cloud Map (Service Discovery)-only target scope loads without a redaction key.
// The scanner records instance counts only and never reads instance attribute
// maps, so it declares no redaction-key requirement and the command derives
// none.
func TestLoadRuntimeConfigDoesNotRequireRedactionKeyForServiceDiscovery(t *testing.T) {
	getenv := mapEnv(map[string]string{
		"ESHU_COLLECTOR_INSTANCES_JSON": `[{
			"instance_id":"collector-aws-1",
			"collector_kind":"aws",
			"mode":"continuous",
			"enabled":true,
			"claims_enabled":true,
			"configuration":{
				"target_scopes":[{
					"account_id":"123456789012",
					"allowed_regions":["us-east-1"],
					"allowed_services":["servicediscovery"],
					"credentials":{
						"mode":"local_workload_identity"
					}
				}]
			}
		}]`,
		"ESHU_AWS_COLLECTOR_INSTANCE_ID": "collector-aws-1",
	})

	config, err := loadRuntimeConfig(getenv)
	if err != nil {
		t.Fatalf("loadRuntimeConfig() error = %v, want nil", err)
	}
	if got, want := config.AWS.Targets[0].AllowedServices[0], awscloud.ServiceServiceDiscovery; got != want {
		t.Fatalf("AllowedServices[0] = %q, want %q", got, want)
	}
	if !config.AWSRedactionKey.IsZero() {
		t.Fatalf("AWSRedactionKey configured for Service Discovery-only target, want zero key")
	}
}
