// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"strings"
	"testing"
)

func TestLoadScheduledSyncConfigDefaultsEnabled(t *testing.T) {
	t.Parallel()

	config, err := LoadScheduledSyncConfig(func(string) string { return "" })
	if err != nil {
		t.Fatalf("LoadScheduledSyncConfig() error = %v, want nil", err)
	}
	if !config.Enabled {
		t.Fatal("Enabled = false, want default true")
	}
}

func TestLoadScheduledSyncConfigParsesDisabled(t *testing.T) {
	t.Parallel()

	config, err := LoadScheduledSyncConfig(func(key string) string {
		if key == repoScheduledSyncEnabledEnv {
			return "false"
		}
		return ""
	})
	if err != nil {
		t.Fatalf("LoadScheduledSyncConfig() error = %v, want nil", err)
	}
	if config.Enabled {
		t.Fatal("Enabled = true, want false")
	}
}

func TestLoadScheduledSyncConfigRejectsInvalidValue(t *testing.T) {
	t.Parallel()

	_, err := LoadScheduledSyncConfig(func(key string) string {
		if key == repoScheduledSyncEnabledEnv {
			return "eventually"
		}
		return ""
	})
	if err == nil {
		t.Fatal("LoadScheduledSyncConfig() error = nil, want invalid value error")
	}
	if !strings.Contains(err.Error(), "ESHU_REPO_SCHEDULED_SYNC_ENABLED") {
		t.Fatalf("LoadScheduledSyncConfig() error = %q, want env name", err)
	}
	if !strings.Contains(err.Error(), "accepted values: true, false, 1, 0, yes, no, on, off") {
		t.Fatalf("LoadScheduledSyncConfig() error = %q, want accepted values", err)
	}
}
