// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package search

// Semantic is the OpenAPI path fragment documenting the
// `/api/v0/search/semantic` route. openapi.Spec concatenates it into the
// published document; keep it in lockstep with the handlers and
// docs/public/reference/http-api.md.
const Semantic = `
    "/api/v0/search/semantic": {
      "post": {
        "tags": ["semantic"],
        "summary": "Search curated semantic context",
        "description": "Runs bounded retrieval over active curated Eshu search documents for one repository corpus. Callers must provide repo_id, query, mode, limit, and timeout_ms. Results are derived retrieval evidence with graph handles, truth labels, freshness, search method, and truncation metadata; retrieval scores are never promoted to canonical graph truth. A 503 with Retry-After means a transient condition that is safe to retry: the scope resolution or index read hit a stale or timed-out PostgreSQL reader; a 503 without Retry-After means a configuration state: no persisted search index is configured on this API instance, or mode semantic was requested with no embedder configured. A scope-resolver fault and a stored result that fails normalization answer 500 (they answered 503 and 400 before #7674), as does any other failed read, with a fixed message. A failed index or local hybrid read on a stale or timed-out reader answers the retryable 503 (it answered 500 before #7674), and a scope-resolution 503 on such a reader now carries Retry-After. The 500 and 503 bodies keep this route's own shape: the envelope when negotiated, else error_code, message, and capability. No 5xx body carries backend error text. A reader-verdict 503 names capability semantic_search.curated_retrieval in its error envelope.",
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
                    "description": "Optional filter over recognized parser-registry language values (e.g. \"go\", \"python\", \"typescript\"). Documents are included only when their Labels array contains \"language:<lang>\" for one of the requested values. An empty array means no language filter. Any non-empty token is accepted and lowercased; the index is the source of truth for which language values exist, so an unmatched value returns an empty result set rather than an error.",
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
                    "query": {"type": "string", "description": "Retrieval query echoed from the request. Owner: semanticsearch.semanticSearchResponseFromRetrieval."},
                    "repo_id": {"type": "string", "description": "Repository id bounding the searched corpus. Owner: semanticsearch.semanticSearchResponseFromRetrieval."},
                    "anchor": {
                      "type": "object",
                      "description": "Smallest available scope selected for the bounded retrieval. Owner: searchretrieval.Anchor via semanticsearch.semanticSearchResponseFromRetrieval.",
                      "properties": {
                        "kind": {"type": "string", "description": "Scope kind that anchored the retrieval. Owner: searchretrieval.Anchor."},
                        "id": {"type": "string", "description": "Scope id that anchored the retrieval. Owner: searchretrieval.Anchor."}
                      }
                    },
                    "mode": {"type": "string", "description": "Requested retrieval mode echoed from the request. Owner: semanticsearch.semanticSearchResponseFromRetrieval."},
                    "search_mode": {"type": "string", "description": "Retrieval mode string carried on the response. Owner: semanticsearch.semanticSearchResponseFromRetrieval."},
                    "limit": {"type": "integer", "description": "Result bound the retrieval ran under. Owner: semanticsearch.semanticSearchResponseFromRetrieval."},
                    "timeout_ms": {"type": "integer", "description": "Retrieval timeout in milliseconds. Owner: semanticsearch.semanticSearchResponseFromRetrieval."},
                    "truncated": {"type": "boolean", "description": "True when the bounded read may hold more documents than the page carries. Owner: semanticsearch.semanticSearchResponseFromRetrieval."},
                    "false_canonical_claim_count": {"type": "integer", "description": "Count of retrieval claims rejected as non-canonical. Owner: semanticsearch.semanticSearchResponseFromRetrieval."},
                    "indexed_document_count": {"type": "integer", "description": "Documents the serving backend reported in the searched corpus. Owner: semanticsearch.semanticSearchResponseFromRetrieval."},
                    "corpus_limit": {"type": "integer", "description": "Bounded-read cap on the searched corpus. Owner: semanticsearch.semanticSearchResponseFromRetrieval."},
                    "corpus_may_be_truncated": {"type": "boolean", "description": "True when the bounded read may not cover the whole corpus. Owner: semanticsearch.semanticSearchResponseFromRetrieval."},
                    "retrieval_state": {"type": "string", "enum": ["keyword_only", "semantic_unavailable", "hybrid_degraded", "semantic_active", "hybrid_active", "policy_denied", "index_unready"], "description": "Which retrieval path answered the request. Owner: semanticsearch.semanticSearchResponseFromRetrieval."},
                    "corpus_state": {"type": "string", "enum": ["empty", "truncated", "populated"], "description": "Corpus condition in words: empty means the corpus holds no documents (nothing to search, retrying with different words is pointless); truncated means a corpus exists but the bounded read may not cover all of it; populated means the corpus was searched and results carry what matched, if anything. results: [] with populated means searched, nothing matched; with empty means nothing to search."},
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
                      "description": "Bounded retrieval results in final rank order; reranking only reorders, never changes truth labels. Owner: semanticsearch.semanticSearchResponseFromRetrieval.",
                      "items": {
                        "type": "object",
                        "properties": {
                          "rank": {"type": "integer", "description": "Final rank of the result in the response order. Owner: semanticsearch.semanticSearchResponseFromRetrieval."},
                          "score": {"type": "number", "description": "Retrieval score of the result. Owner: semanticsearch.semanticSearchResponseFromRetrieval."},
                          "search_method": {"type": "string", "description": "Index path that produced the result. Owner: semanticsearch.semanticSearchMethod."},
                          "document": {
                            "type": "object",
                            "description": "Curated search document carrying graph handles, truth labels, and freshness. Owner: semanticsearch.semanticSearchDocumentFromSearchDoc.",
                            "properties": {
                              "id": {"type": "string", "description": "Search document id. Owner: semanticsearch.semanticSearchDocumentFromSearchDoc."},
                              "repo_id": {"type": "string", "description": "Repository id holding the document. Owner: semanticsearch.semanticSearchDocumentFromSearchDoc."},
                              "source_kind": {"type": "string", "description": "Document source kind. Owner: semanticsearch.semanticSearchDocumentFromSearchDoc."},
                              "title": {"type": "string", "description": "Document title. Owner: semanticsearch.semanticSearchDocumentFromSearchDoc."},
                              "path": {"type": "string", "description": "Repository-relative path, present when the document has one. Owner: semanticsearch.semanticSearchDocumentFromSearchDoc."},
                              "context_text": {"type": "string", "description": "Curated context text, present when the document carries it. Owner: semanticsearch.semanticSearchDocumentFromSearchDoc."},
                              "entity_refs": {
                                "type": "array",
                                "description": "Entity references carried by the document. Owner: semanticsearch.semanticSearchEntityRefs.",
                                "items": {
                                  "type": "object",
                                  "properties": {
                                    "id": {"type": "string", "description": "Referenced entity id. Owner: semanticsearch.semanticSearchEntityRefs."},
                                    "type": {"type": "string", "description": "Referenced entity type, present when known. Owner: semanticsearch.semanticSearchEntityRefs."},
                                    "name": {"type": "string", "description": "Referenced entity name, present when known. Owner: semanticsearch.semanticSearchEntityRefs."},
                                    "path": {"type": "string", "description": "Referenced entity path, present when known. Owner: semanticsearch.semanticSearchEntityRefs."},
                                    "start_line": {"type": "integer", "description": "First referenced line, present when known. Owner: semanticsearch.semanticSearchEntityRefs."},
                                    "end_line": {"type": "integer", "description": "Last referenced line, present when known. Owner: semanticsearch.semanticSearchEntityRefs."}
                                  }
                                }
                              },
                              "graph_handles": {
                                "type": "array",
                                "description": "Graph handles for the document. Owner: semanticsearch.semanticSearchGraphHandles.",
                                "items": {
                                  "type": "object",
                                  "properties": {
                                    "kind": {"type": "string", "description": "Graph handle kind. Owner: semanticsearch.semanticSearchGraphHandles."},
                                    "id": {"type": "string", "description": "Graph handle id. Owner: semanticsearch.semanticSearchGraphHandles."}
                                  }
                                }
                              },
                              "labels": {"type": "array", "items": {"type": "string"}, "description": "Document labels including language:<lang> markers, present when labeled. Owner: semanticsearch.semanticSearchDocumentFromSearchDoc."},
                              "updated_at": {"type": "string", "format": "date-time", "description": "Document update time, present when the store reports one. Owner: semanticsearch.semanticSearchDocumentFromSearchDoc."},
                              "truth_scope": {
                                "type": "object",
                                "description": "Truth labels of the document. Owner: semanticsearch.semanticSearchTruthScope.",
                                "properties": {
                                  "level": {"type": "string", "description": "Truth level of the document. Owner: semanticsearch.semanticSearchTruthScope."},
                                  "basis": {"type": "string", "description": "Truth basis of the document. Owner: semanticsearch.semanticSearchTruthScope."}
                                }
                              },
                              "freshness": {
                                "type": "object",
                                "description": "Freshness of the document. Owner: semanticsearch.semanticSearchFreshness.",
                                "properties": {
                                  "state": {"type": "string", "description": "Freshness state of the document. Owner: semanticsearch.semanticSearchFreshness."}
                                }
                              },
                              "access_scope": {
                                "type": "object",
                                "description": "Access scope of the document. Owner: semanticsearch access scope mapper.",
                                "properties": {
                                  "repo_id": {"type": "string", "description": "Repository id the document access is scoped to, present when scoped. Owner: semanticsearch access scope mapper."}
                                }
                              },
                              "provenance": {
                                "type": "object",
                                "description": "Store provenance of the document. Owner: semanticsearch provenance mapper.",
                                "properties": {
                                  "source_table": {"type": "string", "description": "Source table the document was read from, present when reported. Owner: semanticsearch provenance mapper."},
                                  "source_ids": {"type": "array", "items": {"type": "string"}, "description": "Source ids the document was read from, present when reported. Owner: semanticsearch provenance mapper."}
                                }
                              }
                            }
                          },
                          "graph_handles": {
                            "type": "array",
                            "description": "Graph handles for the result. Owner: semanticsearch.semanticSearchGraphHandles.",
                            "items": {
                              "type": "object",
                              "properties": {
                                "kind": {"type": "string", "description": "Graph handle kind. Owner: semanticsearch.semanticSearchGraphHandles."},
                                "id": {"type": "string", "description": "Graph handle id. Owner: semanticsearch.semanticSearchGraphHandles."}
                              }
                            }
                          },
                          "truth_scope": {
                            "type": "object",
                            "description": "Truth labels of the result. Owner: semanticsearch.semanticSearchTruthScope.",
                            "properties": {
                              "level": {"type": "string", "description": "Truth level of the result. Owner: semanticsearch.semanticSearchTruthScope."},
                              "basis": {"type": "string", "description": "Truth basis of the result. Owner: semanticsearch.semanticSearchTruthScope."}
                            }
                          },
                          "freshness": {
                            "type": "object",
                            "description": "Freshness of the result. Owner: semanticsearch.semanticSearchFreshness.",
                            "properties": {
                              "state": {"type": "string", "description": "Freshness state of the result. Owner: semanticsearch.semanticSearchFreshness."}
                            }
                          },
                          "failures": {"type": "array", "items": {"type": "string"}, "description": "Retrieval failure classes for the result, present when any applied. Owner: semanticsearch.semanticSearchResponseFromRetrieval."},
                          "metadata": {"type": "object", "additionalProperties": {"type": "string"}, "description": "Retrieval metadata keyed by metadata name, present when the index reports any. Owner: semanticsearch.cloneSemanticSearchMetadata."},
                          "ranking_basis": {
                            "type": "object",
                            "description": "Present only when rerank was requested. Preserves the baseline rank and lexical/vector score and lists the graph signals that moved the result.",
                            "properties": {
                              "baseline_rank": {"type": "integer", "description": "Rank before graph-neighborhood reranking. Owner: semanticsearch.semanticSearchRerankResults."},
                              "baseline_score": {"type": "number", "description": "Lexical/vector score before reranking. Owner: semanticsearch.semanticSearchRerankResults."},
                              "final_rank": {"type": "integer", "description": "Rank after reranking. Owner: semanticsearch.semanticSearchRerankResults."},
                              "graph_boost": {"type": "number", "description": "Graph signal boost applied to the baseline score. Owner: semanticsearch.semanticSearchRerankResults."},
                              "contributions": {
                                "type": "array",
                                "description": "Graph signals that moved the result, present when any contributed. Owner: semanticsearch.semanticSearchRerankResults.",
                                "items": {
                                  "type": "object",
                                  "properties": {
                                    "kind": {"type": "string", "description": "Graph signal kind. Owner: semanticsearch.semanticSearchContribution."},
                                    "handle": {"type": "string", "description": "Bounded kind:id handle that moved the result, present when the signal names one. Owner: semanticsearch.semanticSearchContribution."},
                                    "weight": {"type": "number", "description": "Contribution weight. Owner: semanticsearch.semanticSearchContribution."}
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
                        "state": {"type": "string", "enum": ["applied", "inactive", "stale_skipped"], "description": "Reranking outcome for the response. Owner: semanticsearch.semanticSearchRerank."},
                        "applied": {"type": "boolean", "description": "True only when graph signals reordered the results. Owner: semanticsearch.semanticSearchRerank."}
                      }
                    },
                    "recommended_next_calls": {
                      "type": "array",
                      "description": "Bounded first-class read tools to advance the investigation from the results. Present only when rerank was requested.",
                      "items": {
                        "type": "object",
                        "properties": {
                          "tool": {"type": "string", "description": "First-class tool name to call next. Owner: semanticsearch.semanticSearchCall."},
                          "arguments": {"type": "object", "additionalProperties": true, "description": "Bounded tool arguments keyed by argument name. Owner: semanticsearch.semanticSearchCall."},
                          "reason": {"type": "string", "description": "Why the call advances the investigation. Owner: semanticsearch.semanticSearchCall."}
                        }
                      }
                    }
                  }
                }
              }
            }
          },
          "400": {"$ref": "#/components/responses/BadRequest"},
          "403": {"$ref": "#/components/responses/Forbidden"},
          "404": {"$ref": "#/components/responses/NotFound"},
          "409": {"description": "Canonical repository id maps to multiple active ingestion scopes; retry after repairing repository scope identity"},
          "500": {"$ref": "#/components/responses/InternalError"},
          "503": {"$ref": "#/components/responses/ServiceUnavailable"}
        }
      }
    },
`
