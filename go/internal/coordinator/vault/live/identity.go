// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package live

import (
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

// vaultLiveSourceSystem is the source-system label stamped on Vault live
// scopes and work items. It repeats the extracted collector's kind string so
// the coordinator plans identical rows without importing the collector
// package; the contract tests pin the literal value.
const vaultLiveSourceSystem = string(scope.CollectorVaultLive)

// vaultLiveScopeID returns the deterministic durable scope id for one Vault
// cluster and namespace. It mirrors the collector's scope contract
// ({vault_cluster_id, namespace} stable identity under the "VaultLiveScope"
// domain, prefixed by the collector kind) so planner output stays identical
// while the coordinator holds no dependency on the collector package.
func vaultLiveScopeID(clusterID, namespace string) (string, error) {
	clusterID = strings.TrimSpace(clusterID)
	if clusterID == "" {
		return "", fmt.Errorf("vault_cluster_id must not be blank")
	}
	return vaultLiveSourceSystem + ":" + facts.StableID("VaultLiveScope", map[string]any{
		"vault_cluster_id": clusterID,
		"namespace":        strings.TrimSpace(namespace),
	}), nil
}
