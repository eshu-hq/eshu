// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package status

// FactSchemaVersion is the OpenAPI path fragment documenting the
// `/api/v0/fact-schema-versions`, `/api/v0/fact-schema-versions/{fact_kind}`
// routes. openapi.Spec concatenates it into the published document; keep it in
// lockstep with the handlers and docs/public/reference/http-api.md.
const FactSchemaVersion = `
    "/api/v0/fact-schema-versions": {
      "get": {
        "tags": ["status"],
        "summary": "List core fact-kind schema versions",
        "description": "Returns the core fact-kind to supported schema-version registry: the schema version a core reducer or query consumer currently supports for each core fact kind. The data is the static in-binary registry from go/internal/facts; it reads no runtime, graph, or registry state.",
        "operationId": "listFactSchemaVersions",
        "x-scoped-token-support": true,
        "parameters": [
          {
            "name": "limit",
            "in": "query",
            "schema": {"type": "integer", "minimum": 1, "maximum": 500, "default": 200},
            "description": "Maximum number of fact-kind rows to return."
          }
        ],
        "responses": {
          "200": {
            "description": "Core fact-kind schema-version registry",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "schema_version": {"type": "string", "description": "Registry schema version served by the fact-schema-version handler."},
                    "status": {"type": "string", "enum": ["available"], "description": "Registry availability; the registry is static so it always reads available."},
                    "fact_schema_versions": {
                      "type": "array",
                      "description": "One entry per core fact kind in the in-binary registry from go/internal/facts.",
                      "items": {
                        "type": "object",
                        "properties": {
                          "fact_kind": {"type": "string", "description": "Core fact kind, for example terraform_state_resource."},
                          "schema_version": {"type": "string", "description": "Schema version the current binary supports for the fact kind."}
                        }
                      }
                    },
                    "count": {"type": "integer", "description": "Fact-kind rows in this page."},
                    "total_count": {"type": "integer", "description": "Fact-kind rows in the registry."},
                    "limit": {"type": "integer", "description": "Applied page limit."},
                    "truncated": {"type": "boolean", "description": "True when rows were left out by the limit."}
                  }
                }
              }
            }
          },
          "400": {"$ref": "#/components/responses/BadRequest"}
        }
      }
    },
    "/api/v0/fact-schema-versions/{fact_kind}": {
      "get": {
        "tags": ["status"],
        "summary": "Get the schema version for one core fact kind and classify a candidate",
        "description": "Returns the supported schema version for one core fact kind. When the candidate query parameter is supplied, classifies that collector version as supported, unsupported_major, unsupported_minor, or unknown_kind, so a client can detect an incompatible collector fact version safely. Returns not_found when the fact kind is not core-owned.",
        "operationId": "getFactSchemaVersion",
        "x-scoped-token-support": true,
        "parameters": [
          {
            "name": "fact_kind",
            "in": "path",
            "required": true,
            "schema": {"type": "string"},
            "description": "Core fact kind, such as terraform_state_resource or documentation_section."
          },
          {
            "name": "candidate",
            "in": "query",
            "required": false,
            "schema": {"type": "string"},
            "description": "Optional candidate schema version to classify against the supported version."
          }
        ],
        "responses": {
          "200": {
            "description": "Fact-kind schema version and optional compatibility classification",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "schema_version": {"type": "string", "description": "Registry schema version served by the fact-schema-version handler."},
                    "status": {"type": "string", "enum": ["available"], "description": "Registry availability; the registry is static so it always reads available."},
                    "fact_kind": {"type": "string", "description": "Core fact kind from the path parameter."},
                    "supported_version": {"type": "string", "description": "Supported schema version for the fact kind from the in-binary registry."},
                    "candidate": {"type": "string", "description": "Candidate version echoed back; empty when no candidate was supplied."},
                    "compatibility": {"type": "string", "enum": ["supported", "unsupported_major", "unsupported_minor", "unknown_kind"], "description": "Classification of the candidate against the supported version."}
                  }
                }
              }
            }
          },
          "404": {"$ref": "#/components/responses/NotFound"}
        }
      }
    },
`
