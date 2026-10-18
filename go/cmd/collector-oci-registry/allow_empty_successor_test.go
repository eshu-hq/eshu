// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"testing"
)

func TestLoadRuntimeConfigRoundTripsAllowEmptySuccessorTargetKey(t *testing.T) {
	t.Parallel()

	load := func(t *testing.T, targetKey string) bool {
		t.Helper()
		key := ""
		if targetKey != "" {
			key = `"allow_empty_successor":` + targetKey + ","
		}
		env := map[string]string{
			envCollectorInstanceID: "oci-registry-test",
			envTargetsJSON:         `[{"provider":"dockerhub","repository":"busybox","references":["latest"],` + key + `"tag_limit":1}]`,
		}
		config, err := loadRuntimeConfig(func(k string) string { return env[k] })
		if err != nil {
			t.Fatalf("loadRuntimeConfig() error = %v, want nil", err)
		}
		if len(config.Targets) != 1 {
			t.Fatalf("len(Targets) = %d, want 1", len(config.Targets))
		}
		return config.Targets[0].AllowEmptySuccessor
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
		env := map[string]string{
			envCollectorInstanceID: "oci-registry-test",
			envTargetsJSON:         `[{"provider":"dockerhub","repository":"busybox","references":["latest"]}]`,
		}
		if envValue != "" {
			env["ESHU_OCI_REGISTRY_ALLOW_EMPTY_SUCCESSOR"] = envValue
		}
		config, err := loadRuntimeConfig(func(k string) string { return env[k] })
		if err != nil {
			t.Fatalf("loadRuntimeConfig() error = %v, want nil", err)
		}
		return config.AllowEmptySuccessor
	}

	if got := load(t, "true"); !got {
		t.Fatal("AllowEmptySuccessor = false with ESHU_OCI_REGISTRY_ALLOW_EMPTY_SUCCESSOR=true, want true")
	}
	if got := load(t, ""); got {
		t.Fatal("AllowEmptySuccessor = true without ESHU_OCI_REGISTRY_ALLOW_EMPTY_SUCCESSOR, want false default")
	}

	env := map[string]string{
		envCollectorInstanceID:                    "oci-registry-test",
		envTargetsJSON:                            `[{"provider":"dockerhub","repository":"busybox","references":["latest"]}]`,
		"ESHU_OCI_REGISTRY_ALLOW_EMPTY_SUCCESSOR": "not-a-boolean",
	}
	if _, err := loadRuntimeConfig(func(k string) string { return env[k] }); err == nil {
		t.Fatal("loadRuntimeConfig() error = nil, want invalid boolean error")
	}
}
