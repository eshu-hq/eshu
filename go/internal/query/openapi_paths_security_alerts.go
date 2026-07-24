// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

const openAPIPathsSecurityAlerts = `
    "/api/v0/supply-chain/security-alerts/reconciliations": {
      "get": {
        "summary": "List provider security alert reconciliations",
        "description": "Requires limit plus repository_id, provider, package_id, cve_id, or ghsa_id. provider_state and reconciliation_status filter anchored pages only.",
        "operationId": "listSecurityAlertReconciliations",
        "x-scoped-token-support": true,
        "parameters": [
          {"name": "repository_id", "in": "query", "description": "Canonical repository id or human repository selector (name, repo slug, indexed path, local path, or remote URL). Unknown or ambiguous selectors return a selector error instead of an empty page.", "schema": {"type": "string"}},
          {"name": "provider", "in": "query", "schema": {"type": "string"}},
          {"name": "package_id", "in": "query", "schema": {"type": "string"}},
          {"name": "cve_id", "in": "query", "schema": {"type": "string"}},
          {"name": "ghsa_id", "in": "query", "schema": {"type": "string"}},
          {"name": "provider_state", "in": "query", "schema": {"type": "string", "enum": ["open", "fixed", "dismissed", "auto_dismissed"]}},
          {"name": "reconciliation_status", "in": "query", "schema": {"type": "string", "enum": ["matched", "unmatched", "stale", "dismissed", "fixed", "provider_only", "unsupported", "ambiguous"]}},
          {"name": "after_reconciliation_id", "in": "query", "schema": {"type": "string"}},
          {"name": "limit", "in": "query", "required": true, "schema": {"type": "integer", "minimum": 1, "maximum": 200}}
        ],
        "responses": {
          "503": {"$ref": "#/components/responses/ServiceUnavailable"},
          "504": {"$ref": "#/components/responses/GatewayTimeout"},
          "200": {
            "description": "Provider security alert reconciliation page. Provider alert state and Eshu impact state are separate fields.",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "reconciliations": {
                      "type": "array",
                      "items": {
                        "type": "object",
                        "properties": {
                          "reconciliation_id": {"type": "string"},
                          "provider_alert": {
                            "type": "object",
                            "properties": {
                              "provider": {"type": "string"},
                              "provider_alert_id": {"type": "string"},
                              "provider_alert_number": {"type": "integer"},
                              "provider_state": {"type": "string"},
                              "repository_id": {"type": "string"},
                              "package_id": {"type": "string"},
                              "ecosystem": {"type": "string"},
                              "package_name": {"type": "string"},
                              "manifest_path": {"type": "string"},
                              "dependency_scope": {"type": "string"},
                              "relationship": {"type": "string"},
                              "ghsa_ids": {"type": "array", "items": {"type": "string"}},
                              "cve_ids": {"type": "array", "items": {"type": "string"}},
                              "vulnerable_range": {"type": "string"},
                              "patched_version": {"type": "string"},
                              "severity": {"type": "string"},
                              "cvss": {"type": "object"},
                              "epss": {"type": "object", "additionalProperties": {"type": "string"}},
                              "cwes": {"type": "array", "items": {"type": "object", "additionalProperties": {"type": "string"}}},
                              "summary": {"type": "string"},
                              "source_url": {"type": "string"},
                              "created_at": {"type": "string"},
                              "updated_at": {"type": "string"},
                              "fixed_at": {"type": "string"},
                              "dismissed_at": {"type": "string"},
                              "collection_coverage_state": {"type": "string", "enum": ["complete", "incomplete"]},
                              "collection_truncated": {"type": "boolean"},
                              "collection_pages_fetched": {"type": "integer"},
                              "collection_state_filter": {"type": "string", "enum": ["open"]},
                              "collection_incomplete_reasons": {"type": "array", "items": {"type": "string"}}
                            }
                          },
                          "eshu_package": {
                            "type": "object",
                            "description": "Eshu-owned dependency evidence matched to the provider alert. observed_version is populated only from Eshu package evidence, never provider alert fields.",
                            "properties": {
                              "observed_version": {"type": "string", "description": "Installed or observed package version from Eshu-owned dependency evidence when exact evidence exists."},
                              "requested_range": {"type": "string", "description": "Manifest/requested dependency range preserved separately from the installed version."},
                              "dependency_range": {"type": "string", "description": "Raw Eshu dependency range or lockfile version evidence used during matching."},
                              "dependency_evidence_id": {"type": "string"},
                              "dependency_evidence_kind": {"type": "string"},
                              "missing_evidence": {"type": "array", "items": {"type": "string"}, "description": "Explicit evidence gaps such as missing or malformed installed package version evidence."}
                            }
                          },
                          "eshu_impact": {
                            "type": "object",
                            "description": "Reducer-owned impact state matched to the alert, when Eshu has admitted owned evidence.",
                            "properties": {
                              "impact_status": {"type": "string"},
                              "finding_id": {"type": "string"}
                            }
                          },
                          "reconciliation_status": {"type": "string", "enum": ["matched", "unmatched", "stale", "dismissed", "fixed", "provider_only", "unsupported", "ambiguous"]},
                          "reason": {"type": "string"},
                          "reason_code": {"type": "string"},
                          "missing_evidence": {
                            "type": "array",
                            "description": "Structured row-level gaps that explain provider-only, stale, unsupported, or ambiguous outcomes without embedding raw provider payloads.",
                            "items": {
                              "type": "object",
                              "properties": {
                                "kind": {"type": "string", "enum": ["owned_dependency", "impact_finding", "current_manifest", "ecosystem_matcher"]},
                                "reason": {"type": "string"},
                                "evidence_id": {"type": "string"},
                                "detail": {"type": "string"}
                              },
                              "required": ["kind", "reason"]
                            }
                          },
                          "evidence_fact_ids": {"type": "array", "items": {"type": "string"}},
                          "source_freshness": {"type": "string"},
                          "source_confidence": {"type": "string"}
                        },
                        "required": ["reconciliation_id", "provider_alert", "eshu_package", "eshu_impact", "reconciliation_status"]
                      }
                    },
                    "count": {"type": "integer"},
                    "coverage": {
                      "type": "object",
                      "description": "Provider-source coverage for the returned reconciliation page. target_incomplete means at least one row came from a capped open-alert provider read.",
                      "properties": {
                        "state": {"type": "string", "enum": ["complete", "target_incomplete"]},
                        "partial_rows": {"type": "integer"},
                        "rows_considered": {"type": "integer"}
                      }
                    },
                    "limit": {"type": "integer"},
                    "truncated": {"type": "boolean"},
                    "next_cursor": {"type": "object"}
                  }
                }
              }
            }
          }
        }
      }
    },
`
