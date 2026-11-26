// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package chain

// ContainerImageIdentityAggregate is the OpenAPI path fragment documenting the
// `/api/v0/supply-chain/container-images/identities/count`,
// `/api/v0/supply-chain/container-images/identities/inventory` routes.
// openapi.Spec concatenates it into the published document; keep it in
// lockstep with the handlers and docs/public/reference/http-api.md.
const ContainerImageIdentityAggregate = `
    "/api/v0/supply-chain/container-images/identities/count": {
      "get": {
        "summary": "Count container image identities without paging the list endpoint",
        "operationId": "countContainerImageIdentities",
        "x-scoped-token-support": true,
        "parameters": [
          {"name": "digest", "in": "query", "schema": {"type": "string"}},
          {"name": "image_ref", "in": "query", "schema": {"type": "string"}},
          {"name": "source_repository_id", "in": "query", "description": "Source repository id or selector used to count source-to-image bridge evidence. This is not an OCI/image repository id.", "schema": {"type": "string"}},
          {"name": "repository_id", "in": "query", "description": "OCI/image repository identity. This is not a source repository selector.", "schema": {"type": "string"}},
          {"name": "outcome", "in": "query", "schema": {"type": "string", "enum": ["exact_digest", "tag_resolved"]}}
        ],
        "responses": {
          "403": {"$ref": "#/components/responses/Forbidden"},
          "503": {"$ref": "#/components/responses/ServiceUnavailable"},
          "504": {"$ref": "#/components/responses/GatewayTimeout"},
          "200": {
            "description": "Reducer-owned container image identity totals envelope",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "total_identities": {"type": "integer", "description": "Reducer-owned container image identities matching the filters."},
                    "by_outcome": {"type": "object", "additionalProperties": {"type": "integer"}, "description": "Identity counts by outcome (exact_digest, tag_resolved). Bucket names are outcome values; render as bars, never a fixed enum."},
                    "by_identity_strength": {"type": "object", "additionalProperties": {"type": "integer"}, "description": "Identity counts by identity strength. Bucket names are strength values; render as bars, never a fixed enum."},
                    "source_bridge": {"type": "object", "description": "Source-repository-scoped identity evidence; present only when the source_repository_id filter was given. Owner: supply/chain ContainerImageIdentitySourceBridge.", "properties": {
                      "source_repository_id": {"type": "string"},
                      "image_repository_ids": {"type": "array", "items": {"type": "string"}, "description": "Absent when empty."},
                      "missing_evidence": {"type": "array", "items": {"type": "string"}, "description": "Absent when empty."},
                      "warnings": {"type": "array", "items": {"type": "string"}, "description": "Absent when empty."}
                    }},
                    "scope": {"type": "object", "additionalProperties": {"type": "string"}, "description": "Echo of the filters applied to this aggregate, as string pairs (only non-empty filters appear). Render as filter chips, never a fixed form."}
                  }
                }
              }
            }
          }
        }
      }
    },
    "/api/v0/supply-chain/container-images/identities/inventory": {
      "get": {
        "summary": "Group container image identities by one dimension without paging the list endpoint",
        "operationId": "getContainerImageIdentityInventory",
        "x-scoped-token-support": true,
        "parameters": [
          {"name": "group_by", "in": "query", "schema": {"type": "string", "enum": ["outcome", "identity_strength", "repository_id"], "default": "outcome"}},
          {"name": "digest", "in": "query", "schema": {"type": "string"}},
          {"name": "image_ref", "in": "query", "schema": {"type": "string"}},
          {"name": "source_repository_id", "in": "query", "description": "Source repository id or selector used to group source-to-image bridge evidence. This is not an OCI/image repository id.", "schema": {"type": "string"}},
          {"name": "repository_id", "in": "query", "description": "OCI/image repository identity. This is not a source repository selector.", "schema": {"type": "string"}},
          {"name": "outcome", "in": "query", "schema": {"type": "string", "enum": ["exact_digest", "tag_resolved"]}},
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
                      "items": {
                        "type": "object",
                        "properties": {
                          "dimension": {"type": "string", "description": "Bucket dimension value; the label comes from the group_by request value."},
                          "value": {"type": "string", "description": "Group value."},
                          "count": {"type": "integer", "description": "Identities in this bucket."}
                        }
                      }
                    },
                    "count": {"type": "integer", "description": "Buckets in this page."},
                    "limit": {"type": "integer", "description": "Echo of the requested page bound."},
                    "offset": {"type": "integer", "description": "Echo of the requested page offset."},
                    "group_by": {"type": "string", "description": "Echo of the requested grouping dimension."},
                    "truncated": {"type": "boolean", "description": "True when more buckets matched than this page carries; continue with next_offset."},
                    "next_offset": {"type": ["integer", "null"], "description": "Next offset to request when truncated is true; null when the page is complete or when the next offset would exceed the documented maximum (10000)."},
                    "scope": {"type": "object", "additionalProperties": {"type": "string"}, "description": "Echo of the filters applied to this aggregate, as string pairs (only non-empty filters appear). Render as filter chips, never a fixed form."}
                  }
                }
              }
            }
          }
        }
      }
    },
`
