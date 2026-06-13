package query

const openAPIPathsSemanticSearch = `
    "/api/v0/search/semantic": {
      "post": {
        "tags": ["semantic"],
        "summary": "Search curated semantic context",
        "description": "Runs bounded retrieval over active curated Eshu search documents for one repository corpus. Callers must provide repo_id, query, mode, limit, and timeout_ms. Results are derived retrieval evidence with graph handles, truth labels, freshness, search method, and truncation metadata; retrieval scores are never promoted to canonical graph truth.",
        "operationId": "searchSemanticContext",
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
                  }
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
                          "metadata": {"type": "object", "additionalProperties": {"type": "string"}}
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
          "503": {"$ref": "#/components/responses/ServiceUnavailable"}
        }
      }
    },
`
