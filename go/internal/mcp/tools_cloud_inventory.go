// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

// cloudInventoryTools returns the canonical multi-cloud resource inventory
// readback tool. It mirrors the GET /api/v0/cloud/inventory route: a bounded,
// paginated, truth-labeled list of reducer-owned reducer_cloud_resource_identity
// rows filterable by provider, canonical scope, and management_origin. The tool
// is read-only and never returns raw provider locators, raw actors, raw
// identities, tags, assignment scopes, or credentials.
func cloudInventoryTools() []ToolDefinition {
	return []ToolDefinition{
		{
			Name:        "list_cloud_resource_inventory",
			Description: "List canonical multi-cloud resource identities (reducer_cloud_resource_identity) by bounded provider, scope, and management_origin filters. Returns provider, normalized identity, management_origin, per-layer evidence flags, provider-neutral source state, optional keyed tag fingerprints, optional bounded identity-policy evidence, and optional sanitized freshness evidence. Unsupported on lightweight local runtime. account_id/project_id/subscription_id are provider-SPECIFIC aliases (account_id->aws, project_id->gcp, subscription_id->azure): all three resolve against one shared canonical key with no per-provider disambiguation, so a numeric value (an AWS account id and a GCP project number can be the identical decimal string) could otherwise match the wrong provider's resource. Each alias REQUIRES its matching provider exactly; omitting provider, or supplying a mismatched provider (e.g. provider=gcp with account_id), is rejected as invalid_argument. When an account_id/project_id/subscription_id-filtered call returns zero resources, check the response's warning_flags array before concluding the account does not exist: account_alias_rollout_gap means a canonical row in the same provider/access scope predates the account_id rollout (it will resolve once that scope's next collector sync re-admits it, so zero rows does NOT prove no such account), and account_alias_rollout_gap_check_failed means that disambiguation check itself could not run. warning_flags is absent when the check ran and found no such gap -- a genuine no-such-account result -- and is never present for a scope_id-filtered or unfiltered call, or any call that already returned resources.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"provider": map[string]any{
						"type":        "string",
						"description": "Cloud provider filter: aws, gcp, or azure. REQUIRED alongside account_id/project_id/subscription_id, and must match the alias used (account_id->aws, project_id->gcp, subscription_id->azure).",
						"enum":        []string{"aws", "gcp", "azure"},
					},
					"scope_id": map[string]any{
						"type":        "string",
						"description": "Exact canonical ingestion scope id filter (one collector partition, e.g. one AWS account+region+service claim -- not the whole account). Takes precedence over account_id/project_id/subscription_id.",
					},
					"account_id": map[string]any{
						"type":        "string",
						"description": "Raw AWS account number. Requires provider=aws exactly (rejected with any other provider value). Matches every canonical resource whose admitting source fact carried this account_id, which can span multiple region/service partitions (scope ids).",
					},
					"project_id": map[string]any{
						"type":        "string",
						"description": "Raw GCP project id. Requires provider=gcp exactly (rejected with any other provider value). Matches every canonical resource whose admitting source fact carried this project_id.",
					},
					"subscription_id": map[string]any{
						"type":        "string",
						"description": "Raw Azure subscription id. Requires provider=azure exactly (rejected with any other provider value). Matches every canonical resource whose admitting source fact carried this subscription_id.",
					},
					"management_origin": map[string]any{
						"type":        "string",
						"description": "Strongest contributing evidence layer: declared, applied, or observed",
						"enum":        []string{"declared", "applied", "observed"},
					},
					"limit": map[string]any{
						"type":    "integer",
						"default": 50,
						"minimum": 1,
						"maximum": 200,
					},
					"cursor": map[string]any{
						"type":        "string",
						"description": "Continuation cursor: non-negative integer offset from the previous page's next_cursor",
					},
				},
				"required": []string{},
			},
		},
	}
}
