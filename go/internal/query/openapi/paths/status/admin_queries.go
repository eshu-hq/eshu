// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package status

// AdminQueries is the OpenAPI path fragment documenting the admin query and
// mutation routes for fact work items, decisions, dead letters, replay,
// reopen, backfill, and replay events. It was split out of admin.go once
// that file approached the 500-line cap, per this leaf's AGENTS.md
// precedent (compare.go). openapi.Spec concatenates it into the published
// document; keep it in lockstep with the handlers and HTTP API reference.
const AdminQueries = `
    "/api/v0/admin/work-items/query": {
      "post": {
        "tags": ["admin"],
        "summary": "Query fact work items",
        "description": "A 503 with Retry-After means a transient condition that is safe to retry: the read hit a stale or timed-out PostgreSQL reader, or the request checkpoint step failed; a 503 without Retry-After means a configuration state: the admin store or the checkpoint source is not configured on this API instance. No 503 body carries backend error text. The read runs with no route deadline, so it never answers 504. A reader-verdict 503 names capability operator.work_items.list in its error envelope.",
        "requestBody": {
          "required": true,
          "content": {
            "application/json": {
              "schema": {
                "type": "object",
                "description": "Work-item list filters; every filter is optional. Owner: admin.listWorkItems.",
                "properties": {
                  "statuses": {"type": "array", "items": {"type": "string"}, "description": "Return only work items in these statuses, per admin.listWorkItems."},
                  "scope_id": {"type": "string", "description": "Return only work items for this scope, per admin.listWorkItems."},
                  "stage": {"type": "string", "description": "Return only work items in this stage, per admin.listWorkItems."},
                  "failure_class": {"type": "string", "description": "Return only work items in this failure class, per admin.listWorkItems."},
                  "limit": {"type": "integer", "description": "Maximum rows to return; defaults to 100 when absent or non-positive, per admin.listWorkItems."}
                }
              }
            }
          }
        },
        "responses": {
          "200": {"description": "Admin work-item query results. Each item carries replay_attempt_baseline: the attempt_count a projector row had at its last operator replay, or 0 when it was never replayed (#11182).", "content": {"application/json": {"schema": {"type": "object", "description": "Admin work-item query results. Owner: admin.listWorkItems.", "required": ["count", "items"], "properties": {"count": {"type": "integer", "description": "Rows returned. Owner: admin.listWorkItems."}, "items": {"type": "array", "description": "Work-item rows. Owner: admin.listWorkItems via workItemsToSlice.", "items": ` + adminWorkItemSchema + `}}}}}},
          "400": {"$ref": "#/components/responses/BadRequest"},
          "500": {"$ref": "#/components/responses/InternalError"},
          "503": {"$ref": "#/components/responses/ServiceUnavailable"}
        }
      }
    },
    "/api/v0/admin/decisions/query": {
      "post": {
        "tags": ["admin"],
        "summary": "Query projection decisions",
        "description": "A 503 with Retry-After means a transient condition that is safe to retry: the read hit a stale or timed-out PostgreSQL reader, or the request checkpoint step failed; a 503 without Retry-After means a configuration state: the admin store or the checkpoint source is not configured on this API instance. No 503 body carries backend error text. The read runs with no route deadline, so it never answers 504. A reader-verdict 503 names capability operator.decisions.list, or operator.decision_evidence.list for the include_evidence read in its error envelope.",
        "requestBody": {
          "required": true,
          "content": {
            "application/json": {
              "schema": {
                "type": "object",
                "description": "Decision list selectors. Owner: admin.listDecisions.",
                "required": ["repository_id", "source_run_id"],
                "properties": {
                  "repository_id": {"type": "string", "description": "Repository to list decisions for, per admin.listDecisions; required."},
                  "source_run_id": {"type": "string", "description": "Source run to list decisions for, per admin.listDecisions; required."},
                  "decision_type": {"type": "string", "description": "Return only decisions of this type, per admin.listDecisions."},
                  "include_evidence": {"type": "boolean", "description": "Include per-decision evidence rows, per admin.listDecisions."},
                  "limit": {"type": "integer", "description": "Maximum rows to return; defaults to 100 when absent or non-positive, per admin.listDecisions."}
                }
              }
            }
          }
        },
        "responses": {
          "200": {"description": "Projection decision query results", "content": {"application/json": {"schema": {"type": "object", "description": "Projection decision query results. Owner: admin.listDecisions.", "required": ["count", "decisions"], "properties": {"count": {"type": "integer", "description": "Decisions returned. Owner: admin.listDecisions."}, "decisions": {"type": "array", "description": "Decision rows, with evidence only when include_evidence is set. Owner: admin.listDecisions.", "items": ` + adminDecisionSchema + `}}}}}},
          "400": {"$ref": "#/components/responses/BadRequest"},
          "500": {"$ref": "#/components/responses/InternalError"},
          "503": {"$ref": "#/components/responses/ServiceUnavailable"}
        }
      }
    },
    "/api/v0/admin/dead-letters/query": {
      "post": {
        "tags": ["admin"],
        "summary": "Query dead-letter work items",
        "description": "Returns a bounded deterministic page of durable fact_work_items dead letters. Requires limit and timeout_ms, supports failure_class, domain, scope_id, collector_kind, and updated_at window filters, and returns truncated=true when more rows matched than the requested limit. Scoped tokens are restricted to their granted component scopes. A 503 with Retry-After means a transient condition that is safe to retry: the read hit a stale or timed-out PostgreSQL reader, or the request checkpoint step failed; a 503 without Retry-After means a configuration state: the admin store or the checkpoint source is not configured on this API instance. No 503 body carries backend error text. A reader-verdict 503 names capability operator.dead_letters.list in its error envelope.",
        "x-scoped-token-support": true,
        "requestBody": {
          "required": true,
          "content": {
            "application/json": {
              "schema": {
                "type": "object",
                "required": ["limit", "timeout_ms"],
                "properties": {
                  "failure_class": {"type": "string"},
                  "domain": {"type": "string"},
                  "scope_id": {"type": "string"},
                  "collector_kind": {"type": "string"},
                  "updated_after": {"type": "string", "format": "date-time"},
                  "updated_before": {"type": "string", "format": "date-time"},
                  "limit": {"type": "integer", "minimum": 1, "maximum": 500},
                  "timeout_ms": {"type": "integer", "minimum": 1, "maximum": 30000}
                }
              }
            }
          }
        },
        "responses": {
          "200": {"description": "Bounded dead-letter page with schema_version, limit, count, truncated, and items", "content": {"application/json": {"schema": {"type": "object", "description": "Bounded dead-letter page. Owner: admin.writeDeadLetterList.", "required": ["schema_version", "limit", "count", "truncated", "items"], "properties": {"schema_version": {"type": "string", "description": "Page schema version. Owner: admin.deadLetterListSchemaVersion."}, "limit": {"type": "integer", "description": "Requested page limit. Owner: admin.listDeadLetters."}, "count": {"type": "integer", "description": "Rows on this page. Owner: admin.writeDeadLetterList."}, "truncated": {"type": "boolean", "description": "True when more rows matched than the limit. Owner: admin.listDeadLetters."}, "items": {"type": "array", "description": "Dead-letter rows. Owner: admin.deadLetterItemsToSlice.", "items": ` + adminDeadLetterItemSchema + `}}}}}},
          "400": {"$ref": "#/components/responses/BadRequest"},
          "403": {"$ref": "#/components/responses/Forbidden"},
          "504": {"description": "Dead-letter query exceeded its timeout_ms budget while the read ran (fixed message dead-letter query timed out). If the budget expires during reader setup instead, the status depends on the reader topology: a single PostgreSQL reader answers the retryable 503 reader verdict; with reader fleet members, a budget that expires while the reader waits for a member slot or between member attempts answers this 504, and one that expires inside a member attempt answers the retryable 503. Keep timeout_ms well above reader setup latency."},
          "500": {"$ref": "#/components/responses/InternalError"},
          "503": {"$ref": "#/components/responses/ServiceUnavailable"}
        }
      }
    },
    "/api/v0/admin/input-invalid-facts/query": {
      "post": {
        "tags": ["admin"],
        "summary": "Query durable input_invalid quarantine facts",
        "description": "Returns a bounded deterministic page of durable reducer_input_invalid_facts rows (issue #4630): facts the reducer quarantined during typed-payload decode because a required field was missing or null. Requires scope_id, generation_id, limit, and timeout_ms, supports optional domain and fact_kind filters, and returns truncated=true when more rows matched than the requested limit. Scoped tokens are restricted to their granted component scopes. A 503 with Retry-After means a transient condition that is safe to retry: the read hit a stale or timed-out PostgreSQL reader, or the request checkpoint step failed; a 503 without Retry-After means a configuration state: the admin store or the checkpoint source is not configured on this API instance. No 503 body carries backend error text. A reader-verdict 503 names capability operator.reducer_input_invalid_facts.list in its error envelope.",
        "x-scoped-token-support": true,
        "requestBody": {
          "required": true,
          "content": {
            "application/json": {
              "schema": {
                "type": "object",
                "required": ["scope_id", "generation_id", "limit", "timeout_ms"],
                "properties": {
                  "scope_id": {"type": "string"},
                  "generation_id": {"type": "string"},
                  "domain": {"type": "string"},
                  "fact_kind": {"type": "string"},
                  "limit": {"type": "integer", "minimum": 1, "maximum": 500},
                  "timeout_ms": {"type": "integer", "minimum": 1, "maximum": 30000}
                }
              }
            }
          }
        },
        "responses": {
          "200": {"description": "Bounded input_invalid quarantine page with schema_version, limit, count, truncated, and items", "content": {"application/json": {"schema": {"type": "object", "description": "Bounded input_invalid quarantine page. Owner: admin.writeInputInvalidFactList.", "required": ["schema_version", "limit", "count", "truncated", "items"], "properties": {"schema_version": {"type": "string", "description": "Page schema version. Owner: admin.inputInvalidFactListSchemaVersion."}, "limit": {"type": "integer", "description": "Requested page limit. Owner: admin.listInputInvalidFacts."}, "count": {"type": "integer", "description": "Rows on this page. Owner: admin.writeInputInvalidFactList."}, "truncated": {"type": "boolean", "description": "True when more rows matched than the limit. Owner: admin.listInputInvalidFacts."}, "items": {"type": "array", "description": "Quarantined fact rows. Owner: admin.nonNilInputInvalidFacts.", "items": ` + adminInputInvalidFactSchema + `}}}}}},
          "400": {"$ref": "#/components/responses/BadRequest"},
          "403": {"$ref": "#/components/responses/Forbidden"},
          "504": {"description": "Input-invalid-facts query exceeded its timeout_ms budget while the read ran (fixed message input-invalid-facts query timed out). If the budget expires during reader setup instead, the status depends on the reader topology: a single PostgreSQL reader answers the retryable 503 reader verdict; with reader fleet members, a budget that expires while the reader waits for a member slot or between member attempts answers this 504, and one that expires inside a member attempt answers the retryable 503. Keep timeout_ms well above reader setup latency."},
          "500": {"$ref": "#/components/responses/InternalError"},
          "503": {"$ref": "#/components/responses/ServiceUnavailable"}
        }
      }
    },
    "/api/v0/admin/changed-since/poisoned-links/query": {
      "post": {
        "tags": ["admin"],
        "summary": "Query poisoned and retrying changed-since links",
        "description": "Returns a bounded deterministic page of durable changed_since_scope_cursor rows that are poisoned (the link hit its counting-failure limit, #7127 ruling 8.10) or retrying (a counted failure is pending on the scope's head activation). The changed-since link writer (go/internal/storage/postgres/freshness/links) is a runner over its own ledger, not a fact_work_items queue domain, so this state never appears in the dead-letter or reducer_input_invalid_facts surfaces (#7290). Requires limit and timeout_ms; supports optional status (poisoned|retrying), scope_id, and a forward keyset cursor (the last scope_id of a prior page). Returns truncated=true and next_cursor when more rows matched than the requested limit. Scoped tokens are restricted to their granted component scopes. A 503 with Retry-After means a transient condition that is safe to retry: the read hit a stale or timed-out PostgreSQL reader, or the request checkpoint step failed; a 503 without Retry-After means a configuration state: the admin store or the checkpoint source is not configured on this API instance. No 503 body carries backend error text. A reader-verdict 503 names capability operator.changed_since_poisoned_links.list in its error envelope.",
        "x-scoped-token-support": true,
        "requestBody": {
          "required": true,
          "content": {
            "application/json": {
              "schema": {
                "type": "object",
                "required": ["limit", "timeout_ms"],
                "properties": {
                  "status": {"type": "string", "enum": ["poisoned", "retrying"]},
                  "scope_id": {"type": "string"},
                  "cursor": {"type": "string", "description": "The last scope_id of a prior page's items; returns rows with a greater scope_id."},
                  "limit": {"type": "integer", "minimum": 1, "maximum": 500},
                  "timeout_ms": {"type": "integer", "minimum": 1, "maximum": 30000}
                }
              }
            }
          }
        },
        "responses": {
          "200": {"description": "Bounded changed-since poisoned/retrying link page with schema_version, limit, count, truncated, next_cursor, and items", "content": {"application/json": {"schema": {"type": "object", "description": "Bounded poisoned/retrying link page. Owner: admin.writeChangedSincePoisonedLinkList.", "required": ["schema_version", "limit", "count", "truncated", "items"], "properties": {"schema_version": {"type": "string", "description": "Page schema version. Owner: admin.changedSincePoisonedLinkSchemaVersion."}, "limit": {"type": "integer", "description": "Requested page limit. Owner: admin.listChangedSincePoisonedLinks."}, "count": {"type": "integer", "description": "Rows on this page. Owner: admin.writeChangedSincePoisonedLinkList."}, "truncated": {"type": "boolean", "description": "True when more rows matched than the limit. Owner: admin.listChangedSincePoisonedLinks."}, "next_cursor": {"type": "string", "description": "Forward keyset cursor: the last scope_id of this page; present only on a truncated page. Owner: admin.writeChangedSincePoisonedLinkList."}, "items": {"type": "array", "description": "Poisoned or retrying link rows. Owner: admin.nonNilChangedSincePoisonedLinks.", "items": ` + adminPoisonedLinkSchema + `}}}}}},
          "400": {"$ref": "#/components/responses/BadRequest"},
          "403": {"$ref": "#/components/responses/Forbidden"},
          "504": {"description": "Changed-since poisoned-links query exceeded its timeout_ms budget while the read ran (fixed message changed-since poisoned-links query timed out). If the budget expires during reader setup instead, the status depends on the reader topology: a single PostgreSQL reader answers the retryable 503 reader verdict; with reader fleet members, a budget that expires while the reader waits for a member slot or between member attempts answers this 504, and one that expires inside a member attempt answers the retryable 503. Keep timeout_ms well above reader setup latency."},
          "500": {"$ref": "#/components/responses/InternalError"},
          "503": {"$ref": "#/components/responses/ServiceUnavailable"}
        }
      }
    },
    "/api/v0/admin/dead-letter": {
      "post": {
        "tags": ["admin"],
        "summary": "Dead-letter work items",
        "description": "A 503 with Retry-After means the request checkpoint step failed, a transient condition that is safe to retry; a 503 without Retry-After means a configuration state: the admin store or the checkpoint source is not configured on this API instance. No 503 body carries backend error text. At least one selector is required: work_item_ids, scope_id, or stage.",
        "requestBody": {
          "required": true,
          "content": {
            "application/json": {
              "schema": {
                "type": "object",
                "description": "Dead-letter selectors; at least one of work_item_ids, scope_id, or stage is required. Owner: admin.deadLetter.",
                "properties": {
                  "work_item_ids": {"type": "array", "items": {"type": "string"}, "description": "Work items to dead-letter, per admin.deadLetter."},
                  "scope_id": {"type": "string", "description": "Dead-letter every matching work item in this scope, per admin.deadLetter."},
                  "stage": {"type": "string", "description": "Dead-letter every matching work item in this stage, per admin.deadLetter."},
                  "failure_class": {"type": "string", "description": "Dead-letter only work items in this failure class, per admin.deadLetter."},
                  "operator_note": {"type": "string", "description": "Operator note recorded with the transition, per admin.deadLetter."},
                  "limit": {"type": "integer", "description": "Maximum rows to transition; defaults to 100 when absent or non-positive, per admin.deadLetter."}
                }
              }
            }
          }
        },
        "responses": {
          "200": {"description": "Dead-letter request results", "content": {"application/json": {"schema": {"type": "object", "description": "Dead-letter mutation results. Owner: admin.deadLetter.", "required": ["count", "items"], "properties": {"count": {"type": "integer", "description": "Rows transitioned by this call. Owner: admin.deadLetter."}, "items": {"type": "array", "description": "Transitioned work-item rows. Owner: admin.deadLetter via workItemsToSlice.", "items": ` + adminWorkItemSchema + `}}}}}},
          "400": {"$ref": "#/components/responses/BadRequest"},
          "500": {"$ref": "#/components/responses/InternalError"},
          "503": {"$ref": "#/components/responses/ServiceUnavailable"}
        }
      }
    },
    "/api/v0/admin/skip": {
      "post": {
        "tags": ["admin"],
        "summary": "Skip repository work items",
        "description": "Dead-letters at most 100 pending, retrying, or failed work items for a repository or scope. Claimed, running, succeeded, superseded, and already dead-lettered items are left unchanged. The response count includes only rows transitioned by this call. The repository_id accepts a scope id or a source key; a selector matching more than one scope fails closed with 409, naming the matched scopes. A 503 with Retry-After means the request checkpoint step failed, a transient condition that is safe to retry; a 503 without Retry-After means a configuration state: the admin store or the checkpoint source is not configured on this API instance. No 503 body carries backend error text.",
        "requestBody": {
          "required": true,
          "content": {
            "application/json": {
              "schema": {
                "type": "object",
                "description": "Skip selectors. Owner: admin.skip.",
                "required": ["repository_id"],
                "properties": {
                  "repository_id": {"type": "string", "description": "Repository, scope id, or source key whose work items are skipped, per admin.skip; required and must not be empty."},
                  "operator_note": {"type": "string", "description": "Operator note recorded with the transition, per admin.skip."}
                }
              }
            }
          }
        },
        "responses": {
          "200": {"description": "Skip request results", "content": {"application/json": {"schema": {"type": "object", "description": "Skip mutation results. Owner: admin.skip.", "required": ["count", "items"], "properties": {"count": {"type": "integer", "description": "Rows transitioned by this call. Owner: admin.skip."}, "items": {"type": "array", "description": "Transitioned work-item rows. Owner: admin.skip via workItemsToSlice.", "items": ` + adminWorkItemSchema + `}}}}}},
          "400": {"$ref": "#/components/responses/BadRequest"},
          "409": {"$ref": "#/components/responses/Conflict"},
          "500": {"$ref": "#/components/responses/InternalError"},
          "503": {"$ref": "#/components/responses/ServiceUnavailable"}
        }
      }
    },
    "/api/v0/admin/replay": {
      "post": {
        "tags": ["admin"],
        "summary": "Replay failed work items",
        "description": "Safely replays terminal work items. Requires an explicit reason and idempotency_key, an admin (all-scopes) token, and refuses unsafe failure classes (input_invalid, unsafe_payload, and the manual-review dead-letter triage classes projection_bug and resource_exhausted) unless force is set, whether they arrive as the failure_class selector or as explicit work_item_ids. A 200 with replayed_count 0 means nothing matched. Duplicate delivery of the same idempotency_key returns the prior outcome instead of replaying again. A replayed projector row gets the retry allowance of a new row (#11182): the replay keeps attempt_count (the claim fence, never lowered), sets replay_attempt_baseline to it, and resets admission_wait_attempts to 0, so the row gets 3 projection claims, counted as attempt_count - admission_wait_attempts - replay_attempt_baseline (2 retries; the third failure dead-letters it again), and 48 admission waits (47 retries; the 48th wait dead-letters it again); each returned item carries replay_attempt_baseline. A 503 with Retry-After means the request checkpoint step failed, a transient condition that is safe to retry; a 503 without Retry-After means a configuration state: the admin store or the checkpoint source is not configured on this API instance. No 503 body carries backend error text.",
        "requestBody": {
          "required": true,
          "content": {
            "application/json": {
              "schema": {
                "type": "object",
                "required": ["reason", "idempotency_key"],
                "properties": {
                  "work_item_ids": {"type": "array", "items": {"type": "string"}},
                  "scope_id": {"type": "string"},
                  "stage": {"type": "string"},
                  "failure_class": {"type": "string"},
                  "operator_note": {"type": "string"},
                  "reason": {"type": "string", "description": "Why the replay is safe."},
                  "idempotency_key": {"type": "string", "description": "Makes the replay safe under retries and concurrent delivery."},
                  "force": {"type": "boolean", "description": "Replay unsafe failure classes after addressing the cause."},
                  "limit": {"type": "integer"}
                }
              }
            }
          }
        },
        "responses": {
          "200": {"description": "Replay request results (duplicate=true when an idempotent prior outcome is returned)", "content": {"application/json": {"schema": {"description": "Replay outcome: performed by this call, or the prior outcome replayed. Owner: admin.replay and admin.respondDuplicateReplay.", "oneOf": [{"type": "object", "description": "Replay performed by this call. Owner: admin.replay.", "required": ["status", "replayed_count", "work_item_ids", "replayed", "idempotency_key", "duplicate"], "properties": {"status": {"type": "string", "enum": ["replayed"], "description": "Replay disposition. Owner: admin.replay."}, "replayed_count": {"type": "integer", "description": "Work items replayed. Owner: admin.replay."}, "work_item_ids": {"type": "array", "items": {"type": "string"}, "description": "Replayed work-item ids. Owner: admin.replay via workItemIDList."}, "replayed": {"type": "array", "description": "Replayed work-item rows. Owner: admin.replay via workItemsToSlice.", "items": ` + adminWorkItemSchema + `}, "idempotency_key": {"type": "string", "description": "Echo of the request key. Owner: admin.replay via IdempotencyKey."}, "duplicate": {"type": "boolean", "enum": [false], "description": "False: this call performed the replay. Owner: admin.replay."}}}, {"type": "object", "description": "Idempotent replay: this key already completed. Owner: admin.respondDuplicateReplay.", "required": ["status", "replayed_count", "work_item_ids", "idempotency_key", "duplicate"], "properties": {"status": {"type": "string", "enum": ["replayed"], "description": "Replay disposition. Owner: admin.respondDuplicateReplay."}, "replayed_count": {"type": "integer", "description": "Work items the original replay moved. Owner: admin.respondDuplicateReplay via ReplayedCount."}, "work_item_ids": {"type": "array", "items": {"type": "string"}, "description": "Work-item ids the original replay moved. Owner: admin.respondDuplicateReplay via WorkItemIDs."}, "idempotency_key": {"type": "string", "description": "Echo of the request key. Owner: admin.respondDuplicateReplay via IdempotencyKey."}, "duplicate": {"type": "boolean", "enum": [true], "description": "True: the prior outcome was replayed. Owner: admin.respondDuplicateReplay."}}}]}}}},
          "400": {"$ref": "#/components/responses/BadRequest"},
          "403": {"description": "Replay requires an admin (all-scopes) token"},
          "409": {"description": "Idempotency key already in progress or reused with different parameters"},
          "422": {
            "description": "Refused without force: the failure_class selector, or at least one explicit work_item_ids row, is in an unsafe or manual-review failure class. Also refused, even with force: at least one explicit work_item_ids row is a projector row whose scope generation is superseded (failure_class projector_replay_generation_superseded, #7130). A request naming such ids is refused whole, nothing is replayed, the idempotency_key is not consumed, and refused_work_items lists only the offending ids.",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "description": "Replay refusal. Owner: admin.replay, admin.refuseUnsafeExplicitReplay, and admin.refuseSupersededExplicitReplay.",
                  "required": ["status", "reason", "detail"],
                  "properties": {
                    "status": {"type": "string", "enum": ["refused"], "description": "Refusal disposition. Owner: admin.replay."},
                    "reason": {"type": "string", "description": "Machine-readable refusal reason. Owner: admin.replay and admin.refuseUnsafeExplicitReplay."},
                    "detail": {"type": "string", "description": "Operator guidance for the refusal. Owner: admin.replay and admin.refuseUnsafeExplicitReplay."},
                    "failure_class": {"type": "string", "description": "Present when the failure_class selector itself was refused. Owner: admin.replay."},
                    "refused_work_items": {
                      "type": "array",
                      "description": "Present when explicit work_item_ids were refused; sorted by work_item_id. Owner: admin.refuseUnsafeExplicitReplay and admin.refuseSupersededExplicitReplay.",
                      "items": {
                        "type": "object",
                        "description": "One refused work item. Owner: admin.refuseUnsafeExplicitReplay and admin.refuseSupersededExplicitReplay.",
                        "required": ["work_item_id", "failure_class", "reason"],
                        "properties": {
                          "work_item_id": {"type": "string", "description": "Refused work-item id. Owner: admin.refuseUnsafeExplicitReplay."},
                          "failure_class": {"type": "string", "description": "Refused failure class; the superseded-generation class on superseded rows. Owner: admin.refuseUnsafeExplicitReplay and admin.refuseSupersededExplicitReplay."},
                          "generation_id": {"type": "string", "description": "Present for a superseded-generation refusal: the superseded generation the row belongs to. Owner: admin.refuseSupersededExplicitReplay."},
                          "reason": {"type": "string", "description": "Operator guidance for the class. Owner: admin.refuseUnsafeExplicitReplay and admin.refuseSupersededExplicitReplay."}
                        }
                      }
                    }
                  }
                }
              }
            }
          },
          "500": {"$ref": "#/components/responses/InternalError"},
          "503": {"$ref": "#/components/responses/ServiceUnavailable"}
        }
      }
    },
    "/api/v0/admin/reopen": {
      "post": {
        "tags": ["admin"],
        "summary": "Reopen completed work items",
        "description": "Reopens completed reducer or shared-projection work for one domain and scope, the admin surface for the #7285 hand-SQL repair. Requires an explicit domain, scope_id, reason and idempotency_key, and an admin (all-scopes) token. Only repo_dependency (one completed intent row per acceptance unit, at the accepted source run), workload_materialization and submodule_pin (succeeded reducer rows) reopen; every other domain is refused. The store resolves the scope's active generation at run time and locks with SKIP LOCKED under a lock_timeout. A 200 with reopened_total_count 0 means nothing matched. Duplicate delivery of the same idempotency_key returns the prior outcome (duplicate=true, totals without the reducer/intent split) instead of reopening again. A scope_id matching more than one scope fails closed with 409, naming the matched scopes. A 503 with Retry-After means the request checkpoint step failed, a transient condition that is safe to retry; a 503 without Retry-After means a configuration state: the admin store or the checkpoint source is not configured on this API instance. No 503 body carries backend error text.",
        "requestBody": {
          "required": true,
          "content": {
            "application/json": {
              "schema": {
                "type": "object",
                "required": ["domain", "scope_id", "reason", "idempotency_key"],
                "properties": {
                  "domain": {"type": "string", "enum": ["repo_dependency", "workload_materialization", "submodule_pin"], "description": "Which completed work to reopen."},
                  "scope_id": {"type": "string", "description": "Ingestion scope id or source key."},
                  "reason": {"type": "string", "description": "Why the reopen is safe."},
                  "idempotency_key": {"type": "string", "description": "Makes the reopen safe under retries and concurrent delivery."},
                  "limit": {"type": "integer", "description": "Maximum rows to reopen. Defaults to 1000; larger values are clamped to 1000."}
                }
              }
            }
          }
        },
        "responses": {
          "200": {"description": "Reopen request results (duplicate=true when an idempotent prior outcome is returned)", "content": {"application/json": {"schema": {"description": "Reopen outcome: performed by this call with the reducer/intent split, or the prior totals replayed. Owner: admin.reopen and admin.respondDuplicateReopen.", "oneOf": [{"type": "object", "description": "Reopen performed by this call. Owner: admin.reopen.", "required": ["status", "domain", "scope_id", "generation_id", "reopened_reducer_count", "reopened_reducer_ids", "reopened_intent_count", "reopened_intent_ids", "reopened_units", "reopened_total_count", "idempotency_key", "duplicate"], "properties": {"status": {"type": "string", "enum": ["reopened"], "description": "Reopen disposition. Owner: admin.reopen."}, "domain": {"type": "string", "enum": ["repo_dependency", "workload_materialization", "submodule_pin"], "description": "Reopened domain. Owner: admin.reopen via Domain."}, "scope_id": {"type": "string", "description": "Reopened scope id. Owner: admin.reopen via ScopeID."}, "generation_id": {"type": "string", "description": "Active generation work was reopened for. Owner: admin.reopen via GenerationID."}, "reopened_reducer_count": {"type": "integer", "description": "Reducer rows reopened. Owner: admin.reopen."}, "reopened_reducer_ids": {"type": "array", "items": {"type": "string"}, "description": "Reopened reducer work-item ids. Owner: admin.reopen via orEmptyStrings."}, "reopened_intent_count": {"type": "integer", "description": "Shared-projection intents reopened. Owner: admin.reopen."}, "reopened_intent_ids": {"type": "array", "items": {"type": "string"}, "description": "Reopened intent ids. Owner: admin.reopen via orEmptyStrings."}, "reopened_units": {"type": "array", "description": "Reopened acceptance units. Owner: admin.reopen via reopenedUnitsToSlice.", "items": ` + adminReopenedUnitSchema + `}, "reopened_total_count": {"type": "integer", "description": "Total rows reopened. Owner: admin.reopen."}, "idempotency_key": {"type": "string", "description": "Echo of the request key. Owner: admin.reopen via IdempotencyKey."}, "duplicate": {"type": "boolean", "enum": [false], "description": "False: this call performed the reopen. Owner: admin.reopen."}}}, {"type": "object", "description": "Idempotent replay: totals without the reducer/intent split. Owner: admin.respondDuplicateReopen.", "required": ["status", "reopened_total_count", "reopened_ids", "idempotency_key", "duplicate"], "properties": {"status": {"type": "string", "enum": ["reopened"], "description": "Reopen disposition. Owner: admin.respondDuplicateReopen."}, "reopened_total_count": {"type": "integer", "description": "Total rows the original reopen moved. Owner: admin.respondDuplicateReopen via ReplayedCount."}, "reopened_ids": {"type": "array", "items": {"type": "string"}, "description": "Combined reopened ids. Owner: admin.respondDuplicateReopen via orEmptyStrings."}, "idempotency_key": {"type": "string", "description": "Echo of the request key. Owner: admin.respondDuplicateReopen via IdempotencyKey."}, "duplicate": {"type": "boolean", "enum": [true], "description": "True: the prior outcome was replayed. Owner: admin.respondDuplicateReopen."}}}]}}}},
          "400": {"$ref": "#/components/responses/BadRequest"},
          "403": {"$ref": "#/components/responses/Forbidden"},
          "404": {"$ref": "#/components/responses/NotFound"},
          "409": {"$ref": "#/components/responses/Conflict"},
          "422": {
            "description": "Refused before the idempotency claim, so the key is not consumed: the domain is not one of the three reopenable domains, or the scope has no active generation to reopen work for. Nothing is reopened either way. On a resolve race after the claim the same refused body is returned and the key stays in progress.",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "description": "Reopen refusal. Owner: admin.reopen.",
                  "required": ["status", "reason", "detail"],
                  "properties": {
                    "status": {"type": "string", "enum": ["refused"], "description": "Refusal disposition. Owner: admin.reopen."},
                    "domain": {"type": "string", "description": "Present when the domain itself was refused. Owner: admin.reopen."},
                    "reason": {"type": "string", "description": "Machine-readable refusal reason. Owner: admin.reopen."},
                    "detail": {"type": "string", "description": "Operator guidance for the refusal. Owner: admin.reopen."}
                  }
                }
              }
            }
          },
          "500": {"$ref": "#/components/responses/InternalError"},
          "503": {"$ref": "#/components/responses/ServiceUnavailable"}
        }
      }
    },
    "/api/v0/admin/backfill": {
      "post": {
        "tags": ["admin"],
        "summary": "Request a backfill",
        "description": "A 503 with Retry-After means the request checkpoint step failed, a transient condition that is safe to retry; a 503 without Retry-After means a configuration state: the admin store or the checkpoint source is not configured on this API instance. No 503 body carries backend error text. At least one selector is required: scope_id or generation_id.",
        "requestBody": {
          "required": true,
          "content": {
            "application/json": {
              "schema": {
                "type": "object",
                "description": "Backfill selectors; at least one of scope_id or generation_id is required. Owner: admin.backfill.",
                "properties": {
                  "scope_id": {"type": "string", "description": "Scope to backfill, per admin.backfill."},
                  "generation_id": {"type": "string", "description": "Generation to backfill, per admin.backfill."},
                  "operator_note": {"type": "string", "description": "Operator note recorded with the request, per admin.backfill."}
                }
              }
            }
          }
        },
        "responses": {
          "200": {"description": "Backfill request results", "content": {"application/json": {"schema": {"type": "object", "description": "Backfill request record. Owner: admin.backfill.", "required": ["status", "backfill_request"], "properties": {"status": {"type": "string", "enum": ["accepted"], "description": "Request disposition. Owner: admin.backfill."}, "backfill_request": ` + adminBackfillRequestSchema + `}}}}},
          "400": {"$ref": "#/components/responses/BadRequest"},
          "500": {"$ref": "#/components/responses/InternalError"},
          "503": {"$ref": "#/components/responses/ServiceUnavailable"}
        }
      }
    },
    "/api/v0/admin/replay-events/query": {
      "post": {
        "tags": ["admin"],
        "summary": "Query replay events",
        "description": "A 503 with Retry-After means a transient condition that is safe to retry: the read hit a stale or timed-out PostgreSQL reader, or the request checkpoint step failed; a 503 without Retry-After means a configuration state: the admin store or the checkpoint source is not configured on this API instance. No 503 body carries backend error text. The read runs with no route deadline, so it never answers 504. A reader-verdict 503 names capability operator.replay_events.list in its error envelope.",
        "requestBody": {
          "required": true,
          "content": {
            "application/json": {
              "schema": {
                "type": "object",
                "description": "Replay-event list filters; every filter is optional. Owner: admin.listReplayEvents.",
                "properties": {
                  "scope_id": {"type": "string", "description": "Return only events for this scope, per admin.listReplayEvents."},
                  "work_item_id": {"type": "string", "description": "Return only events for this work item, per admin.listReplayEvents."},
                  "failure_class": {"type": "string", "description": "Return only events in this failure class, per admin.listReplayEvents."},
                  "limit": {"type": "integer", "description": "Maximum rows to return; defaults to 100 when absent or non-positive, per admin.listReplayEvents."}
                }
              }
            }
          }
        },
        "responses": {
          "200": {"description": "Replay-event query results. Each event carries replay_attempt_baseline (the baseline the replay set on a projector row) and prior_admission_wait_attempts (the admission waits the row had spent before the replay reset them); both are null for reducer rows and for events written before migration 334 (#11182).", "content": {"application/json": {"schema": {"type": "object", "description": "Replay-event query results. Owner: admin.listReplayEvents.", "required": ["count", "events"], "properties": {"count": {"type": "integer", "description": "Events returned. Owner: admin.listReplayEvents."}, "events": {"type": "array", "description": "Replay-event audit rows. Owner: admin.listReplayEvents.", "items": ` + adminReplayEventSchema + `}}}}}},
          "400": {"$ref": "#/components/responses/BadRequest"},
          "500": {"$ref": "#/components/responses/InternalError"},
          "503": {"$ref": "#/components/responses/ServiceUnavailable"}
        }
      }
    },

`
