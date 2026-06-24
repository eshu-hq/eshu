// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

const openAPIPathsFactSchemaVersion = `
    "/api/v0/fact-schema-versions": {
      "get": {
        "tags": ["status"],
        "summary": "List core fact-kind schema versions",
        "description": "Returns the core fact-kind to supported schema-version registry: the schema version a core reducer or query consumer currently supports for each core fact kind. The data is the static in-binary registry from go/internal/facts; it reads no runtime, graph, or registry state.",
        "operationId": "listFactSchemaVersions",
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
                    "schema_version": {"type": "string"},
                    "status": {"type": "string", "enum": ["available"]},
                    "fact_schema_versions": {
                      "type": "array",
                      "items": {
                        "type": "object",
                        "properties": {
                          "fact_kind": {"type": "string"},
                          "schema_version": {"type": "string"}
                        }
                      }
                    },
                    "count": {"type": "integer"},
                    "total_count": {"type": "integer"},
                    "limit": {"type": "integer"},
                    "truncated": {"type": "boolean"}
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
                    "schema_version": {"type": "string"},
                    "status": {"type": "string", "enum": ["available"]},
                    "fact_kind": {"type": "string"},
                    "supported_version": {"type": "string"},
                    "candidate": {"type": "string"},
                    "compatibility": {"type": "string", "enum": ["supported", "unsupported_major", "unsupported_minor", "unknown_kind"]}
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
