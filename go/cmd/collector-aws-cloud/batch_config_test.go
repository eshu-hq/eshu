// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

// TestLoadRuntimeConfigRequiresRedactionKeyForBatch confirms that a Batch-only
// target scope requires a redaction key because Batch job-definition container
// environment values may carry secrets and route through the shared redact
// library before persistence.
func TestLoadRuntimeConfigRequiresRedactionKeyForBatch(t *testing.T) {
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
					"allowed_services":["batch"],
					"credentials":{
						"mode":"local_workload_identity"
					}
				}]
			}
		}]`,
		"ESHU_AWS_COLLECTOR_INSTANCE_ID": "collector-aws-1",
		"ESHU_AWS_REDACTION_KEY":         "aws-redaction-key",
	})

	config, err := loadRuntimeConfig(getenv)
	if err != nil {
		t.Fatalf("loadRuntimeConfig() error = %v, want nil", err)
	}
	if got, want := config.AWS.Targets[0].AllowedServices[0], awscloud.ServiceBatch; got != want {
		t.Fatalf("AllowedServices[0] = %q, want %q", got, want)
	}
	if config.AWSRedactionKey.IsZero() {
		t.Fatalf("AWSRedactionKey zero for Batch target, want configured key")
	}
}

// TestLoadRuntimeConfigRejectsMissingRedactionKeyForBatch confirms the negative
// path: a Batch-only target with no ESHU_AWS_REDACTION_KEY is rejected with an
// error naming the missing key.
func TestLoadRuntimeConfigRejectsMissingRedactionKeyForBatch(t *testing.T) {
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
					"allowed_services":["batch"],
					"credentials":{
						"mode":"local_workload_identity"
					}
				}]
			}
		}]`,
		"ESHU_AWS_COLLECTOR_INSTANCE_ID": "collector-aws-1",
	})

	_, err := loadRuntimeConfig(getenv)
	if err == nil {
		t.Fatalf("loadRuntimeConfig() error = nil, want missing redaction key rejection")
	}
	if !strings.Contains(err.Error(), "ESHU_AWS_REDACTION_KEY") {
		t.Fatalf("loadRuntimeConfig() error = %v, want ESHU_AWS_REDACTION_KEY", err)
	}
}
