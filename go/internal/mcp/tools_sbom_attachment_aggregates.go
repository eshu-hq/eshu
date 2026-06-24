// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

// sbomAttestationAttachmentAggregateTools returns the cheap-summary
// aggregate tools shipped alongside the existing
// list_sbom_attestation_attachments list tool. They give callers an O(1)
// answer to ecosystem-level questions like "how many attestations are
// verified vs unverified?" without paging through the list endpoint.
func sbomAttestationAttachmentAggregateTools() []ToolDefinition {
	return []ToolDefinition{
		{
			Name:        "count_sbom_attestation_attachments",
			Description: "Return reducer-owned SBOM and attestation attachment totals for one optional scope without paging through individual attachments. Provides total attachments and rollups by attachment_status and artifact_kind.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"subject_digest": map[string]any{
						"type":        "string",
						"description": "Optional image digest that anchors the SBOM or attestation attachment.",
					},
					"document_id": map[string]any{
						"type":        "string",
						"description": "Optional SBOM document or attestation statement ID.",
					},
					"document_digest": map[string]any{
						"type":        "string",
						"description": "Optional digest of the SBOM document or attestation statement.",
					},
					"repository_id": map[string]any{
						"type":        "string",
						"description": "Optional canonical source repository id or human repository selector applied before counting.",
					},
					"workload_id": map[string]any{
						"type":        "string",
						"description": "Optional reducer-admitted workload anchor applied before counting.",
					},
					"service_id": map[string]any{
						"type":        "string",
						"description": "Optional reducer-admitted service anchor applied before counting.",
					},
					"attachment_status": map[string]any{
						"type":        "string",
						"description": "Optional reducer attachment status filter applied before counting.",
						"enum":        []string{"attached_verified", "attached_unverified", "attached_parse_only", "subject_mismatch", "ambiguous_subject", "unknown_subject", "unparseable"},
					},
					"artifact_kind": map[string]any{
						"type":        "string",
						"description": "Optional artifact kind filter applied before counting.",
						"enum":        []string{"sbom", "attestation"},
					},
				},
			},
		},
		{
			Name:        "get_sbom_attestation_attachment_inventory",
			Description: "Return a paginated grouped count of reducer-owned SBOM and attestation attachments along one dimension (attachment_status, artifact_kind, subject_digest). Replaces the page-and-iterate caller pattern for ecosystem-level inventory questions.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"group_by": map[string]any{
						"type":        "string",
						"description": "Grouping dimension. attachment_status (default) groups by reducer attachment status; artifact_kind groups by SBOM vs attestation; subject_digest groups by image subject.",
						"enum":        []string{"attachment_status", "artifact_kind", "subject_digest"},
						"default":     "attachment_status",
					},
					"subject_digest":  map[string]any{"type": "string", "description": "Optional image digest to scope the inventory."},
					"document_id":     map[string]any{"type": "string", "description": "Optional SBOM document ID."},
					"document_digest": map[string]any{"type": "string", "description": "Optional document digest."},
					"repository_id":   map[string]any{"type": "string", "description": "Optional canonical source repository id or human repository selector applied before grouping."},
					"workload_id":     map[string]any{"type": "string", "description": "Optional reducer-admitted workload anchor applied before grouping."},
					"service_id":      map[string]any{"type": "string", "description": "Optional reducer-admitted service anchor applied before grouping."},
					"attachment_status": map[string]any{
						"type":        "string",
						"description": "Optional reducer attachment status filter applied before grouping.",
						"enum":        []string{"attached_verified", "attached_unverified", "attached_parse_only", "subject_mismatch", "ambiguous_subject", "unknown_subject", "unparseable"},
					},
					"artifact_kind": map[string]any{
						"type":        "string",
						"description": "Optional artifact kind filter applied before grouping.",
						"enum":        []string{"sbom", "attestation"},
					},
					"limit": map[string]any{
						"type":        "integer",
						"description": "Maximum buckets to return per page.",
						"default":     100,
						"minimum":     1,
						"maximum":     500,
					},
					"offset": map[string]any{
						"type":        "integer",
						"description": "Zero-based result offset for paging.",
						"default":     0,
						"minimum":     0,
						"maximum":     10000,
					},
				},
			},
		},
	}
}
