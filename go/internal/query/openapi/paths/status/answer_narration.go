// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package status

// AnswerNarration is the OpenAPI path fragment documenting the
// `/api/v0/status/answer-narration` route. openapi.Spec concatenates it into
// the published document; keep it in lockstep with the handlers and
// docs/public/reference/http-api.md.
const AnswerNarration = `
    "/api/v0/status/answer-narration": {
      "get": {
        "tags": ["status"],
        "summary": "Get answer narration status",
        "description": "Returns optional governed answer narration posture. The default is unavailable with deterministic answer packets still available as the canonical fallback; the response never includes prompts, provider responses, credentials, private paths, or source identifiers. A 503 with Retry-After means a transient condition that is safe to retry: the status snapshot read hit a stale or timed-out PostgreSQL reader; this route has no configuration 503, so every 503 it answers carries Retry-After. No 5xx body carries backend error text. A reader-verdict 503 names capability answer_narration.status in its error envelope.",
        "operationId": "getAnswerNarrationStatus",
        "x-scoped-token-support": true,
        "responses": {
          "403": {"$ref": "#/components/responses/Forbidden"},
          "200": {
            "description": "Answer narration status",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "state": {"type": "string", "enum": ["unavailable", "disabled", "available", "provider_unavailable"], "description": "Governed narration posture from answerNarrationStatusToMap; the default is unavailable."},
                    "reason": {"type": "string", "description": "Machine-readable reason for the posture."},
                    "provider_configured": {"type": "boolean", "description": "Whether a narration provider is configured."},
                    "provider_traffic_enabled": {"type": "boolean", "description": "Whether provider traffic is enabled."},
                    "policy_allowed": {"type": "boolean", "description": "Whether policy allows governed narration."},
                    "budget_available": {"type": "boolean", "description": "Whether narration budget remains."},
                    "publish_safety_enabled": {"type": "boolean", "description": "Whether publish safety gating is enabled."},
                    "deterministic_fallback_available": {"type": "boolean", "description": "Whether deterministic answer packets are available as the canonical fallback."},
                    "canonical_truth_affected": {"type": "boolean", "description": "Whether the posture affects canonical truth."},
                    "retention_posture": {"type": "string", "enum": ["metadata_only"], "description": "Retention posture; metadata only, never prompts or provider responses."},
                    "policy_hash": {"type": "string", "description": "Hash of the governing policy; present only when set."},
                    "supported_states": {"type": "array", "items": {"type": "string"}, "description": "Posture states the read model can report."},
                    "supported_reasons": {"type": "array", "items": {"type": "string"}, "description": "Reason codes the read model can report."},
                    "validator_reason_codes": {"type": "array", "items": {"type": "string"}, "description": "Reason codes the narration validator can report."},
                    "detail": {"type": "string", "description": "Human-readable detail; present only when non-empty."},
                    "updated_at": {"type": "string", "format": "date-time", "description": "When the posture was last computed; present only when known."}
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
