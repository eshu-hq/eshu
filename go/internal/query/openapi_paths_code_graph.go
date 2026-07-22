// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

const openAPIPathsCodeGraph = `
    "/api/v0/code/cypher": {
      "post": {
        "tags": ["code"], "summary": "Run bounded read-only Cypher",
        "description": "Diagnostics-only graph query endpoint. Prefer purpose-built code, service, and impact routes for prompt contracts. Queries are read-only, timeout-bound, and server-capped. Shared-key/all-scope callers only: the query text is caller-supplied and unbounded, so there is no selector to intersect against a tenant grant. Scoped and browser-session tokens are rejected before the handler runs.",
        "operationId": "runReadOnlyCypher",
        "x-shared-key-only": true,
        "requestBody": {
          "required": true,
          "content": {
            "application/json": {
              "schema": {
                "type": "object",
                "required": ["cypher_query"],
                "properties": {
                  "cypher_query": {"type": "string"},
                  "limit": {"type": "integer", "default": 100, "maximum": 1000}
                }
              }
            }
          }
        },
        "responses": {
          "200": {
            "description": "Bounded Cypher results",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "results": {"type": "array", "items": {"type": "object"}},
                    "limit": {"type": "integer"},
                    "truncated": {"type": "boolean"}
                  }
                }
              }
            }
          },
          "400": {"$ref": "#/components/responses/BadRequest"},
          "501": {"$ref": "#/components/responses/NotImplemented"},
          "503": {"$ref": "#/components/responses/ServiceUnavailable"},
          "504": {"$ref": "#/components/responses/GatewayTimeout"},
          "500": {"$ref": "#/components/responses/InternalError"}
        }
      }
    },
    "/api/v0/code/bundles": {
      "post": {
        "tags": ["code"],
        "summary": "Search package registry bundle candidates",
        "description": "Searches the pre-indexed package registry catalog (package bundles) by package name, namespace, or PURL, optionally scoped to one ecosystem. A non-empty query or ecosystem scope is required; an unscoped request returns 400. This route does not upload bundle archives or mutate graph state.",
        "operationId": "searchCodeBundles",
        "requestBody": {
          "required": true,
          "content": {
            "application/json": {
              "schema": {
                "type": "object",
                "anyOf": [
                  {"required": ["query"]},
                  {"required": ["ecosystem"]}
                ],
                "properties": {
                  "query": {"type": "string", "minLength": 1, "pattern": "\\S", "description": "Case-insensitive substring matched against package normalized name, namespace, or PURL. Required unless ecosystem is supplied; must contain a non-whitespace character."},
                  "ecosystem": {"type": "string", "minLength": 1, "pattern": "\\S", "description": "Ecosystem scope (e.g. npm, pypi, maven, nuget). Required unless query is supplied; must contain a non-whitespace character."},
                  "unique_only": {"type": "boolean", "description": "Return only distinct package bundles", "default": false},
                  "limit": {"type": "integer", "description": "Max results (default 50, max 200)", "default": 50, "minimum": 1, "maximum": 200}
                }
              }
            }
          }
        },
        "responses": {
          "200": {
            "description": "Bundle candidates",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "bundles": {"type": "array", "items": {"type": "object", "additionalProperties": true}},
                    "count": {"type": "integer"}
                  }
                }
              }
            }
          },
          "400": {"$ref": "#/components/responses/BadRequest"},
          "500": {"$ref": "#/components/responses/InternalError"}
        }
      }
    },
`
