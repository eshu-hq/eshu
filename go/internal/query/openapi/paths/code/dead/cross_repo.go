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
                    "repo_id": {"type": "string", "description": "Producer repository selector echoed by the cross-repo handler."},
                    "language": {"type": "string", "description": "Parser language filter echoed by the cross-repo handler."},
                    "limit": {"type": "integer", "description": "Active-candidate bound echoed by the cross-repo handler."},
                    "consumer_repo_ids": {"type": "array", "description": "Consumer selector bound echoed by the cross-repo handler.", "items": {"type": "string"}},
                    "query_shape": {"type": "string", "enum": ["bounded_cross_repo_dead_code"], "description": "Classification shape identifier emitted by the cross-repo handler."},
                    "truncated": {"type": "boolean", "description": "True when classification was cut at the limit, per the cross-repo handler."},
                    "display_truncated": {"type": "boolean", "description": "True when displayed buckets were clipped at the limit, per the cross-repo handler."},
                    "candidate_scan_truncated": {"type": "boolean", "description": "True when the shared candidate scan limit was reached before all selected labels were exhausted."},
                    "suppressed_truncated": {"type": "boolean", "description": "True when suppressed modeled-root examples exceeded the bounded suppressed bucket, whose size is suppressed_limit."},
                    "suppressed_limit": {"type": "integer", "description": "Maximum rows the suppressed bucket may carry: the smaller of the request limit and 50."},
                    "candidate_scan_limit": {"type": "integer", "description": "Maximum raw candidate rows the classification may inspect across all selected candidate labels."},
                    "candidate_scan_limit_per_label": {"type": "integer", "description": "Maximum share one candidate label may consume from the classification's shared raw-row limit."},
                    "candidate_scan_pages": {"type": "integer", "description": "Number of raw candidate pages read before returning the classification, per the cross-repo handler."},
                    "candidate_scan_rows": {"type": "integer", "description": "Number of raw candidate rows inspected before policy exclusions, per the cross-repo handler."},
                    "docstring_clip_bytes": {"type": "integer", "description": "Read-time docstring ceiling in bytes (512) applied to every row of this response; always present."},
                    "docstring_clipped_rows": {"type": "integer", "description": "Number of returned rows, suppressed rows included, whose docstring was clipped; 0 when none."},
                    "candidate_buckets": {
                      "type": "object",
                      "description": "Every row in every bucket has its docstring clipped at read time to docstring_clip_bytes; a clipped row carries docstring_clipped, docstring_clip_bytes, and docstring_total_bytes.",
                      "properties": {
                        "dead": {"type": "array", "description": "Producer candidates with no visible consumer evidence, per the cross-repo classifier.", "items": {"type": "object", "description": "One dead candidate row: the full candidate entity row plus the cross-repo evidence fields. crossRepoDeadCodeEvidenceMaps is the owner of the consumer_evidence item keys.",
                          "properties": {
                          "consumer_evidence": {"type": "array", "description": "The row's own consumer evidence: every item under evidence_detail full, or at most 5 groups (consumer_repo_id, relationship_type, evidence_family, confidence_label, item_count) under handles. Empty when consumer_evidence_source is repository_boundary: that evidence is the response's boundary_consumer_evidence.", "items": {"type": "object", "description": "One consumer evidence item (deadcode.CrossRepoDeadCodeEvidence). Under handles the item is a group with item_count instead of the full fields.",
                            "properties": {
                              "consumer_repo_id": {"type": "string", "description": "Consumer repository id that keeps the producer candidate live, per crossRepoDeadCodeEvidenceMaps."},
                              "consumer_repo_name": {"type": "string", "description": "Consumer repository name recorded by crossRepoDeadCodeEvidenceMaps."},
                              "consumer_entity_id": {"type": "string", "description": "Consumer entity id recorded by crossRepoDeadCodeEvidenceMaps."},
                              "relationship_type": {"type": "string", "description": "Reachability relationship type recorded by crossRepoDeadCodeEvidenceMaps."},
                              "evidence_family": {"type": "string", "description": "Evidence family recorded by crossRepoDeadCodeEvidenceMaps (defaults to code_reachability)."},
                              "citation": {"type": "string", "description": "Evidence citation recorded by crossRepoDeadCodeEvidenceMaps."},
                              "confidence": {"type": "number", "description": "Evidence confidence recorded by crossRepoDeadCodeEvidenceMaps."},
                              "confidence_label": {"type": "string", "description": "Confidence label derived by crossRepoDeadCodeEvidenceMaps when the row carries none."},
                              "resolution_method": {"type": "string", "description": "How the consumer was resolved, per crossRepoDeadCodeEvidenceMaps."},
                              "depth": {"type": "integer", "description": "Reachability depth recorded by crossRepoDeadCodeEvidenceMaps."},
                              "generation_id": {"type": "string", "description": "Consumer generation id recorded by crossRepoDeadCodeEvidenceMaps."},
                              "generation_status": {"type": "string", "description": "Consumer generation status recorded by crossRepoDeadCodeEvidenceMaps."},
                              "ambiguous": {"type": "boolean", "description": "Whether the evidence is ambiguous, per crossRepoDeadCodeEvidenceMaps."},
                              "needs_evidence": {"type": "boolean", "description": "Whether the evidence still needs corroboration, per crossRepoDeadCodeEvidenceMaps."},
                              "observed_at": {"type": "string", "format": "date-time", "description": "When the evidence was observed, per crossRepoDeadCodeEvidenceMaps; absent when the row carries no timestamp."},
                              "reason": {"type": "string", "description": "Why classification needs this evidence, per crossRepoDeadCodeEvidenceMaps; absent when empty."},
                              "item_count": {"type": "integer", "description": "Grouped evidence rows folded into this group, per the handles reducer; present only under evidence_detail handles."}
                            }}},
                          "consumer_evidence_source": {"type": "string", "enum": ["entity", "repository_boundary"], "description": "entity: consumer_evidence is this entity's own evidence (possibly none); hidden_consumer_evidence_count can still reflect hidden boundary relationships. repository_boundary: no entity-level evidence existed, so classification used the repository-level boundary evidence returned once in boundary_consumer_evidence."},
                          "consumer_evidence_count": {"type": "integer", "description": "Evidence items this row held before any grouping; 0 when consumer_evidence_source is repository_boundary."},
                          "consumer_evidence_group_count": {"type": "integer", "description": "Groups the row's items formed before the 5-group cap. Present only under evidence_detail handles."},
                          "consumer_evidence_handles_truncated": {"type": "boolean", "description": "True only when groups were cut at the cap; the strongest group is always kept. Present only under handles."}
                        },
                          "additionalProperties": true
                        }},
                        "live_by_consumer": {"type": "array", "description": "Producer candidates kept live by another repository's consumers, per the cross-repo classifier.", "items": {"type": "object", "description": "One live-by-consumer candidate row: the full candidate entity row plus the cross-repo evidence fields. crossRepoDeadCodeEvidenceMaps owns the consumer_evidence item keys.",
                          "properties": {
                          "test_only_consumers": {"type": "boolean", "description": "Present only when true: every consumer of this live row has its root entity in a test file (the same test-path rule dead-code uses everywhere), so removing the symbol also means removing or rewriting those tests. It never changes liveness: a test caller is still a caller. Absent when any consumer root is not a test file, a root has no content row, a consumer is hidden from the caller, a consumer repository's snapshot is incomplete, the request named consumer_repo_ids, or the row used the repository-boundary fallback. Same-repository dead-code routes do not carry it. A test method is a reachability root only for C#, Java, Kotlin, Scala, Rust and Swift, so there a test-only consumer is flagged; in Go, Python, JavaScript and TypeScript a test function is never a root, so the flag appears only when another root, such as a main or script entry point, sits in a test path."},
                          "consumer_evidence": {"type": "array", "description": "The row's own consumer evidence: every item under evidence_detail full, or at most 5 groups (consumer_repo_id, relationship_type, evidence_family, confidence_label, item_count) under handles. Empty when consumer_evidence_source is repository_boundary.", "items": {"type": "object", "description": "One consumer evidence item (deadcode.CrossRepoDeadCodeEvidence); a group with item_count under handles.",
                            "properties": {
                              "consumer_repo_id": {"type": "string", "description": "Consumer repository id, per crossRepoDeadCodeEvidenceMaps."},
                              "consumer_repo_name": {"type": "string", "description": "Consumer repository name, per crossRepoDeadCodeEvidenceMaps."},
                              "consumer_entity_id": {"type": "string", "description": "Consumer entity id, per crossRepoDeadCodeEvidenceMaps."},
                              "relationship_type": {"type": "string", "description": "Reachability relationship type, per crossRepoDeadCodeEvidenceMaps."},
                              "evidence_family": {"type": "string", "description": "Evidence family (defaults to code_reachability), per crossRepoDeadCodeEvidenceMaps."},
                              "citation": {"type": "string", "description": "Evidence citation, per crossRepoDeadCodeEvidenceMaps."},
                              "confidence": {"type": "number", "description": "Evidence confidence, per crossRepoDeadCodeEvidenceMaps."},
                              "confidence_label": {"type": "string", "description": "Confidence label derived when the row carries none, per crossRepoDeadCodeEvidenceMaps."},
                              "resolution_method": {"type": "string", "description": "How the consumer was resolved, per crossRepoDeadCodeEvidenceMaps."},
                              "depth": {"type": "integer", "description": "Reachability depth, per crossRepoDeadCodeEvidenceMaps."},
                              "generation_id": {"type": "string", "description": "Consumer generation id, per crossRepoDeadCodeEvidenceMaps."},
                              "generation_status": {"type": "string", "description": "Consumer generation status, per crossRepoDeadCodeEvidenceMaps."},
                              "ambiguous": {"type": "boolean", "description": "Whether the evidence is ambiguous, per crossRepoDeadCodeEvidenceMaps."},
                              "needs_evidence": {"type": "boolean", "description": "Whether the evidence still needs corroboration, per crossRepoDeadCodeEvidenceMaps."},
                              "observed_at": {"type": "string", "format": "date-time", "description": "When the evidence was observed; absent when the row carries no timestamp."},
                              "reason": {"type": "string", "description": "Why classification needs this evidence; absent when empty."},
                              "item_count": {"type": "integer", "description": "Grouped rows folded into this group; present only under evidence_detail handles."}
                            }}},
                          "consumer_evidence_source": {"type": "string", "enum": ["entity", "repository_boundary"], "description": "entity: consumer_evidence is this entity's own evidence (possibly none). repository_boundary: no entity-level evidence existed, so classification used the repository-level boundary evidence returned once in boundary_consumer_evidence."},
                          "consumer_evidence_count": {"type": "integer", "description": "Evidence items this row held before any grouping; 0 when consumer_evidence_source is repository_boundary."},
                          "consumer_evidence_group_count": {"type": "integer", "description": "Groups the row's items formed before the 5-group cap. Present only under evidence_detail handles."},
                          "consumer_evidence_handles_truncated": {"type": "boolean", "description": "True only when groups were cut at the cap; the strongest group is always kept. Present only under handles."}
                        },
                          "additionalProperties": true
                        }},
                        "unknown": {"type": "array", "description": "Producer candidates that cannot be judged yet (ambiguous ownership, stale generations, missing coverage, or scoped-token-hidden consumers), per the cross-repo classifier.", "items": {"type": "object", "description": "One unknown candidate row: the full candidate entity row plus the cross-repo evidence fields. crossRepoDeadCodeEvidenceMaps owns the consumer_evidence item keys.",
                          "properties": {
                          "consumer_evidence": {"type": "array", "description": "The row's own consumer evidence: every item under evidence_detail full, or at most 5 groups (consumer_repo_id, relationship_type, evidence_family, confidence_label, item_count) under handles. Empty when consumer_evidence_source is repository_boundary.", "items": {"type": "object", "description": "One consumer evidence item (deadcode.CrossRepoDeadCodeEvidence); a group with item_count under handles.",
                            "properties": {
                              "consumer_repo_id": {"type": "string", "description": "Consumer repository id, per crossRepoDeadCodeEvidenceMaps."},
                              "consumer_repo_name": {"type": "string", "description": "Consumer repository name, per crossRepoDeadCodeEvidenceMaps."},
                              "consumer_entity_id": {"type": "string", "description": "Consumer entity id, per crossRepoDeadCodeEvidenceMaps."},
                              "relationship_type": {"type": "string", "description": "Reachability relationship type, per crossRepoDeadCodeEvidenceMaps."},
                              "evidence_family": {"type": "string", "description": "Evidence family (defaults to code_reachability), per crossRepoDeadCodeEvidenceMaps."},
                              "citation": {"type": "string", "description": "Evidence citation, per crossRepoDeadCodeEvidenceMaps."},
                              "confidence": {"type": "number", "description": "Evidence confidence, per crossRepoDeadCodeEvidenceMaps."},
                              "confidence_label": {"type": "string", "description": "Confidence label derived when the row carries none, per crossRepoDeadCodeEvidenceMaps."},
                              "resolution_method": {"type": "string", "description": "How the consumer was resolved, per crossRepoDeadCodeEvidenceMaps."},
                              "depth": {"type": "integer", "description": "Reachability depth, per crossRepoDeadCodeEvidenceMaps."},
                              "generation_id": {"type": "string", "description": "Consumer generation id, per crossRepoDeadCodeEvidenceMaps."},
                              "generation_status": {"type": "string", "description": "Consumer generation status, per crossRepoDeadCodeEvidenceMaps."},
                              "ambiguous": {"type": "boolean", "description": "Whether the evidence is ambiguous, per crossRepoDeadCodeEvidenceMaps."},
                              "needs_evidence": {"type": "boolean", "description": "Whether the evidence still needs corroboration, per crossRepoDeadCodeEvidenceMaps."},
                              "observed_at": {"type": "string", "format": "date-time", "description": "When the evidence was observed; absent when the row carries no timestamp."},
                              "reason": {"type": "string", "description": "Why classification needs this evidence; absent when empty."},
                              "item_count": {"type": "integer", "description": "Grouped rows folded into this group; present only under evidence_detail handles."}
                            }}},
                          "consumer_evidence_source": {"type": "string", "enum": ["entity", "repository_boundary"], "description": "entity: consumer_evidence is this entity's own evidence (possibly none). repository_boundary: no entity-level evidence existed, so classification used the repository-level boundary evidence returned once in boundary_consumer_evidence."},
                          "consumer_evidence_count": {"type": "integer", "description": "Evidence items this row held before any grouping; 0 when consumer_evidence_source is repository_boundary."},
                          "consumer_evidence_group_count": {"type": "integer", "description": "Groups the row's items formed before the 5-group cap. Present only under evidence_detail handles."},
                          "consumer_evidence_handles_truncated": {"type": "boolean", "description": "True only when groups were cut at the cap; the strongest group is always kept. Present only under handles."}
                        },
                          "additionalProperties": true
                        }},
                        "suppressed": {"type": "array", "description": "Modeled-root examples suppressed from the buckets (partitionDeadCodeInvestigationResults), at most suppressed_limit rows.", "items": {"type": "object", "description": "One suppressed candidate row: the full candidate entity row plus classification, suppression_reasons, and a source handle. partitionDeadCodeInvestigationResults is the emitting owner.",
                          "properties": {
                            "classification": {"type": "string", "description": "Dead-code classification assigned by partitionDeadCodeInvestigationResults (excluded for suppressed rows)."},
                            "suppression_reasons": {"type": "array", "description": "Why the row was suppressed, per partitionDeadCodeInvestigationResults (a default-root-policy reason or user_decorator_exclusion).", "items": {"type": "string"}},
                            "source_handle": {"type": "object", "description": "Source locator attached by attachDeadCodeSourceHandle.", "additionalProperties": true}
                          },
                          "additionalProperties": true
                        }}
                      }
                    },
                    "boundary_consumer_evidence": {"type": "array", "description": "Repository-level boundary evidence after the request's consumer selector and grant. Returned once for every candidate row whose consumer_evidence_source is repository_boundary, instead of repeated on each row. Always present; empty (and the count 0) when no row used the fallback, including when the repository has no boundary relationships or none the caller may see.", "items": {"type": "object", "description": "One boundary evidence item (deadcode.CrossRepoDeadCodeEvidence) hoisted from filterCrossRepoDeadCodeEvidence. Under handles the five group keys with item_count 1, at most 25.",
                      "properties": {
                        "consumer_repo_id": {"type": "string", "description": "Consumer repository id, per crossRepoDeadCodeEvidenceMaps."},
                        "consumer_repo_name": {"type": "string", "description": "Consumer repository name, per crossRepoDeadCodeEvidenceMaps."},
                        "consumer_entity_id": {"type": "string", "description": "Consumer entity id, per crossRepoDeadCodeEvidenceMaps."},
                        "relationship_type": {"type": "string", "description": "Reachability relationship type, per crossRepoDeadCodeEvidenceMaps."},
                        "evidence_family": {"type": "string", "description": "Evidence family, per crossRepoDeadCodeEvidenceMaps."},
                        "citation": {"type": "string", "description": "Evidence citation, per crossRepoDeadCodeEvidenceMaps."},
                        "confidence": {"type": "number", "description": "Evidence confidence, per crossRepoDeadCodeEvidenceMaps."},
                        "confidence_label": {"type": "string", "description": "Confidence label, per crossRepoDeadCodeEvidenceMaps."},
                        "resolution_method": {"type": "string", "description": "How the consumer was resolved, per crossRepoDeadCodeEvidenceMaps."},
                        "depth": {"type": "integer", "description": "Reachability depth, per crossRepoDeadCodeEvidenceMaps."},
                        "generation_id": {"type": "string", "description": "Consumer generation id, per crossRepoDeadCodeEvidenceMaps."},
                        "generation_status": {"type": "string", "description": "Consumer generation status, per crossRepoDeadCodeEvidenceMaps."},
                        "ambiguous": {"type": "boolean", "description": "Whether the evidence is ambiguous, per crossRepoDeadCodeEvidenceMaps."},
                        "needs_evidence": {"type": "boolean", "description": "Whether the evidence still needs corroboration, per crossRepoDeadCodeEvidenceMaps."},
                        "observed_at": {"type": "string", "format": "date-time", "description": "When the evidence was observed; absent when the row carries no timestamp."},
                        "reason": {"type": "string", "description": "Why classification needs this evidence; absent when empty."},
                        "item_count": {"type": "integer", "description": "Always 1 under evidence_detail handles; absent under full."}
                      }}},
                    "boundary_consumer_evidence_count": {"type": "integer", "description": "Total boundary items; 0 when the list is not emitted. Under handles boundary_consumer_evidence holds at most 25 of them."},
                    "boundary_consumer_evidence_truncated": {"type": "boolean", "description": "True only when evidence_detail handles cut the boundary list at 25."},
                    "consumer_coverage": {"type": "object", "description": "Whether any consumer repository this answer is judged against is a coverage gap (#7547). Omitted when the check produced no answer: no candidate needed classifying, the evidence read was unavailable, or the store cannot answer the check. A candidate with no strong live consumer evidence is dead only when complete is true; otherwise it is unknown_needs_evidence with the reason consumer_coverage_incomplete.", "properties": {
                      "complete": {"type": "boolean", "description": "True when no covered consumer repository is a gap. A repository is a gap when its active generation has code_calls or inheritance_edges work (completed or pending) and no reachability watermark, a truncated one, or one with a verdict_schema_epoch below the current epoch; a repository with no such work cannot be a consumer and is complete. A stale or partly drained snapshot with truncated false, and a zero-root repository before the writer stamps it truncated, read as complete. Name consumer_repo_ids to narrow an unscoped request."},
                      "incomplete_repo_ids": {"type": "array", "items": {"type": "string"}, "description": "Consumer repositories that are gaps: ones the request named, ones in the caller's grant, or, for an unscoped caller, any repository. At most 25, always returned sorted. A request that names consumers or a grant returns the lowest-sorting 25; for an unscoped request with no named consumers the statement stops at the first gaps with no ordering, so with more than 25 gaps which ids come back is an arbitrary subset."},
                      "incomplete_truncated": {"type": "boolean", "description": "True when incomplete_repo_ids and incomplete were cut at their cap."},
                      "coverage_summary": {"type": "string", "description": "One plain sentence over the listed gaps, with the next move. It counts only what is listed: the check stops at its cap and never counts the repositories it checked, so a cut list says 'at least N'. Text derived from the gaps and from whether the request named consumer_repo_ids; no extra data. A request that named them is told to check whether the repositories' framework entry points are modeled, not to name them again. Advice to wait is a hint, not a promise."},
                      "retryable": {"type": "boolean", "description": "A hint, not a promise, that a snapshot is expected to appear or refresh without action for every listed gap: true only when every listed gap is retryable and the list was not cut (incomplete_truncated false), because a cut list hides gaps that may not be retryable. It can stay true for a long time when an active generation is a delta generation or a full generation whose reducer work did not complete. False when complete is true: there is nothing to wait for."},
                      "incomplete": {"type": "array", "description": "The same repositories as incomplete_repo_ids, in the same order, with why each is a gap and whether a snapshot is expected without action (retryable).", "items": {"type": "object", "description": "One incomplete repository gap. Owner: cross-repo dead-code handler.", "required": ["repository_id", "state", "retryable"], "properties": {
                        "repository_id": {"type": "string", "description": "Gap consumer repository id, per the consumer-coverage check."},
                        "state": {"type": "string", "enum": ["no_snapshot_yet", "truncated", "older_epoch", "no_active_scope"], "description": "no_snapshot_yet: the active generation has code edges but no reachability watermark for it; a snapshot is expected. older_epoch: the snapshot was built under an older verdict schema epoch and a refresh is expected. truncated: the snapshot is current but cannot prove absence (zero roots or a depth cutoff); a rebuild does not change that. no_active_scope: a repository the request named has no active repository scope, so nothing is being built. The first two can persist when the active generation is a delta generation or a full generation whose reducer work did not complete. One watermark that is both truncated and older-epoch is reported truncated."},
                        "generation_id": {"type": "string", "description": "The repository scope's active generation: the snapshot expected for it. Omitted for no_active_scope. A repository with several gap scopes reports one: a truncated scope first, then the lowest generation id."},
                        "reason": {"type": "string", "description": "Why this repository cannot be judged yet, in plain words. One wording per state. Omitted for a state this version does not know."},
                        "next_step": {"type": "string", "description": "What an admin can do about it, in plain words. Derived from state, and from whether the request named consumer_repo_ids: a request that did is not told to name them again. Advice to wait is a hint, not a promise (see retryable). Omitted for a state this version does not know."},
                        "retryable": {"type": "boolean", "description": "A hint, not a promise: true for no_snapshot_yet and older_epoch (a snapshot is expected to appear or refresh without action; it can stay true for a long time when the active generation is a delta generation or a full generation whose reducer work did not complete); false for truncated and no_active_scope."}
                      }}}
                    }},
                    "evidence_detail": {"type": "string", "enum": ["full", "handles"], "description": "The detail mode this response used."},
                    "evidence_detail_drilldown": {"type": "object", "additionalProperties": {"type": "string"}, "description": "Present only when handles reduced something: how to get the full rows."},
                    "bucket_counts": {"type": "object", "additionalProperties": {"type": "integer"}, "description": "Candidate counts by bucket (dead, live_by_consumer, unknown, suppressed) emitted by the cross-repo handler; keys are bucket names."},
                    "analysis": {"type": "object", "additionalProperties": true, "description": "Policy analysis for the classification emitted by the cross-repo handler; keys are analysis field names."}
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
