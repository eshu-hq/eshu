// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

const openAPIPathsSupplyChainSBOMAttestations = `
    "/api/v0/supply-chain/sbom-attestations/attachments": {
      "get": {
        "summary": "List SBOM and attestation attachments",
        "operationId": "listSBOMAttestationAttachments",
        "x-scoped-token-support": true,
        "parameters": [
          {"name": "subject_digest", "in": "query", "schema": {"type": "string"}},
          {"name": "digest", "in": "query", "description": "Alias for subject_digest when the caller has an image digest.", "schema": {"type": "string"}},
          {"name": "document_id", "in": "query", "schema": {"type": "string"}},
          {"name": "document_digest", "in": "query", "schema": {"type": "string"}},
          {"name": "repository_id", "in": "query", "description": "Canonical source repository id or human repository selector (name, repo slug, indexed path, local path, or remote URL). Unknown or ambiguous selectors return a selector error before the bounded attachment read.", "schema": {"type": "string"}},
          {"name": "workload_id", "in": "query", "description": "Reducer-admitted workload anchor. Missing workload-to-image evidence remains explicit.", "schema": {"type": "string"}},
          {"name": "service_id", "in": "query", "description": "Reducer-admitted service anchor. Missing service-to-image evidence remains explicit.", "schema": {"type": "string"}},
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
                          "repository_ids": {"type": "array", "items": {"type": "string"}},
                          "workload_ids": {"type": "array", "items": {"type": "string"}},
                          "service_ids": {"type": "array", "items": {"type": "string"}},
                          "attachment_status": {"type": "string"},
                          "parse_status": {"type": "string"},
                          "verification_status": {"type": "string"},
                          "verification_policy": {"type": "string"},
                          "reason": {"type": "string"},
                          "attachment_scope": {"type": "string", "enum": ["image_subject", "parse_only_unanchored", "subject_only_unanchored", "unanchored"]},
                          "missing_evidence": {"type": "array", "items": {"type": "string"}},
                          "canonical_writes": {"type": "integer"},
                          "component_count": {"type": "integer"},
                          "dependency_relationships": {
                            "type": "array",
                            "description": "Bounded, deduplicated sbom.dependency_relationship evidence rows for this document (write-time capped at 100 per document).",
                            "items": {
                              "type": "object",
                              "properties": {
                                "from_component_id": {"type": "string"},
                                "to_component_id": {"type": "string"},
                                "relationship_type": {"type": "string"},
                                "relationship_origin": {"type": "string"},
                                "fact_id": {"type": "string"}
                              }
                            }
                          },
                          "dependency_relationship_count": {
                            "type": "integer",
                            "description": "Full distinct dependency-relationship tuple count before the write-time cap."
                          },
                          "dependency_relationships_truncated": {
                            "type": "boolean",
                            "description": "True when dependency_relationship_count exceeds the number of rows in dependency_relationships."
                          },
                          "external_references": {
                            "type": "array",
                            "description": "Bounded, deduplicated sbom.external_reference evidence rows for this document (write-time capped at 50 per document).",
                            "items": {
                              "type": "object",
                              "properties": {
                                "component_id": {"type": "string"},
                                "reference_type": {"type": "string"},
                                "reference_url": {"type": "string"},
                                "reference_locator": {"type": "string"},
                                "fact_id": {"type": "string"}
                              }
                            }
                          },
                          "external_reference_count": {
                            "type": "integer",
                            "description": "Full distinct external-reference tuple count before the write-time cap."
                          },
                          "external_references_truncated": {
                            "type": "boolean",
                            "description": "True when external_reference_count exceeds the number of rows in external_references."
                          },
                          "slsa_provenance_predicate_type": {
                            "type": "string",
                            "description": "SLSA predicate type URI decoded from the joined attestation.slsa_provenance fact for this statement (for example https://slsa.dev/provenance/v1). Empty when no such fact joined this statement_id."
                          },
                          "slsa_provenance_builder_id": {
                            "type": "string",
                            "description": "SLSA provenance builder identity decoded from the joined attestation.slsa_provenance fact for this statement. Empty when absent from a well-formed predicate, or when no such fact joined this statement_id."
                          },
                          "slsa_provenance_materials": {
                            "type": "array",
                            "description": "Bounded, write-time capped set of the joined attestation.slsa_provenance fact's materials/resolved dependencies (write-time capped at 20 per statement).",
                            "items": {
                              "type": "object",
                              "properties": {
                                "uri": {"type": "string"},
                                "digest": {"type": "object", "additionalProperties": {"type": "string"}}
                              }
                            }
                          },
                          "slsa_provenance_material_count": {
                            "type": "integer",
                            "description": "Full materials count before the write-time cap."
                          },
                          "slsa_provenance_materials_truncated": {
                            "type": "boolean",
                            "description": "True when slsa_provenance_material_count exceeds the number of rows in slsa_provenance_materials."
                          },
                          "slsa_provenance_config_source_uri": {
                            "type": "string",
                            "description": "SLSA provenance build definition config source URI decoded from the joined attestation.slsa_provenance fact (for example a git+https source URL with a ref suffix). Empty when absent or when no such fact joined this statement_id."
                          },
                          "slsa_provenance_config_source_entry_point": {
                            "type": "string",
                            "description": "SLSA provenance build definition config source entry point (for example a workflow file path). Empty when absent."
                          },
                          "slsa_provenance_config_source_digest": {
                            "type": "object",
                            "description": "SLSA provenance build definition config source digest map, keyed by algorithm (for example sha1).",
                            "additionalProperties": {"type": "string"}
                          },
                          "warning_summaries": {
                            "type": "array",
                            "maxItems": 10,
                            "description": "Bounded duplicate-collapsed preview of SBOM warning summaries.",
                            "items": {"type": "string"}
                          },
                          "warning_summary_count": {
                            "type": "integer",
                            "description": "Total SBOM warning occurrences represented by the attachment payload before preview bounding."
                          },
                          "warning_summaries_truncated": {
                            "type": "boolean",
                            "description": "True when warning_summaries omits duplicate, aggregate, or overflow entries from the recorded warning summaries."
                          }
                        }
                      }
                    },
                    "count": {"type": "integer"},
                    "missing_evidence": {"type": "array", "items": {"type": "string"}},
                    "limit": {"type": "integer"},
                    "truncated": {"type": "boolean"},
                    "next_cursor": {"type": "object"},
                    "collector_readiness": {"$ref": "#/components/schemas/CollectorReadinessEnvelope"}
                  }
                }
              }
            }
          }
        }
      }
    },
`
