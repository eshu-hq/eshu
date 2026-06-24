// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cloudhsmv2

import (
	"strings"
	"time"
)

// clusterResourceID returns the resource_id the cluster node publishes. AWS
// CloudHSM v2 clusters have no API ARN, so the node is keyed by the bare cluster
// id (cluster-…). The backup-of-cluster edge keys this same value.
func clusterResourceID(cluster Cluster) string {
	return strings.TrimSpace(cluster.ID)
}

// backupResourceID returns the resource_id the backup node publishes. CloudHSM
// v2 reports a backup ARN, but the backup id (backup-…) is the stable identity
// the API always returns, so the node is keyed by the bare backup id.
func backupResourceID(backup Backup) string {
	return strings.TrimSpace(backup.ID)
}

// timeOrNil returns the UTC time when value is set, or nil for the zero time so
// the attribute payload omits an unknown timestamp instead of emitting an epoch.
func timeOrNil(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return value.UTC()
}

// cloneStringMap returns a trimmed-key copy of input, or nil when it is empty or
// every key trims to empty, keeping omitempty-style payload behavior consistent.
func cloneStringMap(input map[string]string) map[string]string {
	if len(input) == 0 {
		return nil
	}
	output := make(map[string]string, len(input))
	for key, value := range input {
		trimmed := strings.TrimSpace(key)
		if trimmed == "" {
			continue
		}
		output[trimmed] = value
	}
	if len(output) == 0 {
		return nil
	}
	return output
}
