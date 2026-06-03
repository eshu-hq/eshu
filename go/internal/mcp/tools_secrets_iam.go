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
	}
}
