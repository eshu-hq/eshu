// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package chain

// SBOMAttestations is the OpenAPI path fragment documenting the
// `/api/v0/supply-chain/sbom-attestations/attachments` route. openapi.Spec
// concatenates it into the published document; keep it in lockstep with the
// handlers and docs/public/reference/http-api.md.
const SBOMAttestations = `
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
          "403": {"$ref": "#/components/responses/Forbidden"},
          "503": {"$ref": "#/components/responses/ServiceUnavailable"},
          "504": {"$ref": "#/components/responses/GatewayTimeout"},
          "200": {
            "description": "SBOM and attestation attachment page",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "attachments": {
                      "type": "array",
                      "description": "Reducer-owned SBOM and attestation attachment rows emitted by listSBOMAttachments (SBOMAttestationAttachmentResult).",
                      "items": {
                        "type": "object", "description": "One SBOM attestation attachment row. emitted by listSBOMAttachments (SBOMAttestationAttachmentResult).",
                        "properties": {
                          "attachment_id": {"type": "string", "description": "Attachment id emitted by listSBOMAttachments (SBOMAttestationAttachmentResult.AttachmentID)."},
                          "subject_digest": {"type": "string", "description": "Subject digest the attachment anchors to, emitted by listSBOMAttachments (SBOMAttestationAttachmentResult.SubjectDigest)."},
                          "document_id": {"type": "string", "description": "Document id emitted by listSBOMAttachments (SBOMAttestationAttachmentResult.DocumentID)."},
                          "document_digest": {"type": "string", "description": "Document digest emitted by listSBOMAttachments (SBOMAttestationAttachmentResult.DocumentDigest)."},
                          "repository_ids": {"type": "array", "items": {"type": "string"}, "description": "Durable git attribution repository ids emitted by listSBOMAttachments (SBOMAttestationAttachmentResult.RepositoryIDs)."},
                          "workload_ids": {"type": "array", "items": {"type": "string"}, "description": "Workload ids emitted by listSBOMAttachments (SBOMAttestationAttachmentResult.WorkloadIDs)."},
                          "service_ids": {"type": "array", "items": {"type": "string"}, "description": "Service ids emitted by listSBOMAttachments (SBOMAttestationAttachmentResult.ServiceIDs)."},
                          "attachment_status": {"type": "string", "description": "Attachment status emitted by listSBOMAttachments (SBOMAttestationAttachmentResult.AttachmentStatus)."},
                          "parse_status": {"type": "string", "description": "Parse status emitted by listSBOMAttachments (SBOMAttestationAttachmentResult.ParseStatus)."},
                          "verification_status": {"type": "string", "description": "Verification status emitted by listSBOMAttachments (SBOMAttestationAttachmentResult.VerificationStatus)."},
                          "verification_policy": {"type": "string", "description": "Verification policy emitted by listSBOMAttachments (SBOMAttestationAttachmentResult.VerificationPolicy)."},
                          "artifact_kind": {"type": "string", "description": "Artifact kind (sbom, attestation) emitted by listSBOMAttachments (SBOMAttestationAttachmentResult.ArtifactKind)."},
                          "format": {"type": "string", "description": "Document format emitted by listSBOMAttachments (SBOMAttestationAttachmentResult.Format)."},
                          "spec_version": {"type": "string", "description": "Document spec version emitted by listSBOMAttachments (SBOMAttestationAttachmentResult.SpecVersion)."},
                          "reason": {"type": "string", "description": "Status reason emitted by listSBOMAttachments (SBOMAttestationAttachmentResult.Reason)."},
                          "attachment_scope": {"type": "string", "enum": ["image_subject", "parse_only_unanchored", "subject_only_unanchored", "unanchored"], "description": "Attachment scope emitted by listSBOMAttachments (SBOMAttestationAttachmentResult.AttachmentScope)."},
                          "missing_evidence": {"type": "array", "items": {"type": "string"}, "description": "Evidence gaps emitted by listSBOMAttachments (SBOMAttestationAttachmentResult.MissingEvidence)."},
                          "canonical_writes": {"type": "integer", "description": "Canonical write count emitted by listSBOMAttachments (SBOMAttestationAttachmentResult.CanonicalWrites)."},
                          "component_count": {"type": "integer", "description": "True component total before the row cap, emitted by listSBOMAttachments (SBOMAttestationAttachmentResult.ComponentCount)."},
                          "component_evidence": {
                            "type": "array",
                            "description": "Bounded, deduplicated sbom.component evidence rows for this document (write-time capped at 100 per document), emitted by listSBOMAttachments (ComponentEvidenceRow).",
                            "items": {
                              "type": "object", "description": "One component evidence row. emitted by listSBOMAttachments (ComponentEvidenceRow).",
                              "properties": {
                                "component_id": {"type": "string", "description": "Component id emitted by listSBOMAttachments (ComponentEvidenceRow.ComponentID)."},
                                "name": {"type": "string", "description": "Component name emitted by listSBOMAttachments (ComponentEvidenceRow.Name)."},
                                "version": {"type": "string", "description": "Component version emitted by listSBOMAttachments (ComponentEvidenceRow.Version)."},
                                "purl": {"type": "string", "description": "Component package URL emitted by listSBOMAttachments (ComponentEvidenceRow.PURL)."},
                                "cpe": {"type": "string", "description": "Component CPE emitted by listSBOMAttachments (ComponentEvidenceRow.CPE)."},
                                "fact_id": {"type": "string", "description": "Fact id behind the component row, emitted by listSBOMAttachments (ComponentEvidenceRow.FactID)."}
                              }
                            }
                          },
                          "component_evidence_truncated": {
                            "type": "boolean",
                            "description": "True when component_count exceeds the number of rows in component_evidence."
                          },
                          "dependency_relationships": {
                            "type": "array",
                            "description": "Bounded, deduplicated sbom.dependency_relationship evidence rows for this document (write-time capped at 100 per document), emitted by listSBOMAttachments (DependencyRelationshipRow).",
                            "items": {
                              "type": "object", "description": "One dependency relationship row. emitted by listSBOMAttachments (DependencyRelationshipRow).",
                              "properties": {
                                "from_component_id": {"type": "string", "description": "Source component id emitted by listSBOMAttachments (DependencyRelationshipRow.FromComponentID)."},
                                "to_component_id": {"type": "string", "description": "Target component id emitted by listSBOMAttachments (DependencyRelationshipRow.ToComponentID)."},
                                "relationship_type": {"type": "string", "description": "Relationship type emitted by listSBOMAttachments (DependencyRelationshipRow.RelationshipType)."},
                                "relationship_origin": {"type": "string", "description": "Relationship origin emitted by listSBOMAttachments (DependencyRelationshipRow.RelationshipOrigin)."},
                                "fact_id": {"type": "string", "description": "Fact id behind the relationship row, emitted by listSBOMAttachments (DependencyRelationshipRow.FactID)."}
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
                            "description": "Bounded, deduplicated sbom.external_reference evidence rows for this document (write-time capped at 50 per document), emitted by listSBOMAttachments (ExternalReferenceRow).",
                            "items": {
                              "type": "object", "description": "One external reference row. emitted by listSBOMAttachments (ExternalReferenceRow).",
                              "properties": {
                                "component_id": {"type": "string", "description": "Component id emitted by listSBOMAttachments (ExternalReferenceRow.ComponentID)."},
                                "reference_type": {"type": "string", "description": "Reference type emitted by listSBOMAttachments (ExternalReferenceRow.ReferenceType)."},
                                "reference_url": {"type": "string", "description": "Reference URL emitted by listSBOMAttachments (ExternalReferenceRow.ReferenceURL)."},
                                "reference_locator": {"type": "string", "description": "Reference locator emitted by listSBOMAttachments (ExternalReferenceRow.ReferenceLocator)."},
                                "fact_id": {"type": "string", "description": "Fact id behind the reference row, emitted by listSBOMAttachments (ExternalReferenceRow.FactID)."}
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
                            "description": "Bounded, write-time capped set of the joined attestation.slsa_provenance fact's materials/resolved dependencies (write-time capped at 20 per statement), emitted by listSBOMAttachments (SLSAMaterialRow).",
                            "items": {
                              "type": "object", "description": "One SLSA provenance material row. emitted by listSBOMAttachments (SLSAMaterialRow).",
                              "properties": {
                                "uri": {"type": "string", "description": "Material URI emitted by listSBOMAttachments (SLSAMaterialRow.URI)."},
                                "digest": {"type": "object", "additionalProperties": {"type": "string"}, "description": "Material digests by algorithm emitted by listSBOMAttachments (SLSAMaterialRow.Digest)."}
                              }
                            }
                          },
                          "evidence_fact_ids": {"type": "array", "items": {"type": "string"}, "description": "Evidence fact ids emitted by listSBOMAttachments (SBOMAttestationAttachmentResult.EvidenceFactIDs)."},
                          "source_freshness": {"type": "string", "description": "Source freshness signal emitted by listSBOMAttachments (SBOMAttestationAttachmentResult.SourceFreshness)."},
                          "source_confidence": {"type": "string", "description": "Source confidence signal emitted by listSBOMAttachments (SBOMAttestationAttachmentResult.SourceConfidence)."},
                          "slsa_provenance_material_count": {
                            "type": "integer",
                            "description": "Full materials count before the write-time cap, emitted by listSBOMAttachments (SBOMAttestationAttachmentResult.SLSAProvenanceMaterialCount)."
                          },
                          "slsa_provenance_materials_truncated": {
                            "type": "boolean",
                            "description": "True when slsa_provenance_material_count exceeds the number of rows in slsa_provenance_materials, emitted by listSBOMAttachments (SBOMAttestationAttachmentResult.SLSAProvenanceMaterialsTruncated)."
                          },
                          "slsa_provenance_config_source_uri": {
                            "type": "string",
                            "description": "SLSA provenance build definition config source URI decoded from the joined attestation.slsa_provenance fact (for example a git+https source URL with a ref suffix), emitted by listSBOMAttachments (SBOMAttestationAttachmentResult.SLSAProvenanceConfigSourceURI). Empty when absent or when no such fact joined this statement_id."
                          },
                          "slsa_provenance_config_source_entry_point": {
                            "type": "string",
                            "description": "SLSA provenance build definition config source entry point (for example a workflow file path), emitted by listSBOMAttachments (SBOMAttestationAttachmentResult.SLSAProvenanceConfigSourceEntryPoint). Empty when absent."
                          },
                          "slsa_provenance_config_source_digest": {
                            "type": "object",
                            "description": "SLSA provenance build definition config source digest map, keyed by algorithm (for example sha1), emitted by listSBOMAttachments (SBOMAttestationAttachmentResult.SLSAProvenanceConfigSourceDigest).",
                            "additionalProperties": {"type": "string"}
                          },
                          "warning_summaries": {
                            "type": "array",
                            "maxItems": 10,
                            "description": "Bounded duplicate-collapsed preview of SBOM warning summaries, emitted by listSBOMAttachments (SBOMAttestationAttachmentResult.WarningSummaries).",
                            "items": {"type": "string"}
                          },
                          "warning_summary_count": {
                            "type": "integer",
                            "description": "Total SBOM warning occurrences represented by the attachment payload before preview bounding, emitted by listSBOMAttachments (SBOMAttestationAttachmentResult.WarningSummaryCount)."
                          },
                          "warning_summaries_truncated": {
                            "type": "boolean",
                            "description": "True when warning_summaries omits duplicate, aggregate, or overflow entries from the recorded warning summaries, emitted by listSBOMAttachments (SBOMAttestationAttachmentResult.WarningSummariesTruncated)."
                          }
                        }
                      }
                    },
                    "count": {"type": "integer", "description": "Rows on this page, emitted by listSBOMAttachments."},
                    "missing_evidence": {"type": "array", "items": {"type": "string"}, "description": "Scope-level missing-evidence diagnostics for source-anchor reads, emitted by listSBOMAttachments; absent on unanchored pages."},
                    "limit": {"type": "integer", "description": "Requested page size echoed by listSBOMAttachments."},
                    "truncated": {"type": "boolean", "description": "Whether more rows exist past this page, emitted by listSBOMAttachments."},
                    "next_cursor": {"type": "object", "description": "Keyset cursor emitted by listSBOMAttachments only when truncated carries the last attachment id forward; absent otherwise.", "properties": {
                      "after_attachment_id": {"type": "string", "description": "Attachment id of the last row, emitted by listSBOMAttachments for the next page cursor."}
                    }},
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
