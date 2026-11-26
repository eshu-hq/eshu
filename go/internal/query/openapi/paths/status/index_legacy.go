// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package status

// IndexLegacy is the OpenAPI path fragment documenting the
// GET /api/v0/index-status legacy compatibility alias. It lives apart from
// Routes because routes.go sits at the 500-line cap (the
// workflow_retire_unclaimed.go precedent). openapi.Spec concatenates it into
// the published document right after Routes; keep it in lockstep with the
// handler and docs/public/reference/http-api.md.
const IndexLegacy = `
    "/api/v0/index-status": {
      "get": {
        "tags": ["status"],
        "summary": "Get index status",
        "description": "Legacy compatibility alias for the Go-owned index status summary; it shares the handler with GET /api/v0/status/index. Scoped tokens are admitted and receive a scoped shape instead of the deployment-wide report: only version, scoped=true, repository_count (counted over the caller's granted repositories and ingestion scopes inside the graph query, never an unfiltered count), completeness_state=scoped_repository_count_only, and withheld_sections naming status, reasons, queue, queue_blockages, coordinator, scope_activity, aws_materialization, semantic_extraction, terraform_state, repository_count_by_state, and repository_retirements. Those sections are process-global and cannot be attributed to a grant, and a queue_blockages row reports conflict_key as COALESCE(conflict_key, scope_id), a raw scope id, so they are withheld rather than redacted and the status snapshot is not read for a scoped caller. A scoped caller with no granted repository or ingestion scope receives repository_count 0 without a graph query. A failed graph count returns 500, an unavailable graph backend 503, and a graph read deadline 504, never 0. That holds for an unscoped caller too: once the status snapshot read succeeds, a failed graph repository count answers 503 when the graph backend is unavailable, 504 on a graph read deadline, and 500 with a fixed message otherwise, never a silent repository_count 0. Callers holding the shared key receive the full report unchanged. A hosted BrowserSessionRoutePolicy that refuses all-scope callers on grant-bound routes refuses them here with a 403. A 503 with Retry-After means a transient condition that is safe to retry: for an unscoped caller the status snapshot read hit a stale or timed-out PostgreSQL reader or the graph repository count found the graph backend unavailable, and for a scoped caller, whose only read is the granted-repository count in the graph (the status snapshot is not read), the graph backend is unavailable; a 503 without Retry-After means a configuration state: the status reader (unscoped caller) or the graph backend (scoped caller) is not configured on this API instance. A scoped or unscoped caller's graph count that exceeds its read deadline answers 504. No 5xx body carries backend error text. A reader-verdict 503 error envelope names capability operations.index_status.",
        "operationId": "getIndexStatusLegacy",
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
`
