// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package chain

// SBOMAttestationAttachmentAggregate is the OpenAPI path fragment documenting
// the `/api/v0/supply-chain/sbom-attestations/attachments/count`,
// `/api/v0/supply-chain/sbom-attestations/attachments/inventory` routes.
// openapi.Spec concatenates it into the published document; keep it in
// lockstep with the handlers and docs/public/reference/http-api.md.
const SBOMAttestationAttachmentAggregate = `
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
          "403": {"$ref": "#/components/responses/Forbidden"},
          "503": {"$ref": "#/components/responses/ServiceUnavailable"},
          "504": {"$ref": "#/components/responses/GatewayTimeout"},
          "200": {
            "description": "Reducer-owned SBOM and attestation attachment totals envelope",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "total_attachments": {"type": "integer", "description": "Reducer-owned SBOM and attestation attachments matching the filters, emitted by the attachment count aggregate handler."},
                    "by_attachment_status": {"type": "object", "additionalProperties": {"type": "integer"}, "description": "Attachment counts by attachment status (attached_verified, attached_unverified, attached_parse_only, subject_mismatch, ambiguous_subject, unknown_subject, unparseable), emitted by the attachment count aggregate handler. Bucket names are status values; render as bars, never a fixed enum."},
                    "by_artifact_kind": {"type": "object", "additionalProperties": {"type": "integer"}, "description": "Attachment counts by artifact kind (sbom, attestation), emitted by the attachment count aggregate handler. Bucket names are kind values; render as bars, never a fixed enum."},
                    "missing_evidence": {"type": "array", "items": {"type": "string"}, "description": "Source-scope gap classes such as repository_to_image_evidence_missing or image_to_sbom_evidence_missing, emitted by the attachment count aggregate handler."},
                    "scope": {"type": "object", "additionalProperties": {"type": "string"}, "description": "Echo of the filters applied to this aggregate, as string pairs (only non-empty filters appear), emitted by the attachment aggregate scope helper. Render as filter chips, never a fixed form."}
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
          "403": {"$ref": "#/components/responses/Forbidden"},
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
                      "description": "Grouped count buckets ordered by count desc, emitted by the SBOM attestation attachment inventory aggregate handler (SBOMAttestationAttachmentInventoryRow).",
                      "items": {
                        "type": "object",
                        "properties": {
                          "dimension": {"type": "string", "description": "Bucket dimension value emitted by the attachment inventory aggregate handler (SBOMAttestationAttachmentInventoryRow.Dimension); the label comes from the group_by request value."},
                          "value": {"type": "string", "description": "Group value emitted by the attachment inventory aggregate handler (SBOMAttestationAttachmentInventoryRow.Value)."},
                          "count": {"type": "integer", "description": "Attachments in this bucket, emitted by the attachment inventory aggregate handler (SBOMAttestationAttachmentInventoryRow.Count)."}
                        }
                      }
                    },
                    "count": {"type": "integer", "description": "Buckets in this page, emitted by the attachment inventory aggregate handler."},
                    "limit": {"type": "integer", "description": "Echo of the requested page bound, emitted by the attachment inventory aggregate handler."},
                    "offset": {"type": "integer", "description": "Echo of the requested page offset, emitted by the attachment inventory aggregate handler."},
                    "group_by": {"type": "string", "description": "Echo of the requested grouping dimension, emitted by the attachment inventory aggregate handler."},
                    "truncated": {"type": "boolean", "description": "True when more buckets matched than this page carries; continue with next_offset, emitted by the attachment inventory aggregate handler."},
                    "next_offset": {"type": ["integer", "null"], "description": "Next offset to request when truncated is true, emitted by the attachment inventory aggregate handler; null when the page is complete or when the next offset would exceed the documented maximum (10000)."},
                    "scope": {"type": "object", "additionalProperties": {"type": "string"}, "description": "Echo of the filters applied to this aggregate, as string pairs (only non-empty filters appear), emitted by the attachment aggregate scope helper. Render as filter chips, never a fixed form."}
                  }
                }
              }
            }
          }
        }
      }
    },
`
