package mcp

func supplyChainTools() []ToolDefinition {
	return []ToolDefinition{
		{
			Name:        "list_supply_chain_impact_findings",
			Description: "List reducer-owned vulnerability impact findings by CVE, package, repository, image digest, or impact status.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"cve_id": map[string]any{
						"type":        "string",
						"description": "CVE or advisory identifier to inspect.",
					},
					"package_id": map[string]any{
						"type":        "string",
						"description": "Normalized package identity such as pkg:npm/example.",
					},
					"repository_id": map[string]any{
						"type":        "string",
						"description": "Repository identifier from package consumption evidence.",
					},
					"subject_digest": map[string]any{
						"type":        "string",
						"description": "Image or artifact digest from SBOM/runtime evidence.",
					},
					"impact_status": map[string]any{
						"type":        "string",
						"description": "Optional reducer impact status filter.",
						"enum":        []string{"affected_exact", "affected_derived", "possibly_affected", "not_affected_known_fixed", "unknown_impact"},
					},
					"after_finding_id": map[string]any{
						"type":        "string",
						"description": "Finding ID from next_cursor when continuing a truncated page.",
					},
					"limit": map[string]any{
						"type":        "integer",
						"description": "Maximum impact rows to return.",
						"default":     50,
						"minimum":     1,
						"maximum":     200,
					},
				},
			},
		},
		{
			Name:        "list_sbom_attestation_attachments",
			Description: "List reducer-owned SBOM and attestation attachment evidence by image digest or document identity.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"subject_digest": map[string]any{
						"type":        "string",
						"description": "Image or artifact digest that anchors the SBOM or attestation attachment.",
					},
					"document_id": map[string]any{
						"type":        "string",
						"description": "SBOM document or attestation statement ID for exact document lookup.",
					},
					"document_digest": map[string]any{
						"type":        "string",
						"description": "Digest of the SBOM document or attestation statement.",
					},
					"attachment_status": map[string]any{
						"type":        "string",
						"description": "Optional reducer attachment status filter.",
						"enum":        []string{"attached_verified", "attached_unverified", "attached_parse_only", "subject_mismatch", "ambiguous_subject", "unknown_subject", "unparseable"},
					},
					"artifact_kind": map[string]any{
						"type":        "string",
						"description": "Optional artifact kind filter.",
						"enum":        []string{"sbom", "attestation"},
					},
					"after_attachment_id": map[string]any{
						"type":        "string",
						"description": "Attachment ID from next_cursor when continuing a truncated page.",
					},
					"limit": map[string]any{
						"type":        "integer",
						"description": "Maximum attachment rows to return.",
						"default":     50,
						"minimum":     1,
						"maximum":     200,
					},
				},
			},
		},
	}
}
