// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

func investigationPacketTools() []ToolDefinition {
	return []ToolDefinition{
		{
			Name:        "export_supply_chain_impact_packet",
			Description: "Export an investigation_evidence_packet.v2 artifact for one bounded supply-chain impact investigation. The packet is composed by the shared evidence-packet builder used by API and CLI surfaces, preserving truth labels, missing evidence, refusal state, reproduce handles, and source-fact bounds.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": investigationPacketProperties(map[string]any{
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
						"description": "Repository identifier or selector from package consumption evidence.",
					},
					"subject_digest": map[string]any{
						"type":        "string",
						"description": "Image or artifact digest from SBOM/runtime evidence.",
					},
					"image_ref": map[string]any{
						"type":        "string",
						"description": "Exact image reference stored on reducer-owned impact findings.",
					},
					"workload_id": map[string]any{
						"type":        "string",
						"description": "Reducer-admitted workload anchor.",
					},
					"service_id": map[string]any{
						"type":        "string",
						"description": "Reducer-admitted service anchor derived from workload/service evidence.",
					},
				}),
			},
		},
		{
			Name:        "export_deployable_unit_packet",
			Description: "Export an investigation_evidence_packet.v2 artifact for deployable-unit admission truth. The packet carries reducer admission decisions, graph/query answers, missing evidence, reproduce handles, and refusal state without synthesizing unproven deployment correlation.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": investigationPacketProperties(map[string]any{
					"scope_id": map[string]any{
						"type":        "string",
						"description": "Ingestion scope id that bounds the admission-decision read.",
					},
					"generation_id": map[string]any{
						"type":        "string",
						"description": "Scope generation id that bounds the admission-decision read.",
					},
					"repository_id": map[string]any{
						"type":        "string",
						"description": "Optional repository anchor used to narrow deployable-unit decisions.",
					},
					"repo_id": map[string]any{
						"type":        "string",
						"description": "Alias for repository_id.",
					},
				}),
				"required": []string{"scope_id", "generation_id"},
			},
		},
		{
			Name:        "export_cloud_runtime_drift_packet",
			Description: "Export an investigation_evidence_packet.v2 artifact for bounded provider-neutral runtime drift findings. The packet preserves source-state, safety/refusal posture, missing evidence, and reproduce handles for a canonical cloud scope.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": investigationPacketProperties(map[string]any{
					"scope_id": map[string]any{
						"type":        "string",
						"description": "Canonical ingestion scope id. Required unless an account/project/subscription alias is set.",
					},
					"account_id": map[string]any{
						"type":        "string",
						"description": "Alias for scope_id for AWS account scope.",
					},
					"project_id": map[string]any{
						"type":        "string",
						"description": "Alias for scope_id for GCP project scope.",
					},
					"subscription_id": map[string]any{
						"type":        "string",
						"description": "Alias for scope_id for Azure subscription scope.",
					},
					"provider": map[string]any{
						"type":        "string",
						"description": "Cloud provider filter: aws, gcp, or azure.",
						"enum":        []string{"aws", "gcp", "azure"},
					},
					"cloud_resource_uid": map[string]any{
						"type":        "string",
						"description": "Optional exact canonical resource uid to inspect.",
					},
				}),
			},
		},
	}
}

func investigationPacketProperties(properties map[string]any) map[string]any {
	properties["max_source_facts"] = map[string]any{
		"type":        "integer",
		"description": "Optional lower cap for the packet source_facts layer.",
		"minimum":     1,
	}
	return properties
}
