package mcp

func supplyChainTools() []ToolDefinition {
	return []ToolDefinition{
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
