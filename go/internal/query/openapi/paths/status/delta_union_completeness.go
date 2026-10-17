// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package status

// DeltaUnionCompleteness is the OpenAPI fragment for the delta-union graph
// completeness endpoint. openapi.Spec concatenates it after Admin; keep it in
// lockstep with its handler and the HTTP API reference.
const DeltaUnionCompleteness = `
    "/api/v0/admin/delta-union/completeness": {
      "get": {
        "tags": ["admin"],
        "summary": "Check delta-union graph completeness",
        "description": "Read-only prod completeness check (#7797): per repository, the graph's file set against the ACTIVE set (base-full overlaid with the active delta). Repeat the scope query parameter to name scopes; at least one scope is required (an empty filter fans out and is rejected). Past the repository cap the call fails loudly instead of paging silently. A mismatch names the diff and the generations to re-parse; it never repairs. 503 when the check is not configured on the instance. A failed ACTIVE or graph read answers 500 with the fixed detail \"delta-union completeness check failed\" (499 when the client canceled); a filter past the repository cap answers 500 with the narrow-the-filter text. No 5xx body carries backend error text.",
        "parameters": [
          {"name": "scope", "in": "query", "description": "Scope id filter, repeatable, at least one required.", "schema": {"type": "string"}}
        ],
        "responses": {
          "200": {"description": "Completeness report: one entry per checked repository with outcome complete, mismatch, or skipped."},
          "400": {"$ref": "#/components/responses/BadRequest"},
          "500": {"$ref": "#/components/responses/InternalError"},
          "503": {"description": "The completeness check is not configured on this API instance"}
        }
      }
    },
`
