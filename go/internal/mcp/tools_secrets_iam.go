// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

// secretsIAMTools returns read-only MCP tools over reducer-owned secrets/IAM
// trust-chain facts (issue #25). The tools are bounded, scoped, and
// provenance-only; they never promote graph edges and never expose secret
// values, raw paths, or token claims.
func secretsIAMTools() []ToolDefinition {
	return []ToolDefinition{
		{
			Name:        "list_secrets_iam_identity_trust_chains",
			Description: "List reducer-owned secrets/IAM identity trust chains (workload to ServiceAccount to IAM role to Vault policy) by scope, chain, workload object, service account join key, or IAM role fingerprint. State is one of exact, partial, unresolved, stale, permission_hidden, or unsupported; only exact chains have every hop resolved with explicit evidence.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"scope_id": map[string]any{
						"type":        "string",
						"description": "Optional reducer scope ID for a secrets/IAM trust-chain generation.",
					},
					"chain_id": map[string]any{
						"type":        "string",
						"description": "Identity trust-chain ID (the chain_id returned as a row identifier and next_cursor.after_chain_id) to anchor lookup.",
					},
					"workload_object_id": map[string]any{
						"type":        "string",
						"description": "Durable workload object ID to anchor lookup.",
					},
					"service_account_join_key": map[string]any{
						"type":        "string",
						"description": "ServiceAccount join-key fingerprint to anchor lookup.",
					},
					"iam_role_fingerprint": map[string]any{
						"type":        "string",
						"description": "IAM role fingerprint to anchor lookup.",
					},
					"state": map[string]any{
						"type":        "string",
						"description": "Optional trust-chain state filter.",
						"enum":        []string{"exact", "partial", "unresolved", "stale", "permission_hidden", "unsupported"},
					},
					"after_chain_id": map[string]any{
						"type":        "string",
						"description": "Pagination cursor from a previous truncated page (next_cursor.after_chain_id).",
					},
					"limit": map[string]any{
						"type":        "integer",
						"description": "Maximum rows to return (1-200).",
						"minimum":     1,
						"maximum":     200,
					},
				},
			},
		},
		{
			Name:        "list_secrets_iam_privilege_posture_observations",
			Description: "List reducer-owned secrets/IAM privilege posture observations: risky broad or partial posture evidence (for example a role with external trust and no sts:ExternalId) that the reducer keeps provenance-only and never promotes to an exact path. Filter by scope, observation, risk type, severity, or state.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"scope_id":             map[string]any{"type": "string", "description": "Reducer scope ID (required unless observation_id is given)."},
					"observation_id":       map[string]any{"type": "string", "description": "Observation ID to anchor lookup."},
					"risk_type":            map[string]any{"type": "string", "description": "Optional risk type filter (for example external_trust_without_external_id, wildcard_principal, wildcard_action)."},
					"severity":             map[string]any{"type": "string", "description": "Optional severity filter."},
					"state":                map[string]any{"type": "string", "enum": []string{"exact", "partial", "unresolved", "stale", "permission_hidden", "unsupported"}},
					"after_observation_id": map[string]any{"type": "string", "description": "Pagination cursor from a previous truncated page."},
					"limit":                map[string]any{"type": "integer", "description": "Maximum rows to return (1-200).", "minimum": 1, "maximum": 200},
				},
			},
		},
		{
			Name:        "list_secrets_iam_secret_access_paths",
			Description: "List reducer-owned secrets/IAM secret access paths: Vault policy-to-KV metadata paths reported only as reachable from an exact identity chain, as fingerprints and capabilities, never as secret values. Filter by scope, path, parent chain, vault mount join key, or state.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"scope_id":             map[string]any{"type": "string", "description": "Reducer scope ID (required unless path_id, chain_id, or vault_mount_join_key is given)."},
					"path_id":              map[string]any{"type": "string", "description": "Secret access path ID to anchor lookup."},
					"chain_id":             map[string]any{"type": "string", "description": "Parent identity trust-chain ID to anchor lookup."},
					"vault_mount_join_key": map[string]any{"type": "string", "description": "Vault mount join-key fingerprint to anchor lookup."},
					"state":                map[string]any{"type": "string", "enum": []string{"exact", "partial", "unresolved", "stale", "permission_hidden", "unsupported"}},
					"after_path_id":        map[string]any{"type": "string", "description": "Pagination cursor from a previous truncated page."},
					"limit":                map[string]any{"type": "integer", "description": "Maximum rows to return (1-200).", "minimum": 1, "maximum": 200},
				},
			},
		},
		{
			Name:        "list_secrets_iam_posture_gaps",
			Description: "List reducer-owned secrets/IAM posture gaps: missing, stale, permission_hidden, or unsupported evidence that blocks exact trust-chain truth, surfaced rather than silently dropped. Filter by scope, gap, gap type, service account join key, or state.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"scope_id":                 map[string]any{"type": "string", "description": "Reducer scope ID (required unless gap_id or service_account_join_key is given)."},
					"gap_id":                   map[string]any{"type": "string", "description": "Posture gap ID to anchor lookup."},
					"gap_type":                 map[string]any{"type": "string", "description": "Optional gap type filter (for example missing_evidence, stale_generation, permission_hidden, unsupported_layer)."},
					"service_account_join_key": map[string]any{"type": "string", "description": "ServiceAccount join-key fingerprint to anchor lookup."},
					"state":                    map[string]any{"type": "string", "enum": []string{"exact", "partial", "unresolved", "stale", "permission_hidden", "unsupported"}},
					"after_gap_id":             map[string]any{"type": "string", "description": "Pagination cursor from a previous truncated page."},
					"limit":                    map[string]any{"type": "integer", "description": "Maximum rows to return (1-200).", "minimum": 1, "maximum": 200},
				},
			},
		},
		{
			Name:        "count_secrets_iam_posture",
			Description: "Summarize reducer-owned secrets/IAM posture for one scope as provenance-only grouped counts: identity trust chains by state, privilege posture observations by risk type and severity, secret access paths by state, and posture gaps by gap type, plus S3 external-principal grant posture (total, by grant outcome, by resolution mode, and public/cross-account/service-principal tallies) read from the canonical GRANTS_ACCESS_TO graph edges. Returns counts only — no fingerprints, principal identities, paths, or evidence. Requires scope_id.",
			InputSchema: map[string]any{
				"type":     "object",
				"required": []string{"scope_id"},
				"properties": map[string]any{
					"scope_id": map[string]any{"type": "string", "description": "Reducer scope ID to summarize."},
				},
			},
		},
	}
}
