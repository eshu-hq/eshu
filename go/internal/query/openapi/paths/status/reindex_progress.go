// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package status

// ReindexProgress is the OpenAPI path fragment documenting
// GET /api/v0/admin/reindex/progress. It lives apart from Admin because
// admin.go was already near the 500-line cap (the compare.go precedent).
// openapi.Spec concatenates it into the published document right after
// Admin; keep it in lockstep with the handler and
// docs/public/reference/http-api/status-admin-reindex-progress.md.
const ReindexProgress = `
    "/api/v0/admin/reindex/progress": {
      "get": {
        "tags": ["admin"],
        "summary": "Report reindex watermark progress per scope",
        "description": "Read-only: records nothing, claims nothing, and completes nothing. Compares each named scope's newest activated full generation, by its own ingest time, against its effective reindex watermark — the later of the fleet watermark and the scope's per-repository watermark (GREATEST semantics: a per-repository row at or before the fleet watermark is already subsumed by it) — using the same rule the git reconciliation sweep decides on. A scope is satisfied when that full was ingested at or after its watermark, pending when it has an activated full older than that, and never_seen when it has no activated full at all; a re-projection acknowledgement never satisfies a scope because it re-projects old parse output instead of storing a re-parse. status is complete only when no named scope is pending or never_seen. fleet_requested_at is the watermark compared: the live fleet watermark by default, or the fleet_requested_at query override when given. requested_at echoes each scope's effective watermark and last_stored_full_at its newest activated full ingest time, both null when absent. Counts cover every named scope; scopes carries at most 10 samples, unfinished scopes first, with truncated set when scopes were left out. A 503 with Retry-After means the request checkpoint step failed, a transient condition that is safe to retry; a 503 without Retry-After means a configuration state: reindex progress reads are not configured on this API instance. No 503 body carries backend error text.",
        "parameters": [
          {"name": "scope_id", "in": "query", "required": true, "description": "Ingestion scope IDs to evaluate, repeated; 1 to 100, deduplicated.", "schema": {"type": "array", "minItems": 1, "maxItems": 100, "items": {"type": "string", "minLength": 1}}},
          {"name": "fleet_requested_at", "in": "query", "required": false, "description": "RFC 3339 fleet watermark override for what-if comparisons; defaults to the live fleet watermark.", "schema": {"type": "string", "format": "date-time"}}
        ],
        "responses": {
          "200": {
            "description": "Reindex progress report.",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "required": ["status", "fleet_requested_at", "counts", "scopes", "truncated"],
                  "properties": {
                    "status": {"type": "string", "enum": ["complete", "incomplete"]},
                    "fleet_requested_at": {"type": ["string", "null"], "format": "date-time", "description": "The fleet watermark compared, in UTC; null when no fleet reindex was ever requested and no override was given."},
                    "counts": {
                      "type": "object",
                      "required": ["satisfied", "pending", "never_seen"],
                      "properties": {
                        "satisfied": {"type": "integer"},
                        "pending": {"type": "integer"},
                        "never_seen": {"type": "integer"}
                      }
                    },
                    "scopes": {
                      "type": "array",
                      "maxItems": 10,
                      "description": "At most 10 samples, unfinished scopes first; never null.",
                      "items": {
                        "type": "object",
                        "required": ["scope_id", "state", "requested_at", "last_stored_full_at", "active_generation_id"],
                        "properties": {
                          "scope_id": {"type": "string"},
                          "state": {"type": "string", "enum": ["satisfied", "pending", "never_seen"]},
                          "requested_at": {"type": ["string", "null"], "format": "date-time", "description": "The scope's effective watermark in UTC; null when no watermark applies."},
                          "last_stored_full_at": {"type": ["string", "null"], "format": "date-time", "description": "The ingest time of the scope's newest activated full generation; null when it has none."},
                          "active_generation_id": {"type": "string", "description": "The scope's active generation; empty when it has none."}
                        }
                      }
                    },
                    "truncated": {"type": "boolean"}
                  }
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
`
