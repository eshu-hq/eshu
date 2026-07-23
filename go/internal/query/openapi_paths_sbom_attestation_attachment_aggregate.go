// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

const openAPIPathsSBOMAttestationAttachmentAggregate = `
    "/api/v0/supply-chain/sbom-attestations/attachments/count": {
      "get": {
        "summary": "Count SBOM and attestation attachments without paging the list endpoint",
        "operationId": "countSBOMAttestationAttachments",
        "x-scoped-token-support": true,
        "parameters": [
          {"name": "subject_digest", "in": "query", "schema": {"type": "string"}},
          {"name": "document_id", "in": "query", "schema": {"type": "string"}},
          {"name": "document_digest", "in": "query", "schema": {"type": "string"}},
          {"name": "repository_id", "in": "query", "description": "Canonical source repository id or human repository selector (name, repo slug, indexed path, local path, or remote URL). Unknown or ambiguous selectors return a selector error before the bounded aggregate read.", "schema": {"type": "string"}},
          {"name": "workload_id", "in": "query", "schema": {"type": "string"}},
          {"name": "service_id", "in": "query", "schema": {"type": "string"}},
          {"name": "attachment_status", "in": "query", "schema": {"type": "string", "enum": ["attached_verified", "attached_unverified", "attached_parse_only", "subject_mismatch", "ambiguous_subject", "unknown_subject", "unparseable"]}},
          {"name": "artifact_kind", "in": "query", "schema": {"type": "string", "enum": ["sbom", "attestation"]}}
        ],
        "responses": {
          "503": {"$ref": "#/components/responses/ServiceUnavailable"},
          "504": {"$ref": "#/components/responses/GatewayTimeout"},
          "200": {
            "description": "Reducer-owned SBOM and attestation attachment totals envelope",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "total_attachments": {"type": "integer"},
                    "by_attachment_status": {"type": "object", "additionalProperties": {"type": "integer"}},
                    "by_artifact_kind": {"type": "object", "additionalProperties": {"type": "integer"}},
                    "missing_evidence": {"type": "array", "items": {"type": "string"}, "description": "Source-scope gap classes such as repository_to_image_evidence_missing or image_to_sbom_evidence_missing."},
                    "scope": {"type": "object"}
                  }
                }
              }
            }
          }
        }
      }
    },
    "/api/v0/supply-chain/sbom-attestations/attachments/inventory": {
      "get": {
        "summary": "Group SBOM and attestation attachments by one dimension without paging the list endpoint",
        "operationId": "getSBOMAttestationAttachmentInventory",
        "x-scoped-token-support": true,
        "parameters": [
          {"name": "group_by", "in": "query", "schema": {"type": "string", "enum": ["attachment_status", "artifact_kind", "subject_digest"], "default": "attachment_status"}},
          {"name": "subject_digest", "in": "query", "schema": {"type": "string"}},
          {"name": "document_id", "in": "query", "schema": {"type": "string"}},
          {"name": "document_digest", "in": "query", "schema": {"type": "string"}},
          {"name": "repository_id", "in": "query", "description": "Canonical source repository id or human repository selector (name, repo slug, indexed path, local path, or remote URL). Unknown or ambiguous selectors return a selector error before the bounded inventory read.", "schema": {"type": "string"}},
          {"name": "workload_id", "in": "query", "schema": {"type": "string"}},
          {"name": "service_id", "in": "query", "schema": {"type": "string"}},
          {"name": "attachment_status", "in": "query", "schema": {"type": "string", "enum": ["attached_verified", "attached_unverified", "attached_parse_only", "subject_mismatch", "ambiguous_subject", "unknown_subject", "unparseable"]}},
          {"name": "artifact_kind", "in": "query", "schema": {"type": "string", "enum": ["sbom", "attestation"]}},
          {"name": "limit", "in": "query", "schema": {"type": "integer", "minimum": 1, "maximum": 500, "default": 100}},
          {"name": "offset", "in": "query", "schema": {"type": "integer", "minimum": 0, "maximum": 10000, "default": 0}}
        ],
        "responses": {
          "503": {"$ref": "#/components/responses/ServiceUnavailable"},
          "504": {"$ref": "#/components/responses/GatewayTimeout"},
          "200": {
            "description": "Grouped count buckets ordered by count desc",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "buckets": {
                      "type": "array",
                      "items": {
                        "type": "object",
                        "properties": {
                          "dimension": {"type": "string"},
                          "value": {"type": "string"},
                          "count": {"type": "integer"}
                        }
                      }
                    },
                    "count": {"type": "integer"},
                    "limit": {"type": "integer"},
                    "offset": {"type": "integer"},
                    "group_by": {"type": "string"},
                    "truncated": {"type": "boolean"},
                    "next_offset": {"type": ["integer", "null"], "description": "Next offset to request when truncated is true; null when the page is complete or when the next offset would exceed the documented maximum (10000)."},
                    "scope": {"type": "object"}
                  }
                }
              }
            }
          }
        }
      }
    },
`
