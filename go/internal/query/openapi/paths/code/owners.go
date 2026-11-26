// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package code

// Owners is the OpenAPI path fragment documenting the
// `/api/v0/codeowners/ownership` route. openapi.Spec concatenates it into the
// published document; keep it in lockstep with the handlers and
// docs/public/reference/http-api.md.
const Owners = `
    "/api/v0/codeowners/ownership": {
      "get": {
        "tags": ["codeowners"],
        "summary": "List a repository's CODEOWNERS ownership declarations",
        "description": "Bounded read of one repository's Phase 3 DECLARES_CODEOWNER graph edges (issue #5419), plus an effective_owner resolved by manifest-vs-codeowners precedence: a service-catalog manifest declaration with an exact or derived reducer outcome wins; otherwise the CODEOWNERS last-match-wins rule (the highest order_index rule) applies; otherwise effective_owner is empty. Scoped tokens receive an empty ownership page and effective_owner when repository_id is outside their granted repository set.",
        "operationId": "listCodeownersOwnership",
        "x-scoped-token-support": true,
        "parameters": [
          {"name": "repository_id", "in": "query", "required": true, "schema": {"type": "string"}, "description": "Repository to anchor the read on."},
          {"name": "limit", "in": "query", "schema": {"type": "integer", "minimum": 1, "maximum": 200, "default": 50}, "description": "Maximum rows to return. Defaults to 50, capped at 200."},
          {"name": "after_order_index", "in": "query", "schema": {"type": "integer"}, "description": "Keyset cursor order_index component from a prior next_cursor. Must be sent with after_pattern and after_ref."},
          {"name": "after_pattern", "in": "query", "schema": {"type": "string"}, "description": "Keyset cursor pattern component from a prior next_cursor. Must be sent with after_order_index and after_ref."},
          {"name": "after_ref", "in": "query", "schema": {"type": "string"}, "description": "Keyset cursor owner_ref component from a prior next_cursor. Must be sent with after_order_index and after_pattern."}
        ],
        "responses": {
          "403": {"$ref": "#/components/responses/Forbidden"},
          "504": {"$ref": "#/components/responses/GatewayTimeout"},
          "200": {
            "description": "Bounded codeowners ownership page",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "description": "Bounded CODEOWNERS ownership page emitted by ListOwnership.",
                  "properties": {
                    "ownership": {
                      "type": "array",
                      "description": "DECLARES_CODEOWNER edges for the repository, per ListOwnership.",
                      "items": {
                        "type": "object",
                        "description": "One ownership declaration row: pattern, source path, order, and owner.",
                        "properties": {
                          "pattern": {"type": "string", "description": "CODEOWNERS pattern reported by ListOwnership."},
                          "source_path": {"type": "string", "description": "Source path of the CODEOWNERS rule reported by ListOwnership."},
                          "order_index": {"type": "integer", "description": "Rule order index (last-match-wins) reported by ListOwnership."},
                          "owner_ref": {"type": "string", "description": "Owner reference reported by ListOwnership."}
                        },
                        "required": ["pattern", "source_path", "order_index", "owner_ref"]
                      }
                    },
                    "repository_id": {"type": "string", "description": "Repository the read was anchored on, per ListOwnership."},
                    "count": {"type": "integer", "description": "Rows on this page counted by listCodeownersOwnership."},
                    "limit": {"type": "integer", "description": "Page bound echoed by listCodeownersOwnership."},
                    "truncated": {"type": "boolean", "description": "True when more declarations exist past this page, per ListOwnership."},
                    "next_cursor": {
                      "type": "object",
                      "description": "Keyset cursor for the next page, per listCodeownersOwnership; present only when truncated is true.",
                      "properties": {
                        "after_order_index": {"type": "integer", "description": "Cursor order_index component, per ListOwnership."},
                        "after_pattern": {"type": "string", "description": "Cursor pattern component, per ListOwnership."},
                        "after_ref": {"type": "string", "description": "Cursor owner_ref component, per ListOwnership."}
                      },
                      "required": ["after_order_index", "after_pattern", "after_ref"]
                    },
                    "effective_owner": {
                      "type": "object",
                      "description": "Owner resolved by manifest-vs-codeowners precedence, per ListOwnership. Empty ({}) when nothing resolves, per the omitempty fields of EffectiveRepositoryOwner.",
                      "properties": {
                        "owner_ref": {"type": "string", "description": "Resolved owner reference, per ListOwnership."},
                        "source": {"type": "string", "enum": ["service_catalog", "codeowners"], "description": "Which declaration won precedence, per ListOwnership."}
                      }
                    }
                  },
                  "required": ["ownership", "repository_id", "count", "limit", "truncated", "effective_owner"]
                }
              }
            }
          },
          "400": {"description": "Missing repository_id, invalid limit, or a partial keyset cursor"},
          "500": {"description": "Codeowners ownership graph read failed, or resolving the effective repository owner failed"},
          "501": {"description": "Capability unsupported for the active query profile"},
          "503": {"description": "Authoritative graph backend unavailable"}
        }
      }
    },`
