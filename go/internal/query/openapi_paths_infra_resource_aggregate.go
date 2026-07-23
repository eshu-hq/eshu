// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

const openAPIPathsInfraResourceAggregate = `
    "/api/v0/infra/resources/count": {
      "get": {
        "summary": "Count graph-backed infrastructure resources without paging the search endpoint",
        "operationId": "countInfraResources",
        "x-scoped-token-support": true,
        "parameters": [
          {"name": "category", "in": "query", "schema": {"type": "string", "enum": ["k8s", "terraform", "argocd", "crossplane", "helm", "cloud"]}},
          {"name": "kind", "in": "query", "schema": {"type": "string"}},
          {"name": "resource_type", "in": "query", "schema": {"type": "string"}},
          {"name": "provider", "in": "query", "schema": {"type": "string"}},
          {"name": "environment", "in": "query", "schema": {"type": "string"}},
          {"name": "resource_service", "in": "query", "schema": {"type": "string"}},
          {"name": "resource_category", "in": "query", "schema": {"type": "string"}}
        ],
        "responses": {
          "503": {"$ref": "#/components/responses/ServiceUnavailable"},
          "504": {"$ref": "#/components/responses/GatewayTimeout"},
          "200": {
            "description": "Graph-backed infrastructure resource totals envelope",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "total_resources": {"type": "integer"},
                    "by_provider": {"type": "object", "additionalProperties": {"type": "integer"}},
                    "by_environment": {"type": "object", "additionalProperties": {"type": "integer"}},
                    "by_label": {"type": "object", "additionalProperties": {"type": "integer"}},
                    "scope": {"type": "object"}
                  }
                }
              }
            }
          }
        }
      }
    },
    "/api/v0/infra/resources/inventory": {
      "get": {
        "summary": "Group graph-backed infrastructure resources by one dimension without paging the search endpoint",
        "operationId": "getInfraResourceInventory",
        "x-scoped-token-support": true,
        "parameters": [
          {"name": "group_by", "in": "query", "schema": {"type": "string", "enum": ["provider", "environment", "resource_category", "resource_service", "label"], "default": "provider"}},
          {"name": "category", "in": "query", "schema": {"type": "string", "enum": ["k8s", "terraform", "argocd", "crossplane", "helm", "cloud"]}},
          {"name": "kind", "in": "query", "schema": {"type": "string"}},
          {"name": "resource_type", "in": "query", "schema": {"type": "string"}},
          {"name": "provider", "in": "query", "schema": {"type": "string"}},
          {"name": "environment", "in": "query", "schema": {"type": "string"}},
          {"name": "resource_service", "in": "query", "schema": {"type": "string"}},
          {"name": "resource_category", "in": "query", "schema": {"type": "string"}},
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
