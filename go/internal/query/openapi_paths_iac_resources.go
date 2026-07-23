// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

// openAPIPathsIaCResources documents the bounded Terraform/IaC resource list
// browse read. It is concatenated into the public OpenAPI spec in openapi.go
// and must stay in lockstep with the iac_resources.go handler and
// docs/public/reference/http-api/iac-content-infra.md.
const openAPIPathsIaCResources = `
    "/api/v0/iac/resources": {
      "get": {
        "tags": ["iac"],
        "summary": "List Terraform/IaC resources",
        "description": "Bounded, enveloped browse over the current active-generation Terraform/IaC inventory, hydrated from the authoritative graph. Defaults to Terraform resources; set kind to list modules or data sources. Search and filters execute server-side across the full caller-authorized current inventory. The list is keyset-paginated by (name, id); follow next_cursor.after_name and next_cursor.after_id to page. Requires the local-authoritative profile or higher.",
        "operationId": "listIaCResources",
        "x-scoped-token-support": true,
        "parameters": [
          {
            "name": "q",
            "in": "query",
            "required": false,
            "description": "Case-insensitive full-inventory search across name, source path, type, provider, module, repository id, and kind.",
            "schema": {"type": "string"}
          },
          {
            "name": "kind",
            "in": "query",
            "required": false,
            "description": "IaC node kind to list: resource (default), module, or data-source.",
            "schema": {"type": "string", "enum": ["resource", "module", "data-source"], "default": "resource"}
          },
          {
            "name": "type",
            "in": "query",
            "required": false,
            "description": "Filter by Terraform resource type (e.g. aws_iam_role) or, for data sources, the data type.",
            "schema": {"type": "string"}
          },
          {
            "name": "provider",
            "in": "query",
            "required": false,
            "description": "Filter by provider (e.g. aws). Present only on canonical-sourced nodes.",
            "schema": {"type": "string"}
          },
          {
            "name": "module",
            "in": "query",
            "required": false,
            "description": "Filter by module name. For resources and data sources this matches the module.\"<name>\". address prefix; for modules it matches the module name exactly.",
            "schema": {"type": "string"}
          },
          {
            "name": "repository",
            "in": "query",
            "required": false,
            "description": "Filter by canonical repository id.",
            "schema": {"type": "string"}
          },
          {
            "name": "include_facets",
            "in": "query",
            "required": false,
            "description": "Include current caller-authorized totals and bounded kind, type, provider, module, and repository facets.",
            "schema": {"type": "boolean", "default": false}
          },
          {
            "name": "limit",
            "in": "query",
            "required": false,
            "description": "Maximum resources to return (1-200, default 50).",
            "schema": {"type": "integer", "minimum": 1, "maximum": 200, "default": 50}
          },
          {
            "name": "after_name",
            "in": "query",
            "required": false,
            "description": "Keyset cursor: return rows whose (name, id) sorts after this name. Use the value from next_cursor.",
            "schema": {"type": "string"}
          },
          {
            "name": "after_id",
            "in": "query",
            "required": false,
            "description": "Keyset cursor: tiebreaker id paired with after_name. Use the value from next_cursor.",
            "schema": {"type": "string"}
          }
        ],
        "responses": {
          "504": {"$ref": "#/components/responses/GatewayTimeout"},
          "200": {
            "description": "Bounded IaC resource list",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "kind": {"type": "string"},
                    "count": {"type": "integer"},
                    "limit": {"type": "integer"},
                    "truncated": {"type": "boolean"},
                    "summary": {
                      "type": "object",
                      "description": "Authoritative current totals and bounded selector facets; present when include_facets=true.",
                      "properties": {
                        "total": {"type": "integer"},
                        "by_kind": {"type": "object", "additionalProperties": {"type": "integer"}},
                        "types": {"type": "array", "items": {"type": "object", "properties": {"kind": {"type": "string"}, "value": {"type": "string"}, "count": {"type": "integer"}}}},
                        "providers": {"type": "array", "items": {"type": "object", "properties": {"kind": {"type": "string"}, "value": {"type": "string"}, "count": {"type": "integer"}}}},
                        "modules": {"type": "array", "items": {"type": "object", "properties": {"kind": {"type": "string"}, "value": {"type": "string"}, "count": {"type": "integer"}}}},
                        "repositories": {"type": "array", "items": {"type": "object", "properties": {"value": {"type": "string"}, "count": {"type": "integer"}}}},
                        "facet_limit": {"type": "integer"},
                        "truncated": {"type": "object", "additionalProperties": {"type": "boolean"}}
                      }
                    },
                    "next_cursor": {
                      "type": "object",
                      "properties": {
                        "after_name": {"type": "string"},
                        "after_id": {"type": "string"}
                      }
                    },
                    "resources": {
                      "type": "array",
                      "items": {
                        "type": "object",
                        "properties": {
                          "id": {"type": "string"},
                          "kind": {"type": "string"},
                          "name": {"type": "string"},
                          "resource_name": {"type": "string"},
                          "type": {"type": "string"},
                          "provider": {"type": "string"},
                          "resource_service": {"type": "string"},
                          "resource_category": {"type": "string"},
                          "module": {"type": "string"},
                          "repo_id": {"type": "string"},
                          "relative_path": {"type": "string"},
                          "line_number": {"type": "integer"}
                        }
                      }
                    }
                  }
                }
              }
            }
          },
          "400": {"$ref": "#/components/responses/BadRequest"},
          "501": {"$ref": "#/components/responses/NotImplemented"},
          "503": {"$ref": "#/components/responses/ServiceUnavailable"},
          "500": {"$ref": "#/components/responses/InternalError"}
        }
      }
    },
`
