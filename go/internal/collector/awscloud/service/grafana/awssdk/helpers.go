// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

// workspaceARN builds the partition-aware Managed Grafana workspace ARN.
// DescribeWorkspace does not report an ARN, so the adapter synthesizes one. The
// partition is derived from the scan boundary, never hardcoded, so a GovCloud or
// China workspace id resolves to an ARN in its own partition instead of
// dangling the workspace node and its outgoing edges.
func workspaceARN(boundary awscloud.Boundary, workspaceID string) string {
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" {
		return ""
	}
	return fmt.Sprintf("arn:%s:grafana:%s:%s:/workspaces/%s",
		awscloud.PartitionForBoundary(boundary), boundary.Region, boundary.AccountID, workspaceID)
}
