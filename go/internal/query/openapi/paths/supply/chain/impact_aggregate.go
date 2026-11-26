// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package chain

// ImpactAggregate is the OpenAPI path fragment documenting the
// `/api/v0/supply-chain/impact/findings/count`,
// `/api/v0/supply-chain/impact/inventory` routes. openapi.Spec concatenates it
// into the published document; keep it in lockstep with the handlers and
// docs/public/reference/http-api.md.
const ImpactAggregate = `
    "/api/v0/supply-chain/impact/findings/count": {
      "get": {
        "summary": "Count supply-chain impact findings without paging the list endpoint",
        "description": "A winners-eligible call served from the maintained winners read model carries freshness with the maintainer watermark. When the model is absent, older than the freshness window, or its probe fails, the call answers 503 backend_unavailable instead of an approximate total; there is no fallback to the legacy read. A scoped-token call is served from the per-support table, is current by construction, and reports fresh with no watermark; while that table is not ready it answers 503 backend_unavailable with Retry-After 30.",
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
          {"name": "suppression_state", "in": "query", "description": "Filter by reducer suppression decision. Hidden states (not_affected, accepted_risk, false_positive, ignored, advisory_retracted) require include_suppressed=true to be counted.", "schema": {"type": "string", "enum": ["active", "not_affected", "accepted_risk", "false_positive", "ignored", "expired", "provider_dismissed", "scope_mismatch", "advisory_retracted"]}},
          {"name": "include_suppressed", "in": "query", "description": "Include findings hidden by an operator-asserted suppression or by a source retraction of the advisory (advisory_retracted). Defaults to false, matching the list endpoint.", "schema": {"type": "boolean", "default": false}}
        ],
        "responses": {
          "400": {"$ref": "#/components/responses/BadRequest"},
          "403": {"$ref": "#/components/responses/Forbidden"},
          "500": {"$ref": "#/components/responses/InternalError"},
          "501": {"$ref": "#/components/responses/NotImplemented"},
          "503": {"$ref": "#/components/responses/ServiceUnavailable"},
          "504": {"$ref": "#/components/responses/GatewayTimeout"},
          "200": {
            "description": "Reducer-owned vulnerability impact totals envelope",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "total_findings": {"type": "integer", "description": "Reducer-owned impact findings matching the filters, across all impact states."},
                    "affected_findings": {"type": "integer", "description": "Findings with a conclusive affected verdict (exact plus derived)."},
                    "affected_exact": {"type": "integer", "description": "Findings with an exact affected verdict."},
                    "affected_derived": {"type": "integer", "description": "Findings with a derived affected verdict."},
                    "possibly_affected": {"type": "integer", "description": "Findings with an inconclusive possibly-affected verdict."},
                    "not_affected": {"type": "integer", "description": "Findings with a conclusive not-affected verdict."},
                    "by_priority_bucket": {"type": "object", "additionalProperties": {"type": "integer"}, "description": "Finding counts by reducer triage priority bucket (critical, high, medium, low, informational). Bucket names are dynamic; render as bars, never a fixed enum."},
                    "by_severity": {"type": "object", "additionalProperties": {"type": "integer"}, "description": "Finding counts by CVSS severity bucket (critical, high, medium, low, none). Bucket names are dynamic; render as bars, never a fixed enum."},
                    "detection_profile": {"type": "string", "enum": ["precise", "comprehensive"], "description": "Echo of the detection profile applied to the aggregate; precise is returned when no profile parameter was supplied."},
                    "freshness": {"type": "object", "description": "Evidence freshness behind the totals. The legacy live-facts read reports fresh with no watermark; a winners-served answer reports fresh with the maintainer watermark.", "properties": {"state": {"type": "string", "enum": ["fresh"], "description": "Freshness state behind the totals; always fresh on the count route. Owner: supply/chain Handler.countImpactFindings."}, "latest_observed_at": {"type": "string", "description": "Maintainer watermark; present only on winners-served answers. Owner: supply/chain Handler.countImpactFindings."}}},
                    "scope": {"type": "object", "additionalProperties": {"type": "string"}, "description": "Echo of the filters applied to this aggregate, as string pairs (profile is always present). Render as filter chips, never a fixed form."}
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
        "description": "The five single-column dimensions served from the maintained winners read model carry freshness with the maintainer watermark. When the model is absent, older than the freshness window, or its probe fails, the call answers 503 backend_unavailable instead of approximate buckets; there is no fallback to the legacy read. A scoped-token call is served from the per-support table, is current by construction, and reports fresh with no watermark; while that table is not ready it answers 503 backend_unavailable with Retry-After 30.",
        "operationId": "getSupplyChainImpactInventory",
        "x-scoped-token-support": true,
        "parameters": [
          {"name": "group_by", "in": "query", "description": "Grouping dimension. service_id, workload_id, and environment resolve each finding to its runtime service, workload, or environment at read time, so a finding with two services counts in both buckets and bucket counts can sum above distinct_findings. They return per-bucket completeness, by_impact_status, by_severity, max_priority_score, and missing_evidence, plus a unattributed bucket (value empty, unattributed true) for findings with no resolved group. They answer 503 when the grouped read model is not available, for an unscoped call with image_ref or profile=precise (the default) because the winners read model holds only the canonical copy, and for a scoped-token call while the per-support read model is not ready (Retry-After 30). A scoped-token call is served from the per-support table, which serves image_ref and profile=precise from every granted copy. Composition works: group_by=environment&service_id=X groups one service's findings by environment.", "schema": {"type": "string", "enum": ["impact_status", "priority_bucket", "severity", "repository_id", "ecosystem", "service_id", "workload_id", "environment"], "default": "impact_status"}},
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
          {"name": "suppression_state", "in": "query", "description": "Filter by reducer suppression decision. Hidden states (not_affected, accepted_risk, false_positive, ignored, advisory_retracted) require include_suppressed=true to be counted.", "schema": {"type": "string", "enum": ["active", "not_affected", "accepted_risk", "false_positive", "ignored", "expired", "provider_dismissed", "scope_mismatch", "advisory_retracted"]}},
          {"name": "include_suppressed", "in": "query", "description": "Include findings hidden by an operator-asserted suppression or by a source retraction of the advisory (advisory_retracted). Defaults to false, matching the list endpoint.", "schema": {"type": "boolean", "default": false}},
          {"name": "limit", "in": "query", "schema": {"type": "integer", "minimum": 1, "maximum": 500, "default": 100}},
          {"name": "offset", "in": "query", "schema": {"type": "integer", "minimum": 0, "maximum": 10000, "default": 0}}
        ],
        "responses": {
          "400": {"$ref": "#/components/responses/BadRequest"},
          "403": {"$ref": "#/components/responses/Forbidden"},
          "500": {"$ref": "#/components/responses/InternalError"},
          "501": {"$ref": "#/components/responses/NotImplemented"},
          "503": {"$ref": "#/components/responses/ServiceUnavailable"},
          "504": {"$ref": "#/components/responses/GatewayTimeout"},
          "200": {
            "description": "Grouped count buckets. The existing dimensions are ordered by count desc; service_id, workload_id, and environment are ordered by max_priority_score desc, then count desc, then value asc.",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "buckets": {
                      "type": "array",
                      "description": "Grouped count buckets. Owner: supply/chain Handler.impactInventory.",
                      "items": {
                        "type": "object",
                        "description": "One grouped count bucket. Owner: supply/chain Handler.impactInventory.",
                        "properties": {
                          "dimension": {"type": "string", "description": "Bucket dimension value; the label comes from the group_by request value."},
                          "value": {"type": "string", "description": "Group value. Empty for the unattributed bucket of a grouped dimension."},
                          "count": {"type": "integer", "description": "Findings in this bucket."},
                          "unattributed": {"type": "boolean", "description": "Grouped dimensions only. True for the one bucket of findings with no resolved service, workload, or environment; its value is empty and its completeness is unattributed, never complete."},
                          "by_impact_status": {"type": "object", "additionalProperties": {"type": "integer"}, "description": "Grouped dimensions only. Findings in the bucket by reducer impact_status."},
                          "by_severity": {"type": "object", "additionalProperties": {"type": "integer"}, "description": "Grouped dimensions only. Findings in the bucket by CVSS severity bucket."},
                          "max_priority_score": {"type": "integer", "description": "Grouped dimensions only. Highest reducer priority score among the bucket's findings."},
                          "completeness": {"type": "string", "enum": ["complete", "partial", "unattributed", "read_model_unavailable"], "description": "Grouped dimensions only. partial means the page was truncated, or, under a scoped grant, that some finding mapped to the bucket exists that the grant does not admit, independent of the request filters (counts never include it). read_model_unavailable means the winners read model is stale or its freshness probe failed. Distinct from the finding readiness_state."},
                          "missing_evidence": {"type": "array", "items": {"type": "string"}, "description": "Grouped dimensions only. Reasons the bucket is not fully attributed: service_mapping, workload_mapping, or current_deploy_evidence for the unattributed bucket of that dimension; readiness_unavailable for a bucket whose completeness is read_model_unavailable."}
                        }
                      }
                    },
                    "count": {"type": "integer", "description": "Buckets in this page."},
                    "limit": {"type": "integer", "description": "Echo of the requested page bound."},
                    "offset": {"type": "integer", "description": "Echo of the requested page offset."},
                    "group_by": {"type": "string", "description": "Echo of the requested grouping dimension."},
                    "detection_profile": {"type": "string", "enum": ["precise", "comprehensive"], "description": "Echo of the detection profile applied to the aggregate; precise is returned when no profile parameter was supplied."},
                    "truncated": {"type": "boolean", "description": "True when more buckets matched than this page carries; continue with next_offset."},
                    "next_offset": {"type": ["integer", "null"], "description": "Next offset to request when truncated is true; null when the page is complete or when the next offset would exceed the documented maximum (10000)."},
                    "distinct_findings": {"type": "integer", "description": "Grouped dimensions only. Each finding counted once across the whole filtered set, independent of limit and offset (a truncated page does not change it). Equals total_findings of the count route for the same filters. An unscoped call reads it from the winners read model, so it reflects the last winners resweep while buckets read read_model_unavailable (stale winners); a scoped-token call reads the per-support table, which applies the grant to each copy before the canonical copy is picked, so a scope-id grant no longer differs. A rank filter (priority_bucket, severity, min_priority_score) tests the canonical copy, as the contract defines. An unscoped call with image_ref or profile=precise (the default profile) tests every copy of a finding: image_ref reads the per-support table, and profile=precise reads the winners read model plus a supplement of the best precise copy of each finding whose canonical copy is not precise. When the per-support table is not ready the call answers 503 with Retry-After. Bucket counts summed over every page are at least this value."},
                    "freshness": {"type": "object", "description": "Evidence freshness behind the page. Grouped dimensions report fresh, stale, or unknown; the five single-column dimensions report fresh with the maintainer watermark when served from the winners read model, and fresh with no watermark on the legacy read.", "properties": {"state": {"type": "string", "enum": ["fresh", "stale", "unknown"], "description": "Freshness state behind the page. Owner: supply/chain Handler.impactInventory."}, "latest_observed_at": {"type": "string", "description": "Maintainer watermark; present only on winners-served answers. Owner: supply/chain Handler.impactInventory."}}},
                    "scope": {"type": "object", "additionalProperties": {"type": "string"}, "description": "Echo of the filters applied to this aggregate, as string pairs (profile is always present). Render as filter chips, never a fixed form."}
                  }
                }
              }
            }
          }
        }
      }
    },
`
