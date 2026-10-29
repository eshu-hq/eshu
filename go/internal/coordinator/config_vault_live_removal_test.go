// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package coordinator

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/scope"
)

// testVaultLiveRemovalConfig is a vault_live configuration that passes
// instance-level validation, so it reaches the startup claim-scheduling
// gate under test.
const testVaultLiveRemovalConfig = `{"targets":[{"vault_cluster_id":"vault-a","address":"https://vault.example.com","token_env":"VAULT_TOKEN"}]}`

// TestConfigValidateRejectsEnabledVaultLiveInstance is the #8869 removal
// regression: the in-tree vault/live planner is deleted, so an enabled,
// claims-enabled vault_live instance must fail startup validation loudly
// instead of being admitted and silently never planned.
func TestConfigValidateRejectsEnabledVaultLiveInstance(t *testing.T) {
	t.Parallel()

	instance := desiredSelectorInstance(scope.CollectorVaultLive, testVaultLiveRemovalConfig)
	err := activeConfigWithInstance(instance).Validate()
	if err == nil || !strings.Contains(err.Error(), "vault_live") {
		t.Fatalf("Config.Validate() error = %v, want the vault_live extraction error", err)
	}
}
