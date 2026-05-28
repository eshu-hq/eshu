package query

const openAPIPathsSupplyChain = `
    "/api/v0/supply-chain/container-images/identities": {
      "get": {
        "summary": "List container image identities",
        "operationId": "listContainerImageIdentities",
        "parameters": [
          {"name": "digest", "in": "query", "schema": {"type": "string"}},
          {"name": "image_ref", "in": "query", "schema": {"type": "string"}},
          {"name": "repository_id", "in": "query", "description": "OCI/image repository identity observed for the container image. This is not a source repository selector.", "schema": {"type": "string"}},
          {"name": "outcome", "in": "query", "schema": {"type": "string", "enum": ["exact_digest", "tag_resolved"]}},
          {"name": "after_identity_id", "in": "query", "schema": {"type": "string"}},
          {"name": "limit", "in": "query", "required": true, "schema": {"type": "integer", "minimum": 1, "maximum": 200}}
        ],
        "responses": {
          "200": {
            "description": "Container image identity page",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "identities": {
                      "type": "array",
                      "items": {
                        "type": "object",
                        "properties": {
                          "identity_id": {"type": "string"},
                          "digest": {"type": "string"},
                          "image_ref": {"type": "string"},
                          "repository_id": {"type": "string"},
                          "outcome": {"type": "string"},
                          "reason": {"type": "string"},
                          "identity_strength": {"type": "string"},
                          "source_layers": {"type": "array", "items": {"type": "string"}},
                          "evidence_fact_ids": {"type": "array", "items": {"type": "string"}}
                        }
                      }
                    },
                    "count": {"type": "integer"},
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
    "/api/v0/supply-chain/impact/findings": {
      "get": {
        "summary": "List supply-chain impact findings",
        "operationId": "listSupplyChainImpactFindings",
        "parameters": [
          {"name": "cve_id", "in": "query", "schema": {"type": "string"}},
          {"name": "package_id", "in": "query", "schema": {"type": "string"}},
          {"name": "repository_id", "in": "query", "description": "Canonical repository id or human repository selector (name, repo slug, indexed path, local path, or remote URL). Unknown or ambiguous selectors return a selector error instead of an empty page.", "schema": {"type": "string"}},
          {"name": "subject_digest", "in": "query", "schema": {"type": "string"}},
          {"name": "impact_status", "in": "query", "schema": {"type": "string", "enum": ["affected_exact", "affected_derived", "possibly_affected", "not_affected_known_fixed", "unknown_impact"]}},
          {"name": "profile", "in": "query", "schema": {"type": "string", "enum": ["precise", "comprehensive"], "default": "precise"}, "description": "Detection profile selector. precise (default) returns only findings backed by exact installed-version anchors. comprehensive also returns range-only manifest, SBOM/CPE-derived, malformed range, unsupported ecosystem, and missing-version rows. Each row keeps its truth labels (impact_status, confidence, runtime_reachability) and missing-evidence reasons."},
          {"name": "priority_bucket", "in": "query", "schema": {"type": "string", "enum": ["critical", "high", "medium", "low", "informational"]}, "description": "Reducer triage priority filter; does not change impact truth."},
          {"name": "min_priority_score", "in": "query", "schema": {"type": "integer", "minimum": 0, "maximum": 100}, "description": "Minimum reducer priority score. Zero is the default no-op value and does not bound a request by itself."},
          {"name": "sort", "in": "query", "schema": {"type": "string", "enum": ["finding_id", "priority", "priority_score_desc", "priority_score_asc"]}},
          {"name": "suppression_state", "in": "query", "description": "Filter by reducer suppression decision. Operator-asserted hidden states (not_affected, accepted_risk, false_positive, ignored) require include_suppressed=true to be returned.", "schema": {"type": "string", "enum": ["active", "not_affected", "accepted_risk", "false_positive", "ignored", "expired", "provider_dismissed", "scope_mismatch"]}},
          {"name": "include_suppressed", "in": "query", "description": "Include findings hidden by operator-asserted suppression. Expired, provider-dismissed, and scope-mismatched findings stay visible regardless because they preserve operator audit signal.", "schema": {"type": "boolean", "default": false}},
          {"name": "after_finding_id", "in": "query", "schema": {"type": "string"}},
          {"name": "limit", "in": "query", "required": true, "schema": {"type": "integer", "minimum": 1, "maximum": 200}}
        ],
        "responses": {
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
                          "match_reason": {"type": "string", "description": "Reducer reason for the version/range decision, including unsupported or malformed evidence states."},
                          "detection_profile": {"type": "string", "enum": ["precise", "comprehensive"], "description": "Evidence tier the row meets. precise requires an exact installed-version anchor and ecosystem-aware match. comprehensive covers SBOM/CPE-derived, range-only, malformed, unsupported, or missing-version rows that still have an owned anchor."},
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
                          "repository_id": {"type": "string"},
                          "subject_digest": {"type": "string"},
                          "image_ref": {"type": "string"},
                          "dependency_scope": {"type": "string"},
                          "workload_ids": {"type": "array", "items": {"type": "string"}},
                          "service_ids": {"type": "array", "items": {"type": "string"}},
                          "environments": {"type": "array", "items": {"type": "string"}},
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
                              "reason": {"type": "string", "enum": ["direct_upgrade_allowed", "direct_range_blocked", "transitive_parent_upgrade_required", "no_patched_version", "multiple_patched_branches", "package_manager_unsupported", "manifest_range_missing", "manifest_range_malformed", "installed_version_missing", "installed_version_malformed"]},
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
                        "readiness_state": {"type": "string", "enum": ["not_configured", "target_incomplete", "evidence_incomplete", "ready_zero_findings", "ready_with_findings", "readiness_unavailable", "unsupported"], "description": "Coverage classification for the bounded vulnerability impact answer. unsupported fires when Eshu observed real target evidence the matcher cannot resolve (unsupported ecosystem, package-manager file with unsupported lockfile feature, malformed/unsupported SBOM document, or unsupported image target)."},
                        "target_scope": {
                          "type": "object",
                          "properties": {
                            "cve_id": {"type": "string"},
                            "package_id": {"type": "string"},
                            "repository_id": {"type": "string"},
                            "subject_digest": {"type": "string"},
                            "impact_status": {"type": "string"}
                          }
                        },
                        "evidence_sources": {
                          "type": "array",
                          "items": {
                            "type": "object",
                            "properties": {
                              "family": {"type": "string", "enum": ["vulnerability.advisory", "vulnerability.exploitability", "package.consumption", "package.registry", "sbom.component", "sbom.attestation", "container_image.identity"]},
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
                              "target_kind": {"type": "string", "enum": ["ecosystem", "package_manager_file", "sbom_target", "package_registry_metadata", "image_target"], "description": "Family of unsupported target observed: dependency in an unsupported ecosystem, package-manager file with an unsupported lockfile feature, malformed/unsupported SBOM document tied to the requested subject digest, oversized package-registry metadata, or container image target without a supported analyzer."},
                              "reason": {"type": "string", "description": "Stable reason code explaining why the target is unsupported (e.g., unsupported_ecosystem, lockfile_unsupported_feature, unsupported_field, malformed_document, metadata_too_large)."},
                              "count": {"type": "integer", "minimum": 1},
                              "ecosystem": {"type": "string"},
                              "lockfile_flavor": {"type": "string"},
                              "feature_token": {"type": "string"}
                            },
                            "required": ["target_kind", "reason", "count"]
                          }
                        },
                        "missing_evidence": {"type": "array", "items": {"type": "string", "enum": ["advisory_sources", "owned_packages", "sbom_or_image_evidence", "target_collection_incomplete", "readiness_unavailable", "unsupported_targets"]}},
                        "incomplete_reasons": {"type": "array", "items": {"type": "string"}, "description": "Collector-emitted reasons explaining why source collection is still in flight; only present when readiness_state is target_incomplete."},
                        "freshness": {"type": "string", "enum": ["fresh", "stale", "unknown", "pending", "rate_limited", "failed", "partial"]},
                        "counts": {
                          "type": "object",
                          "properties": {
                            "findings_returned": {"type": "integer", "description": "Number of findings in this page; not the total population. Compare with truncated to know if more pages exist."},
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
    "/api/v0/supply-chain/impact/explain": {
      "get": {
        "summary": "Explain one supply-chain impact finding",
        "operationId": "explainSupplyChainImpact",
        "parameters": [
          {"name": "finding_id", "in": "query", "schema": {"type": "string"}},
          {"name": "advisory_id", "in": "query", "schema": {"type": "string"}},
          {"name": "cve_id", "in": "query", "schema": {"type": "string"}},
          {"name": "package_id", "in": "query", "schema": {"type": "string"}},
          {"name": "repository_id", "in": "query", "description": "Canonical repository id or human source repository selector (name, repo slug, indexed path, local path, or remote URL). Unknown or ambiguous selectors return a selector error instead of a no-evidence explanation.", "schema": {"type": "string"}},
          {"name": "subject_digest", "in": "query", "schema": {"type": "string"}}
        ],
        "responses": {
          "200": {
            "description": "Bounded finding explanation or no-evidence response",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "outcome": {"type": "string", "enum": ["finding_explained", "no_finding"]},
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
                        "services": {"type": "array", "items": {"type": "string"}},
                        "environments": {"type": "array", "items": {"type": "string"}},
                        "provider_alerts": {"type": "array", "items": {"type": "object"}},
                        "evidence_fact_ids": {"type": "array", "items": {"type": "string"}}
                      }
                    },
                    "impact_path": {
                      "type": "array",
                      "description": "Reducer-owned present and missing hops from advisory/package evidence to repository, image, workload, service, and environment evidence. Missing hops remain explicit and are not inferred from names or tags.",
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
                        "reason": {"type": "string", "enum": ["direct_upgrade_allowed", "direct_range_blocked", "transitive_parent_upgrade_required", "no_patched_version", "multiple_patched_branches", "package_manager_unsupported", "manifest_range_missing", "manifest_range_malformed", "installed_version_missing", "installed_version_malformed"]},
                        "missing_evidence": {"type": "array", "items": {"type": "string"}}
                      }
                    }
                  },
                  "required": ["outcome", "input", "advisory", "component", "version", "anchors", "evidence", "readiness", "freshness"]
                }
              }
            }
          },
          "400": {"description": "Unbounded input"},
          "409": {"description": "Scope matched more than one finding"}
        }
      }
    },
    "/api/v0/supply-chain/security-alerts/reconciliations": {
      "get": {
        "summary": "List provider security alert reconciliations",
        "description": "Requires limit plus repository_id, provider, package_id, cve_id, or ghsa_id. provider_state and reconciliation_status filter anchored pages only.",
        "operationId": "listSecurityAlertReconciliations",
        "parameters": [
          {"name": "repository_id", "in": "query", "description": "Canonical repository id or human repository selector (name, repo slug, indexed path, local path, or remote URL). Unknown or ambiguous selectors return a selector error instead of an empty page.", "schema": {"type": "string"}},
          {"name": "provider", "in": "query", "schema": {"type": "string"}},
          {"name": "package_id", "in": "query", "schema": {"type": "string"}},
          {"name": "cve_id", "in": "query", "schema": {"type": "string"}},
          {"name": "ghsa_id", "in": "query", "schema": {"type": "string"}},
          {"name": "provider_state", "in": "query", "schema": {"type": "string", "enum": ["open", "fixed", "dismissed", "auto_dismissed"]}},
          {"name": "reconciliation_status", "in": "query", "schema": {"type": "string", "enum": ["matched", "unmatched", "stale", "dismissed", "fixed", "provider_only"]}},
          {"name": "after_reconciliation_id", "in": "query", "schema": {"type": "string"}},
          {"name": "limit", "in": "query", "required": true, "schema": {"type": "integer", "minimum": 1, "maximum": 200}}
        ],
        "responses": {
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
                          "eshu_impact": {
                            "type": "object",
                            "description": "Reducer-owned impact state matched to the alert, when Eshu has admitted owned evidence.",
                            "properties": {
                              "impact_status": {"type": "string"},
                              "finding_id": {"type": "string"}
                            }
                          },
                          "reconciliation_status": {"type": "string"},
                          "reason": {"type": "string"},
                          "evidence_fact_ids": {"type": "array", "items": {"type": "string"}},
                          "source_freshness": {"type": "string"},
                          "source_confidence": {"type": "string"}
                        },
                        "required": ["reconciliation_id", "provider_alert", "eshu_impact", "reconciliation_status"]
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
