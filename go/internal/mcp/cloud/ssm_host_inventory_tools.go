// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cloudtools

import "github.com/eshu-hq/eshu/go/internal/mcp/contract/tool"

// SSMHostInventoryTools returns the admitted SSM host inventory observation
// readback tool. It mirrors the GET /api/v0/cloud/ssm-host-inventory route:
// a bounded, host-paged, truth-labeled list of committed SSMHostApplication
// and SSMHostPlatform observation nodes with the read-time capture status
// overlay, filterable by instance, account, region, and status. The tool is
// read-only and surfaces observations only: it never synthesizes
// vulnerability, package-correlation, or supply-chain findings from host
// data (#9209 owns correlation).
func SSMHostInventoryTools() []toolcontract.ToolDefinition {
	return []toolcontract.ToolDefinition{
		{
			Name:        "list_ssm_host_inventory",
			Description: "List admitted SSM host inventory observations with capture status by bounded instance, account, region, and status filters. Returns committed allowlisted application rows and platform observations per EC2 managed host with a read-time status overlay (successful, partial, stale, unknown). Statuses partial/stale/unknown preserve the prior generation's rows; the response never synthesizes vulnerability or supply-chain findings. Unsupported on lightweight local and authoritative runtimes (graph-backed reads).",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"instance_id": map[string]any{
						"type":        "string",
						"description": "Exact EC2 managed node instance id filter.",
					},
					"account_id": map[string]any{
						"type":        "string",
						"description": "Exact AWS account id filter.",
					},
					"region": map[string]any{
						"type":        "string",
						"description": "Exact AWS region filter.",
					},
					"status": map[string]any{
						"type":        "string",
						"description": "Read-time capture status filter: successful, partial, stale, or unknown. Applies after host paging, so a filtered page may be short.",
						"enum":        []string{"successful", "partial", "stale", "unknown"},
					},
					"limit": map[string]any{
						"type":    "integer",
						"default": 10,
						"minimum": 1,
						"maximum": 100,
					},
					"cursor": map[string]any{
						"type":        "string",
						"description": "Continuation cursor: non-negative integer host offset from the previous page's next_cursor",
					},
				},
				"required": []string{},
			},
		},
	}
}
