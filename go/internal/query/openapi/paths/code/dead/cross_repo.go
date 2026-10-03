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
                  "evidence_detail": {"type": "string", "enum": ["full", "handles"], "description": "Row detail for consumer evidence. full (the HTTP default) returns every evidence item on each row. handles returns at most 5 groups per row {consumer_repo_id, relationship_type, evidence_family, confidence_label, item_count}, strongest first, and at most 25 boundary items; counts, truncation markers, and truth.omissions carry what was reduced. Classification is identical in both modes. An unknown value is rejected with HTTP 400. The MCP find_cross_repo_dead_code tool defaults to handles."},
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
                          "consumer_evidence": {"type": "array", "items": {"type": "object"}, "description": "The row's own consumer evidence: every item under evidence_detail full, or at most 5 groups (consumer_repo_id, relationship_type, evidence_family, confidence_label, item_count) under handles. Empty when consumer_evidence_source is repository_boundary: that evidence is the response's boundary_consumer_evidence."},
                          "consumer_evidence_source": {"type": "string", "enum": ["entity", "repository_boundary"], "description": "entity: consumer_evidence is this entity's own evidence (possibly none); hidden_consumer_evidence_count can still reflect hidden boundary relationships. repository_boundary: no entity-level evidence existed, so classification used the repository-level boundary evidence returned once in boundary_consumer_evidence."},
                          "consumer_evidence_count": {"type": "integer", "description": "Evidence items this row held before any grouping; 0 when consumer_evidence_source is repository_boundary."},
                          "consumer_evidence_group_count": {"type": "integer", "description": "Groups the row's items formed before the 5-group cap. Present only under evidence_detail handles."},
                          "consumer_evidence_handles_truncated": {"type": "boolean", "description": "True only when groups were cut at the cap; the strongest group is always kept. Present only under handles."}
                        }}},
                        "live_by_consumer": {"type": "array", "items": {"type": "object", "properties": {
                          "consumer_evidence": {"type": "array", "items": {"type": "object"}, "description": "The row's own consumer evidence: every item under evidence_detail full, or at most 5 groups (consumer_repo_id, relationship_type, evidence_family, confidence_label, item_count) under handles. Empty when consumer_evidence_source is repository_boundary: that evidence is the response's boundary_consumer_evidence."},
                          "consumer_evidence_source": {"type": "string", "enum": ["entity", "repository_boundary"], "description": "entity: consumer_evidence is this entity's own evidence (possibly none). repository_boundary: no entity-level evidence existed, so classification used the repository-level boundary evidence returned once in boundary_consumer_evidence."},
                          "consumer_evidence_count": {"type": "integer", "description": "Evidence items this row held before any grouping; 0 when consumer_evidence_source is repository_boundary."},
                          "consumer_evidence_group_count": {"type": "integer", "description": "Groups the row's items formed before the 5-group cap. Present only under evidence_detail handles."},
                          "consumer_evidence_handles_truncated": {"type": "boolean", "description": "True only when groups were cut at the cap; the strongest group is always kept. Present only under handles."}
                        }}},
                        "unknown": {"type": "array", "items": {"type": "object", "properties": {
                          "consumer_evidence": {"type": "array", "items": {"type": "object"}, "description": "The row's own consumer evidence: every item under evidence_detail full, or at most 5 groups (consumer_repo_id, relationship_type, evidence_family, confidence_label, item_count) under handles. Empty when consumer_evidence_source is repository_boundary: that evidence is the response's boundary_consumer_evidence."},
                          "consumer_evidence_source": {"type": "string", "enum": ["entity", "repository_boundary"], "description": "entity: consumer_evidence is this entity's own evidence (possibly none). repository_boundary: no entity-level evidence existed, so classification used the repository-level boundary evidence returned once in boundary_consumer_evidence."},
                          "consumer_evidence_count": {"type": "integer", "description": "Evidence items this row held before any grouping; 0 when consumer_evidence_source is repository_boundary."},
                          "consumer_evidence_group_count": {"type": "integer", "description": "Groups the row's items formed before the 5-group cap. Present only under evidence_detail handles."},
                          "consumer_evidence_handles_truncated": {"type": "boolean", "description": "True only when groups were cut at the cap; the strongest group is always kept. Present only under handles."}
                        }}},
                        "suppressed": {"type": "array", "items": {"type": "object"}}
                      }
                    },
                    "boundary_consumer_evidence": {"type": "array", "items": {"type": "object"}, "description": "Repository-level boundary evidence (relationship_type, citation, confidence and the other consumer evidence fields; under handles the five group keys with item_count 1, at most 25) after the request's consumer selector and grant. Returned once for every candidate row whose consumer_evidence_source is repository_boundary, instead of repeated on each row. Always present; empty (and the count 0) when no row used the fallback, including when the repository has no boundary relationships or none the caller may see."},
                    "boundary_consumer_evidence_count": {"type": "integer", "description": "Total boundary items; 0 when the list is not emitted. Under handles boundary_consumer_evidence holds at most 25 of them."},
                    "boundary_consumer_evidence_truncated": {"type": "boolean", "description": "True only when evidence_detail handles cut the boundary list at 25."},
                    "evidence_detail": {"type": "string", "enum": ["full", "handles"], "description": "The detail mode this response used."},
                    "evidence_detail_drilldown": {"type": "object", "additionalProperties": {"type": "string"}, "description": "Present only when handles reduced something: how to get the full rows."},
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
