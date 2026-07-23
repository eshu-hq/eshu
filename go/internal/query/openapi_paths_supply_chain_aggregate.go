// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

const openAPIPathsSupplyChainImpactAggregate = `
    "/api/v0/supply-chain/impact/findings/count": {
      "get": {
        "summary": "Count supply-chain impact findings without paging the list endpoint",
        "operationId": "countSupplyChainImpactFindings",
        "x-scoped-token-support": true,
        "parameters": [
          {"name": "cve_id", "in": "query", "schema": {"type": "string"}},
          {"name": "advisory_id", "in": "query", "description": "Exact source advisory identifier such as GHSA, OSV, GLAD, vendor advisory, or CVE id.", "schema": {"type": "string"}},
          {"name": "ghsa_id", "in": "query", "description": "GHSA advisory identifier alias for advisory_id.", "schema": {"type": "string"}},
          {"name": "osv_id", "in": "query", "description": "OSV advisory identifier alias for advisory_id.", "schema": {"type": "string"}},
          {"name": "package_id", "in": "query", "schema": {"type": "string"}},
            {"name": "repository_id", "in": "query", "description": "Canonical repository id or human repository selector (name, repo slug, indexed path, local path, or remote URL). Unknown or ambiguous selectors return a selector error instead of an empty aggregate.", "schema": {"type": "string"}},
            {"name": "subject_digest", "in": "query", "schema": {"type": "string"}},
            {"name": "image_ref", "in": "query", "schema": {"type": "string"}, "description": "Exact image reference stored on reducer-owned impact findings."},
            {"name": "impact_status", "in": "query", "schema": {"type": "string", "enum": ["affected_exact", "affected_derived", "possibly_affected", "not_affected_known_fixed", "unknown_impact"]}},
          {"name": "ecosystem", "in": "query", "schema": {"type": "string"}},
          {"name": "workload_id", "in": "query", "schema": {"type": "string"}},
          {"name": "service_id", "in": "query", "schema": {"type": "string"}},
          {"name": "environment", "in": "query", "schema": {"type": "string"}},
          {"name": "severity", "in": "query", "schema": {"type": "string", "enum": ["critical", "high", "medium", "low", "none"]}},
          {"name": "profile", "in": "query", "schema": {"type": "string", "enum": ["precise", "comprehensive"], "default": "precise"}, "description": "Detection profile selector. Matches the list endpoint: precise is the default, comprehensive also includes range-only, SBOM/CPE-derived, malformed range, and missing-version rows. Unsupported non-OS package ecosystems are readiness gaps, not finding rows."},
          {"name": "priority_bucket", "in": "query", "schema": {"type": "string", "enum": ["critical", "high", "medium", "low", "informational"]}, "description": "Reducer triage priority filter; does not change impact truth."},
          {"name": "min_priority_score", "in": "query", "schema": {"type": "integer", "minimum": 0, "maximum": 100}, "description": "Minimum reducer priority score. Zero is the default no-op value."},
          {"name": "suppression_state", "in": "query", "description": "Filter by reducer suppression decision. Operator-asserted hidden states (not_affected, accepted_risk, false_positive, ignored) require include_suppressed=true to be counted.", "schema": {"type": "string", "enum": ["active", "not_affected", "accepted_risk", "false_positive", "ignored", "expired", "provider_dismissed", "scope_mismatch"]}},
          {"name": "include_suppressed", "in": "query", "description": "Include findings hidden by operator-asserted suppression. Defaults to false, matching the list endpoint.", "schema": {"type": "boolean", "default": false}}
        ],
        "responses": {
          "503": {"$ref": "#/components/responses/ServiceUnavailable"},
          "504": {"$ref": "#/components/responses/GatewayTimeout"},
          "200": {
            "description": "Reducer-owned vulnerability impact totals envelope",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "total_findings": {"type": "integer"},
                    "affected_findings": {"type": "integer"},
                    "affected_exact": {"type": "integer"},
                    "affected_derived": {"type": "integer"},
                    "possibly_affected": {"type": "integer"},
                    "not_affected": {"type": "integer"},
                    "by_priority_bucket": {"type": "object", "additionalProperties": {"type": "integer"}},
                    "by_severity": {"type": "object", "additionalProperties": {"type": "integer"}},
                    "detection_profile": {"type": "string", "enum": ["precise", "comprehensive"], "description": "Echo of the detection profile applied to the aggregate; precise is returned when no profile parameter was supplied."},
                    "scope": {"type": "object"}
                  }
                }
              }
            }
          }
        }
      }
    },
    "/api/v0/supply-chain/impact/inventory": {
      "get": {
        "summary": "Group supply-chain impact findings by one dimension without paging the list endpoint",
        "operationId": "getSupplyChainImpactInventory",
        "x-scoped-token-support": true,
        "parameters": [
          {"name": "group_by", "in": "query", "schema": {"type": "string", "enum": ["impact_status", "priority_bucket", "severity", "repository_id", "ecosystem"], "default": "impact_status"}},
          {"name": "cve_id", "in": "query", "schema": {"type": "string"}},
          {"name": "advisory_id", "in": "query", "description": "Exact source advisory identifier such as GHSA, OSV, GLAD, vendor advisory, or CVE id.", "schema": {"type": "string"}},
          {"name": "ghsa_id", "in": "query", "description": "GHSA advisory identifier alias for advisory_id.", "schema": {"type": "string"}},
          {"name": "osv_id", "in": "query", "description": "OSV advisory identifier alias for advisory_id.", "schema": {"type": "string"}},
          {"name": "package_id", "in": "query", "schema": {"type": "string"}},
            {"name": "repository_id", "in": "query", "description": "Canonical repository id or human repository selector (name, repo slug, indexed path, local path, or remote URL). Unknown or ambiguous selectors return a selector error instead of an empty aggregate.", "schema": {"type": "string"}},
            {"name": "subject_digest", "in": "query", "schema": {"type": "string"}},
            {"name": "image_ref", "in": "query", "schema": {"type": "string"}, "description": "Exact image reference stored on reducer-owned impact findings."},
            {"name": "impact_status", "in": "query", "schema": {"type": "string", "enum": ["affected_exact", "affected_derived", "possibly_affected", "not_affected_known_fixed", "unknown_impact"]}},
          {"name": "ecosystem", "in": "query", "schema": {"type": "string"}},
          {"name": "workload_id", "in": "query", "schema": {"type": "string"}},
          {"name": "service_id", "in": "query", "schema": {"type": "string"}},
          {"name": "environment", "in": "query", "schema": {"type": "string"}},
          {"name": "severity", "in": "query", "schema": {"type": "string", "enum": ["critical", "high", "medium", "low", "none"]}},
          {"name": "profile", "in": "query", "schema": {"type": "string", "enum": ["precise", "comprehensive"], "default": "precise"}, "description": "Detection profile selector. Matches the list endpoint: precise is the default, comprehensive also includes range-only, SBOM/CPE-derived, malformed range, and missing-version rows. Unsupported non-OS package ecosystems are readiness gaps, not finding rows."},
          {"name": "priority_bucket", "in": "query", "schema": {"type": "string", "enum": ["critical", "high", "medium", "low", "informational"]}, "description": "Reducer triage priority filter; does not change impact truth."},
          {"name": "min_priority_score", "in": "query", "schema": {"type": "integer", "minimum": 0, "maximum": 100}, "description": "Minimum reducer priority score. Zero is the default no-op value."},
          {"name": "suppression_state", "in": "query", "description": "Filter by reducer suppression decision. Operator-asserted hidden states (not_affected, accepted_risk, false_positive, ignored) require include_suppressed=true to be counted.", "schema": {"type": "string", "enum": ["active", "not_affected", "accepted_risk", "false_positive", "ignored", "expired", "provider_dismissed", "scope_mismatch"]}},
          {"name": "include_suppressed", "in": "query", "description": "Include findings hidden by operator-asserted suppression. Defaults to false, matching the list endpoint.", "schema": {"type": "boolean", "default": false}},
          {"name": "limit", "in": "query", "schema": {"type": "integer", "minimum": 1, "maximum": 500, "default": 100}},
          {"name": "offset", "in": "query", "schema": {"type": "integer", "minimum": 0, "maximum": 10000, "default": 0}}
        ],
        "responses": {
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
                    "detection_profile": {"type": "string", "enum": ["precise", "comprehensive"], "description": "Echo of the detection profile applied to the aggregate; precise is returned when no profile parameter was supplied."},
                    "truncated": {"type": "boolean"},
                    "next_offset": {"type": ["integer", "null"], "description": "Next offset to request when truncated is true; null when the page is complete or when the next offset would exceed the documented maximum (10000)."},
                    "scope": {"type": "object"}
                  }
                }
              }
            }
          }
        }
      }
    },
`
