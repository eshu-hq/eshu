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
                    "consumer_coverage": {"type": "object", "description": "Whether any consumer repository this answer is judged against is a coverage gap (#7547). Omitted when the check produced no answer: no candidate needed classifying, the evidence read was unavailable, or the store cannot answer the check. A candidate with no strong live consumer evidence is dead only when complete is true; otherwise it is unknown_needs_evidence with the reason consumer_coverage_incomplete.", "properties": {
                      "complete": {"type": "boolean", "description": "True when no covered consumer repository is a gap. A repository is a gap when its active generation has code_calls or inheritance_edges work (completed or pending) and no reachability watermark, a truncated one, or one with a verdict_schema_epoch below the current epoch; a repository with no such work cannot be a consumer and is complete. A stale or partly drained snapshot with truncated false, and a zero-root repository before the writer stamps it truncated, read as complete. Name consumer_repo_ids to narrow an unscoped request."},
                      "incomplete_repo_ids": {"type": "array", "items": {"type": "string"}, "description": "Consumer repositories that are gaps: ones the request named, ones in the caller's grant, or, for an unscoped caller, any repository. At most 25, always returned sorted. A request that names consumers or a grant returns the lowest-sorting 25; for an unscoped request with no named consumers the statement stops at the first gaps with no ordering, so with more than 25 gaps which ids come back is an arbitrary subset."},
                      "incomplete_truncated": {"type": "boolean", "description": "True when incomplete_repo_ids and incomplete were cut at their cap."},
                      "retryable": {"type": "boolean", "description": "Whether waiting can close the coverage: true only when every listed gap is retryable and the list was not cut (incomplete_truncated false), because a cut list hides gaps that may not be retryable. False when complete is true: there is nothing to wait for."},
                      "incomplete": {"type": "array", "description": "The same repositories as incomplete_repo_ids, in the same order, with why each is a gap and whether waiting can fix it.", "items": {"type": "object", "required": ["repository_id", "state", "retryable"], "properties": {
                        "repository_id": {"type": "string"},
                        "state": {"type": "string", "enum": ["no_snapshot_yet", "truncated", "older_epoch", "no_active_scope"], "description": "no_snapshot_yet: the active generation has code edges but no reachability watermark yet; waiting fixes it. older_epoch: the snapshot was built under an older verdict schema epoch and is being rebuilt; waiting fixes it. truncated: the snapshot is current but cannot prove absence (zero roots or a depth cutoff); waiting does not fix it. no_active_scope: a repository the request named has no active repository scope, so nothing is being built. One watermark that is both truncated and older-epoch is reported truncated."},
                        "generation_id": {"type": "string", "description": "The repository scope's active generation: the snapshot being waited for. Omitted for no_active_scope. A repository with several gap scopes reports one: a truncated scope first, then the lowest generation id."},
                        "retryable": {"type": "boolean", "description": "True for no_snapshot_yet and older_epoch; false for truncated and no_active_scope."}
                      }}}
                    }},
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
