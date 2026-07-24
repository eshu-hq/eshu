// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

const openAPIPathsPackageRegistryAggregate = `
    "/api/v0/package-registry/packages/count": {
      "get": {
        "summary": "Count graph-backed package registry packages without paging the list endpoint",
        "description": "Scoped tokens have visibility forced to 'public' regardless of the visibility filter parameter (a private/unknown filter value returns an empty envelope without querying); the response scope reflects the value actually applied.",
        "operationId": "countPackageRegistryPackages",
        "x-scoped-token-support": true,
        "parameters": [
          {"name": "ecosystem", "in": "query", "schema": {"type": "string"}},
          {"name": "registry", "in": "query", "schema": {"type": "string"}},
          {"name": "namespace", "in": "query", "schema": {"type": "string"}},
          {"name": "package_manager", "in": "query", "schema": {"type": "string"}},
          {"name": "visibility", "in": "query", "schema": {"type": "string", "enum": ["public", "private", "unknown"]}}
        ],
        "responses": {
          "503": {"$ref": "#/components/responses/ServiceUnavailable"},
          "504": {"$ref": "#/components/responses/GatewayTimeout"},
          "200": {
            "description": "Graph-backed (:Package) totals envelope with per-ecosystem rollup",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "total_packages": {"type": "integer"},
                    "by_ecosystem": {"type": "object", "additionalProperties": {"type": "integer"}},
                    "scope": {"type": "object"}
                  }
                }
              }
            }
          }
        }
      }
    },
    "/api/v0/package-registry/packages/inventory": {
      "get": {
        "summary": "Group graph-backed package registry packages by one dimension without paging the list endpoint",
        "description": "Scoped tokens have visibility forced to 'public' regardless of the visibility filter parameter (a private/unknown filter value returns an empty envelope without querying); group_by=visibility therefore degenerates to a single 'public' bucket for scoped callers. The response scope reflects the value actually applied.",
        "operationId": "getPackageRegistryPackageInventory",
        "x-scoped-token-support": true,
        "parameters": [
          {"name": "group_by", "in": "query", "schema": {"type": "string", "enum": ["ecosystem", "registry", "namespace", "package_manager", "visibility"], "default": "ecosystem"}},
          {"name": "ecosystem", "in": "query", "schema": {"type": "string"}},
          {"name": "registry", "in": "query", "schema": {"type": "string"}},
          {"name": "namespace", "in": "query", "schema": {"type": "string"}},
          {"name": "package_manager", "in": "query", "schema": {"type": "string"}},
          {"name": "visibility", "in": "query", "schema": {"type": "string", "enum": ["public", "private", "unknown"]}},
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
