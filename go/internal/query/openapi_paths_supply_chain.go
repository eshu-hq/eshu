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
                          "missing_evidence": {"type": "array", "items": {"type": "string"}}
                        }
                      }
                    },
                    "count": {"type": "integer"},
                    "limit": {"type": "integer"},
                    "truncated": {"type": "boolean"},
                    "next_cursor": {"type": "object"},
                    "readiness": {
                      "type": "object",
                      "description": "Bounded coverage metadata so zero findings can be distinguished from missing target collection or missing required evidence.",
                      "properties": {
                        "readiness_state": {"type": "string", "enum": ["not_configured", "target_incomplete", "evidence_incomplete", "unsupported", "ready_zero_findings", "ready_with_findings"]},
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
                              "family": {"type": "string"},
                              "fact_count": {"type": "integer"},
                              "latest_observed_at": {"type": "string"},
                              "freshness": {"type": "string", "enum": ["fresh", "stale", "unknown"]}
                            }
                          }
                        },
                        "missing_evidence": {"type": "array", "items": {"type": "string"}},
                        "unsupported_targets": {"type": "array", "items": {"type": "string"}},
                        "freshness": {"type": "string", "enum": ["fresh", "stale", "unknown"]},
                        "counts": {
                          "type": "object",
                          "properties": {
                            "findings_returned": {"type": "integer"},
                            "findings_truncated": {"type": "boolean"},
                            "findings_by_status": {"type": "object", "additionalProperties": {"type": "integer"}},
                            "evidence_facts_total": {"type": "integer"}
                          }
                        }
                      }
                    }
                  }
                }
              }
            }
          }
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
