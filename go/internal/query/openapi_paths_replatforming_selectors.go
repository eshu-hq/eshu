// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

// openAPIPathsReplatformingSelectors documents the bounded inventory of active
// AWS collector scopes that can truthfully anchor replatforming plan reads.
const openAPIPathsReplatformingSelectors = `
    "/api/v0/replatforming/selectors": {
      "get": {
        "tags": ["iac"],
        "summary": "List active AWS scopes for bounded replatforming review",
        "operationId": "listReplatformingSelectors",
        "x-scoped-token-support": true,
        "description": "Lists active AWS collector scopes and active-generation drift-finding counts that can anchor the existing bounded replatforming routes. Active scopes with zero findings are included as authoritative-empty choices. Scoped callers see only exact granted AWS scope ids; repository-only and empty grants return an empty page. The route is read-only and never scans inactive or superseded generations.",
        "parameters": [
          {"name": "limit", "in": "query", "description": "Maximum active scopes to return. One lookahead row makes truncation explicit.", "schema": {"type": "integer", "minimum": 1, "maximum": 200, "default": 100}}
        ],
        "responses": {
          "200": {
            "description": "Bounded active AWS replatforming selector inventory",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "scopes": {
                      "type": "array",
                      "items": {
                        "type": "object",
                        "properties": {
                          "scope_id": {"type": "string", "description": "Canonical AWS collector scope id."},
                          "account_id": {"type": "string", "description": "AWS account id parsed from the canonical scope."},
                          "region": {"type": "string"},
                          "service": {"type": "string"},
                          "label": {"type": "string", "description": "Human-readable service, region, and masked-account label."},
                          "finding_count": {"type": "integer", "minimum": 0, "description": "Active-generation replatforming finding count; zero is authoritative when the scope is active."}
                        },
                        "required": ["scope_id", "account_id", "region", "service", "label", "finding_count"]
                      }
                    },
                    "count": {"type": "integer", "minimum": 0},
                    "limit": {"type": "integer"},
                    "truncated": {"type": "boolean"},
                    "empty_scope_count": {"type": "integer", "minimum": 0},
                    "supported_scope_kinds": {"type": "array", "items": {"type": "string", "enum": ["account", "region", "service"]}},
                    "finding_kinds": {"type": "array", "items": {"type": "string"}},
                    "page_sizes": {"type": "array", "items": {"type": "integer"}},
                    "readiness": {
                      "type": "object",
                      "properties": {
                        "state": {"type": "string", "enum": ["ready", "collector_evidence_absent", "no_authorized_scopes"]},
                        "detail": {"type": "string"},
                        "next_action": {"type": "string"}
                      },
                      "required": ["state", "detail", "next_action"]
                    }
                  },
                  "required": ["scopes", "count", "limit", "truncated", "empty_scope_count", "supported_scope_kinds", "finding_kinds", "page_sizes", "readiness"]
                }
              }
            }
          },
          "400": {"$ref": "#/components/responses/BadRequest"},
          "501": {"$ref": "#/components/responses/ServiceUnavailable"},
          "503": {"$ref": "#/components/responses/ServiceUnavailable"},
          "500": {"$ref": "#/components/responses/InternalError"}
        }
      }
    },
`
