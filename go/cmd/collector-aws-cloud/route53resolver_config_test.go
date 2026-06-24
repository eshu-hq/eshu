// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

// TestLoadRuntimeConfigDoesNotRequireRedactionKeyForRoute53Resolver confirms a
// route53resolver-only target scope is accepted without ESHU_AWS_REDACTION_KEY.
// The scanner drops DNS Firewall domain list contents by never mapping them,
// so no HMAC redaction is required.
func TestLoadRuntimeConfigDoesNotRequireRedactionKeyForRoute53Resolver(t *testing.T) {
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
					"allowed_services":["route53resolver"],
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
	if got, want := config.AWS.Targets[0].AllowedServices[0], awscloud.ServiceRoute53Resolver; got != want {
		t.Fatalf("AllowedServices[0] = %q, want %q", got, want)
	}
	if !config.AWSRedactionKey.IsZero() {
		t.Fatalf("AWSRedactionKey configured for route53resolver-only target, want zero key")
	}
}
