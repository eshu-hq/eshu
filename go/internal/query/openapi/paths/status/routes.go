// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package status

// collectorsListSchema is the listCollectors 200-response schema shared by
// the canonical collector status route and its legacy alias. Both mux to
// query.status listCollectors, so the alias carries the identical payload;
// a single const keeps the two fragments from drifting.
const collectorsListSchema = `{
                  "type": "object",
                  "properties": {
                    "version": {"type": "string", "description": "Server build version. Owner: query.status listCollectors."},
                    "collectors": {
                      "type": "array",
                      "description": "Collector runtime status rows. Owner: query.status listCollectors.",
                      "items": {
                        "type": "object",
                        "description": "One collector runtime row. Owner: query.status listCollectors.",
                        "properties": {
                          "instance_id": {"type": "string", "description": "Collector instance id. Owner: query.status listCollectors."},
                          "collector_kind": {"type": "string", "description": "Collector kind. Owner: query.status listCollectors."},
                          "mode": {"type": "string", "description": "Collector mode. Owner: query.status listCollectors."},
                          "runtime_mode": {"type": "string", "description": "Collector runtime mode. Owner: query.status listCollectors."},
                          "status_category": {
                            "type": "string",
                            "enum": ["coordinator_managed", "direct_mode", "profile_gated", "disabled", "unregistered"],
                            "description": "Registration classification. Owner: query.status listCollectors."
                          },
                          "coordinator_registered": {"type": "boolean", "description": "Whether the workflow coordinator registered this instance. Owner: query.status listCollectors."},
                          "enabled": {"type": "boolean", "description": "Whether the collector is enabled. Owner: query.status listCollectors."},
                          "claims_enabled": {"type": "boolean", "description": "Whether the collector accepts claims. Owner: query.status listCollectors."},
                          "evidence_sources": {"type": "array", "items": {"type": "string"}, "description": "Evidence sources backing this collector. Owner: query.status listCollectors."},
                          "source_systems": {"type": "array", "items": {"type": "string"}, "description": "Source systems this collector reads. Owner: query.status listCollectors."},
                          "observation_count": {"type": "integer", "description": "Observations backing this row. Owner: query.status listCollectors."},
                          "health": {"type": "string", "description": "Collector health. Owner: query.status listCollectors."},
                          "detail": {"type": "string", "description": "Human-readable collector detail. Owner: query.status listCollectors."},
                          "last_failure_class": {"type": "string", "enum": ["analyzer_failed", "archived", "attempt_budget_exhausted", "auth_denied", "collect_failure", "commit_failed", "commit_failure", "cpu_limit_exceeded", "deleted", "expired_lease_budget_exhausted", "extension_defined", "fact_limit_exceeded", "file_limit_exceeded", "ghsa_cache_replay", "ghsa_covered_through_out_of_range", "ghsa_window_held", "ghsa_window_no_progress", "ghsa_window_unarmed", "identity_mismatch", "input_limit_exceeded", "invalid_claim", "invalid_result", "launch_failure", "memory_limit_exceeded", "non_retryable", "not_found", "nvd_baseline_seed_rollback", "nvd_baseline_window_writer_armed", "operator_retired_unclaimed", "permission_denied", "permission_hidden", "rate_limited", "registry_auth_denied", "registry_canceled", "registry_metadata_too_large", "registry_not_found", "registry_rate_limited", "registry_retryable_failure", "registry_terminal_failure", "retryable", "run_age_bound_exceeded", "source_state_record", "source_unavailable", "stale_fence", "status_record", "target_unavailable", "terminal", "timeout", "unsupported_permission", "unsupported_target", "waiting_on_git_generation", "unknown"], "description": "Bounded class of the newest unresolved terminal fetch failure; present only with last_failure_terminal. Owner: query.status listCollectors."},
                          "last_failure_operation": {"type": "string", "description": "Bounded snake_case operation token of that failure, or unknown. Owner: query.status listCollectors."},
                          "last_failure_terminal": {"type": "boolean", "description": "Whether the newest failure is terminal. Owner: query.status listCollectors."},
                          "last_failure_at": {"type": "string", "format": "date-time", "description": "When that failure became terminal; present only with last_failure_terminal. Owner: query.status listCollectors."},
                          "acknowledged_failure": {
                            "type": "object",
                            "description": "An operator-acknowledged terminal failure (POST /api/v0/admin/collector-failures/acknowledge). Health reads at least partial, never healthy (a stale vulnerability source or a degraded failure on the same instance keeps the higher health); last_failure_* stays absent unless another scope has an unacknowledged failure. Bounded tokens and times only; the operator's reason is never returned here. Omitted when none. Owner: query.status listCollectors.",
                            "properties": {
                              "failure_class": {"type": "string", "enum": ["analyzer_failed", "archived", "attempt_budget_exhausted", "auth_denied", "collect_failure", "commit_failed", "commit_failure", "cpu_limit_exceeded", "deleted", "expired_lease_budget_exhausted", "extension_defined", "fact_limit_exceeded", "file_limit_exceeded", "ghsa_cache_replay", "ghsa_covered_through_out_of_range", "ghsa_window_held", "ghsa_window_no_progress", "ghsa_window_unarmed", "identity_mismatch", "input_limit_exceeded", "invalid_claim", "invalid_result", "launch_failure", "memory_limit_exceeded", "non_retryable", "not_found", "nvd_baseline_seed_rollback", "nvd_baseline_window_writer_armed", "operator_retired_unclaimed", "permission_denied", "permission_hidden", "rate_limited", "registry_auth_denied", "registry_canceled", "registry_metadata_too_large", "registry_not_found", "registry_rate_limited", "registry_retryable_failure", "registry_terminal_failure", "retryable", "run_age_bound_exceeded", "source_state_record", "source_unavailable", "stale_fence", "status_record", "target_unavailable", "terminal", "timeout", "unsupported_permission", "unsupported_target", "waiting_on_git_generation", "unknown"], "description": "Bounded failure class. Owner: query.status listCollectors."},
                              "failure_operation": {"type": "string", "description": "Bounded operation token. Owner: query.status listCollectors."},
                              "failed_at": {"type": "string", "format": "date-time", "description": "When the failure became terminal. Owner: query.status listCollectors."},
                              "acknowledged_at": {"type": "string", "format": "date-time", "description": "When the operator acknowledged the failure. Owner: query.status listCollectors."}
                            }
                          }
                        }
                      }
                    },
                    "count": {"type": "integer", "description": "Collector rows returned. Owner: query.status listCollectors."},
                    "classification_basis": {"type": "string", "description": "What classified the rows. Owner: query.status listCollectors."},
                    "updated_at": {"type": "string", "format": "date-time", "nullable": true, "description": "When the snapshot was taken. Owner: query.status listCollectors."}
                  }
                }`

// Routes is the OpenAPI path fragment documenting the `/api/v0/*` routes.
// openapi.Spec concatenates it into the published document; keep it in
// lockstep with the handlers and docs/public/reference/http-api.md.
const Routes = `
    "/api/v0/status/pipeline": {
      "get": {
        "tags": ["status"],
        "summary": "Get pipeline status",
        "description": "Returns the full pipeline status report. A 503 with Retry-After means a transient condition that is safe to retry: the status snapshot read hit a stale or timed-out PostgreSQL reader; a 503 without Retry-After means a configuration state: the status reader is not configured on this API instance. No 5xx body carries backend error text. A reader-verdict 503 error envelope names capability operations.pipeline_status.",
        "operationId": "getPipelineStatus",
        "responses": {
          "200": {
            "description": "Pipeline status",
            "content": {
              "application/json": {
                "schema": {"type": "object", "properties": {"active_work_source": {"$ref": "#/components/schemas/ActiveWorkSource"}, "coordinator_source": {"$ref": "#/components/schemas/CoordinatorSource"}, "terraform_state_source": {"$ref": "#/components/schemas/TerraformStateSource"}}}
              }
            }
          },
          "500": {"$ref": "#/components/responses/InternalError"},
          "503": {"$ref": "#/components/responses/ServiceUnavailable"}
        }
      }
    },
    "/api/v0/status/ingesters": {
      "get": {
        "tags": ["status"],
        "summary": "List ingesters",
        "description": "Returns known ingesters with basic health info.",
        "operationId": "listIngesters",
        "x-scoped-token-support": true,
        "responses": {
          "403": {"$ref": "#/components/responses/Forbidden"},
          "200": {
            "description": "List of ingesters",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "description": "Known ingesters with basic health info. Owner: query.status listIngesters.",
                  "properties": {
                    "version": {"type": "string", "description": "Server build version. Owner: query.status listIngesters via buildinfo.AppVersion."},
                    "active_work_source": {"$ref": "#/components/schemas/ActiveWorkSource"},
                    "coordinator_source": {"$ref": "#/components/schemas/CoordinatorSource"},
                    "ingesters": {"type": "array", "description": "Known ingesters with basic health info. Owner: query.status listIngesters.", "items": {"type": "object", "description": "One known ingester row. Owner: query.status listIngesters.", "required": ["name", "runtime_family", "aliases"], "properties": {"name": {"type": "string", "description": "Ingester name; the only known ingester is repository. Owner: query.status listIngesters."}, "runtime_family": {"type": "string", "description": "Runtime family; always ingester. Owner: query.status listIngesters."}, "aliases": {"type": "array", "description": "Accepted ingester name aliases (repository, bootstrap-index, repo-sync, workspace-index). Owner: query.status listIngesters.", "items": {"type": "string"}}, "health": {"type": "string", "description": "Ingester health state from the status report; present only when a status reader is configured. Owner: query.status listIngesters via report.Health.State."}, "queue_outstanding": {"type": "integer", "description": "Outstanding queue depth from the status report; present only when a status reader is configured. Owner: query.status listIngesters via report.Queue.Outstanding."}, "collector_instances": {"type": "integer", "description": "Registered collector instances from the status report; present only when a status reader is configured and the report carries a coordinator. Owner: query.status listIngesters."}}}},
                    "count": {"type": "integer", "description": "Ingesters returned. Owner: query.status listIngesters."}
                  }
                }
              }
            }
          },
          "500": {"$ref": "#/components/responses/InternalError"}
        }
      }
    },
    "/api/v0/status/collectors": {
      "get": {
        "tags": ["status"],
        "summary": "List collectors",
        "description": "Returns collector runtime status classified by workflow coordinator registration, direct status evidence, and persisted source or reducer fact evidence. An unresolved terminal fetch failure marks the instance health degraded and adds last_failure_class, last_failure_operation, last_failure_terminal, and last_failure_at. An operator can acknowledge a failure (POST /api/v0/admin/collector-failures/acknowledge): the instance then reads promotion_state partial with an acknowledged_failure object instead of degraded, and its health reads at least partial (never healthy): a stale vulnerability source or a degraded failure on the same instance keeps the higher health. Scoped callers receive only the per-kind health aggregate; last_failure_* fields are withheld from them. A 503 with Retry-After means a transient condition that is safe to retry: the status snapshot read hit a stale or timed-out PostgreSQL reader; a 503 without Retry-After means a configuration state: the status reader is not configured on this API instance. No 5xx body carries backend error text. A reader-verdict 503 error envelope names capability operations.collector_status.",
        "operationId": "listCollectors",
        "x-scoped-token-support": true,
        "responses": {
          "403": {"$ref": "#/components/responses/Forbidden"},
          "200": {
            "description": "List of collector runtimes",
            "content": {
              "application/json": {
                "schema": ` + collectorsListSchema + `
              }
            }
          },
          "500": {"$ref": "#/components/responses/InternalError"},
          "503": {"$ref": "#/components/responses/ServiceUnavailable"}
        }
      }
    },
    "/api/v0/status/ingesters/{ingester}": {
      "get": {
        "tags": ["status"],
        "summary": "Get ingester status",
        "description": "Returns detailed status for a specific ingester. A 503 with Retry-After means a transient condition that is safe to retry: the status snapshot read hit a stale or timed-out PostgreSQL reader; a 503 without Retry-After means a configuration state: the status reader is not configured on this API instance. No 5xx body carries backend error text. A reader-verdict 503 error envelope names capability operations.ingester_status.",
        "operationId": "getIngesterStatus",
        "x-scoped-token-support": true,
        "parameters": [
          {
            "name": "ingester",
            "in": "path",
            "required": true,
            "schema": {"type": "string"},
            "description": "Ingester name"
          }
        ],
        "responses": {
          "403": {"$ref": "#/components/responses/Forbidden"},
          "200": {
            "description": "Ingester status",
            "content": {
              "application/json": {
                "schema": {"type": "object", "description": "Detailed status for one ingester. Owner: query.status getIngesterStatus.", "properties": {
                  "version": {"type": "string", "description": "Server build version. Owner: query.status getIngesterStatus via buildinfo.AppVersion."},
                  "ingester": {"type": "string", "description": "Ingester name echoed from the path. Owner: query.status getIngesterStatus."},
                  "runtime_family": {"type": "string", "description": "Runtime family; always ingester. Owner: query.status getIngesterStatus."},
                  "health": ` + healthSchema + `,
                  "queue": ` + queueSnapshotSchema + `,
                  "coordinator": ` + coordinatorSchema + `,
                  "scope_activity": ` + scopeActivitySchema + `,
                  "stage_summaries": {"type": "array", "description": "Per-stage pipeline summaries. Owner: query.status getIngesterStatus via stageSummariesToSlice.", "items": ` + stageSummaryItemSchema + `},
                  "domain_backlogs": {"type": "array", "description": "Per-domain backlog aggregates with the blocked share. Owner: query.status getIngesterStatus via domainBacklogsToSlice.", "items": ` + domainBacklogItemSchema + `},
                  "active_work_source": {"$ref": "#/components/schemas/ActiveWorkSource"},
                  "coordinator_source": {"$ref": "#/components/schemas/CoordinatorSource"}}}
              }
            }
          },
          "400": {"$ref": "#/components/responses/BadRequest"},
          "404": {"$ref": "#/components/responses/NotFound"},
          "500": {"$ref": "#/components/responses/InternalError"},
          "503": {"$ref": "#/components/responses/ServiceUnavailable"}
        }
      }
    },
    "/api/v0/ingesters": {
      "get": {
        "tags": ["status"],
        "summary": "List ingesters",
        "description": "Legacy compatibility alias for the Go-owned ingester status list. Serves the identical listIngesters payload as the canonical route.",
        "operationId": "listIngestersLegacy",
        "responses": {
          "200": {
            "description": "List of ingesters",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "description": "Known ingesters with basic health info. Owner: query.status listIngesters.",
                  "properties": {
                    "version": {"type": "string", "description": "Server build version. Owner: query.status listIngesters via buildinfo.AppVersion."},
                    "active_work_source": {"$ref": "#/components/schemas/ActiveWorkSource"},
                    "coordinator_source": {"$ref": "#/components/schemas/CoordinatorSource"},
                    "ingesters": {"type": "array", "description": "Known ingesters with basic health info. Owner: query.status listIngesters.", "items": {"type": "object", "description": "One known ingester row. Owner: query.status listIngesters.", "required": ["name", "runtime_family", "aliases"], "properties": {"name": {"type": "string", "description": "Ingester name; the only known ingester is repository. Owner: query.status listIngesters."}, "runtime_family": {"type": "string", "description": "Runtime family; always ingester. Owner: query.status listIngesters."}, "aliases": {"type": "array", "description": "Accepted ingester name aliases (repository, bootstrap-index, repo-sync, workspace-index). Owner: query.status listIngesters.", "items": {"type": "string"}}, "health": {"type": "string", "description": "Ingester health state from the status report; present only when a status reader is configured. Owner: query.status listIngesters via report.Health.State."}, "queue_outstanding": {"type": "integer", "description": "Outstanding queue depth from the status report; present only when a status reader is configured. Owner: query.status listIngesters via report.Queue.Outstanding."}, "collector_instances": {"type": "integer", "description": "Registered collector instances from the status report; present only when a status reader is configured and the report carries a coordinator. Owner: query.status listIngesters."}}}},
                    "count": {"type": "integer", "description": "Ingesters returned. Owner: query.status listIngesters."}
                  }
                }
              }
            }
          },
          "500": {"$ref": "#/components/responses/InternalError"}
        }
      }
    },
    "/api/v0/collectors": {
      "get": {
        "tags": ["status"],
        "summary": "List collectors",
        "description": "Legacy compatibility alias for collector runtime status. A 503 with Retry-After means a transient condition that is safe to retry: the status snapshot read hit a stale or timed-out PostgreSQL reader; a 503 without Retry-After means a configuration state: the status reader is not configured on this API instance. No 5xx body carries backend error text. A reader-verdict 503 error envelope names capability operations.collector_status.",
        "operationId": "listCollectorsLegacy",
        "responses": {
          "200": {
            "description": "List of collector runtimes",
            "content": {
              "application/json": {
                "schema": ` + collectorsListSchema + `
              }
            }
          },
          "500": {"$ref": "#/components/responses/InternalError"},
          "503": {"$ref": "#/components/responses/ServiceUnavailable"}
        }
      }
    },
    "/api/v0/ingesters/{ingester}": {
      "get": {
        "tags": ["status"],
        "summary": "Get ingester status",
        "description": "Legacy compatibility alias for the Go-owned ingester status detail route. A 503 with Retry-After means a transient condition that is safe to retry: the status snapshot read hit a stale or timed-out PostgreSQL reader; a 503 without Retry-After means a configuration state: the status reader is not configured on this API instance. No 5xx body carries backend error text. A reader-verdict 503 error envelope names capability operations.ingester_status.",
        "operationId": "getIngesterStatusLegacy",
        "parameters": [
          {
            "name": "ingester",
            "in": "path",
            "required": true,
            "schema": {"type": "string"},
            "description": "Ingester name"
          }
        ],
        "responses": {
          "200": {
            "description": "Ingester status",
            "content": {
              "application/json": {
                "schema": {"type": "object", "description": "Detailed status for one ingester. Owner: query.status getIngesterStatus; the legacy alias serves the identical payload.", "properties": {
                  "version": {"type": "string", "description": "Server build version. Owner: query.status getIngesterStatus via buildinfo.AppVersion."},
                  "ingester": {"type": "string", "description": "Ingester name echoed from the path. Owner: query.status getIngesterStatus."},
                  "runtime_family": {"type": "string", "description": "Runtime family; always ingester. Owner: query.status getIngesterStatus."},
                  "health": ` + healthSchema + `,
                  "queue": ` + queueSnapshotSchema + `,
                  "coordinator": ` + coordinatorSchema + `,
                  "scope_activity": ` + scopeActivitySchema + `,
                  "stage_summaries": {"type": "array", "description": "Per-stage pipeline summaries. Owner: query.status getIngesterStatus via stageSummariesToSlice.", "items": ` + stageSummaryItemSchema + `},
                  "domain_backlogs": {"type": "array", "description": "Per-domain backlog aggregates with the blocked share. Owner: query.status getIngesterStatus via domainBacklogsToSlice.", "items": ` + domainBacklogItemSchema + `},
                  "active_work_source": {"$ref": "#/components/schemas/ActiveWorkSource"},
                  "coordinator_source": {"$ref": "#/components/schemas/CoordinatorSource"}}}
              }
            }
          },
          "400": {"$ref": "#/components/responses/BadRequest"},
          "404": {"$ref": "#/components/responses/NotFound"},
          "500": {"$ref": "#/components/responses/InternalError"},
          "503": {"$ref": "#/components/responses/ServiceUnavailable"}
        }
      }
    },
    "/api/v0/status/index": {
      "get": {
        "tags": ["status"],
        "summary": "Get index status",
        "description": "Returns the index status summary. Scoped tokens are admitted and receive a scoped shape instead of the deployment-wide report: only version, scoped=true, repository_count (counted over the caller's granted repositories and ingestion scopes inside the graph query, never an unfiltered count), completeness_state=scoped_repository_count_only, and withheld_sections naming status, reasons, queue, queue_blockages, coordinator, scope_activity, aws_materialization, semantic_extraction, terraform_state, repository_count_by_state, and repository_retirements. Those sections are process-global and cannot be attributed to a grant, and a queue_blockages row reports conflict_key as COALESCE(conflict_key, scope_id), a raw scope id, so they are withheld rather than redacted and the status snapshot is not read for a scoped caller. A scoped caller with no granted repository or ingestion scope receives repository_count 0 without a graph query. A failed graph count returns 500, an unavailable graph backend 503, and a graph read deadline 504, never 0. That holds for an unscoped caller too: once the status snapshot read succeeds, a failed graph repository count answers 503 when the graph backend is unavailable, 504 on a graph read deadline, and 500 with a fixed message otherwise, never a silent repository_count 0. Callers holding the shared key receive the full report unchanged. A hosted BrowserSessionRoutePolicy that refuses all-scope callers on grant-bound routes refuses them here with a 403. A 503 with Retry-After means a transient condition that is safe to retry: for an unscoped caller the status snapshot read hit a stale or timed-out PostgreSQL reader or the graph repository count found the graph backend unavailable, and for a scoped caller, whose only read is the granted-repository count in the graph (the status snapshot is not read), the graph backend is unavailable; a 503 without Retry-After means a configuration state: the status reader (unscoped caller) or the graph backend (scoped caller) is not configured on this API instance. A scoped or unscoped caller's graph count that exceeds its read deadline answers 504. No 5xx body carries backend error text. A reader-verdict 503 error envelope names capability operations.index_status.",
        "operationId": "getIndexStatus",
        "x-scoped-token-support": true,
        "responses": {
          "200": {
            "description": "Index status",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "active_work_source": {"$ref": "#/components/schemas/ActiveWorkSource"},
                    "coordinator_source": {"$ref": "#/components/schemas/CoordinatorSource"},
                    "terraform_state_source": {"$ref": "#/components/schemas/TerraformStateSource"},
                    "version": {"type": "string", "description": "Server build version, always present on both the full and scoped shapes. Owner: query.StatusHandler.getIndexStatus and getScopedIndexStatus via buildinfo.AppVersion."},
                    "status": {"type": "string", "description": "Aggregate index health state token. Owner: query.StatusHandler.getIndexStatus over status.BuildReport."},
                    "scoped": {"type": "boolean", "description": "true only on the scoped shape; absent for shared-key callers."},
                    "completeness_state": {"type": "string", "enum": ["scoped_repository_count_only"], "description": "Present only on the scoped shape."},
                    "withheld_sections": {"type": "array", "items": {"type": "string"}, "description": "Present only on the scoped shape: the deployment-wide sections not returned."},
                    "reasons": {"type": "array", "description": "Machine-readable reasons behind a non-healthy state; empty when healthy. Owner: query.StatusHandler.getIndexStatus over status.BuildReport.", "items": {"type": "string"}},
                    "repository_count": {"type": "integer", "description": "Repository rows counted in the graph; on the scoped shape counted over the caller's granted repositories only. Owner: query.StatusHandler.getIndexStatus."},
                    "repository_count_by_state": {"type": "object", "description": "Deployment-wide repository count split by graph evidence source (#7766): indexed rows carry evidence_source projector/canonical, every other row reads stub. Withheld from scoped callers. Owner: query.status_index_retirement indexStatusRepositoryCountByState.", "properties": {"indexed": {"type": "integer", "description": "Indexed repository rows. Owner: query.status_index_retirement indexStatusRepositoryCountByState."}, "stub": {"type": "integer", "description": "Stub repository rows. Owner: query.status_index_retirement indexStatusRepositoryCountByState."}}},
                    "repository_retirements": {"type": "object", "description": "Bounded retirement tombstone aggregate (#7766). Counts only, no ids. Withheld from scoped callers. Owner: query.status_index_retirement indexRetirementSummaryToMap.", "properties": {"counts_by_state": {"type": "object", "description": "Tombstone rows by state. Owner: query.status_index_retirement indexRetirementSummaryToMap.", "additionalProperties": {"type": "integer"}}, "oldest_pending_age_seconds": {"type": "number", "description": "Now minus the oldest open pending row requested_at; absent with no pending row."}, "last_completed_at": {"type": "string", "format": "date-time", "description": "Newest complete row retired_at; absent with no completion."}}},
                    "queue": ` + queueSnapshotSchema + `,
                    "queue_blockages": {
                      "type": "array",
                      "description": "Queue blockages with raw conflict keys. Withheld from scoped callers; see withheld_sections.",
                      "items": ` + queueBlockageItemSchema + `
                    },
                    "coordinator": ` + coordinatorSchema + `,
                    "scope_activity": ` + scopeActivitySchema + `,
                    "aws_materialization": ` + awsMaterializationSchema + `,
                    "semantic_extraction": ` + semanticExtractionSchema + `,
                    "terraform_state": ` + terraformStateSchema + `
                  }
                }
              }
            }
          },
          "403": {"$ref": "#/components/responses/Forbidden"},
          "500": {"$ref": "#/components/responses/InternalError"},
          "504": {"$ref": "#/components/responses/GatewayTimeout"},
          "503": {"$ref": "#/components/responses/ServiceUnavailable"}
        }
      }
    },
    "/api/v0/openapi.json": {
      "get": {
        "tags": ["health"],
        "summary": "OpenAPI specification",
        "description": "Returns the OpenAPI 3.0 specification for this API.",
        "operationId": "getOpenAPISpec",
        "responses": {
          "200": {
            "description": "OpenAPI specification",
            "content": {
              "application/json": {
                "schema": {"type": "object", "additionalProperties": true, "description": "The served OpenAPI 3.0 document itself: its openapi, info, paths, and components blocks. The shape is self-referential, so no fixed property set is pinned. Owner: query openapi.Spec."}
              }
            }
          }
        }
      }
    },
`
