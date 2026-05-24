package query

const openAPIPathsSupplyChain = `
    "/api/v0/supply-chain/container-images/identities": {
      "get": {
        "summary": "List container image identities",
        "operationId": "listContainerImageIdentities",
        "parameters": [
          {"name": "digest", "in": "query", "schema": {"type": "string"}},
          {"name": "image_ref", "in": "query", "schema": {"type": "string"}},
          {"name": "repository_id", "in": "query", "schema": {"type": "string"}},
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
          {"name": "repository_id", "in": "query", "schema": {"type": "string"}},
          {"name": "subject_digest", "in": "query", "schema": {"type": "string"}},
          {"name": "impact_status", "in": "query", "schema": {"type": "string", "enum": ["affected_exact", "affected_derived", "possibly_affected", "not_affected_known_fixed", "unknown_impact"]}},
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
                          "impact_status": {"type": "string"},
                          "confidence": {"type": "string"},
                          "runtime_reachability": {"type": "string"},
                          "dependency_path": {"type": "array", "items": {"type": "string"}},
                          "dependency_depth": {"type": "integer"},
                          "direct_dependency": {"type": "boolean"},
                          "missing_evidence": {"type": "array", "items": {"type": "string"}},
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
                    "readiness": {
                      "type": "object",
                      "description": "Bounded coverage metadata so zero findings can be distinguished from missing target collection or missing required evidence. readiness_unavailable means the readiness lookup itself failed; the findings page is still returned but coverage cannot be classified.",
                      "properties": {
                        "readiness_state": {"type": "string", "enum": ["not_configured", "target_incomplete", "evidence_incomplete", "ready_zero_findings", "ready_with_findings", "readiness_unavailable"]},
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
                        "missing_evidence": {"type": "array", "items": {"type": "string", "enum": ["advisory_sources", "owned_packages", "sbom_or_image_evidence", "target_collection_incomplete", "readiness_unavailable"]}},
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
          {"name": "repository_id", "in": "query", "schema": {"type": "string"}},
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
                        "workloads": {"type": "array", "items": {"type": "string"}},
                        "provider_alerts": {"type": "array", "items": {"type": "object"}},
                        "evidence_fact_ids": {"type": "array", "items": {"type": "string"}}
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
    "/api/v0/supply-chain/sbom-attestations/attachments": {
      "get": {
        "summary": "List SBOM and attestation attachments",
        "operationId": "listSBOMAttestationAttachments",
        "parameters": [
          {"name": "subject_digest", "in": "query", "schema": {"type": "string"}},
          {"name": "document_id", "in": "query", "schema": {"type": "string"}},
          {"name": "document_digest", "in": "query", "schema": {"type": "string"}},
          {"name": "attachment_status", "in": "query", "schema": {"type": "string", "enum": ["attached_verified", "attached_unverified", "attached_parse_only", "subject_mismatch", "ambiguous_subject", "unknown_subject", "unparseable"]}},
          {"name": "artifact_kind", "in": "query", "schema": {"type": "string", "enum": ["sbom", "attestation"]}},
          {"name": "after_attachment_id", "in": "query", "schema": {"type": "string"}},
          {"name": "limit", "in": "query", "required": true, "schema": {"type": "integer", "minimum": 1, "maximum": 200}}
        ],
        "responses": {
          "200": {
            "description": "SBOM and attestation attachment page",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "attachments": {
                      "type": "array",
                      "items": {
                        "type": "object",
                        "properties": {
                          "attachment_id": {"type": "string"},
                          "subject_digest": {"type": "string"},
                          "document_id": {"type": "string"},
                          "document_digest": {"type": "string"},
                          "attachment_status": {"type": "string"},
                          "parse_status": {"type": "string"},
                          "verification_status": {"type": "string"},
                          "verification_policy": {"type": "string"},
                          "component_count": {"type": "integer"}
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
`
