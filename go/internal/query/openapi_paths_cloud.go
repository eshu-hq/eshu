// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

const openAPIPathsCloud = `
    "/api/v0/cloud/resources": {
      "get": {
        "summary": "List cloud provider resources from the authoritative graph (bounded, filterable, keyset-paged)",
        "description": "Selects an authorized, current page from the graph owner ledger before hydrating only those resource IDs from the authoritative graph. Scoped tokens are filtered by repository and ingestion-scope grants before the page limit.",
        "operationId": "listCloudResources",
        "x-scoped-token-support": true,
        "parameters": [
          {"name": "provider", "in": "query", "description": "Filter by collector provider (for example aws).", "schema": {"type": "string"}},
          {"name": "resource_type", "in": "query", "description": "Filter by resource type (for example aws_iam_role).", "schema": {"type": "string"}},
          {"name": "region", "in": "query", "description": "Filter by region (for example us-east-1).", "schema": {"type": "string"}},
          {"name": "account_id", "in": "query", "description": "Filter by cloud account id.", "schema": {"type": "string"}},
          {"name": "limit", "in": "query", "description": "Page size.", "schema": {"type": "integer", "minimum": 1, "maximum": 200, "default": 50}},
          {"name": "after_resource_type", "in": "query", "description": "Keyset cursor: resource_type of the last row from the previous page. Use the values returned in next_cursor.", "schema": {"type": "string"}},
          {"name": "after_id", "in": "query", "description": "Keyset cursor: id of the last row from the previous page. Use the values returned in next_cursor.", "schema": {"type": "string"}}
        ],
        "responses": {
          "200": {
            "description": "Bounded cloud resource list envelope ordered by resource_type then id",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "resources": {
                      "type": "array",
                      "items": {
                        "type": "object",
                        "properties": {
                          "id": {"type": "string"},
                          "resource_type": {"type": "string"},
                          "name": {"type": "string"},
                          "provider": {"type": "string"},
                          "region": {"type": "string"},
                          "account_id": {"type": "string"},
                          "arn": {"type": "string"},
                          "service_name": {"type": "string"},
                          "state": {"type": "string"}
                        }
                      }
                    },
                    "count": {"type": "integer"},
                    "limit": {"type": "integer"},
                    "truncated": {"type": "boolean"},
                    "next_cursor": {
                      "type": "object",
                      "description": "Present only when truncated is true. Pass these values back as after_resource_type and after_id to fetch the next page.",
                      "properties": {
                        "after_resource_type": {"type": "string"},
                        "after_id": {"type": "string"}
                      }
                    },
                    "scope": {"type": "object", "additionalProperties": {"type": "string"}}
                  }
                }
              }
            }
          },
          "400": {"description": "Invalid limit or incomplete cursor"},
          "501": {"description": "Capability unsupported by the active query profile"},
          "503": {"description": "Authoritative graph backend unavailable"}
        }
      }
    },
`
