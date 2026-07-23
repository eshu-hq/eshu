// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

const openAPIPathsSupplyChainImpactExplain = `
    "/api/v0/supply-chain/impact/explain": {
      "get": {
        "summary": "Explain one supply-chain impact finding",
        "operationId": "explainSupplyChainImpact",
        "x-scoped-token-support": true,
        "description": "Scoped tokens receive an explanation intersected with granted repositories: the matched finding's repository_id/scope_id must fall within the token's grant, so an out-of-grant finding_id or a bare advisory/CVE plus package/image/workload/service anchor that would otherwise resolve to another tenant's finding returns a no_finding outcome instead of leaking it. A scoped token with no granted repositories always receives the bounded no-evidence explanation without a store read.",
        "parameters": [
          {"name": "finding_id", "in": "query", "schema": {"type": "string"}},
          {"name": "advisory_id", "in": "query", "schema": {"type": "string"}},
          {"name": "cve_id", "in": "query", "schema": {"type": "string"}},
          {"name": "package_id", "in": "query", "schema": {"type": "string"}},
          {"name": "repository_id", "in": "query", "description": "Canonical repository id or human source repository selector (name, repo slug, indexed path, local path, or remote URL). Unknown or ambiguous selectors return a selector error instead of a no-evidence explanation.", "schema": {"type": "string"}},
          {"name": "subject_digest", "in": "query", "schema": {"type": "string"}},
          {"name": "image_ref", "in": "query", "description": "Exact image reference stored on reducer-owned impact findings.", "schema": {"type": "string"}},
          {"name": "workload_id", "in": "query", "description": "Reducer-admitted workload anchor. Missing runtime mapping remains missing evidence.", "schema": {"type": "string"}},
          {"name": "service_id", "in": "query", "description": "Reducer-admitted service anchor derived from workload/service evidence.", "schema": {"type": "string"}}
        ],
        "responses": {
          "503": {"$ref": "#/components/responses/ServiceUnavailable"},
          "504": {"$ref": "#/components/responses/GatewayTimeout"},
          "200": {
            "description": "Bounded finding explanation, no-evidence response, or ambiguous-scope refusal envelope",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "outcome": {"type": "string", "enum": ["finding_explained", "no_finding", "ambiguous_scope"]},
                    "evidence_packet_handle": {"type": "string", "description": "Opaque stable handle for this bounded explanation packet. Finding packets use the returned finding id; no-finding scopes use a hashed normalized scope so private anchors are not exposed in the handle."},
                    "input": {"type": "object"},
                    "finding": {"type": "object"},
                    "advisory": {
                      "type": "object",
                      "properties": {
                        "cve_id": {"type": "string"},
                        "advisory_id": {"type": "string"},
                        "vulnerable_range": {"type": "string"},
                        "range_source": {"type": "string"},
                        "selected_severity_source": {"type": "string"},
                        "selected_fixed_version_source": {"type": "string"},
                        "sources": {"type": "array", "items": {"type": "object"}},
                        "references": {"type": "array", "items": {"type": "string"}}
                      }
                    },
                    "component": {
                      "type": "object",
                      "properties": {
                        "package_id": {"type": "string"},
                        "ecosystem": {"type": "string"},
                        "package_name": {"type": "string"},
                        "purl": {"type": "string"},
                        "product_criteria": {"type": "string"},
                        "match_criteria_id": {"type": "string"},
                        "observed_version": {"type": "string"},
                        "manifest_range": {"type": "string"}
                      }
                    },
                    "version": {
                      "type": "object",
                      "properties": {
                        "observed_version": {"type": "string"},
                        "manifest_range": {"type": "string"},
                        "vulnerable_range": {"type": "string"},
                        "fixed_version": {"type": "string"},
                        "version_evidence": {"type": "string", "enum": ["exact", "range_only", "missing"]}
                      },
                      "required": ["version_evidence"]
                    },
                    "dependency_chain": {
                      "type": "object",
                      "properties": {
                        "path": {"type": "array", "items": {"type": "string"}},
                        "depth": {"type": "integer"},
                        "direct_dependency": {"type": "boolean"}
                      }
                    },
                    "anchors": {
                      "type": "object",
                      "properties": {
                        "repository_id": {"type": "string"},
                        "subject_digest": {"type": "string"},
                        "manifest_paths": {"type": "array", "items": {"type": "string"}},
                        "lockfile_paths": {"type": "array", "items": {"type": "string"}},
                        "sbom_documents": {"type": "array", "items": {"type": "string"}},
                        "image_digests": {"type": "array", "items": {"type": "string"}},
                        "image_refs": {"type": "array", "items": {"type": "string"}},
                        "workloads": {"type": "array", "items": {"type": "string"}},
                        "deployments": {"type": "array", "items": {"type": "string"}},
                        "services": {"type": "array", "items": {"type": "string"}},
                        "environments": {"type": "array", "items": {"type": "string"}},
                        "catalog_entities": {"type": "array", "items": {"type": "string"}},
                        "catalog_owners": {"type": "array", "items": {"type": "string"}},
                        "provider_alerts": {"type": "array", "items": {"type": "object"}},
                        "evidence_fact_ids": {"type": "array", "items": {"type": "string"}}
                      }
                    },
                    "impact_path": {
                      "type": "array",
                      "description": "Reducer-owned present and missing hops from advisory/package evidence to repository, image, workload, deployment, service, and environment evidence. Missing hops remain explicit and are not inferred from names or tags.",
                      "items": {
                        "type": "object",
                        "properties": {
                          "hop": {"type": "string"},
                          "status": {"type": "string", "enum": ["present", "missing_evidence"]},
                          "evidence_fact_ids": {"type": "array", "items": {"type": "string"}},
                          "missing_evidence": {"type": "array", "items": {"type": "string"}}
                        },
                        "required": ["hop", "status"]
                      }
                    },
                    "evidence": {
                      "type": "array",
                      "description": "Compact previews of the bounded evidence fact ids referenced by the finding; raw advisory and provider payloads are not returned.",
                      "items": {
                        "type": "object",
                        "properties": {
                          "fact_id": {"type": "string"},
                          "fact_kind": {"type": "string"},
                          "source_system": {"type": "string"},
                          "source_confidence": {"type": "string"},
                          "observed_at": {"type": "string"}
                        },
                        "required": ["fact_id", "fact_kind"]
                      }
                    },
                    "readiness": {"type": "object"},
                    "missing_evidence": {"type": "array", "items": {"type": "string"}},
                    "freshness": {
                      "type": "object",
                      "properties": {
                        "state": {"type": "string", "enum": ["fresh", "stale", "unknown"]},
                        "latest_observed_at": {"type": "string"},
                        "evidence_fact_count": {"type": "integer"}
                      },
                      "required": ["state", "evidence_fact_count"]
                    },
                    "remediation": {
                      "type": "object",
                      "description": "Advisory-only safe-upgrade recommendation for this finding (issue #595). Mirrors the remediation block on the finding row and enriches it with vulnerable_range, manifest_range, observed_version, and dependency direct/transitive evidence pulled from the referenced source facts. The reducer never auto-opens pull requests; this block is strictly advisory.",
                      "properties": {
                        "ecosystem": {"type": "string"},
                        "current_version": {"type": "string"},
                        "vulnerable_range": {"type": "string"},
                        "fixed_version_source": {"type": "string"},
                        "match_reason": {"type": "string"},
                        "first_patched_version": {"type": "string"},
                        "patched_version_branches": {
                          "type": "array",
                          "items": {
                            "type": "object",
                            "properties": {
                              "version": {"type": "string"},
                              "source": {"type": "string"}
                            },
                            "required": ["version", "source"]
                          }
                        },
                        "manifest_range": {"type": "string"},
                        "manifest_allows_fix": {"type": "string", "enum": ["allowed", "blocked", "unknown"]},
                        "direct": {"type": "boolean"},
                        "parent_package": {"type": "string"},
                        "confidence": {"type": "string", "enum": ["exact", "partial", "unknown"]},
                        "reason": {"type": "string", "enum": ["direct_upgrade_allowed", "direct_range_blocked", "transitive_parent_upgrade_required", "already_fixed", "no_patched_version", "multiple_patched_branches", "package_manager_unsupported", "manifest_range_missing", "manifest_range_malformed", "installed_version_missing", "installed_version_malformed"]},
                        "missing_evidence": {"type": "array", "items": {"type": "string"}}
                      }
                    }
                  },
                  "required": ["outcome", "input", "advisory", "component", "version", "anchors", "evidence", "readiness", "freshness"]
                }
              }
            }
          },
          "400": {"description": "Unbounded input"}
        }
      }
    },
`
