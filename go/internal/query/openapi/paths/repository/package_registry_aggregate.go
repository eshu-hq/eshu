// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package repository

// PackageRegistryAggregate is the OpenAPI path fragment documenting the
// `/api/v0/package-registry/packages/count`,
// `/api/v0/package-registry/packages/inventory` routes. openapi.Spec
// concatenates it into the published document; keep it in lockstep with the
// handlers and docs/public/reference/http-api.md.
const PackageRegistryAggregate = `
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
          "403": {"$ref": "#/components/responses/Forbidden"},
          "503": {"$ref": "#/components/responses/ServiceUnavailable"},
          "504": {"$ref": "#/components/responses/GatewayTimeout"},
          "400": {"$ref": "#/components/responses/BadRequest"},
          "501": {"$ref": "#/components/responses/NotImplemented"},
          "500": {"$ref": "#/components/responses/InternalError"},
          "200": {
            "description": "Graph-backed (:Package) totals envelope with per-ecosystem rollup",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "total_packages": {"type": "integer", "description": "Graph-backed packages matching the filters."},
                    "by_ecosystem": {"type": "object", "additionalProperties": {"type": "integer"}, "description": "Package counts by ecosystem. Bucket names are ecosystem names; render as bars, never a fixed enum."},
                    "scope": {"type": "object", "additionalProperties": {"type": "string"}, "description": "Echo of the filters applied to this aggregate, as string pairs (only non-empty filters appear; scoped callers see the forced visibility value). Render as filter chips, never a fixed form."}
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
          "403": {"$ref": "#/components/responses/Forbidden"},
          "503": {"$ref": "#/components/responses/ServiceUnavailable"},
          "504": {"$ref": "#/components/responses/GatewayTimeout"},
          "400": {"$ref": "#/components/responses/BadRequest"},
          "501": {"$ref": "#/components/responses/NotImplemented"},
          "500": {"$ref": "#/components/responses/InternalError"},
          "200": {
            "description": "Grouped count buckets ordered by count desc",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "buckets": {
                      "type": "array",
                      "description": "Grouped count buckets ordered by count desc, emitted by the registry aggregates handler as []registry.InventoryRow.",
                      "items": {
                        "type": "object",
                        "description": "One grouped count bucket. Owner: registry aggregates handler.",
                        "properties": {
                          "dimension": {"type": "string", "description": "Bucket dimension value; the label comes from the group_by request value."},
                          "value": {"type": "string", "description": "Group value."},
                          "count": {"type": "integer", "description": "Packages in this bucket."}
                        }
                      }
                    },
                    "count": {"type": "integer", "description": "Buckets in this page."},
                    "limit": {"type": "integer", "description": "Echo of the requested page bound."},
                    "offset": {"type": "integer", "description": "Echo of the requested page offset."},
                    "group_by": {"type": "string", "description": "Echo of the requested grouping dimension."},
                    "truncated": {"type": "boolean", "description": "True when more buckets matched than this page carries; continue with next_offset."},
                    "next_offset": {"type": ["integer", "null"], "description": "Next offset to request when truncated is true; null when the page is complete or when the next offset would exceed the documented maximum (10000)."},
                    "scope": {"type": "object", "additionalProperties": {"type": "string"}, "description": "Echo of the filters applied to this aggregate, as string pairs (only non-empty filters appear; scoped callers see the forced visibility value). Render as filter chips, never a fixed form."}
                  }
                }
              }
            }
          }
        }
      }
    },
`
