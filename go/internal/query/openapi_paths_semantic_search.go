// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

const openAPIPathsSemanticSearch = `
    "/api/v0/search/semantic": {
      "post": {
        "tags": ["semantic"],
        "summary": "Search curated semantic context",
        "description": "Runs bounded retrieval over active curated Eshu search documents for one repository corpus. Callers must provide repo_id, query, mode, limit, and timeout_ms. Results are derived retrieval evidence with graph handles, truth labels, freshness, search method, and truncation metadata; retrieval scores are never promoted to canonical graph truth.",
        "operationId": "searchSemanticContext",
        "x-scoped-token-support": true,
        "requestBody": {
          "required": true,
          "content": {
            "application/json": {
              "schema": {
                "type": "object",
                "required": ["repo_id", "query", "mode", "limit", "timeout_ms"],
                "properties": {
                  "repo_id": {"type": "string", "description": "Repository id that bounds the searchable corpus."},
                  "query": {"type": "string"},
                  "mode": {"type": "string", "enum": ["keyword", "semantic", "hybrid"]},
                  "limit": {"type": "integer", "minimum": 1, "maximum": 100},
                  "timeout_ms": {"type": "integer", "minimum": 1},
                  "service_id": {"type": "string"},
                  "workload_id": {"type": "string"},
                  "environment": {"type": "string"},
                  "source_kinds": {
                    "type": "array",
                    "items": {"type": "string", "enum": ["code_entity", "repository_file", "runtime_summary", "semantic_context"]}
                  },
                  "languages": {
                    "type": "array",
                    "description": "Optional filter over recognized parser-registry language values (e.g. \"go\", \"python\", \"typescript\"). Documents are included only when their Labels array contains \"language:<lang>\" for one of the requested values. An empty array means no language filter. Unknown values are rejected with HTTP 400.",
                    "items": {"type": "string"}
                  },
                  "rerank": {"type": "boolean", "description": "Opt into graph-neighborhood reranking over the in-scope results. When true the response reports the reranking state, per-result ranking basis, and recommended next calls. Off by default."}
                }
              }
            }
          }
        },
        "responses": {
          "200": {
            "description": "Bounded semantic context search response",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "query": {"type": "string"},
                    "repo_id": {"type": "string"},
                    "anchor": {"type": "object"},
                    "mode": {"type": "string"},
                    "search_mode": {"type": "string"},
                    "limit": {"type": "integer"},
                    "timeout_ms": {"type": "integer"},
                    "truncated": {"type": "boolean"},
                    "false_canonical_claim_count": {"type": "integer"},
                    "indexed_document_count": {"type": "integer"},
                    "corpus_limit": {"type": "integer"},
                    "corpus_may_be_truncated": {"type": "boolean"},
                    "retrieval_state": {"type": "string", "enum": ["keyword_only", "semantic_unavailable", "hybrid_degraded", "semantic_active", "hybrid_active", "policy_denied", "index_unready"]},
                    "facets": {
                      "type": "object",
                      "description": "Per-dimension aggregate counts derived from the post-filter result set. Always present. Computed over the documents returned by the bounded index query — no second scan.",
                      "properties": {
                        "languages": {
                          "type": "object",
                          "description": "Map of language value to count of results carrying that language label in the result set.",
                          "additionalProperties": {"type": "integer"}
                        }
                      }
                    },
                    "results": {
                      "type": "array",
                      "items": {
                        "type": "object",
                        "properties": {
                          "rank": {"type": "integer"},
                          "score": {"type": "number"},
                          "search_method": {"type": "string"},
                          "document": {
                            "type": "object",
                            "properties": {
                              "id": {"type": "string"},
                              "repo_id": {"type": "string"},
                              "source_kind": {"type": "string"},
                              "title": {"type": "string"},
                              "path": {"type": "string"},
                              "context_text": {"type": "string"},
                              "entity_refs": {"type": "array", "items": {"type": "object"}},
                              "graph_handles": {"type": "array", "items": {"type": "object"}},
                              "truth_scope": {"type": "object"},
                              "freshness": {"type": "object"},
                              "access_scope": {"type": "object"},
                              "provenance": {"type": "object"}
                            }
                          },
                          "graph_handles": {"type": "array", "items": {"type": "object"}},
                          "truth_scope": {"type": "object"},
                          "freshness": {"type": "object"},
                          "failures": {"type": "array", "items": {"type": "string"}},
                          "metadata": {"type": "object", "additionalProperties": {"type": "string"}},
                          "ranking_basis": {
                            "type": "object",
                            "description": "Present only when rerank was requested. Preserves the baseline rank and lexical/vector score and lists the graph signals that moved the result.",
                            "properties": {
                              "baseline_rank": {"type": "integer"},
                              "baseline_score": {"type": "number"},
                              "final_rank": {"type": "integer"},
                              "graph_boost": {"type": "number"},
                              "contributions": {
                                "type": "array",
                                "items": {
                                  "type": "object",
                                  "properties": {
                                    "kind": {"type": "string"},
                                    "handle": {"type": "string"},
                                    "weight": {"type": "number"}
                                  }
                                }
                              }
                            }
                          }
                        }
                      }
                    },
                    "rerank": {
                      "type": "object",
                      "description": "Present only when rerank was requested; the block is absent (not 'disabled') when rerank is off. Reports which reranking path answered.",
                      "properties": {
                        "state": {"type": "string", "enum": ["applied", "inactive", "stale_skipped"]},
                        "applied": {"type": "boolean"}
                      }
                    },
                    "recommended_next_calls": {
                      "type": "array",
                      "description": "Bounded first-class read tools to advance the investigation from the results. Present only when rerank was requested.",
                      "items": {
                        "type": "object",
                        "properties": {
                          "tool": {"type": "string"},
                          "arguments": {"type": "object"},
                          "reason": {"type": "string"}
                        }
                      }
                    }
                  }
                }
              }
            }
          },
          "400": {"$ref": "#/components/responses/BadRequest"},
          "404": {"$ref": "#/components/responses/NotFound"},
          "409": {"description": "Canonical repository id maps to multiple active ingestion scopes; retry after repairing repository scope identity"},
          "503": {"$ref": "#/components/responses/ServiceUnavailable"}
        }
      }
    },
`
