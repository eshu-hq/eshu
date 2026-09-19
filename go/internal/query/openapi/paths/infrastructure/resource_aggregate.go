// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package infrastructure

// ResourceAggregate is the OpenAPI path fragment documenting the
// `/api/v0/infra/resources/count`, `/api/v0/infra/resources/inventory` routes.
// openapi.Spec concatenates it into the published document; keep it in
// lockstep with the handlers and docs/public/reference/http-api.md.
const ResourceAggregate = `
    "/api/v0/infra/resources/count": {
      "get": {
        "summary": "Count graph-backed infrastructure resources without paging the search endpoint",
        "description": "Counts the canonical graph population of the infrastructure labels. Once the infra read model backfill has completed, unscoped reads count content-derived nodes from the Postgres infra_resource_entities table, and CloudResource, TerraformStateResource, and the Terraform state projector's TerraformModule and TerraformOutput nodes from the graph, and report truth basis hybrid. A category that needs no graph read (category=argocd, crossplane, helm, or k8s) is served from the table alone and reports truth basis content_index; category=cloud needs only the graph and reports truth basis authoritative_graph. Scoped tokens, and every read before the backfill completes, read only the graph and report truth basis authoritative_graph.",
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
          "403": {"$ref": "#/components/responses/Forbidden"},
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
        "description": "Groups the canonical graph population of the infrastructure labels by one dimension. Uses the same serving rules as countInfraResources: unscoped reads after the infra read model backfill are truth basis hybrid, except a category that needs no graph read (category=argocd, crossplane, helm, or k8s), which is content_index, and category=cloud, which reads only the graph and is authoritative_graph. Scoped reads and reads before the backfill are authoritative_graph.",
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
