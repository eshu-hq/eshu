// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package openapi

// componentsRecovery documents the DeltaActiveScopesReport and
// ReindexRequestsWrittenReport schemas the refinalize responses carry (#7797).
// Keep them in lockstep with recovery.DeltaActiveScopesReport and
// recovery.ReindexRequestsWrittenReport.
const componentsRecovery = `      "DeltaActiveScopesReport": {
        "type": "object",
        "description": "Re-enqueued scopes whose re-projected generation is a delta (POST /api/v0/admin/recover-generations, POST /api/v0/admin/refinalize). A delta generation carries only the files that changed since its baseline, so re-projecting it onto an empty graph restores only those files: the graph for these scopes is incomplete until a full generation activates (#7797). Always present in a performed refinalize, with empty objects and an empty detail when no re-projected generation was a delta.",
        "required": ["total", "by_outcome", "sample_scope_ids", "detail"],
        "properties": {
          "total": {"type": "integer", "description": "Scopes re-projected through a delta generation across every outcome."},
          "by_outcome": {
            "type": "object",
            "description": "Exact count per outcome. Outcomes: reindex_requested (a git default-branch repository scope; the refinalize recorded a per-repository reindex watermark in the same transaction; a git ingester's scheduled selection forces a full re-parse when it next syncs the repository, at most ESHU_REPO_RECONCILE_MAX_PER_CYCLE per cycle and later while an in-flight or recently failed full holds the scope, and a webhook-only ingester forces it only when a webhook triggers that repository), reindex_unsupported (a git ref scope or another collector's scope; no reindex watermark can force it, so only that collector's next full generation repairs the graph).",
            "additionalProperties": {"type": "integer"}
          },
          "sample_scope_ids": {
            "type": "object",
            "description": "Up to 10 delta-active scope ids per outcome, ascending.",
            "additionalProperties": {"type": "array", "items": {"type": "string"}}
          },
          "detail": {"type": "string", "description": "Operator message stating that the graph is incomplete for these scopes until a full generation activates. Empty when total is 0."}
        }
      },
      "ReindexRequestsWrittenReport": {
        "type": "object",
        "description": "Per-repository reindex watermarks this refinalize wrote to repository_reindex_requests in its own transaction, one per delta-active git default-branch scope (#7797). The same rows POST /api/v0/admin/reindex with scope repository writes. A git ingester's scheduled selection honors a row when it next syncs the repository, at most ESHU_REPO_RECONCILE_MAX_PER_CYCLE per cycle and subject to the in-flight and retry-backoff throttle; a webhook-only ingester honors it only for a repository a webhook triggers. A row is satisfied when a full generation ingested at or after its requested_at activates.",
        "required": ["count", "scope_ids"],
        "properties": {
          "count": {"type": "integer", "description": "Exact number of scopes that got a reindex watermark."},
          "scope_ids": {"type": "array", "items": {"type": "string"}, "description": "Up to 10 of those scope ids, ascending. Never null."}
        }
      },
`
