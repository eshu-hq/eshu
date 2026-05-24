package mcp

func supplyChainTools() []ToolDefinition {
	return []ToolDefinition{
		{
			Name:        "list_container_image_identities",
			Description: "List reducer-owned container image identity facts by digest, image reference, repository, or outcome.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"digest": map[string]any{
						"type":        "string",
						"description": "Image digest such as sha256:...",
					},
					"image_ref": map[string]any{
						"type":        "string",
						"description": "Original image reference observed in source or runtime evidence.",
					},
					"repository_id": map[string]any{
						"type":        "string",
						"description": "OCI repository identity such as oci-registry://registry.example/team/api.",
					},
					"outcome": map[string]any{
						"type":        "string",
						"description": "Optional reducer identity outcome filter.",
						"enum":        []string{"exact_digest", "tag_resolved"},
					},
					"after_identity_id": map[string]any{
						"type":        "string",
						"description": "Identity ID from next_cursor when continuing a truncated page.",
					},
					"limit": map[string]any{
						"type":        "integer",
						"description": "Maximum identity rows to return.",
						"default":     50,
						"minimum":     1,
						"maximum":     200,
					},
				},
			},
		},
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
			Name:        "explain_supply_chain_impact",
			Description: "Explain one reducer-owned vulnerability finding or bounded advisory/package/repository path with evidence, anchors, remediation, freshness, and missing-evidence reasons.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"finding_id": map[string]any{
						"type":        "string",
						"description": "Exact reducer-owned finding id. Preferred when known.",
					},
					"advisory_id": map[string]any{
						"type":        "string",
						"description": "Advisory identifier such as GHSA, OSV, GLAD, vendor advisory, or CVE id.",
					},
					"cve_id": map[string]any{
						"type":        "string",
						"description": "CVE identifier when advisory_id is not the canonical CVE field.",
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
