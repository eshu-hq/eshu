// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package status

// Admin is the OpenAPI path fragment documenting the admin mutation and
// query routes in this file. openapi.Spec concatenates it into the published
// document; keep it in lockstep with the handlers and HTTP API reference.
// Reindex progress and delta-union completeness have their own fragments.
const Admin = `
    "/api/v0/admin/refinalize": {
      "post": {
        "tags": ["admin"],
        "summary": "Refinalize scopes",
        "description": "A 503 with Retry-After means the request checkpoint step failed, a transient condition that is safe to retry; a 503 without Retry-After means a configuration state: the recovery handler or the checkpoint source is not configured on this API instance. No 503 body carries backend error text.",
        "requestBody": {
          "required": true,
          "content": {
            "application/json": {
              "schema": {
                "type": "object",
                "description": "Refinalize selectors. Owner: admin.refinalize.",
                "required": ["scope_ids"],
                "properties": {
                  "scope_ids": {"type": "array", "minItems": 1, "items": {"type": "string"}, "description": "Scopes to re-enqueue, per admin.refinalize; must not be empty."}
                }
              }
            }
          }
        },
        "responses": {
          "200": {
            "description": "Refinalize request accepted. Alongside status, enqueued, scope_ids, and the five dedup counters it carries skipped_scopes, the scopes named in the request that were considered but not re-enqueued, by reason, delta_active_scopes, the re-enqueued scopes whose generation is a delta and whose graph stays incomplete until a full generation activates, and reindex_requests_written, the per-repository reindex watermarks written for them in the same transaction.",
            "content": {"application/json": {"schema": {"type": "object", "description": "Refinalize outcome. Owner: admin.refinalize serving recovery.RefinalizeResult.", "required": ["status", "enqueued", "scope_ids", "reducer_work_deleted", "shared_intents_reopened", "readiness_phases_cleared", "generations_retired", "shared_projection_acceptance_cleared", "skipped_scopes", "delta_active_scopes", "reindex_requests_written"], "properties": {"status": {"type": "string", "enum": ["accepted"], "description": "Request disposition. Owner: admin.refinalize."}, "enqueued": {"type": "integer", "description": "Scope generations re-enqueued. Owner: admin.refinalize via Enqueued."}, "scope_ids": {"type": "array", "items": {"type": "string"}, "description": "Scopes re-enqueued. Owner: admin.refinalize via ScopeIDs."}, "reducer_work_deleted": {"type": "integer", "description": "Succeeded reducer work items removed. Owner: admin.refinalize via ReducerWorkDeleted."}, "shared_intents_reopened": {"type": "integer", "description": "Shared projection intents reopened. Owner: admin.refinalize via SharedIntentsReopened."}, "readiness_phases_cleared": {"type": "integer", "description": "Graph projection phase rows removed. Owner: admin.refinalize via ReadinessPhasesCleared."}, "generations_retired": {"type": "integer", "description": "Active relationship generations superseded. Owner: admin.refinalize via GenerationsRetired."}, "shared_projection_acceptance_cleared": {"type": "integer", "description": "Shared projection acceptance rows deleted. Owner: admin.refinalize via SharedProjectionAcceptanceCleared."}, "skipped_scopes": {"$ref": "#/components/schemas/SkippedScopesReport"}, "delta_active_scopes": {"$ref": "#/components/schemas/DeltaActiveScopesReport"}, "reindex_requests_written": {"$ref": "#/components/schemas/ReindexRequestsWrittenReport"}}}}}
          },
          "400": {"$ref": "#/components/responses/BadRequest"},
          "409": {"description": "Recovery could not acquire the reducer claim fence within its bounded retry window; response contains blocked/claim_fence_busy. Retry after queue contention subsides."},
          "500": {"$ref": "#/components/responses/InternalError"},
          "503": {"$ref": "#/components/responses/ServiceUnavailable"}
        }
      }
    },
    "/api/v0/admin/reindex": {
      "post": {
        "tags": ["admin"],
        "summary": "Request a fleet-wide or per-repository reindex",
        "description": "Records a reindex watermark, stamped by Postgres and never moved backward. With scope workspace (the default) it records the fleet watermark, returned as requested_at: on each sync cycle every git ingester shard forces a full re-parse (reconcile reason reindex_requested) of each repository it owns whose newest activated full generation was ingested before requested_at. With scope repository it resolves each repositories selector to exactly one git default-branch repository scope and records a watermark for each, returned per repository; the owning shard forces a full re-parse (reconcile reason repository_reindex_requested) and syncs requested repositories first. A repository-scoped request is all or nothing: any selector that matches no repository, matches several, or is not a git default-branch scope fails the request with 400 naming every such selector, and nothing is recorded. Both share the reconciliation sweep's per-cycle budget (ESHU_REPO_RECONCILE_MAX_PER_CYCLE) and its in-flight and retry-backoff throttle. A request is satisfied repository by repository as those full generations activate; it is never claimed and has no completion status. Webhook-only ingesters reach only the repositories they are triggered for, and filesystem source mode does not read either watermark. Send the request only after every git ingester runs the target binary: the watermark records when a full generation was ingested, not which parser produced it. Unknown body fields are rejected. A 503 with Retry-After means a transient condition that is safe to retry, and nothing was recorded: the repository catalog lookup hit a stale or timed-out PostgreSQL reader, or the request checkpoint step failed; a 503 without Retry-After means a configuration state: reindex requests, repository-scoped reindex requests, or the checkpoint source are not configured on this API instance. No 503 body carries backend error text. The lookup runs with no route deadline, so the route never answers 504. A reader-verdict 503 names capability operator.repository_catalog.lookup in its error envelope.",
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
                  "description": "Reindex watermark recorded. Owner: admin.reindex and admin.reindexRepositories.",
                  "required": ["status", "ingester", "scope", "force", "detail"],
                  "properties": {
                    "status": {"type": "string", "enum": ["accepted"], "description": "Request disposition. Owner: admin.reindex."},
                    "ingester": {"type": "string", "enum": ["repository"], "description": "Honoring ingester. Owner: admin.decodeReindexRequest via Ingester."},
                    "scope": {"type": "string", "enum": ["workspace", "repository"], "description": "Request scope. Owner: admin.decodeReindexRequest via Scope."},
                    "force": {"type": "boolean", "enum": [true], "description": "A reindex always forces a full re-parse. Owner: admin.reindex via Force."},
                    "requested_at": {"type": "string", "format": "date-time", "description": "scope workspace only: the stored fleet reindex watermark in UTC. A request never lowers it. Owner: admin.reindex via RequestedAt."},
                    "repositories": {
                      "type": "array",
                      "description": "scope repository only: one entry per distinct repository scope, sorted by scope_id. Owner: admin.reindexRepositories.",
                      "items": {
                        "type": "object",
                        "description": "One repository watermark. Owner: admin.reindexRepositories.",
                        "required": ["repository_id", "scope_id", "requested_at"],
                        "properties": {
                          "repository_id": {"type": "string", "description": "Reindexed repository id. Owner: admin.reindexRepositories via repositoryIDByScope."},
                          "scope_id": {"type": "string", "description": "Reindexed scope id. Owner: admin.reindexRepositories via ScopeID."},
                          "requested_at": {"type": "string", "format": "date-time", "description": "The stored per-repository reindex watermark in UTC. A request never lowers it. Owner: admin.reindexRepositories via RequestedAt."}
                        }
                      }
                    },
                    "detail": {"type": "string", "description": "Watermark contract text. Owner: admin.reindexAcceptedDetail and admin.repositoryReindexAcceptedDetail."}
                  },
                  "oneOf": [
                    {"required": ["requested_at"], "properties": {"scope": {"enum": ["workspace"], "description": "Workspace scope: fleet watermark in requested_at. Owner: admin.reindex."}}},
                    {"required": ["repositories"], "properties": {"scope": {"enum": ["repository"], "description": "Repository scope: per-repository watermarks in repositories. Owner: admin.reindexRepositories."}}}
                  ]
                }
              }
            }
          },
          "400": {"$ref": "#/components/responses/BadRequest"},
          "500": {"$ref": "#/components/responses/InternalError"},
          "503": {"$ref": "#/components/responses/ServiceUnavailable"}
        }
      }
    },
    "/api/v0/admin/recover-generations": {
      "post": {
        "tags": ["admin"],
        "summary": "Recover wedged generations",
        "description": "Operator escape hatch for generations that wedge active without advancing past canonical-nodes-committed, and the disaster-recovery entry point for rebuilding the graph from preserved Postgres facts. Durably re-enqueues projector work through the same Go work queue refinalize uses (re-driving reduce -> readiness -> projection over existing facts, no re-clone) and records the action in the admin_replay_requests ledger. Send either scope_ids or all_scopes, never both. all_scopes re-enqueues every recoverable scope, which is what a graph rebuild after a Postgres restore or a graph-backend swap needs: each active scope through its active generation, and each failed scope with no active generation through its newest failed generation (a named failed scope in scope_ids is recovered the same way). Scopes it cannot re-enqueue are reported in skipped_scopes, by reason, so a partial rebuild is visible. Requires an explicit reason and idempotency_key and an admin (all-scopes) token. Duplicate delivery of the same idempotency_key returns the prior outcome (duplicate=true) instead of re-enqueuing; a key reused across the two modes conflicts. In the same transaction it clears the dedup state for exactly those generations - succeeded reducer work items, completed shared projection intents, graph projection phase rows, active relationship generations, and shared projection acceptance rows - because all five outlive a graph wipe and would otherwise tell the pipeline the work is already done, leaving the rebuild stuck at source-local structure. A 503 with Retry-After means the request checkpoint step failed, a transient condition that is safe to retry; a 503 without Retry-After means a configuration state: the recovery handler, the admin store, or the checkpoint source is not configured on this API instance. No 503 body carries backend error text.",
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
                        "status": {"type": "string", "enum": ["recovered"], "description": "Recovery disposition. Owner: admin.recoverGenerations."},
                        "enqueued": {"type": "integer", "description": "Scope generations re-enqueued for projection. Owner: admin.recoverGenerations via Enqueued."},
                        "scope_ids": {"type": "array", "items": {"type": "string"}, "description": "Scopes actually re-enqueued. Owner: admin.recoverGenerations via ScopeIDs."},
                        "reducer_work_deleted": {"type": "integer", "description": "Succeeded reducer work items removed so the re-projection's enqueue is not deduplicated away. Owner: admin.recoverGenerations via ReducerWorkDeleted."},
                        "shared_intents_reopened": {"type": "integer", "description": "Shared projection intents whose completed_at was cleared so the partition workers drain them again. Owner: admin.recoverGenerations via SharedIntentsReopened."},
                        "readiness_phases_cleared": {"type": "integer", "description": "Graph projection phase rows removed, because they outlive a graph wipe and would otherwise assert canonical nodes are committed for an empty graph. Owner: admin.recoverGenerations via ReadinessPhasesCleared."},
                        "generations_retired": {"type": "integer", "description": "Active relationship generations superseded so the re-projection never consumes the prior wave's resolved rows as current truth. Owner: admin.recoverGenerations via GenerationsRetired."},
                        "shared_projection_acceptance_cleared": {"type": "integer", "description": "Shared projection acceptance rows deleted so the repo_dependency lane cannot project edges for a refinalized generation from its pre-wipe watermark; the re-projection's intent commits re-advance acceptance. Owner: admin.recoverGenerations via SharedProjectionAcceptanceCleared."},
                        "skipped_scopes": {"$ref": "#/components/schemas/SkippedScopesReport"},
                        "delta_active_scopes": {"$ref": "#/components/schemas/DeltaActiveScopesReport"},
                        "reindex_requests_written": {"$ref": "#/components/schemas/ReindexRequestsWrittenReport"},
                        "idempotency_key": {"type": "string", "description": "Echo of the request key. Owner: admin.recoverGenerations via IdempotencyKey."},
                        "duplicate": {"type": "boolean", "enum": [false], "description": "False: this call performed the recovery. Owner: admin.recoverGenerations."}
                      }
                    },
                    {
                      "type": "object",
                      "description": "Idempotent replay: this key already completed, and nothing was re-enqueued. The five dedup counters, skipped_scopes, delta_active_scopes, and reindex_requests_written are absent because the admin_replay_requests ledger does not persist them, so a retry issued after the original response was lost cannot report what that recovery cleared. Read the counters from the original response, or from the projector queue and shared-intent backlog directly.",
                      "required": ["status", "enqueued", "scope_ids", "idempotency_key", "duplicate"],
                      "properties": {
                        "status": {"type": "string", "enum": ["recovered"], "description": "Recovery disposition. Owner: admin.respondDuplicateRecoverGenerations."},
                        "enqueued": {"type": "integer", "description": "Scope generations the original recovery re-enqueued. Owner: admin.respondDuplicateRecoverGenerations via ReplayedCount."},
                        "scope_ids": {"type": "array", "items": {"type": "string"}, "description": "Scopes the original recovery re-enqueued. Owner: admin.respondDuplicateRecoverGenerations via WorkItemIDs."},
                        "idempotency_key": {"type": "string", "description": "Echo of the request key. Owner: admin.respondDuplicateRecoverGenerations via IdempotencyKey."},
                        "duplicate": {"type": "boolean", "enum": [true], "description": "True: the prior outcome was replayed. Owner: admin.respondDuplicateRecoverGenerations."}
                      }
                    }
                  ]
                }
              }
            }
          },
          "400": {"$ref": "#/components/responses/BadRequest"},
          "403": {"description": "Recovery requires an admin (all-scopes) token"},
          "409": {"description": "Idempotency key already in progress or reused with different scope_ids, or recovery could not acquire the reducer claim fence within its bounded retry window. On claim-fence exhaustion, the response contains blocked/claim_fence_busy, the idempotency key remains in_progress, and retry requires a fresh key after contention subsides."},
          "500": {"$ref": "#/components/responses/InternalError"},
          "503": {"$ref": "#/components/responses/ServiceUnavailable"}
        }
      }
    },
    "/api/v0/admin/shared-projection/tuning-report": {
      "get": {
        "tags": ["admin"],
        "summary": "Get shared projection tuning guidance",
        "responses": {
          "200": {"description": "Shared projection tuning report", "content": {"application/json": {"schema": {"type": "object", "description": "Pinned static tuning report. Owner: admin.tuningReport serving SharedProjectionTuningReportStatus and SharedProjectionTuningReportDetail.", "required": ["status", "detail"], "properties": {"status": {"type": "string", "enum": ["not_applicable"], "description": "Pinned report status; shared-write tuning is not an operator surface. Owner: admin.SharedProjectionTuningReportStatus."}, "detail": {"type": "string", "description": "Pinned report detail. Owner: admin.SharedProjectionTuningReportDetail."}}}}}}
        }
      }
    },

`
