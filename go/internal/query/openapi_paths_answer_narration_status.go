// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

const openAPIPathsAnswerNarrationStatus = `
    "/api/v0/status/answer-narration": {
      "get": {
        "tags": ["status"],
        "summary": "Get answer narration status",
        "description": "Returns optional governed answer narration posture. The default is unavailable with deterministic answer packets still available as the canonical fallback; the response never includes prompts, provider responses, credentials, private paths, or source identifiers.",
        "operationId": "getAnswerNarrationStatus",
        "responses": {
          "200": {
            "description": "Answer narration status",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "state": {"type": "string", "enum": ["unavailable", "disabled", "available", "provider_unavailable"]},
                    "reason": {"type": "string"},
                    "provider_configured": {"type": "boolean"},
                    "provider_traffic_enabled": {"type": "boolean"},
                    "policy_allowed": {"type": "boolean"},
                    "budget_available": {"type": "boolean"},
                    "publish_safety_enabled": {"type": "boolean"},
                    "deterministic_fallback_available": {"type": "boolean"},
                    "canonical_truth_affected": {"type": "boolean"},
                    "retention_posture": {"type": "string", "enum": ["metadata_only"]},
                    "policy_hash": {"type": "string"},
                    "supported_states": {"type": "array", "items": {"type": "string"}},
                    "supported_reasons": {"type": "array", "items": {"type": "string"}},
                    "validator_reason_codes": {"type": "array", "items": {"type": "string"}},
                    "detail": {"type": "string"},
                    "updated_at": {"type": "string", "format": "date-time"}
                  }
                }
              }
            }
          },
          "500": {"$ref": "#/components/responses/InternalError"}
        }
      }
    },
`
