// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package iac

// ReplatformingSelectors documents the bounded inventory of active
// AWS collector scopes that can truthfully anchor replatforming plan reads.
const ReplatformingSelectors = `
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
                      "description": "Active AWS collector scopes that can anchor replatforming reads, emitted by the iac selectors handler from the active-generation inventory.",
                      "items": {
                        "type": "object",
                        "description": "One active scope row. Owner: iac selectors handler.",
                        "properties": {
                          "scope_id": {"type": "string", "description": "Canonical AWS collector scope id."},
                          "account_id": {"type": "string", "description": "AWS account id parsed from the canonical scope."},
                          "region": {"type": "string", "description": "AWS region parsed from the canonical scope, emitted by the iac selectors handler scope rows."},
                          "service": {"type": "string", "description": "Collector service parsed from the canonical scope, emitted by the iac selectors handler scope rows."},
                          "label": {"type": "string", "description": "Human-readable service, region, and masked-account label."},
                          "finding_count": {"type": "integer", "minimum": 0, "description": "Active-generation replatforming finding count; zero is authoritative when the scope is active."}
                        },
                        "required": ["scope_id", "account_id", "region", "service", "label", "finding_count"]
                      }
                    },
                    "count": {"type": "integer", "minimum": 0, "description": "Scopes returned in this page, emitted by the iac selectors handler as len(scopes)."},
                    "limit": {"type": "integer", "description": "Echo of the caller's page size, emitted by the iac selectors handler from the normalized limit."},
                    "truncated": {"type": "boolean", "description": "True when more scopes exist past this page, emitted by the iac selectors handler from the lookahead row."},
                    "empty_scope_count": {"type": "integer", "minimum": 0, "description": "Active scopes with zero findings included as authoritative-empty choices, counted by the iac selectors handler."},
                    "supported_scope_kinds": {"type": "array", "items": {"type": "string", "enum": ["account", "region", "service"]}, "description": "Scope dimensions the replatforming reads accept, emitted by the iac selectors handler (always account, region, service)."},
                    "finding_kinds": {"type": "array", "items": {"type": "string"}, "description": "Finding kinds anchoring the selector counts, emitted by the iac selectors handler from the replatforming finding kinds."},
                    "page_sizes": {"type": "array", "items": {"type": "integer"}, "description": "Supported page sizes for the replatforming reads, emitted by the iac selectors handler (always 25, 50, 100, 200)."},
                    "readiness": {
                      "type": "object",
                      "description": "Selector readiness verdict, emitted by iac.replatformingSelectorReadiness.",
                      "properties": {
                        "state": {"type": "string", "enum": ["ready", "collector_evidence_absent", "no_authorized_scopes"], "description": "Readiness state emitted by iac.replatformingSelectorReadiness."},
                        "detail": {"type": "string", "description": "Human-readable readiness detail emitted by iac.replatformingSelectorReadiness."},
                        "next_action": {"type": "string", "description": "Next action for a non-ready state, emitted by iac.replatformingSelectorReadiness."}
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
          "403": {"$ref": "#/components/responses/Forbidden"},
          "501": {"$ref": "#/components/responses/NotImplemented"},
          "503": {"$ref": "#/components/responses/ServiceUnavailable"},
          "500": {"$ref": "#/components/responses/InternalError"}
        }
      }
    },
`
