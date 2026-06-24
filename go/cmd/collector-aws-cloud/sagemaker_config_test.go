// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

// TestLoadRuntimeConfigDoesNotRequireRedactionKeyForSageMaker confirms a
// SageMaker-only target validates and that the SageMaker scanner needs no
// redaction key: it is metadata-only and drops sensitive payloads by never
// reading them rather than by redacting them.
func TestLoadRuntimeConfigDoesNotRequireRedactionKeyForSageMaker(t *testing.T) {
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
					"allowed_services":["sagemaker"],
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
	if got, want := config.AWS.Targets[0].AllowedServices[0], awscloud.ServiceSageMaker; got != want {
		t.Fatalf("AllowedServices[0] = %q, want %q", got, want)
	}
	if !config.AWSRedactionKey.IsZero() {
		t.Fatalf("AWSRedactionKey configured for SageMaker-only target, want zero key")
	}
}
