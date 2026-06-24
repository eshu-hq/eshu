// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

// TestLoadRuntimeConfigRequiresRedactionKeyForAppMesh confirms that an App
// Mesh-only target scope requires a redaction key because route HTTP header
// match values may carry credentials or customer identifiers that route
// through the shared redact library before persistence.
func TestLoadRuntimeConfigRequiresRedactionKeyForAppMesh(t *testing.T) {
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
					"allowed_services":["appmesh"],
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
	if got, want := config.AWS.Targets[0].AllowedServices[0], awscloud.ServiceAppMesh; got != want {
		t.Fatalf("AllowedServices[0] = %q, want %q", got, want)
	}
	if config.AWSRedactionKey.IsZero() {
		t.Fatalf("AWSRedactionKey zero for App Mesh target, want configured key")
	}
}

// TestLoadRuntimeConfigRejectsMissingRedactionKeyForAppMesh confirms the
// negative path: an App Mesh-only target with no ESHU_AWS_REDACTION_KEY is
// rejected with an error naming the missing key.
func TestLoadRuntimeConfigRejectsMissingRedactionKeyForAppMesh(t *testing.T) {
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
					"allowed_services":["appmesh"],
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
