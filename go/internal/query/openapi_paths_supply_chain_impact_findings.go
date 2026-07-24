// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

const openAPIPathsSupplyChainImpactFindings = `
    "/api/v0/supply-chain/impact/findings": {
      "get": {
        "summary": "List supply-chain impact findings",
        "operationId": "listSupplyChainImpactFindings",
        "x-scoped-token-support": true,
        "parameters": [
          {"name": "cve_id", "in": "query", "schema": {"type": "string"}},
          {"name": "advisory_id", "in": "query", "description": "Exact source advisory identifier such as GHSA, OSV, GLAD, vendor advisory, or CVE id.", "schema": {"type": "string"}},
          {"name": "ghsa_id", "in": "query", "description": "GHSA advisory identifier alias for advisory_id.", "schema": {"type": "string"}},
          {"name": "osv_id", "in": "query", "description": "OSV advisory identifier alias for advisory_id.", "schema": {"type": "string"}},
          {"name": "package_id", "in": "query", "schema": {"type": "string"}},
            {"name": "repository_id", "in": "query", "description": "Canonical repository id or human repository selector (name, repo slug, indexed path, local path, or remote URL). Unknown or ambiguous selectors return a selector error instead of an empty page.", "schema": {"type": "string"}},
            {"name": "subject_digest", "in": "query", "schema": {"type": "string"}},
            {"name": "image_ref", "in": "query", "schema": {"type": "string"}, "description": "Exact image reference stored on reducer-owned impact findings."},
            {"name": "impact_status", "in": "query", "schema": {"type": "string", "enum": ["affected_exact", "affected_derived", "possibly_affected", "not_affected_known_fixed", "unknown_impact"]}},
          {"name": "ecosystem", "in": "query", "schema": {"type": "string"}, "description": "Package ecosystem from reducer-owned impact facts."},
          {"name": "workload_id", "in": "query", "schema": {"type": "string"}, "description": "Reducer-admitted workload anchor; missing runtime mapping remains missing evidence."},
          {"name": "service_id", "in": "query", "schema": {"type": "string"}, "description": "Reducer-admitted service anchor derived from workload/service evidence."},
          {"name": "environment", "in": "query", "schema": {"type": "string"}, "description": "Reducer-admitted environment anchor; the read layer does not infer aliases."},
          {"name": "severity", "in": "query", "schema": {"type": "string", "enum": ["critical", "high", "medium", "low", "none"]}, "description": "CVSS-derived severity bucket."},
          {"name": "profile", "in": "query", "schema": {"type": "string", "enum": ["precise", "comprehensive"], "default": "precise"}, "description": "Detection profile selector. precise (default) returns only findings backed by exact installed-version anchors resolved by supported matchers such as npm, PyPI, Maven, Cargo, Pub, NuGet, Swift, or vendor-backed RPM OS packages. comprehensive also returns range-only manifest, SBOM/CPE-derived, malformed range, and missing-version rows. Unsupported non-OS package ecosystems are reported as readiness unsupported_targets, not finding rows. Each finding row keeps its truth labels (impact_status, confidence, runtime_reachability) and missing-evidence reasons."},
          {"name": "priority_bucket", "in": "query", "schema": {"type": "string", "enum": ["critical", "high", "medium", "low", "informational"]}, "description": "Reducer triage priority filter; does not change impact truth."},
          {"name": "min_priority_score", "in": "query", "schema": {"type": "integer", "minimum": 0, "maximum": 100}, "description": "Minimum reducer priority score. Zero is the default no-op value and does not bound a request by itself."},
          {"name": "sort", "in": "query", "schema": {"type": "string", "enum": ["finding_id", "priority", "priority_score_desc", "priority_score_asc"]}},
          {"name": "suppression_state", "in": "query", "description": "Filter by reducer suppression decision. Operator-asserted hidden states (not_affected, accepted_risk, false_positive, ignored) require include_suppressed=true to be returned.", "schema": {"type": "string", "enum": ["active", "not_affected", "accepted_risk", "false_positive", "ignored", "expired", "provider_dismissed", "scope_mismatch"]}},
          {"name": "include_suppressed", "in": "query", "description": "Include findings hidden by operator-asserted suppression. Expired, provider-dismissed, and scope-mismatched findings stay visible regardless because they preserve operator audit signal.", "schema": {"type": "boolean", "default": false}},
          {"name": "after_finding_id", "in": "query", "schema": {"type": "string"}},
          {"name": "limit", "in": "query", "required": true, "schema": {"type": "integer", "minimum": 1, "maximum": 200}}
        ],
        "responses": {
          "503": {"$ref": "#/components/responses/ServiceUnavailable"},
          "504": {"$ref": "#/components/responses/GatewayTimeout"},
          "200": {
            "description": "Supply-chain impact finding page with readiness coverage",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "findings": {
                      "type": "array",
                      "items": {
                        "type": "object",
                        "properties": {
                          "finding_id": {"type": "string"},
                          "cve_id": {"type": "string"},
                          "package_id": {"type": "string"},
                          "product_criteria": {"type": "string"},
                          "match_criteria_id": {"type": "string"},
                          "observed_version": {"type": "string", "description": "Exact installed version from lockfile, manifest, SBOM, or image evidence when known."},
                          "requested_range": {"type": "string", "description": "Original manifest/requested dependency range preserved separately from the installed version."},
                          "fixed_version": {"type": "string", "description": "Source-selected fixed version when advisory evidence reports one."},
                          "vulnerable_range": {"type": "string", "description": "Source-reported affected range expression copied from the advisory the reducer's provenance selector picked. Persisted on the canonical finding payload so list responses expose the same expression as the explain route. Older rows may omit this value."},
                          "match_reason": {"type": "string", "description": "Reducer reason for the version/range decision, including supported matches, range-only evidence, malformed evidence states, or missing version evidence. Unsupported non-OS package ecosystems are surfaced in readiness.unsupported_targets rather than finding rows."},
                          "detection_profile": {"type": "string", "enum": ["precise", "comprehensive"], "description": "Evidence tier the row meets. precise requires an exact installed-version anchor and ecosystem-aware match, including supported npm, PyPI, Maven, Cargo, Pub, NuGet, Swift, and vendor-backed RPM paths. comprehensive covers SBOM/CPE-derived, range-only, malformed, or missing-version rows that still have an owned anchor."},
                          "impact_status": {"type": "string"},
                          "confidence": {"type": "string"},
                          "cvss_score": {"type": "number"},
                          "advisory_published_at": {"type": "string"},
                          "advisory_updated_at": {"type": "string"},
                          "epss_probability": {"type": "string"},
                          "epss_percentile": {"type": "string"},
                          "known_exploited": {"type": "boolean"},
                          "priority_score": {"type": "integer", "minimum": 0, "maximum": 100},
                          "priority_bucket": {"type": "string", "enum": ["critical", "high", "medium", "low", "informational"]},
                          "priority_reason_codes": {"type": "array", "items": {"type": "string"}},
                          "priority_contributions": {"type": "array", "description": "Explainable scoring inputs. Priority is triage metadata and never proves affected truth.", "items": {"type": "object", "properties": {"reason_code": {"type": "string"}, "input": {"type": "string"}, "value": {"type": "string"}, "contribution": {"type": "integer"}}, "required": ["reason_code", "input", "contribution"]}},
                          "runtime_reachability": {"type": "string"},
                          "reachability": {"type": "object", "description": "Cross-language reachability enrichment. This is prioritization metadata and never downgrades or hides impact findings. missing_evidence and unavailable are not clean results.", "properties": {"state": {"type": "string", "enum": ["reachable", "not_called", "unknown", "unavailable", "missing_evidence"]}, "confidence": {"type": "string"}, "source": {"type": "string"}, "evidence": {"type": "string"}, "reason": {"type": "string"}, "language_maturity": {"type": "string", "enum": ["implemented", "partial", "unavailable", "unsupported"]}, "missing_evidence": {"type": "array", "items": {"type": "string"}}}, "required": ["state"]},
                          "repository_id": {"type": "string"},
                          "subject_digest": {"type": "string"},
                          "image_ref": {"type": "string"},
                          "dependency_scope": {"type": "string"},
                          "workload_ids": {"type": "array", "items": {"type": "string"}},
                          "deployment_ids": {"type": "array", "items": {"type": "string"}, "description": "Deployment mapping anchors from repository-scoped reducer_platform_materialization evidence. These prove a repository has deployment-lane evidence but do not imply workload identity, environment, or runtime image proof."},
                          "service_ids": {"type": "array", "items": {"type": "string"}},
                          "environments": {"type": "array", "items": {"type": "string"}},
                          "catalog_entity_refs": {"type": "array", "items": {"type": "string"}, "description": "Reducer-admitted service-catalog entity references attached to the finding evidence path. These are catalog anchors and do not become service_ids unless the reducer fact carries an explicit service_id."},
                          "catalog_owner_refs": {"type": "array", "items": {"type": "string"}, "description": "Reducer-admitted service-catalog owners attached to the finding evidence path. These preserve ownership context without inventing service or workload identity."},
                          "dependency_path": {"type": "array", "items": {"type": "string"}},
                          "dependency_depth": {"type": "integer"},
                          "direct_dependency": {"type": "boolean"},
                          "missing_evidence": {"type": "array", "items": {"type": "string"}},
                          "evidence_path": {"type": "array", "items": {"type": "string"}},
                          "evidence_fact_ids": {"type": "array", "items": {"type": "string"}},
                          "suppression": {
                            "type": "object",
                            "description": "Reducer VEX/operator-policy suppression decision for this finding. Always populated; state=active means no suppression matched. Operator suppressions (not_affected, accepted_risk, false_positive, ignored) hide the finding from the default view; expired, provider_dismissed, and scope_mismatch states keep the finding visible while preserving suppression provenance.",
                            "properties": {
                              "state": {"type": "string", "enum": ["active", "not_affected", "accepted_risk", "false_positive", "ignored", "expired", "provider_dismissed", "scope_mismatch"]},
                              "suppression_id": {"type": "string"},
                              "source": {"type": "string", "enum": ["vex_statement", "eshu_policy", "provider_dismissal"]},
                              "justification": {"type": "string", "enum": ["not_affected", "accepted_risk", "false_positive", "ignored", "provider_dismissed"]},
                              "author": {"type": "string"},
                              "authored_at": {"type": "string"},
                              "expires_at": {"type": "string"},
                              "reason": {"type": "string"},
                              "evidence_ref": {"type": "string", "description": "Pointer to the originating evidence (fact ID, VEX URL, provider alert)."},
                              "vex_document_id": {"type": "string"},
                              "vex_statement_id": {"type": "string"}
                            },
                            "required": ["state"]
                          },
                          "remediation": {
                            "type": "object",
                            "description": "Advisory-only safe-upgrade recommendation for this finding (issue #595). The reducer never auto-opens pull requests. confidence is one of exact, partial, or unknown; reason is a closed enum that names the recommended action; manifest_allows_fix labels whether the manifest range already admits the first patched version. Older rows that predate remediation computation omit this block.",
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
                          },
                          "provenance": {
                            "type": "object",
                            "description": "Per-source advisory provenance. Reducers preserve every source observation behind a finding so callers see which advisory source supplied the selected severity, fixed version, and vulnerable range plus alternates other sources reported. Selection uses documented per-ecosystem priority (vendor advisory for OS package classes, GHSA/GLAD/OSV/NVD for language ecosystems).",
                            "properties": {
                              "selected_severity_source": {"type": "string", "description": "Advisory source whose severity was selected."},
                              "selected_severity_score": {"type": "number"},
                              "selected_severity_vector": {"type": "string"},
                              "selected_severity_label": {"type": "string"},
                              "selected_fixed_version_source": {"type": "string", "description": "Advisory source whose fixed-version branch was selected."},
                              "selected_range_source": {"type": "string", "description": "Advisory source whose vulnerable-range expression was selected."},
                              "alternate_severities": {
                                "type": "array",
                                "description": "Severities reported by other sources that were not selected.",
                                "items": {
                                  "type": "object",
                                  "properties": {
                                    "source": {"type": "string"},
                                    "score": {"type": "number"},
                                    "vector": {"type": "string"},
                                    "label": {"type": "string"}
                                  },
                                  "required": ["source"]
                                }
                              },
                              "fixed_version_branches": {
                                "type": "array",
                                "description": "Every source-reported fixed-version branch, with the originating advisory source preserved.",
                                "items": {
                                  "type": "object",
                                  "properties": {
                                    "version": {"type": "string"},
                                    "source": {"type": "string"}
                                  },
                                  "required": ["version", "source"]
                                }
                              },
                              "advisory_sources": {
                                "type": "array",
                                "description": "Every advisory source observation, including source-reported update timestamp and withdrawal timestamp.",
                                "items": {
                                  "type": "object",
                                  "properties": {
                                    "source": {"type": "string"},
                                    "advisory_id": {"type": "string"},
                                    "source_updated_at": {"type": "string"},
                                    "withdrawn_at": {"type": "string"}
                                  },
                                  "required": ["source"]
                                }
                              }
                            }
                          }
                        }
                      }
                    },
                    "count": {"type": "integer"},
                    "limit": {"type": "integer"},
                    "truncated": {"type": "boolean"},
                    "next_cursor": {"type": "object"},
                    "detection_profile": {"type": "string", "enum": ["precise", "comprehensive"], "description": "Echo of the detection profile the caller requested; precise is returned when no profile parameter was supplied."},
                    "readiness": {
                      "type": "object",
                      "description": "Bounded coverage metadata so zero findings can be distinguished from missing target collection or missing required evidence. readiness_unavailable means the readiness lookup itself failed; the findings page is still returned but coverage cannot be classified.",
                      "properties": {
                        "readiness_state": {"type": "string", "enum": ["not_configured", "target_incomplete", "evidence_incomplete", "ready_zero_findings", "ready_with_findings", "ambiguous_scope", "readiness_unavailable", "unsupported"], "description": "Coverage classification for the bounded vulnerability impact answer. ambiguous_scope fires when a single explain scope matches multiple reducer-owned findings and the caller must narrow the request; unsupported fires when Eshu observed real target evidence the matcher cannot resolve (unsupported ecosystem, package-manager file with unsupported lockfile feature, VCS/path/URL/editable dependency source, malformed/unsupported SBOM document, or unsupported image target)."},
                        "target_scope": {
                          "type": "object",
                          "properties": {
                              "cve_id": {"type": "string"},
                              "advisory_id": {"type": "string"},
                              "package_id": {"type": "string"},
                              "repository_id": {"type": "string"},
                              "subject_digest": {"type": "string"},
                              "image_ref": {"type": "string"},
                              "ecosystem": {"type": "string"},
                              "workload_id": {"type": "string"},
                              "service_id": {"type": "string"},
                              "environment": {"type": "string"},
                              "severity": {"type": "string"},
                              "impact_status": {"type": "string"}
                          }
                        },
                        "evidence_sources": {
                          "type": "array",
                          "items": {
                            "type": "object",
                            "properties": {
                              "family": {"type": "string", "enum": ["vulnerability.advisory", "vulnerability.exploitability", "package.consumption", "package.registry", "sbom.component", "sbom.attestation", "container_image.identity", "vulnerability.os_package", "scanner_worker.analysis"]},
                              "fact_count": {"type": "integer", "minimum": 0},
                              "latest_observed_at": {"type": "string"},
                              "freshness": {"type": "string", "enum": ["fresh", "stale", "unknown"]}
                            },
                            "required": ["family", "fact_count"]
                          }
                        },
                        "source_snapshots": {
                          "type": "array",
                          "description": "Vulnerability source snapshot cache metadata; raw advisory payloads are not returned.",
                          "items": {
                            "type": "object",
                            "properties": {
                              "source": {"type": "string"},
                              "ecosystem": {"type": "string"},
                              "cache_artifact_version": {"type": "string"},
                              "snapshot_digest": {"type": "string"},
                              "last_updated_at": {"type": "string"},
                              "freshness": {"type": "string", "enum": ["fresh", "stale", "unknown"]},
                              "complete": {"type": "boolean"},
                              "warning_code": {"type": "string"},
                              "warning_message": {"type": "string"}
                            },
                            "required": ["source", "complete"]
                          }
                        },
                        "source_states": {
                          "type": "array",
                          "description": "Durable source freshness, checkpoint, retry, and terminal status per bounded vulnerability source target.",
                          "items": {
                            "type": "object",
                            "properties": {
                              "collector_instance_id": {"type": "string"},
                              "scope_id": {"type": "string"},
                              "source": {"type": "string"},
                              "ecosystem": {"type": "string"},
                              "collection_window": {
                                "type": "object",
                                "properties": {
                                  "start": {"type": "string"},
                                  "end": {"type": "string"}
                                }
                              },
                              "last_attempt_at": {"type": "string"},
                              "last_success_at": {"type": "string"},
                              "next_retry_at": {"type": "string"},
                              "last_error_class": {"type": "string"},
                              "freshness_state": {"type": "string", "enum": ["not_configured", "pending", "fresh", "stale", "rate_limited", "failed", "partial"]},
                              "terminal_status": {"type": "string", "enum": ["pending", "succeeded", "partial", "failed_retryable", "failed_terminal"]},
                              "result_count": {"type": "integer"},
                              "warning_count": {"type": "integer"},
                              "updated_at": {"type": "string"}
                            },
                            "required": ["scope_id", "source", "freshness_state", "terminal_status", "result_count", "warning_count"]
                          }
                        },
                        "unsupported_targets": {
                          "type": "array",
                          "description": "Observed vulnerability target evidence the matcher cannot resolve. Each entry is bounded coverage-gap evidence; counts MUST NOT be interpreted as clean or affected results. Surfaced alongside readiness_state=unsupported when no finding can be admitted for the scope, and additively for ready states when only some targets are unsupported.",
                          "items": {
                            "type": "object",
                            "properties": {
                              "target_kind": {"type": "string", "enum": ["ecosystem", "package_manager_file", "dependency_source", "sbom_target", "package_registry_metadata", "image_target"], "description": "Family of unsupported target observed: dependency in an unsupported ecosystem, package-manager file with an unsupported lockfile feature, VCS/path/URL/editable dependency source evidence, malformed/unsupported SBOM document tied to the requested subject digest, oversized package-registry metadata, or container image target without a supported analyzer."},
                              "reason": {"type": "string", "description": "Stable reason code explaining why the target is unsupported (e.g., unsupported_ecosystem, lockfile_unsupported_feature, vcs_dependency_unsupported, path_dependency_unsupported, unsupported_field, malformed_document, metadata_too_large)."},
                              "count": {"type": "integer", "minimum": 1},
                              "ecosystem": {"type": "string"},
                              "lockfile_flavor": {"type": "string"},
                              "feature_token": {"type": "string"}
                            },
                            "required": ["target_kind", "reason", "count"]
                          }
                        },
                        "missing_evidence": {"type": "array", "items": {"type": "string", "enum": ["advisory_sources", "owned_packages", "package_registry_metadata", "sbom_or_image_evidence", "target_collection_incomplete", "ambiguous_scope", "readiness_unavailable", "unsupported_targets"]}},
                        "incomplete_reasons": {"type": "array", "items": {"type": "string"}, "description": "Collector-emitted reasons explaining why source collection is still in flight; only present when readiness_state is target_incomplete."},
                        "freshness": {"type": "string", "enum": ["fresh", "stale", "unknown", "pending", "rate_limited", "failed", "partial"]},
                        "counts": {
                          "type": "object",
                          "properties": {
                            "findings_returned": {"type": "integer", "description": "Number of findings in this page; not the total population. Compare with truncated to know if more pages exist. For ambiguous explain responses, this is the matched reducer finding count observed by the ambiguity probe while individual findings are withheld."},
                            "findings_truncated": {"type": "boolean"},
                            "findings_by_status": {"type": "object", "additionalProperties": {"type": "integer"}, "description": "Counts by impact_status across the returned page only."},
                            "evidence_facts_total": {"type": "integer"}
                          }
                        }
                      },
                      "required": ["readiness_state", "target_scope", "evidence_sources", "freshness", "counts"]
                    }
                  }
                }
              }
            }
          }
        }
      }
    },
`
