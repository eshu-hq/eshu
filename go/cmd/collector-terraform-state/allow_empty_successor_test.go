// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"testing"
)

func tfstateBaseEnv(t *testing.T, configuration string) map[string]string {
	t.Helper()
	return map[string]string{
		"ESHU_COLLECTOR_INSTANCES_JSON": `[
			{
				"instance_id": "terraform-state-prod",
				"collector_kind": "terraform_state",
				"mode": "continuous",
				"enabled": true,
				"claims_enabled": true,
				"configuration": ` + configuration + `
			}
		]`,
		"ESHU_TFSTATE_COLLECTOR_INSTANCE_ID":     "terraform-state-prod",
		"ESHU_TFSTATE_REDACTION_KEY":             "test-redaction-key",
		"ESHU_TFSTATE_REDACTION_RULESET_VERSION": "test-schema-v1",
	}
}

func tfstateLocalSeedConfiguration(attested string) string {
	key := ""
	if attested != "" {
		key = `"allow_empty_successor":` + attested + ","
	}
	return `{` + key + `"discovery":{"seeds":[{"kind":"local","path":"/tmp/prod.tfstate","repo_id":"platform-infra"}]}}`
}

func TestLoadRuntimeConfigRoundTripsAllowEmptySuccessorConfigKey(t *testing.T) {
	t.Parallel()

	load := func(t *testing.T, configKey string) bool {
		t.Helper()
		config, err := loadRuntimeConfig(func(key string) string {
			return tfstateBaseEnv(t, tfstateLocalSeedConfiguration(configKey))[key]
		})
		if err != nil {
			t.Fatalf("loadRuntimeConfig() error = %v, want nil", err)
		}
		return config.AllowEmptySuccessor
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
		values := tfstateBaseEnv(t, tfstateLocalSeedConfiguration(""))
		if envValue != "" {
			values["ESHU_TFSTATE_ALLOW_EMPTY_SUCCESSOR"] = envValue
		}
		config, err := loadRuntimeConfig(func(key string) string { return values[key] })
		if err != nil {
			t.Fatalf("loadRuntimeConfig() error = %v, want nil", err)
		}
		return config.AllowEmptySuccessor
	}

	if got := load(t, "true"); !got {
		t.Fatal("AllowEmptySuccessor = false with ESHU_TFSTATE_ALLOW_EMPTY_SUCCESSOR=true, want true")
	}
	if got := load(t, ""); got {
		t.Fatal("AllowEmptySuccessor = true without ESHU_TFSTATE_ALLOW_EMPTY_SUCCESSOR, want false default")
	}

	values := tfstateBaseEnv(t, tfstateLocalSeedConfiguration(""))
	values["ESHU_TFSTATE_ALLOW_EMPTY_SUCCESSOR"] = "not-a-boolean"
	if _, err := loadRuntimeConfig(func(key string) string { return values[key] }); err == nil {
		t.Fatal("loadRuntimeConfig() error = nil, want invalid boolean error")
	}
}
