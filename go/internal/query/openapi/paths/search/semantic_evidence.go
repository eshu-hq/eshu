// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package search

// SemanticEvidence is the OpenAPI path fragment documenting the
// `/api/v0/semantic/code-hints`, `/api/v0/semantic/documentation-observations`
// routes. openapi.Spec concatenates it into the published document; keep it in
// lockstep with the handlers and docs/public/reference/http-api.md.
const SemanticEvidence = `
    "/api/v0/semantic/documentation-observations": {
      "get": {
        "tags": ["semantic"],
        "summary": "List semantic documentation observations",
        "description": "Lists opt-in LLM-assisted documentation observations from durable semantic evidence facts. Rows include truth basis, freshness, provider profile, prompt version, redaction version, and policy state without raw prompt payloads, credentials, or private provider responses. A 503 with Retry-After means a transient condition that is safe to retry: the semantic evidence read hit a stale or timed-out PostgreSQL reader; this route has no configuration 503, so every 503 it answers carries Retry-After. No 5xx body carries backend error text. A reader-verdict 503 names capability semantic_evidence.documentation_observations.list in its error envelope.",
        "operationId": "listSemanticDocumentationObservations",
        "x-scoped-token-support": true,
        "parameters": [
          {"name": "fact_id", "in": "query", "schema": {"type": "string"}},
          {"name": "scope_id", "in": "query", "schema": {"type": "string"}},
          {"name": "generation_id", "in": "query", "schema": {"type": "string"}, "description": "Omit to read each scope's active generation; set to read one exact generation, including a superseded one. Page binding and truth freshness label it."},
          {"name": "repo", "in": "query", "schema": {"type": "string"}},
          {"name": "target_kind", "in": "query", "schema": {"type": "string"}},
          {"name": "target_id", "in": "query", "schema": {"type": "string"}},
          {"name": "service_id", "in": "query", "schema": {"type": "string"}},
          {"name": "source_class", "in": "query", "schema": {"type": "string"}},
          {"name": "source_id", "in": "query", "schema": {"type": "string"}},
          {"name": "document_id", "in": "query", "schema": {"type": "string"}},
          {"name": "section_id", "in": "query", "schema": {"type": "string"}},
          {"name": "provider_profile_id", "in": "query", "schema": {"type": "string"}},
          {"name": "provider_kind", "in": "query", "schema": {"type": "string"}},
          {"name": "prompt_version", "in": "query", "schema": {"type": "string"}},
          {"name": "redaction_version", "in": "query", "schema": {"type": "string"}},
          {"name": "extraction_mode", "in": "query", "schema": {"type": "string"}},
          {"name": "policy_state", "in": "query", "schema": {"type": "string"}},
          {"name": "redaction_state", "in": "query", "schema": {"type": "string"}},
          {"name": "freshness_state", "in": "query", "schema": {"type": "string"}},
          {"name": "admission_state", "in": "query", "schema": {"type": "string"}},
          {"name": "observation_type", "in": "query", "schema": {"type": "string"}},
          {"name": "q", "in": "query", "schema": {"type": "string"}},
          {"name": "updated_since", "in": "query", "schema": {"type": "string"}},
          {"name": "limit", "in": "query", "schema": {"type": "integer", "minimum": 1, "maximum": 200, "default": 50}},
          {"name": "cursor", "in": "query", "schema": {"type": "string"}}
        ],
        "responses": {
          "200": {"description": "Semantic documentation observation page", "content": {"application/json": {"schema": {"$ref": "#/components/schemas/SemanticObservationList"}}}},
          "400": {"$ref": "#/components/responses/BadRequest"},
          "403": {"$ref": "#/components/responses/Forbidden"},
          "500": {"$ref": "#/components/responses/InternalError"},
          "501": {
            "description": "Not implemented. With no Postgres fact read model wired, the route answers error code documentation_read_model_unavailable: the plain body carries error_code, message, and capability, and the envelope carries code, message, and capability. A runtime profile that does not support the capability answers error code unsupported_capability: the plain body carries error and detail, and the envelope carries code, message, capability, and the current and required profiles. Every runtime profile supports this capability today.",
            "content": {
              "application/json": {
                "schema": {"$ref": "#/components/schemas/ErrorResponse"}
              }
            }
          },
          "503": {"$ref": "#/components/responses/ServiceUnavailable"}
        }
      }
    },
    "/api/v0/semantic/code-hints": {
      "get": {
        "tags": ["semantic"],
        "summary": "List semantic code hints",
        "description": "Lists opt-in non-canonical code hints from durable semantic evidence facts. Code hints remain separate from deterministic code, graph, and relationship routes unless callers request this surface explicitly. A 503 with Retry-After means a transient condition that is safe to retry: the semantic evidence read hit a stale or timed-out PostgreSQL reader; this route has no configuration 503, so every 503 it answers carries Retry-After. No 5xx body carries backend error text. A reader-verdict 503 names capability semantic_evidence.code_hints.list in its error envelope.",
        "operationId": "listSemanticCodeHints",
        "x-scoped-token-support": true,
        "parameters": [
          {"name": "fact_id", "in": "query", "schema": {"type": "string"}},
          {"name": "scope_id", "in": "query", "schema": {"type": "string"}},
          {"name": "generation_id", "in": "query", "schema": {"type": "string"}, "description": "Omit to read each scope's active generation; set to read one exact generation, including a superseded one. Page binding and truth freshness label it."},
          {"name": "repo", "in": "query", "schema": {"type": "string"}},
          {"name": "target_kind", "in": "query", "schema": {"type": "string"}},
          {"name": "target_id", "in": "query", "schema": {"type": "string"}},
          {"name": "service_id", "in": "query", "schema": {"type": "string"}},
          {"name": "source_class", "in": "query", "schema": {"type": "string"}},
          {"name": "source_id", "in": "query", "schema": {"type": "string"}},
          {"name": "relative_path", "in": "query", "schema": {"type": "string"}},
          {"name": "entity_id", "in": "query", "schema": {"type": "string"}},
          {"name": "provider_profile_id", "in": "query", "schema": {"type": "string"}},
          {"name": "provider_kind", "in": "query", "schema": {"type": "string"}},
          {"name": "prompt_version", "in": "query", "schema": {"type": "string"}},
          {"name": "redaction_version", "in": "query", "schema": {"type": "string"}},
          {"name": "extraction_mode", "in": "query", "schema": {"type": "string"}},
          {"name": "policy_state", "in": "query", "schema": {"type": "string"}},
          {"name": "redaction_state", "in": "query", "schema": {"type": "string"}},
          {"name": "freshness_state", "in": "query", "schema": {"type": "string"}},
          {"name": "corroboration_state", "in": "query", "schema": {"type": "string"}},
          {"name": "hint_type", "in": "query", "schema": {"type": "string"}},
          {"name": "relationship_kind", "in": "query", "schema": {"type": "string"}},
          {"name": "q", "in": "query", "schema": {"type": "string"}},
          {"name": "updated_since", "in": "query", "schema": {"type": "string"}},
          {"name": "limit", "in": "query", "schema": {"type": "integer", "minimum": 1, "maximum": 200, "default": 50}},
          {"name": "cursor", "in": "query", "schema": {"type": "string"}}
        ],
        "responses": {
          "200": {"description": "Semantic code hint page", "content": {"application/json": {"schema": {"$ref": "#/components/schemas/SemanticCodeHintList"}}}},
          "400": {"$ref": "#/components/responses/BadRequest"},
          "403": {"$ref": "#/components/responses/Forbidden"},
          "500": {"$ref": "#/components/responses/InternalError"},
          "501": {
            "description": "Not implemented. With no Postgres fact read model wired, the route answers error code documentation_read_model_unavailable: the plain body carries error_code, message, and capability, and the envelope carries code, message, and capability. A runtime profile that does not support the capability answers error code unsupported_capability: the plain body carries error and detail, and the envelope carries code, message, capability, and the current and required profiles. Every runtime profile supports this capability today.",
            "content": {
              "application/json": {
                "schema": {"$ref": "#/components/schemas/ErrorResponse"}
              }
            }
          },
          "503": {"$ref": "#/components/responses/ServiceUnavailable"}
        }
      }
    },
`
