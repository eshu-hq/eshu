// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

const openAPIPathsSupplyChainContainerImages = `
    "/api/v0/supply-chain/container-images/identities": {
      "get": {
        "summary": "List container image identities",
        "operationId": "listContainerImageIdentities",
        "x-scoped-token-support": true,
        "parameters": [
          {"name": "digest", "in": "query", "schema": {"type": "string"}},
          {"name": "image_ref", "in": "query", "schema": {"type": "string"}},
          {"name": "repository_id", "in": "query", "description": "OCI/image repository identity observed for the container image. This is not a source repository selector.", "schema": {"type": "string"}},
          {"name": "source_repository_id", "in": "query", "description": "Source repository id or selector used to read reducer-owned source-to-image bridge evidence. This does not reinterpret the OCI/image repository_id field.", "schema": {"type": "string"}},
          {"name": "outcome", "in": "query", "schema": {"type": "string", "enum": ["exact_digest", "tag_resolved"]}},
          {"name": "after_identity_id", "in": "query", "schema": {"type": "string"}},
          {"name": "limit", "in": "query", "required": true, "schema": {"type": "integer", "minimum": 1, "maximum": 200}}
        ],
        "responses": {
          "503": {"$ref": "#/components/responses/ServiceUnavailable"},
          "504": {"$ref": "#/components/responses/GatewayTimeout"},
          "200": {
            "description": "Container image identity page",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "identities": {
                      "type": "array",
                      "items": {
                        "type": "object",
                        "properties": {
                          "identity_id": {"type": "string"},
                          "digest": {"type": "string"},
                          "image_ref": {"type": "string"},
                          "repository_id": {"type": "string"},
                          "source_repository_ids": {"type": "array", "items": {"type": "string"}},
                          "source_revision": {"type": "string"},
                          "source_revision_provenance": {"type": "string"},
                          "workload_ids": {"type": "array", "items": {"type": "string"}},
                          "service_ids": {"type": "array", "items": {"type": "string"}},
                          "outcome": {"type": "string"},
                          "reason": {"type": "string"},
                          "identity_strength": {"type": "string"},
                          "source_layers": {"type": "array", "items": {"type": "string"}},
                          "evidence_fact_ids": {"type": "array", "items": {"type": "string"}},
                          "missing_evidence": {"type": "array", "items": {"type": "string"}}
                        }
                      }
                    },
                    "source_bridge": {
                      "type": "object",
                      "properties": {
                        "source_repository_id": {"type": "string"},
                        "image_repository_ids": {"type": "array", "items": {"type": "string"}},
                        "missing_evidence": {"type": "array", "items": {"type": "string"}},
                        "warnings": {"type": "array", "items": {"type": "string"}}
                      }
                    },
                    "count": {"type": "integer"},
                    "limit": {"type": "integer"},
                    "truncated": {"type": "boolean"},
                    "next_cursor": {"type": "object"},
                    "collector_readiness": {"$ref": "#/components/schemas/CollectorReadinessEnvelope"}
                  }
                }
              }
            }
          }
        }
      }
    },
`
