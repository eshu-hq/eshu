// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package status

// RepositoryRetirement is the OpenAPI path fragment documenting
// POST /api/v0/admin/repository-retirements. It lives apart from Admin
// because admin.go is already near the 500-line cap (the compare.go
// precedent). openapi.Spec concatenates it into the published document
// right after Admin; keep it in lockstep with the handler and
// docs/public/reference/http-api/status-admin-repository-retirement.md.
const RepositoryRetirement = `
    "/api/v0/admin/repository-retirements": {
      "post": {
        "tags": ["admin"],
        "summary": "Retire repositories: mark, fence, then purge their indexed state",
        "description": "Operator-driven repository retirement (#8602). Resolves every selector to one git default-branch repository, then runs the tombstone mark, the phase-1 fence, and the narrowed purge in order for the resolved set. A 202 carries the per-repository outcomes inline: completed repositories were purged and tombstoned, blocked repositories keep their fenced tombstone open for a later pass (terminal_not_reached names live generations only a new fence can move; scope_lock_busy names reducer-retention contention worth one retry; derive_lock_busy names a derive advisory lock held past the purge's wait for it, 10 s per lock and 30 s across the request, then 100 ms attempts, also worth a retry). A 409 names the fence refusal reason (projector_lease_live, inflight_reducers, lock_timeout, claim_fence_busy, request_too_large) or an already-open retirement under a different idempotency key. A 409 or 500 from the trigger step carries the per-repository entries inline (#8614): one entry per requested repository in request order with the idempotency key and duplicate flag; repositories marked before a mid-request mark failure stay committed and open with their terminal outcome, and repositories never attempted report the response-only not_attempted outcome. A re-POST with the same idempotency key resumes the open retirement and reports duplicate true. A 503 means repository retirement is not configured on this API instance. A graph read deadline during selector resolution answers 504. No 503 or 500 body carries backend error text.",
        "requestBody": {
          "required": true,
          "content": {
            "application/json": {
              "schema": {
                "type": "object",
                "required": ["repositories", "reason", "idempotency_key"],
                "properties": {
                  "repositories": {"type": "array", "minItems": 1, "maxItems": 25, "items": {"type": "string", "minLength": 1}, "description": "Repository selectors (repository ID, scope ID, name, slug, or path); every selector must resolve to exactly one git default-branch repository, 1 to 25, deduplicated by scope."},
                  "reason": {"type": "string", "minLength": 1, "description": "Why this retirement is safe; stored only as a safe hash, never in plaintext."},
                  "idempotency_key": {"type": "string", "minLength": 1, "description": "Idempotency key; a retry with the same key resumes the open retirement instead of opening a second one."}
                }
              }
            }
          }
        },
        "responses": {
          "202": {
            "description": "Retirement outcomes per repository, blocked exits inline.",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "description": "Retirement trigger outcome. Owner: admin.retirement.triggerResponse.",
                  "required": ["status", "retirements", "idempotency_key", "duplicate"],
                  "properties": {
                    "status": {"type": "string", "enum": ["accepted"], "description": "Request disposition. Owner: admin.retirement.triggerResponse."},
                    "retirements": {
                      "type": "array",
                      "description": "One entry per requested repository in request order. Owner: admin.retirement.retirementEntries.",
                      "items": {
                        "type": "object",
                        "description": "One repository retirement outcome. Owner: admin.retirement.retirementEntries.",
                        "required": ["retirement_id", "repository_id", "scope_id", "outcome"],
                        "properties": {
                          "retirement_id": {"type": "string", "description": "Server-side tombstone key for this repository. Owner: admin.retirement.retirementEntries via repo.RetirementID."},
                          "repository_id": {"type": "string", "description": "Retired repository id. Owner: admin.retirement.retirementEntries via repo.RepoID."},
                          "scope_id": {"type": "string", "description": "Retired repository scope id. Owner: admin.retirement.retirementEntries via repo.ScopeID."},
                          "outcome": {"type": "string", "enum": ["completed", "blocked"], "description": "Per-repository outcome on the 202 path (refused/failed/not_attempted appear only on error bodies). Owner: admin.retirement.retirementEntries via repo.Outcome."},
                          "blocked_reason": {"type": "string", "description": "Bounded purge-block reason, present only when outcome is blocked. Owner: admin.retirement.retirementEntries via repo.BlockedReason."},
                          "blocked_detail": {"type": "string", "description": "Bounded purge-block detail, present only when outcome is blocked. Owner: admin.retirement.retirementEntries via repo.BlockedDetail."},
                          "blocking_statuses": {"type": "string", "description": "Purge-block live-generation statuses, present only when outcome is blocked. Owner: admin.retirement.retirementEntries via repo.BlockedStatuses."}
                        }
                      }
                    },
                    "idempotency_key": {"type": "string", "description": "Echo of the request idempotency key. Owner: admin.retirement.triggerResponse."},
                    "duplicate": {"type": "boolean", "description": "True when a re-POST resumed an open retirement under the same key. Owner: admin.retirement.triggerResponse via result.Duplicate."}
                  }
                }
              }
            }
          },
          "400": {"$ref": "#/components/responses/BadRequest"},
          "403": {"$ref": "#/components/responses/Forbidden"},
          "409": {"$ref": "#/components/responses/Conflict"},
          "500": {"$ref": "#/components/responses/InternalError"},
          "503": {"$ref": "#/components/responses/ServiceUnavailable"},
          "504": {"$ref": "#/components/responses/GatewayTimeout"}
        }
      }
    },
`
