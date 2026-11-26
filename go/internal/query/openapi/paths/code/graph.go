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
                  "description": "Bounded read-only Cypher result page emitted by handleCypherQuery.",
                  "properties": {
                    "results": {"type": "array", "description": "Raw Neo4j driver result rows written by handleCypherQuery; keys are the caller query's RETURN aliases, so no fixed key set can be named.", "items": {"type": "object", "additionalProperties": true, "description": "One raw result row; keys are the caller query's RETURN aliases with arbitrary JSON values."}},
                    "limit": {"type": "integer", "description": "Row bound applied by handleCypherQuery."},
                    "truncated": {"type": "boolean", "description": "True when more rows existed than the limit, per handleCypherQuery."}
                  },
                  "required": ["results", "limit", "truncated"]
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
                      "description": "Renderable subgraph packet (visualization.VisualizationPacket) projected from the graph entities the query returned by handleVisualizeQuery.",
                      "properties": {
                        "view": {"type": "string", "description": "Derived-view family the packet was built from, per visualization.NewVisualizationBuilder."},
                        "title": {"type": "string", "description": "Short human-readable subgraph subject, per visualization.NewVisualizationBuilder."},
                        "supported": {"type": "boolean", "description": "False when no subgraph could be derived; the packet then carries limitations and recommended_next_calls, per visualization.BuildGraphQueryVisualizationPacket."},
                        "nodes": {"type": "array", "description": "Bounded deterministically ordered nodes (visualization.VisualizationNode) retained in the packet.", "items": {"type": "object", "description": "One packet node: stable id, type, label, and optional classification, truth, and evidence-handle fields.",
                          "properties": {
                            "id": {"type": "string", "description": "Stable deterministic node identifier derived from the underlying identity, per visualization.VisualizationNode."},
                            "type": {"type": "string", "description": "Node class (service, repository, citation, incident, evidence_slot), per visualization.VisualizationNode."},
                            "label": {"type": "string", "description": "Bounded human-readable render label, per visualization.VisualizationNode."},
                            "category": {"type": "string", "description": "Optional sub-classification for layout, per visualization.VisualizationNode."},
                            "role": {"type": "string", "description": "What the source evidence proves this node does in the service story, per visualization.VisualizationNode."},
                            "roles": {"type": "array", "description": "Every source-proven role reconciled onto this node, per visualization.VisualizationNode.", "items": {"type": "string"}},
                            "canonical_key": {"type": "string", "description": "Privacy-safe repository identity from the source evidence, per visualization.VisualizationNode."},
                            "scope_key": {"type": "string", "description": "Privacy-safe observation-scope discriminator, per visualization.VisualizationNode."},
                            "scope_keys": {"type": "array", "description": "Every privacy-safe observation scope reconciled onto this node, per visualization.VisualizationNode.", "items": {"type": "string"}},
                            "truth_label": {"type": "string", "description": "Per-node truth label carried from the source response, per visualization.VisualizationNode."},
                            "evidence_handle": {"type": "object", "description": "Evidence-citation handle back to the source response evidence, per visualization.VisualizationNode.", "additionalProperties": true},
                            "evidence_handles": {"type": "array", "description": "Every supported observation handle reconciled onto this node, per visualization.VisualizationNode.", "items": {"type": "object", "description": "One reconciled observation handle (open map). Owner: querycontract/visualization VisualizationNode.", "additionalProperties": true}}
                          },
                          "required": ["id", "type", "label"]
                        }},
                        "edges": {"type": "array", "description": "Bounded deterministically ordered edges (visualization.VisualizationEdge) retained in the packet.", "items": {"type": "object", "description": "One packet edge between stable node ids, per visualization.VisualizationEdge.",
                          "properties": {
                            "id": {"type": "string", "description": "Stable deterministic edge identifier derived from source, target, and relationship, per visualization.VisualizationEdge."},
                            "source": {"type": "string", "description": "Stable id of the originating node, per visualization.VisualizationEdge."},
                            "target": {"type": "string", "description": "Stable id of the destination node, per visualization.VisualizationEdge."},
                            "relationship": {"type": "string", "description": "Relationship label (graph relationship type or incident slot transition), per visualization.VisualizationEdge."},
                            "truth_label": {"type": "string", "description": "Truth label carried from the source response, per visualization.VisualizationEdge."},
                            "evidence_handle": {"type": "object", "description": "Evidence-citation handle for the relationship, per visualization.VisualizationEdge.", "additionalProperties": true},
                            "evidence_handles": {"type": "array", "description": "Every supported relationship handle collapsed onto this edge, per visualization.VisualizationEdge.", "items": {"type": "object", "description": "One collapsed relationship handle (open map). Owner: querycontract/visualization VisualizationEdge.", "additionalProperties": true}}
                          },
                          "required": ["id", "source", "target", "relationship"]
                        }},
                        "truth": {"type": "object", "description": "Copy of the source response TruthEnvelope; the canonical truth metadata for the subgraph, per visualization.VisualizationPacket. Omitted when the source carried none.", "additionalProperties": true},
                        "limits": {"type": "object", "description": "Payload bounds and retained counts (visualization.VisualizationLimits) so a client can show N of M.",
                          "properties": {
                            "max_nodes": {"type": "integer", "description": "Node bound applied, per visualization.VisualizationLimits."},
                            "max_edges": {"type": "integer", "description": "Edge bound applied, per visualization.VisualizationLimits."},
                            "ordering": {"type": "string", "description": "Deterministic ordering contract (stable_id), per visualization.VisualizationLimits."},
                            "node_count": {"type": "integer", "description": "Nodes retained in the packet, per visualization.VisualizationLimits."},
                            "edge_count": {"type": "integer", "description": "Edges retained in the packet, per visualization.VisualizationLimits."}
                          },
                          "required": ["max_nodes", "max_edges", "ordering", "node_count", "edge_count"]
                        },
                        "truncation": {"type": "object", "description": "What the packet dropped to stay within bounds (visualization.VisualizationTruncation), so truncation is explicit.",
                          "properties": {
                            "truncated": {"type": "boolean", "description": "True when any node or edge was dropped, per visualization.VisualizationTruncation."},
                            "dropped_node_count": {"type": "integer", "description": "Nodes dropped over the bound, per visualization.VisualizationTruncation."},
                            "dropped_edge_count": {"type": "integer", "description": "Edges dropped (over-bound plus dangling), per visualization.VisualizationTruncation."},
                            "dropped_node_ids": {"type": "array", "description": "Stable ids of dropped nodes, sorted and bounded, per visualization.VisualizationTruncation.", "items": {"type": "string"}}
                          },
                          "required": ["truncated", "dropped_node_count", "dropped_edge_count"]
                        },
                        "limitations": {"type": "array", "description": "Bounded human-readable caveats (unsupported view, truncation, missing source fields), per visualization.VisualizationPacket.", "items": {"type": "string"}},
                        "recommended_next_calls": {"type": "array", "description": "Bounded follow-up calls in the evidence-citation recommended_next_calls shape, per visualization.VisualizationPacket.", "items": {"type": "object", "additionalProperties": true, "description": "One follow-up call; keys follow the evidence-citation recommended_next_calls convention (tool, arguments, reason)."}}
                      },
                      "required": ["view", "supported", "nodes", "edges", "limits", "truncation"]
                    },
                    "limit": {"type": "integer", "description": "Row bound applied before projection, per handleVisualizeQuery."},
                    "truncated": {"type": "boolean", "description": "True when the row window was cut at the limit before projection, per handleVisualizeQuery."}
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
        "description": "Searches the pre-indexed package registry catalog (package bundles) by package name, namespace, or PURL, optionally scoped to one ecosystem. A non-empty query or ecosystem scope is required; an unscoped request returns 400. This route does not upload bundle archives or mutate graph state. A scoped-token caller sees only packages whose visibility is exactly public: a private package, and a package whose fact carries no visibility at all, are hidden from it, and it is not served correlation-granted private packages either. A package node carries no repository key to bind a grant to, so the read is gated on that visibility, and a scoped token with an empty grant gets an empty page without a graph read. Results are ordered by ecosystem, normalized name, and package id; truncated reports whether more matches exist than the limit. The shared key and all-scope callers see every package.",
        "operationId": "searchCodeBundles",
        "x-scoped-token-support": true,
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
                  "description": "Bounded package-registry bundle candidate page emitted by searchRegistryBundles.",
                  "properties": {
                    "bundles": {"type": "array", "description": "Bundle candidates ordered by ecosystem, normalized name, and package id, per handleSearchBundles. Each row is a package node projection; keys are the registry row fields.", "items": {"type": "object", "additionalProperties": true, "description": "One bundle candidate row keyed by the package registry row fields."}},
                    "count": {"type": "integer", "description": "Candidates on this page emitted by searchRegistryBundles."},
                    "limit": {"type": "integer", "description": "Page bound echoed by handleSearchBundles."},
                    "truncated": {"type": "boolean", "description": "True when more matches exist than the limit, per handleSearchBundles."}
                  },
                  "required": ["bundles", "count", "limit", "truncated"]
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
