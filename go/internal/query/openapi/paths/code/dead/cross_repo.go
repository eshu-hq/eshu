// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package dead

// CrossRepo is the OpenAPI path fragment documenting the
// `/api/v0/code/dead-code/cross-repo` route. openapi.Spec concatenates it into
// the published document; keep it in lockstep with the handlers and
// docs/public/reference/http-api.md.
const CrossRepo = `
    "/api/v0/code/dead-code/cross-repo": {
      "post": {
        "tags": ["code"],
        "summary": "Find cross-repo dead-code candidates",
        "description": "Classifies producer repository dead-code candidates against deterministic consumer evidence. Symbols or routes kept live by another repository are returned as live_by_consumer. Ambiguous ownership, stale generations, missing read-model coverage, and scoped-token-hidden consumers are returned as unknown_needs_evidence rather than dead. Scoped tokens receive only granted repositories; an ungranted repository selector is rejected with HTTP 400.",
        "operationId": "findCrossRepoDeadCode",
        "x-scoped-token-support": true,
        "requestBody": {
          "required": true,
          "content": {
            "application/json": {
              "schema": {
                "type": "object",
                "required": ["repo_id"],
                "properties": {
                  "repo_id": {"type": "string", "description": "Producer repository selector (canonical ID, name, slug, or path)."},
                  "consumer_repo_ids": {
                    "type": "array",
                    "description": "Optional consumer repository selectors that bound cross-repo liveness evidence.",
                    "items": {"type": "string"}
                  },
                  "language": {"type": "string", "description": "Optional parser language filter."},
                  "limit": {"type": "integer", "description": "Maximum active producer candidates to classify (default 100, max 500).", "default": 100},
                  "exclude_decorated_with": {
                    "type": "array",
                    "description": "Optional decorator names to suppress from active candidates.",
                    "items": {"type": "string"}
                  }
                }
              }
            }
          }
        },
        "responses": {
          "403": {"$ref": "#/components/responses/Forbidden"},
          "503": {"$ref": "#/components/responses/ServiceUnavailable"},
          "504": {"$ref": "#/components/responses/GatewayTimeout"},
          "200": {
            "description": "Cross-repo dead-code classification packet",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "repo_id": {"type": "string"},
                    "language": {"type": "string"},
                    "limit": {"type": "integer"},
                    "consumer_repo_ids": {"type": "array", "items": {"type": "string"}},
                    "query_shape": {"type": "string", "enum": ["bounded_cross_repo_dead_code"]},
                    "truncated": {"type": "boolean"},
                    "display_truncated": {"type": "boolean"},
                    "candidate_scan_truncated": {"type": "boolean", "description": "True when the shared candidate scan limit was reached before all selected labels were exhausted."},
                    "suppressed_truncated": {"type": "boolean", "description": "True when suppressed modeled-root examples exceeded the bounded suppressed bucket, whose size is suppressed_limit."},
                    "suppressed_limit": {"type": "integer", "description": "Maximum rows the suppressed bucket may carry: the smaller of the request limit and 50."},
                    "candidate_scan_limit": {"type": "integer", "description": "Maximum raw candidate rows the classification may inspect across all selected candidate labels."},
                    "candidate_scan_limit_per_label": {"type": "integer", "description": "Maximum share one candidate label may consume from the classification's shared raw-row limit."},
                    "candidate_scan_pages": {"type": "integer"},
                    "candidate_scan_rows": {"type": "integer"},
                    "docstring_clip_bytes": {"type": "integer", "description": "Read-time docstring ceiling in bytes (512) applied to every row of this response; always present."},
                    "docstring_clipped_rows": {"type": "integer", "description": "Number of returned rows, suppressed rows included, whose docstring was clipped; 0 when none."},
                    "candidate_buckets": {
                      "type": "object",
                      "description": "Every row in every bucket has its docstring clipped at read time to docstring_clip_bytes; a clipped row carries docstring_clipped, docstring_clip_bytes, and docstring_total_bytes.",
                      "properties": {
                        "dead": {"type": "array", "items": {"type": "object", "properties": {
                          "consumer_evidence": {"type": "array", "items": {"type": "object"}, "description": "The row's own consumer evidence. Empty when consumer_evidence_source is repository_boundary: that evidence is the response's boundary_consumer_evidence."},
                          "consumer_evidence_source": {"type": "string", "enum": ["entity", "repository_boundary"], "description": "entity: consumer_evidence is this entity's own evidence (possibly none). repository_boundary: no entity-level evidence existed, so classification used the repository-level boundary evidence returned once in boundary_consumer_evidence."},
                          "consumer_evidence_count": {"type": "integer", "description": "Length of this row's consumer_evidence."}
                        }}},
                        "live_by_consumer": {"type": "array", "items": {"type": "object", "properties": {
                          "consumer_evidence": {"type": "array", "items": {"type": "object"}, "description": "The row's own consumer evidence. Empty when consumer_evidence_source is repository_boundary: that evidence is the response's boundary_consumer_evidence."},
                          "consumer_evidence_source": {"type": "string", "enum": ["entity", "repository_boundary"], "description": "entity: consumer_evidence is this entity's own evidence (possibly none). repository_boundary: no entity-level evidence existed, so classification used the repository-level boundary evidence returned once in boundary_consumer_evidence."},
                          "consumer_evidence_count": {"type": "integer", "description": "Length of this row's consumer_evidence."}
                        }}},
                        "unknown": {"type": "array", "items": {"type": "object", "properties": {
                          "consumer_evidence": {"type": "array", "items": {"type": "object"}, "description": "The row's own consumer evidence. Empty when consumer_evidence_source is repository_boundary: that evidence is the response's boundary_consumer_evidence."},
                          "consumer_evidence_source": {"type": "string", "enum": ["entity", "repository_boundary"], "description": "entity: consumer_evidence is this entity's own evidence (possibly none). repository_boundary: no entity-level evidence existed, so classification used the repository-level boundary evidence returned once in boundary_consumer_evidence."},
                          "consumer_evidence_count": {"type": "integer", "description": "Length of this row's consumer_evidence."}
                        }}},
                        "suppressed": {"type": "array", "items": {"type": "object"}}
                      }
                    },
                    "boundary_consumer_evidence": {"type": "array", "items": {"type": "object"}, "description": "Repository-level boundary evidence (relationship_type, citation, confidence and the other consumer evidence fields) after the request's consumer selector and grant. Returned once for every candidate row whose consumer_evidence_source is repository_boundary, instead of repeated on each row. Always present, empty when the repository has no boundary relationships."},
                    "boundary_consumer_evidence_count": {"type": "integer", "description": "Length of boundary_consumer_evidence."},
                    "bucket_counts": {"type": "object", "additionalProperties": true},
                    "analysis": {"type": "object", "additionalProperties": true}
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
