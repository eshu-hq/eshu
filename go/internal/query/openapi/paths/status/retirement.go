// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package status

// RetirementTrigger is the OpenAPI path fragment documenting
// POST /api/v0/admin/repositories/retire. It lives apart from Admin because
// admin.go was already near the 500-line cap (the reindex_progress.go
// precedent). openapi.Spec concatenates it into the published document
// right after Admin; keep it in lockstep with the handler and
// docs/public/reference/http-api/admin-retirement-trigger.md.
const RetirementTrigger = `
    "/api/v0/admin/repositories/retire": {
      "post": {
        "tags": ["admin"],
        "summary": "Retire repositories",
        "description": "Marks the named repositories retiring, fences their scopes, then purges the fenced rows, in that order: the phase-1 fence commits the tombstone rows and generation marks in one transaction, and the purge runs only after the fence committed, one transaction per repository. Requires repositories (1 to 25 selectors resolving to git default-branch repositories), reason, idempotency_key, and an admin (all-scopes) token. A 200 with status retired means every repository purged; status blocked means a repository's generations were still live, so the purge wrote nothing and the fenced tombstone stays open for a later pass with the same idempotency key. An unfenced tombstone or a tombstone past pending/fenced is refused with 409, as is a blocked fence, an already-open retirement under a different key, or an ambiguous selector. Unknown selectors are 404 and non-git scopes are 422, both before anything is marked. A 503 with Retry-After means the request checkpoint step failed, a transient condition that is safe to retry; a 503 without Retry-After means a configuration state: the retirement trigger is not configured on this API instance. No 503 body carries backend error text.",
        "requestBody": {
          "required": true,
          "content": {
            "application/json": {
              "schema": {
                "type": "object",
                "required": ["repositories", "reason", "idempotency_key"],
                "properties": {
                  "repositories": {"type": "array", "items": {"type": "string"}, "description": "Repository selectors (repository ID, scope ID, name, slug, or path), 1 to 25. Every selector must resolve to one git default-branch repository or the whole request is refused."},
                  "reason": {"type": "string", "description": "Why the retirement is safe. Stored as a safe hash, never in cleartext."},
                  "idempotency_key": {"type": "string", "description": "Makes the retirement safe under retries: a retry with the same key resumes the open tombstone rows instead of opening second ones."}
                }
              }
            }
          }
        },
        "responses": {
          "200": {"description": "Retirement trigger outcome: status retired when every repository purged, blocked when a repository's generations were still live and its fenced tombstone stays open"},
          "400": {"$ref": "#/components/responses/BadRequest"},
          "403": {"$ref": "#/components/responses/Forbidden"},
          "404": {"$ref": "#/components/responses/NotFound"},
          "409": {"$ref": "#/components/responses/Conflict"},
          "422": {
            "description": "Refused before anything was marked, so the idempotency key is not consumed: a selector resolved to a non-git scope and only git default-branch repositories retire.",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "required": ["status", "reason", "detail"],
                  "properties": {
                    "status": {"type": "string", "enum": ["refused"]},
                    "reason": {"type": "string"},
                    "detail": {"type": "string"}
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
`
