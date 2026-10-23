// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package chain

// SecurityAlertAggregate is the OpenAPI path fragment documenting the
// `/api/v0/supply-chain/security-alerts/reconciliations/count`,
// `/api/v0/supply-chain/security-alerts/reconciliations/inventory` routes.
// openapi.Spec concatenates it into the published document; keep it in
// lockstep with the handlers and docs/public/reference/http-api.md.
const SecurityAlertAggregate = `
    "/api/v0/supply-chain/security-alerts/reconciliations/count": {
      "get": {
        "summary": "Count provider security alert reconciliations without paging the list endpoint",
        "operationId": "countSecurityAlertReconciliations",
        "x-scoped-token-support": true,
        "parameters": [
          {"name": "repository_id", "in": "query", "description": "Canonical repository id or human repository selector (name, repo slug, indexed path, local path, or remote URL). Unknown or ambiguous selectors return a selector error instead of an empty aggregate.", "schema": {"type": "string"}},
          {"name": "provider", "in": "query", "schema": {"type": "string"}},
          {"name": "package_id", "in": "query", "schema": {"type": "string"}},
          {"name": "cve_id", "in": "query", "schema": {"type": "string"}},
          {"name": "ghsa_id", "in": "query", "schema": {"type": "string"}},
          {"name": "provider_state", "in": "query", "schema": {"type": "string"}},
          {"name": "reconciliation_status", "in": "query", "schema": {"type": "string"}}
        ],
        "responses": {
          "403": {"$ref": "#/components/responses/Forbidden"},
          "503": {"$ref": "#/components/responses/ServiceUnavailable"},
          "504": {"$ref": "#/components/responses/GatewayTimeout"},
          "200": {
            "description": "Reducer-owned provider alert reconciliation totals envelope",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "total_reconciliations": {"type": "integer"},
                    "by_reconciliation_status": {"type": "object", "additionalProperties": {"type": "integer"}},
                    "by_provider": {"type": "object", "additionalProperties": {"type": "integer"}},
                    "by_provider_state": {"type": "object", "additionalProperties": {"type": "integer"}},
                    "by_source_freshness": {"type": "object", "additionalProperties": {"type": "integer"}},
                    ` + securityAlertCoverageSchema + `,
                    "scope": {"type": "object"}
                  }
                }
              }
            }
          }
        }
      }
    },
    "/api/v0/supply-chain/security-alerts/reconciliations/inventory": {
      "get": {
        "summary": "Group provider security alert reconciliations by one dimension without paging the list endpoint",
        "operationId": "getSecurityAlertReconciliationInventory",
        "x-scoped-token-support": true,
        "parameters": [
          {"name": "group_by", "in": "query", "schema": {"type": "string", "enum": ["reconciliation_status", "provider", "provider_state", "repository_id", "package_id"], "default": "reconciliation_status"}},
          {"name": "repository_id", "in": "query", "description": "Canonical repository id or human repository selector (name, repo slug, indexed path, local path, or remote URL). Unknown or ambiguous selectors return a selector error instead of an empty aggregate.", "schema": {"type": "string"}},
          {"name": "provider", "in": "query", "schema": {"type": "string"}},
          {"name": "package_id", "in": "query", "schema": {"type": "string"}},
          {"name": "cve_id", "in": "query", "schema": {"type": "string"}},
          {"name": "ghsa_id", "in": "query", "schema": {"type": "string"}},
          {"name": "provider_state", "in": "query", "schema": {"type": "string"}},
          {"name": "reconciliation_status", "in": "query", "schema": {"type": "string"}},
          {"name": "limit", "in": "query", "schema": {"type": "integer", "minimum": 1, "maximum": 500, "default": 100}},
          {"name": "offset", "in": "query", "schema": {"type": "integer", "minimum": 0, "maximum": 10000, "default": 0}}
        ],
        "responses": {
          "403": {"$ref": "#/components/responses/Forbidden"},
          "503": {"$ref": "#/components/responses/ServiceUnavailable"},
          "504": {"$ref": "#/components/responses/GatewayTimeout"},
          "200": {
            "description": "Grouped count buckets ordered by count desc",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "buckets": {
                      "type": "array",
                      "items": {
                        "type": "object",
                        "properties": {
                          "dimension": {"type": "string"},
                          "value": {"type": "string"},
                          "count": {"type": "integer"}
                        }
                      }
                    },
                    "count": {"type": "integer"},
                    "limit": {"type": "integer"},
                    "offset": {"type": "integer"},
                    "group_by": {"type": "string"},
                    "truncated": {"type": "boolean"},
                    "next_offset": {"type": ["integer", "null"], "description": "Next offset to request when truncated is true; null when the page is complete or when the next offset would exceed the documented maximum (10000)."},
                    "scope": {"type": "object"},
                    ` + securityAlertCoverageSchema + `
                  }
                }
              }
            }
          }
        }
      }
    },
`

// securityAlertCoverageSchema documents the coverage object every
// security-alert read route returns (#8803). The reasons enum must equal the
// handler's closed reason set; supply/chain's
// TestSecurityAlertCoverageOpenAPIEnum fails when they drift.
const securityAlertCoverageSchema = `"coverage": {
                      "type": "object",
                      "description": "Provider-source coverage. On an anchored read (repository_id), complete means an enabled security_alert collector target covers the repository and has a committed (projected) generation with no later terminal fetch failure. On an unanchored read, complete means every configured target does; it reports configured-target health only and says nothing about whether any particular repository is inside a target, so anchor by repository_id for repository coverage. complete also requires that no counted row came from a capped open-alert read. Coverage reflects each target's latest projected generation, so a repository added to a target after it reads covered until the next cycle projects. A zero-row answer is clean only when coverage.state is complete and the reducer's security_alert_reconciliation domain has no pending or dead-lettered work for the scope (get_freshness_causality). target_incomplete means a covering target is still collecting, failed terminally, or at least one row came from a capped read; reason terminal_fetch_failure:retryable means the retry budget ran out on a transient provider failure. not_configured means no enabled target covers the requested repository, or, with reason no_authorized_repositories, a scoped token's grants authorize no repository (no collector state is read for it). On an empty answer, reasons names why the answer is not complete; it is absent when coverage is complete and on non-empty answers. Reasons never carry a repository name, allowlist, target id, or failure message.",
                      "properties": {
                        "state": {"type": "string", "enum": ["complete", "target_incomplete", "not_configured"]},
                        "partial_rows": {"type": "integer"},
                        "rows_considered": {"type": "integer"},
                        "reasons": {
                          "type": "array",
                          "items": {"type": "string", "enum": ["collector_state_unavailable", "collector_not_configured", "no_authorized_repositories", "no_committed_generation", "repository_outside_target_set", "terminal_fetch_failure:attempt_budget_exhausted", "terminal_fetch_failure:auth_denied", "terminal_fetch_failure:collect_failure", "terminal_fetch_failure:commit_failure", "terminal_fetch_failure:identity_mismatch", "terminal_fetch_failure:not_found", "terminal_fetch_failure:rate_limited", "terminal_fetch_failure:retryable", "terminal_fetch_failure:terminal", "terminal_fetch_failure:unclassified"]}
                        }
                      }
                    },
                    "collector_readiness": {"$ref": "#/components/schemas/CollectorReadinessEnvelope"}`
