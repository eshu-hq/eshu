// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package status

// Semantic is the OpenAPI path fragment documenting the
// `/api/v0/status/semantic-extraction` route. openapi.Spec concatenates it
// into the published document; keep it in lockstep with the handlers and
// docs/public/reference/http-api.md.
const Semantic = `
    "/api/v0/status/semantic-extraction": {
      "get": {
        "tags": ["status"],
        "summary": "Get semantic extraction status",
        "description": "Returns optional semantic extraction capability status. No-provider mode is reported as unavailable without changing index, reducer, API, MCP, or documentation fact health. A 503 with Retry-After means a transient condition that is safe to retry: the status snapshot read hit a stale or timed-out PostgreSQL reader; this route has no configuration 503, so every 503 it answers carries Retry-After. No 5xx body carries backend error text. A reader-verdict 503 names capability semantic_extraction.status in its error envelope.",
        "operationId": "getSemanticExtractionStatus",
        "x-scoped-token-support": true,
        "responses": {
          "403": {"$ref": "#/components/responses/Forbidden"},
          "200": {
            "description": "Semantic extraction capability status",
            "content": {
              "application/json": {
                "schema": ` + semanticExtractionSchema + `
              }
            }
          },
          "500": {"$ref": "#/components/responses/InternalError"},
          "503": {"$ref": "#/components/responses/ServiceUnavailable"}
        }
      }
    },
`
