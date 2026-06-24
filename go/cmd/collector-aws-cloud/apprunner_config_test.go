// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

// TestLoadRuntimeConfigDoesNotRequireRedactionKeyForAppRunner confirms an App
// Runner-only target loads without a redaction key. The App Runner scanner
// drops runtime environment-variable values entirely (it records names only and
// carries secret references as ARN edges), so it has no redaction-key
// dependency.
func TestLoadRuntimeConfigDoesNotRequireRedactionKeyForAppRunner(t *testing.T) {
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
					"allowed_services":["apprunner"],
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
	if got, want := config.AWS.Targets[0].AllowedServices[0], awscloud.ServiceAppRunner; got != want {
		t.Fatalf("AllowedServices[0] = %q, want %q", got, want)
	}
	if !config.AWSRedactionKey.IsZero() {
		t.Fatalf("AWSRedactionKey configured for App Runner-only target, want zero key")
	}
}
