// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"testing"
)

func awsAttestedInstancesJSON(t *testing.T, attested string) string {
	t.Helper()
	key := ""
	if attested != "" {
		key = `"allow_empty_successor":` + attested + ","
	}
	return `[{
		"instance_id":"collector-aws-1",
		"collector_kind":"aws",
		"mode":"continuous",
		"enabled":true,
		"claims_enabled":true,
		"configuration":{
			"target_scopes":[{
				"account_id":"123456789012",
				"allowed_regions":["us-east-1"],
				"allowed_services":["iam"],
				` + key + `
				"credentials":{
					"mode":"local_workload_identity"
				}
			}]
		}
	}]`
}

func TestLoadRuntimeConfigRoundTripsAllowEmptySuccessorTargetKey(t *testing.T) {
	t.Parallel()

	load := func(t *testing.T, targetKey string) bool {
		t.Helper()
		config, err := loadRuntimeConfig(mapEnv(map[string]string{
			"ESHU_COLLECTOR_INSTANCES_JSON":    awsAttestedInstancesJSON(t, targetKey),
			"ESHU_AWS_COLLECTOR_INSTANCE_ID":   "collector-aws-1",
			"ESHU_AWS_ALLOW_EMPTY_SUCCESSOR":   "",
			"ESHU_AWS_COLLECTOR_POLL_INTERVAL": "2s",
		}))
		if err != nil {
			t.Fatalf("loadRuntimeConfig() error = %v, want nil", err)
		}
		if len(config.AWS.Targets) != 1 {
			t.Fatalf("len(Targets) = %d, want 1", len(config.AWS.Targets))
		}
		return config.AWS.Targets[0].AllowEmptySuccessor
	}

	if got := load(t, "true"); !got {
		t.Fatal("AllowEmptySuccessor = false with allow_empty_successor:true, want true")
	}
	if got := load(t, ""); got {
		t.Fatal("AllowEmptySuccessor = true without allow_empty_successor, want false default")
	}
}

func TestLoadRuntimeConfigRoundTripsAllowEmptySuccessorEnv(t *testing.T) {
	t.Parallel()

	load := func(t *testing.T, envValue string) bool {
		t.Helper()
		values := map[string]string{
			"ESHU_COLLECTOR_INSTANCES_JSON":  awsAttestedInstancesJSON(t, ""),
			"ESHU_AWS_COLLECTOR_INSTANCE_ID": "collector-aws-1",
		}
		if envValue != "" {
			values["ESHU_AWS_ALLOW_EMPTY_SUCCESSOR"] = envValue
		}
		config, err := loadRuntimeConfig(mapEnv(values))
		if err != nil {
			t.Fatalf("loadRuntimeConfig() error = %v, want nil", err)
		}
		return config.AWS.AllowEmptySuccessor
	}

	if got := load(t, "true"); !got {
		t.Fatal("AllowEmptySuccessor = false with ESHU_AWS_ALLOW_EMPTY_SUCCESSOR=true, want true")
	}
	if got := load(t, ""); got {
		t.Fatal("AllowEmptySuccessor = true without ESHU_AWS_ALLOW_EMPTY_SUCCESSOR, want false default")
	}

	_, err := loadRuntimeConfig(mapEnv(map[string]string{
		"ESHU_COLLECTOR_INSTANCES_JSON":  awsAttestedInstancesJSON(t, ""),
		"ESHU_AWS_COLLECTOR_INSTANCE_ID": "collector-aws-1",
		"ESHU_AWS_ALLOW_EMPTY_SUCCESSOR": "not-a-boolean",
	}))
	if err == nil {
		t.Fatal("loadRuntimeConfig() error = nil, want invalid boolean error")
	}
}
