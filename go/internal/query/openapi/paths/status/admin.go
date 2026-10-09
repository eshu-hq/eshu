// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package status

// Admin is the OpenAPI path fragment documenting the 14 `/api/v0/admin/*`
// routes. openapi.Spec concatenates it into the published document; keep it in
// lockstep with the handlers and docs/public/reference/http-api.md.
const Admin = `
    "/api/v0/admin/refinalize": {
      "post": {
        "tags": ["admin"],
        "summary": "Refinalize scopes",
        "responses": {
          "200": {
            "description": "Refinalize request accepted. Alongside status, enqueued, scope_ids, and the five dedup counters it carries skipped_scopes, the scopes named in the request that were considered but not re-enqueued, by reason, delta_active_scopes, the re-enqueued scopes whose generation is a delta and whose graph stays incomplete until a full generation activates, and reindex_requests_written, the per-repository reindex watermarks written for them in the same transaction.",
            "content": {"application/json": {"schema": {"type": "object", "properties": {"skipped_scopes": {"$ref": "#/components/schemas/SkippedScopesReport"}, "delta_active_scopes": {"$ref": "#/components/schemas/DeltaActiveScopesReport"}, "reindex_requests_written": {"$ref": "#/components/schemas/ReindexRequestsWrittenReport"}}}}}
          },
          "400": {"$ref": "#/components/responses/BadRequest"},
          "500": {"$ref": "#/components/responses/InternalError"}
        }
      }
    },
    "/api/v0/admin/reindex": {
      "post": {
        "tags": ["admin"],
        "summary": "Request a fleet-wide or per-repository reindex",
        "description": "Records a reindex watermark, stamped by Postgres and never moved backward. With scope workspace (the default) it records the fleet watermark, returned as requested_at: on each sync cycle every git ingester shard forces a full re-parse (reconcile reason reindex_requested) of each repository it owns whose newest activated full generation was ingested before requested_at. With scope repository it resolves each repositories selector to exactly one git default-branch repository scope and records a watermark for each, returned per repository; the owning shard forces a full re-parse (reconcile reason repository_reindex_requested) and syncs requested repositories first. A repository-scoped request is all or nothing: any selector that matches no repository, matches several, or is not a git default-branch scope fails the request with 400 naming every such selector, and nothing is recorded. Both share the reconciliation sweep's per-cycle budget (ESHU_REPO_RECONCILE_MAX_PER_CYCLE) and its in-flight and retry-backoff throttle. A request is satisfied repository by repository as those full generations activate; it is never claimed and has no completion status. Webhook-only ingesters reach only the repositories they are triggered for, and filesystem source mode does not read either watermark. Send the request only after every git ingester runs the target binary: the watermark records when a full generation was ingested, not which parser produced it. Unknown body fields are rejected.",
        "requestBody": {
          "required": true,
          "content": {
            "application/json": {
              "schema": {
                "type": "object",
                "additionalProperties": false,
                "properties": {
                  "ingester": {"type": "string", "enum": ["repository"], "default": "repository", "description": "Only the git repository ingesters honor reindex requests."},
                  "scope": {"type": "string", "enum": ["workspace", "repository"], "default": "workspace", "description": "workspace reindexes every repository the git ingesters own; repository reindexes only the repositories listed in repositories."},
                  "repositories": {"type": "array", "minItems": 1, "maxItems": 100, "items": {"type": "string", "minLength": 1}, "description": "Repository selectors (repository ID, scope ID, name, slug, or path) for scope repository. Required with scope repository and rejected with scope workspace."},
                  "force": {"type": "boolean", "default": true, "description": "Must be true or omitted: a reindex always forces a full re-parse, and false is rejected with 400."}
                }
              }
            }
          }
        },
        "responses": {
          "202": {
            "description": "Reindex watermark recorded.",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "required": ["status", "ingester", "scope", "force", "detail"],
                  "properties": {
                    "status": {"type": "string", "enum": ["accepted"]},
                    "ingester": {"type": "string", "enum": ["repository"]},
                    "scope": {"type": "string", "enum": ["workspace", "repository"]},
                    "force": {"type": "boolean", "enum": [true]},
                    "requested_at": {"type": "string", "format": "date-time", "description": "scope workspace only: the stored fleet reindex watermark in UTC. A request never lowers it."},
                    "repositories": {
                      "type": "array",
                      "description": "scope repository only: one entry per distinct repository scope, sorted by scope_id.",
                      "items": {
                        "type": "object",
                        "required": ["repository_id", "scope_id", "requested_at"],
                        "properties": {
                          "repository_id": {"type": "string"},
                          "scope_id": {"type": "string"},
                          "requested_at": {"type": "string", "format": "date-time", "description": "The stored per-repository reindex watermark in UTC. A request never lowers it."}
                        }
                      }
                    },
                    "detail": {"type": "string"}
                  },
                  "oneOf": [
                    {"required": ["requested_at"], "properties": {"scope": {"enum": ["workspace"]}}},
                    {"required": ["repositories"], "properties": {"scope": {"enum": ["repository"]}}}
                  ]
                }
              }
            }
          },
          "400": {"$ref": "#/components/responses/BadRequest"},
          "500": {"$ref": "#/components/responses/InternalError"},
          "503": {"description": "Reindex requests, or repository-scoped reindex requests, are not configured on this API instance."}
        }
      }
    },
    "/api/v0/admin/recover-generations": {
      "post": {
        "tags": ["admin"],
        "summary": "Recover wedged generations",
        "description": "Operator escape hatch for generations that wedge active without advancing past canonical-nodes-committed, and the disaster-recovery entry point for rebuilding the graph from preserved Postgres facts. Durably re-enqueues projector work through the same Go work queue refinalize uses (re-driving reduce -> readiness -> projection over existing facts, no re-clone) and records the action in the admin_replay_requests ledger. Send either scope_ids or all_scopes, never both. all_scopes re-enqueues every recoverable scope, which is what a graph rebuild after a Postgres restore or a graph-backend swap needs: each active scope through its active generation, and each failed scope with no active generation through its newest failed generation (a named failed scope in scope_ids is recovered the same way). Scopes it cannot re-enqueue are reported in skipped_scopes, by reason, so a partial rebuild is visible. Requires an explicit reason and idempotency_key and an admin (all-scopes) token. Duplicate delivery of the same idempotency_key returns the prior outcome (duplicate=true) instead of re-enqueuing; a key reused across the two modes conflicts. In the same transaction it clears the dedup state for exactly those generations - succeeded reducer work items, completed shared projection intents, graph projection phase rows, active relationship generations, and shared projection acceptance rows - because all five outlive a graph wipe and would otherwise tell the pipeline the work is already done, leaving the rebuild stuck at source-local structure.",
        "requestBody": {
          "required": true,
          "content": {
            "application/json": {
              "schema": {
                "type": "object",
                "required": ["reason", "idempotency_key"],
                "properties": {
                  "scope_ids": {"type": "array", "items": {"type": "string"}, "description": "Scopes whose wedged generations should be re-driven: an active scope through its active generation, a failed scope through its newest failed generation. Required unless all_scopes is true."},
                  "all_scopes": {"type": "boolean", "default": false, "description": "Re-enqueue every recoverable scope (active scopes and failed scopes with a failed generation), for rebuilding the graph from preserved facts when no scope list is available. Cannot be combined with scope_ids."},
                  "reason": {"type": "string", "description": "Why the recovery is safe."},
                  "idempotency_key": {"type": "string", "description": "Makes the recovery safe under retries and concurrent delivery."}
                },
                "oneOf": [
                  {"required": ["scope_ids"], "properties": {"scope_ids": {"minItems": 1}, "all_scopes": {"enum": [false]}}},
                  {"required": ["all_scopes"], "properties": {"all_scopes": {"enum": [true]}, "scope_ids": {"maxItems": 0}}}
                ]
              }
            }
          }
        },
        "responses": {
          "200": {
            "description": "A recovery this call performed, or the prior outcome replayed for an idempotency_key that already completed. The two shapes differ: only the first reports the dedup state cleared.",
            "content": {
              "application/json": {
                "schema": {
                  "oneOf": [
                    {
                      "type": "object",
                      "description": "Recovery performed by this call. Alongside status, enqueued, and scope_ids it reports the dedup state cleared so the re-projection rebuilds the whole graph rather than only its source-local layer. After a graph wipe all five counters should be non-zero; five zeros mean the rebuild will restore source-local structure and nothing else.",
                      "required": ["status", "enqueued", "scope_ids", "reducer_work_deleted", "shared_intents_reopened", "readiness_phases_cleared", "generations_retired", "shared_projection_acceptance_cleared", "skipped_scopes", "delta_active_scopes", "reindex_requests_written", "idempotency_key", "duplicate"],
                      "properties": {
                        "status": {"type": "string", "enum": ["recovered"]},
                        "enqueued": {"type": "integer", "description": "Scope generations re-enqueued for projection."},
                        "scope_ids": {"type": "array", "items": {"type": "string"}, "description": "Scopes actually re-enqueued."},
                        "reducer_work_deleted": {"type": "integer", "description": "Succeeded reducer work items removed so the re-projection's enqueue is not deduplicated away."},
                        "shared_intents_reopened": {"type": "integer", "description": "Shared projection intents whose completed_at was cleared so the partition workers drain them again."},
                        "readiness_phases_cleared": {"type": "integer", "description": "Graph projection phase rows removed, because they outlive a graph wipe and would otherwise assert canonical nodes are committed for an empty graph."},
                        "generations_retired": {"type": "integer", "description": "Active relationship generations superseded so the re-projection never consumes the prior wave's resolved rows as current truth."},
                        "shared_projection_acceptance_cleared": {"type": "integer", "description": "Shared projection acceptance rows deleted so the repo_dependency lane cannot project edges for a refinalized generation from its pre-wipe watermark; the re-projection's intent commits re-advance acceptance."},
                        "skipped_scopes": {"$ref": "#/components/schemas/SkippedScopesReport"},
                        "delta_active_scopes": {"$ref": "#/components/schemas/DeltaActiveScopesReport"},
                        "reindex_requests_written": {"$ref": "#/components/schemas/ReindexRequestsWrittenReport"},
                        "idempotency_key": {"type": "string"},
                        "duplicate": {"type": "boolean", "enum": [false]}
                      }
                    },
                    {
                      "type": "object",
                      "description": "Idempotent replay: this key already completed, and nothing was re-enqueued. The five dedup counters, skipped_scopes, delta_active_scopes, and reindex_requests_written are absent because the admin_replay_requests ledger does not persist them, so a retry issued after the original response was lost cannot report what that recovery cleared. Read the counters from the original response, or from the projector queue and shared-intent backlog directly.",
                      "required": ["status", "enqueued", "scope_ids", "idempotency_key", "duplicate"],
                      "properties": {
                        "status": {"type": "string", "enum": ["recovered"]},
                        "enqueued": {"type": "integer", "description": "Scope generations the original recovery re-enqueued."},
                        "scope_ids": {"type": "array", "items": {"type": "string"}, "description": "Scopes the original recovery re-enqueued."},
                        "idempotency_key": {"type": "string"},
                        "duplicate": {"type": "boolean", "enum": [true]}
                      }
                    }
                  ]
                }
              }
            }
          },
          "400": {"$ref": "#/components/responses/BadRequest"},
          "403": {"description": "Recovery requires an admin (all-scopes) token"},
          "409": {"description": "Idempotency key already in progress or reused with different scope_ids"},
          "500": {"$ref": "#/components/responses/InternalError"}
        }
      }
    },
    "/api/v0/admin/shared-projection/tuning-report": {
      "get": {
        "tags": ["admin"],
        "summary": "Get shared projection tuning guidance",
        "responses": {
          "200": {"description": "Shared projection tuning report"}
        }
      }
    },
    "/api/v0/admin/work-items/query": {
      "post": {
        "tags": ["admin"],
        "summary": "Query fact work items",
        "responses": {
          "200": {"description": "Admin work-item query results"},
          "400": {"$ref": "#/components/responses/BadRequest"},
          "500": {"$ref": "#/components/responses/InternalError"}
        }
      }
    },
    "/api/v0/admin/decisions/query": {
      "post": {
        "tags": ["admin"],
        "summary": "Query projection decisions",
        "responses": {
          "200": {"description": "Projection decision query results"},
          "400": {"$ref": "#/components/responses/BadRequest"},
          "500": {"$ref": "#/components/responses/InternalError"}
        }
      }
    },
    "/api/v0/admin/dead-letters/query": {
      "post": {
        "tags": ["admin"],
        "summary": "Query dead-letter work items",
        "description": "Returns a bounded deterministic page of durable fact_work_items dead letters. Requires limit and timeout_ms, supports failure_class, domain, scope_id, collector_kind, and updated_at window filters, and returns truncated=true when more rows matched than the requested limit. Scoped tokens are restricted to their granted component scopes.",
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
          "200": {"description": "Bounded dead-letter page with schema_version, limit, count, truncated, and items"},
          "400": {"$ref": "#/components/responses/BadRequest"},
          "403": {"$ref": "#/components/responses/Forbidden"},
          "504": {"description": "Dead-letter query timed out"},
          "500": {"$ref": "#/components/responses/InternalError"}
        }
      }
    },
    "/api/v0/admin/input-invalid-facts/query": {
      "post": {
        "tags": ["admin"],
        "summary": "Query durable input_invalid quarantine facts",
        "description": "Returns a bounded deterministic page of durable reducer_input_invalid_facts rows (issue #4630): facts the reducer quarantined during typed-payload decode because a required field was missing or null. Requires scope_id, generation_id, limit, and timeout_ms, supports optional domain and fact_kind filters, and returns truncated=true when more rows matched than the requested limit. Scoped tokens are restricted to their granted component scopes.",
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
          "200": {"description": "Bounded input_invalid quarantine page with schema_version, limit, count, truncated, and items"},
          "400": {"$ref": "#/components/responses/BadRequest"},
          "403": {"$ref": "#/components/responses/Forbidden"},
          "504": {"description": "Input-invalid-facts query timed out"},
          "500": {"$ref": "#/components/responses/InternalError"}
        }
      }
    },
    "/api/v0/admin/changed-since/poisoned-links/query": {
      "post": {
        "tags": ["admin"],
        "summary": "Query poisoned and retrying changed-since links",
        "description": "Returns a bounded deterministic page of durable changed_since_scope_cursor rows that are poisoned (the link hit its counting-failure limit, #7127 ruling 8.10) or retrying (a counted failure is pending on the scope's head activation). The changed-since link writer (go/internal/storage/postgres/freshness/links) is a runner over its own ledger, not a fact_work_items queue domain, so this state never appears in the dead-letter or reducer_input_invalid_facts surfaces (#7290). Requires limit and timeout_ms; supports optional status (poisoned|retrying), scope_id, and a forward keyset cursor (the last scope_id of a prior page). Returns truncated=true and next_cursor when more rows matched than the requested limit. Scoped tokens are restricted to their granted component scopes.",
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
          "200": {"description": "Bounded changed-since poisoned/retrying link page with schema_version, limit, count, truncated, next_cursor, and items"},
          "400": {"$ref": "#/components/responses/BadRequest"},
          "403": {"$ref": "#/components/responses/Forbidden"},
          "504": {"description": "Changed-since poisoned-links query timed out"},
          "500": {"$ref": "#/components/responses/InternalError"}
        }
      }
    },
    "/api/v0/admin/dead-letter": {
      "post": {
        "tags": ["admin"],
        "summary": "Dead-letter work items",
        "responses": {
          "200": {"description": "Dead-letter request results"},
          "400": {"$ref": "#/components/responses/BadRequest"},
          "500": {"$ref": "#/components/responses/InternalError"}
        }
      }
    },
    "/api/v0/admin/skip": {
      "post": {
        "tags": ["admin"],
        "summary": "Skip repository work items",
        "description": "Dead-letters at most 100 pending, retrying, or failed work items for a repository or scope. Claimed, running, succeeded, superseded, and already dead-lettered items are left unchanged. The response count includes only rows transitioned by this call. The repository_id accepts a scope id or a source key; a selector matching more than one scope fails closed with 409, naming the matched scopes.",
        "responses": {
          "200": {"description": "Skip request results"},
          "400": {"$ref": "#/components/responses/BadRequest"},
          "409": {"$ref": "#/components/responses/Conflict"},
          "500": {"$ref": "#/components/responses/InternalError"}
        }
      }
    },
    "/api/v0/admin/replay": {
      "post": {
        "tags": ["admin"],
        "summary": "Replay failed work items",
        "description": "Safely replays terminal work items. Requires an explicit reason and idempotency_key, an admin (all-scopes) token, and refuses unsafe failure classes (input_invalid, unsafe_payload, and the manual-review dead-letter triage classes projection_bug and resource_exhausted) unless force is set, whether they arrive as the failure_class selector or as explicit work_item_ids. A 200 with replayed_count 0 means nothing matched. Duplicate delivery of the same idempotency_key returns the prior outcome instead of replaying again.",
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
          "200": {"description": "Replay request results (duplicate=true when an idempotent prior outcome is returned)"},
          "400": {"$ref": "#/components/responses/BadRequest"},
          "403": {"description": "Replay requires an admin (all-scopes) token"},
          "409": {"description": "Idempotency key already in progress or reused with different parameters"},
          "422": {
            "description": "Refused without force: the failure_class selector, or at least one explicit work_item_ids row, is in an unsafe or manual-review failure class. Also refused, even with force: at least one explicit work_item_ids row is a projector row whose scope generation is superseded (failure_class projector_replay_generation_superseded, #7130). A request naming such ids is refused whole, nothing is replayed, the idempotency_key is not consumed, and refused_work_items lists only the offending ids.",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "required": ["status", "reason", "detail"],
                  "properties": {
                    "status": {"type": "string", "enum": ["refused"]},
                    "reason": {"type": "string"},
                    "detail": {"type": "string"},
                    "failure_class": {"type": "string", "description": "Present when the failure_class selector itself was refused."},
                    "refused_work_items": {
                      "type": "array",
                      "description": "Present when explicit work_item_ids were refused; sorted by work_item_id.",
                      "items": {
                        "type": "object",
                        "required": ["work_item_id", "failure_class", "reason"],
                        "properties": {
                          "work_item_id": {"type": "string"},
                          "failure_class": {"type": "string"},
                          "generation_id": {"type": "string", "description": "Present for a superseded-generation refusal: the superseded generation the row belongs to."},
                          "reason": {"type": "string", "description": "Operator guidance for the class."}
                        }
                      }
                    }
                  }
                }
              }
            }
          },
          "500": {"$ref": "#/components/responses/InternalError"}
        }
      }
    },
    "/api/v0/admin/reopen": {
      "post": {
        "tags": ["admin"],
        "summary": "Reopen completed work items",
        "description": "Reopens completed reducer or shared-projection work for one domain and scope, the admin surface for the #7285 hand-SQL repair. Requires an explicit domain, scope_id, reason and idempotency_key, and an admin (all-scopes) token. Only repo_dependency (one completed intent row per acceptance unit, at the accepted source run), workload_materialization and submodule_pin (succeeded reducer rows) reopen; every other domain is refused. The store resolves the scope's active generation at run time and locks with SKIP LOCKED under a lock_timeout. A 200 with reopened_total_count 0 means nothing matched. Duplicate delivery of the same idempotency_key returns the prior outcome (duplicate=true, totals without the reducer/intent split) instead of reopening again. A scope_id matching more than one scope fails closed with 409, naming the matched scopes.",
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
          "200": {"description": "Reopen request results (duplicate=true when an idempotent prior outcome is returned)"},
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
                  "required": ["status", "reason", "detail"],
                  "properties": {
                    "status": {"type": "string", "enum": ["refused"]},
                    "domain": {"type": "string", "description": "Present when the domain itself was refused."},
                    "reason": {"type": "string"},
                    "detail": {"type": "string"}
                  }
                }
              }
            }
          },
          "500": {"$ref": "#/components/responses/InternalError"}
        }
      }
    },
    "/api/v0/admin/backfill": {
      "post": {
        "tags": ["admin"],
        "summary": "Request a backfill",
        "responses": {
          "200": {"description": "Backfill request results"},
          "400": {"$ref": "#/components/responses/BadRequest"},
          "500": {"$ref": "#/components/responses/InternalError"}
        }
      }
    },
    "/api/v0/admin/replay-events/query": {
      "post": {
        "tags": ["admin"],
        "summary": "Query replay events",
        "responses": {
          "200": {"description": "Replay-event query results"},
          "400": {"$ref": "#/components/responses/BadRequest"},
          "500": {"$ref": "#/components/responses/InternalError"}
        }
      }
    },
`
