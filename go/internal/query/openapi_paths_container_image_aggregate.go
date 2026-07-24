// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

const openAPIPathsContainerImageIdentityAggregate = `
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
          "503": {"$ref": "#/components/responses/ServiceUnavailable"},
          "504": {"$ref": "#/components/responses/GatewayTimeout"},
          "200": {
            "description": "Reducer-owned container image identity totals envelope",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "total_identities": {"type": "integer"},
                    "by_outcome": {"type": "object", "additionalProperties": {"type": "integer"}},
                    "by_identity_strength": {"type": "object", "additionalProperties": {"type": "integer"}},
                    "source_bridge": {"type": "object", "description": "Source-scoped bridge diagnostics, including missing_evidence when no source-to-image proof is present."},
                    "scope": {"type": "object"}
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
