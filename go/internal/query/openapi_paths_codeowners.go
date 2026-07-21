// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

const openAPIPathsCodeowners = `
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
          "200": {
            "description": "Bounded codeowners ownership page",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "ownership": {
                      "type": "array",
                      "items": {
                        "type": "object",
                        "properties": {
                          "pattern": {"type": "string"},
                          "source_path": {"type": "string"},
                          "order_index": {"type": "integer"},
                          "owner_ref": {"type": "string"}
                        },
                        "required": ["pattern", "source_path", "order_index", "owner_ref"]
                      }
                    },
                    "repository_id": {"type": "string"},
                    "count": {"type": "integer"},
                    "limit": {"type": "integer"},
                    "truncated": {"type": "boolean"},
                    "next_cursor": {
                      "type": "object",
                      "properties": {
                        "after_order_index": {"type": "integer"},
                        "after_pattern": {"type": "string"},
                        "after_ref": {"type": "string"}
                      }
                    },
                    "effective_owner": {
                      "type": "object",
                      "properties": {
                        "owner_ref": {"type": "string"},
                        "source": {"type": "string", "enum": ["service_catalog", "codeowners"]}
                      }
                    }
                  },
                  "required": ["ownership", "repository_id", "count", "limit", "truncated", "effective_owner"]
                }
              }
            }
          },
          "400": {"description": "Missing repository_id, invalid limit, or a partial keyset cursor"},
          "501": {"description": "Capability unsupported for the active query profile"},
          "503": {"description": "Authoritative graph backend unavailable"}
        }
      }
    },`
