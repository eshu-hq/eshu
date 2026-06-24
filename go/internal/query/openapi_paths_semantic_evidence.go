// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

const openAPIPathsSemanticEvidence = `
    "/api/v0/semantic/documentation-observations": {
      "get": {
        "tags": ["semantic"],
        "summary": "List semantic documentation observations",
        "description": "Lists opt-in LLM-assisted documentation observations from durable semantic evidence facts. Rows include truth basis, freshness, provider profile, prompt version, redaction version, and policy state without raw prompt payloads, credentials, or private provider responses.",
        "operationId": "listSemanticDocumentationObservations",
        "parameters": [
          {"name": "fact_id", "in": "query", "schema": {"type": "string"}},
          {"name": "scope_id", "in": "query", "schema": {"type": "string"}},
          {"name": "generation_id", "in": "query", "schema": {"type": "string"}},
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
          "500": {"$ref": "#/components/responses/InternalError"}
        }
      }
    },
    "/api/v0/semantic/code-hints": {
      "get": {
        "tags": ["semantic"],
        "summary": "List semantic code hints",
        "description": "Lists opt-in non-canonical code hints from durable semantic evidence facts. Code hints remain separate from deterministic code, graph, and relationship routes unless callers request this surface explicitly.",
        "operationId": "listSemanticCodeHints",
        "parameters": [
          {"name": "fact_id", "in": "query", "schema": {"type": "string"}},
          {"name": "scope_id", "in": "query", "schema": {"type": "string"}},
          {"name": "generation_id", "in": "query", "schema": {"type": "string"}},
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
          "500": {"$ref": "#/components/responses/InternalError"}
        }
      }
    },
`
