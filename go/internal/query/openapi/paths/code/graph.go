// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package code

// Graph is the OpenAPI path fragment documenting the `/api/v0/code/bundles`,
// `/api/v0/code/cypher`, `/api/v0/code/visualize` routes. openapi.Spec
// concatenates it into the published document; keep it in lockstep with the
// handlers and docs/public/reference/http-api.md.
const Graph = `
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
    "/api/v0/code/visualize": {
      "post": {
        "tags": ["code"], "summary": "Run bounded read-only Cypher and project a visualization packet",
        "description": "Shares the read-only Cypher safety path of POST /api/v0/code/cypher: mutation keywords are rejected, the query is bounded with an injected terminal LIMIT, the read runs under a timeout against an AccessModeRead session, and the row window is capped. Instead of raw result rows, the response projects a bounded, renderable visualization packet (nodes and edges) from the graph entities the query returned. Shared-key/all-scope callers only: the query text is caller-supplied and unbounded, so there is no selector to intersect against a tenant grant. Scoped and browser-session tokens are rejected before the handler runs.",
        "operationId": "runReadOnlyCypherVisualization",
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
            "description": "Bounded graph-query visualization packet",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "visualization_packet": {
                      "type": "object",
                      "properties": {
                        "view": {"type": "string"},
                        "title": {"type": "string"},
                        "supported": {"type": "boolean"},
                        "nodes": {"type": "array", "items": {"type": "object"}},
                        "edges": {"type": "array", "items": {"type": "object"}},
                        "truth": {"type": "object"},
                        "limits": {"type": "object"},
                        "truncation": {"type": "object"},
                        "limitations": {"type": "array", "items": {"type": "string"}},
                        "recommended_next_calls": {"type": "array", "items": {"type": "object"}}
                      },
                      "required": ["view", "supported", "nodes", "edges", "limits", "truncation"]
                    },
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
        "description": "Searches the pre-indexed package registry catalog (package bundles) by package name, namespace, or PURL, optionally scoped to one ecosystem. A non-empty query or ecosystem scope is required; an unscoped request returns 400. This route does not upload bundle archives or mutate graph state. Scoped tokens, all-scope bearer tokens included, are refused with a 403, and so is every browser session except a tenant-bound all-scope console session, because the handler never intersects the caller's repository grant and a Package node carries visibility and scope_id but no repository key to bind one to. That console session is admitted only when ESHU_GOVERNANCE_MODE is local_no_policy, hosted_single_tenant, or unset (which defaults to local_no_policy); hosted_multi_tenant and any unrecognized mode refuse it with the same 403. The route stays on the #5167 pending row-filtering ledger.",
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
          "503": {"$ref": "#/components/responses/ServiceUnavailable"},
          "504": {"$ref": "#/components/responses/GatewayTimeout"},
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
          "403": {"$ref": "#/components/responses/Forbidden"},
          "500": {"$ref": "#/components/responses/InternalError"}
        }
      }
    },
`
