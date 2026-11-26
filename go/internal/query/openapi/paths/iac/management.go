// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package iac

// ManagementStatus documents the exact IaC management status read. It is
// split from Routes to keep that file under the source size cap.
// managementFindingShapeBody is the shared finding-node body for the two
// exact management reads. Both handlers emit the same *ManagementFindingRow
// from iac.loadExactIaCManagementFinding (nil when no active finding), so
// both fragments concatenate this body instead of maintaining two copies.
const managementFindingShapeBody = `"type": ["object", "null"], "description": "The exact active management finding as an iac.ManagementFindingRow (null when no active finding), loaded by iac.loadExactIaCManagementFinding.", "properties": {
                      "id": {"type": "string", "description": "Finding identity emitted by iac.ManagementFindingRow.ID."},
                      "provider": {"type": "string", "description": "Cloud provider emitted by iac.ManagementFindingRow.Provider."},
                      "account_id": {"type": "string", "description": "AWS account id emitted by iac.ManagementFindingRow.AccountID; absent when empty."},
                      "region": {"type": "string", "description": "AWS region emitted by iac.ManagementFindingRow.Region; absent when empty."},
                      "resource_type": {"type": "string", "description": "Cloud resource type emitted by iac.ManagementFindingRow.ResourceType; absent when empty."},
                      "resource_id": {"type": "string", "description": "Cloud resource id emitted by iac.ManagementFindingRow.ResourceID; absent when empty."},
                      "arn": {"type": "string", "description": "Cloud resource ARN emitted by iac.ManagementFindingRow.ARN; absent when empty."},
                      "tags": {"type": "object", "additionalProperties": {"type": "string"}, "description": "Resource tags emitted by iac.ManagementFindingRow.Tags; absent when empty."},
                      "finding_kind": {"type": "string", "description": "Finding kind emitted by iac.ManagementFindingRow.FindingKind."},
                      "management_status": {"type": "string", "description": "Current management status emitted by iac.ManagementFindingRow.ManagementStatus."},
                      "confidence": {"type": "number", "description": "Finding confidence emitted by iac.ManagementFindingRow.Confidence."},
                      "scope_id": {"type": "string", "description": "Owning collector scope emitted by iac.ManagementFindingRow.ScopeID."},
                      "generation_id": {"type": "string", "description": "Reducer generation that produced the finding, emitted by iac.ManagementFindingRow.GenerationID."},
                      "source_system": {"type": "string", "description": "Source system of the finding, emitted by iac.ManagementFindingRow.SourceSystem."},
                      "candidate_id": {"type": "string", "description": "Candidate identity emitted by iac.ManagementFindingRow.CandidateID; absent when empty."},
                      "matched_terraform_state_address": {"type": "string", "description": "Matched Terraform state address emitted by iac.ManagementFindingRow.MatchedTerraformStateAddress; absent when empty."},
                      "matched_terraform_config_file": {"type": "string", "description": "Matched Terraform config file emitted by iac.ManagementFindingRow.MatchedTerraformConfigFile; absent when empty."},
                      "matched_terraform_module_path": {"type": "string", "description": "Matched Terraform module path emitted by iac.ManagementFindingRow.MatchedTerraformModulePath; absent when empty."},
                      "matched_other_iac_source": {"type": "string", "description": "Matched non-Terraform IaC source emitted by iac.ManagementFindingRow.MatchedOtherIaCSource; absent when empty."},
                      "service_candidates": {"type": "array", "items": {"type": "string"}, "description": "Candidate service names emitted by iac.ManagementFindingRow.ServiceCandidates; absent when empty."},
                      "environment_candidates": {"type": "array", "items": {"type": "string"}, "description": "Candidate environment names emitted by iac.ManagementFindingRow.EnvironmentCandidates; absent when empty."},
                      "dependency_paths": {"type": "array", "items": {"type": "string"}, "description": "Dependency paths emitted by iac.ManagementFindingRow.DependencyPaths; absent when empty."},
                      "recommended_action": {"type": "string", "description": "Recommended operator action emitted by iac.ManagementFindingRow.RecommendedAction."},
                      "missing_evidence": {"type": "array", "items": {"type": "string"}, "description": "Evidence keys still missing, emitted by iac.ManagementFindingRow.MissingEvidence; absent when empty."},
                      "warning_flags": {"type": "array", "items": {"type": "string"}, "description": "Warning flags emitted by iac.ManagementFindingRow.WarningFlags; absent when empty."},
                      "safety_gate": {"type": "object", "description": "Safety decision carried on the finding, emitted by iac.ManagementFindingRow.SafetyGate.", "properties": {
                        "outcome": {"type": "string", "description": "Safety outcome emitted by packet.ManagementSafetyGate.Outcome."},
                        "read_only": {"type": "boolean", "description": "Whether the finding is read-only, emitted by packet.ManagementSafetyGate.ReadOnly."},
                        "review_required": {"type": "boolean", "description": "Whether human review is required, emitted by packet.ManagementSafetyGate.ReviewRequired."},
                        "refused_actions": {"type": "array", "items": {"type": "string"}, "description": "Actions the gate refuses, emitted by packet.ManagementSafetyGate.RefusedActions; absent when empty."},
                        "warnings": {"type": "array", "items": {"type": "string"}, "description": "Gate warnings emitted by packet.ManagementSafetyGate.Warnings; absent when empty."},
                        "redactions": {"type": "array", "items": {"type": "string"}, "description": "Fields the gate redacted, emitted by packet.ManagementSafetyGate.Redactions; absent when empty."},
                        "audit_expectation": {"type": "string", "description": "Audit expectation set by the gate, emitted by packet.ManagementSafetyGate.AuditExpectation."}
                      }, "required": ["outcome", "read_only", "review_required", "audit_expectation"]},
                      "evidence": {"type": "array", "description": "Evidence atoms behind the finding, emitted by iac.ManagementFindingRow.Evidence.", "items": {"type": "object", "description": "One evidence atom. Owner: iac.ManagementEvidenceRow.", "properties": {
                        "id": {"type": "string", "description": "Evidence atom identity emitted by iac.ManagementEvidenceRow.ID."},
                        "source_system": {"type": "string", "description": "Source system of the atom, emitted by iac.ManagementEvidenceRow.SourceSystem."},
                        "evidence_type": {"type": "string", "description": "Atom type emitted by iac.ManagementEvidenceRow.EvidenceType."},
                        "scope_id": {"type": "string", "description": "Scope the atom's data came from, emitted by iac.ManagementEvidenceRow.ScopeID."},
                        "key": {"type": "string", "description": "Evidence key emitted by iac.ManagementEvidenceRow.Key."},
                        "value": {"type": "string", "description": "Evidence value with sensitive values redacted, emitted by iac.ManagementEvidenceRow.Value."},
                        "confidence": {"type": "number", "description": "Atom confidence emitted by iac.ManagementEvidenceRow.Confidence."},
                        "provenance_only": {"type": "boolean", "description": "Whether the atom is provenance-only, emitted by iac.ManagementEvidenceRow.ProvenanceOnly."}
                      }, "required": ["id", "source_system", "evidence_type", "scope_id", "key", "value", "confidence", "provenance_only"]}},
                      "drifted_attributes": {"type": "array", "description": "Bounded declared/observed value pairs emitted by iac.ManagementFindingRow.DriftedAttributes; present only on an image_version_drift finding, absent otherwise.", "items": {"type": "object", "description": "One drifted attribute. Owner: querycontract.DriftedAttributeView.", "properties": {
                        "attribute": {"type": "string", "description": "Allowlisted comparable attribute name emitted by querycontract.DriftedAttributeView.Attribute."},
                        "declared_value": {"type": "string", "description": "Terraform-state value emitted by querycontract.DriftedAttributeView.Declared."},
                        "observed_value": {"type": "string", "description": "AWS-observed cloud value emitted by querycontract.DriftedAttributeView.Observed."}
                      }, "required": ["attribute", "declared_value", "observed_value"]}}
                    }, "required": ["id", "provider", "finding_kind", "management_status", "confidence", "scope_id", "generation_id", "source_system", "recommended_action", "safety_gate", "evidence"]`

const ManagementStatus = `
    "/api/v0/iac/management-status": {
      "post": {
        "tags": ["iac"],
        "summary": "Get IaC management status for one cloud resource",
        "description": "Returns the current read-only IaC management status for one exact AWS resource identity. Requests must be bounded by scope_id or account_id and by arn or resource_id. Sensitive evidence values are redacted and sensitive, ambiguous, unknown, or stale findings require security review before import-plan use.",
        "operationId": "getIaCManagementStatus",
        "x-scoped-token-support": true,
        "requestBody": {
          "required": true,
          "content": {
            "application/json": {
              "schema": {
                "type": "object",
                "properties": {
                  "scope_id": {"type": "string"},
                  "account_id": {"type": "string"},
                  "region": {"type": "string"},
                  "arn": {"type": "string"},
                  "resource_id": {"type": "string"},
                  "finding_kinds": {"type": "array", "items": {"type": "string"}}
                },
                "anyOf": [
                  {"required": ["scope_id", "arn"]},
                  {"required": ["scope_id", "resource_id"]},
                  {"required": ["account_id", "arn"]},
                  {"required": ["account_id", "resource_id"]}
                ]
              }
            }
          }
        },
        "responses": {
          "200": {
            "description": "IaC management status",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "story": {"type": "string", "description": "One-line read summary rendered by iac.managementStatusStory from the exact selector and the loaded finding."},
                    "arn": {"type": "string", "description": "Echo of the caller's exact ARN selector, emitted by the iac management-status handler from the normalized filter."},
                    "scope_id": {"type": "string", "description": "Echo of the caller's scope_id selector, emitted by the iac management-status handler from the normalized filter."},
                    "account_id": {"type": "string", "description": "Echo of the caller's account_id selector, emitted by the iac management-status handler from the normalized filter."},
                    "region": {"type": "string", "description": "Echo of the caller's region selector, emitted by the iac management-status handler from the normalized filter."},
                    "management_status": {"type": "string", "description": "Current management status for the exact resource (unknown when no active finding), emitted by the iac management-status handler from the loaded finding."},
                    "analysis_status": {"type": "string", "description": "Read status label emitted by the iac management-status handler (active_management_finding or no_active_management_finding)."},
                    "finding": {` + managementFindingShapeBody + `},
                    "safety_gate": {
                      "type": "object",
                      "description": "Safety decision for the exact finding (the empty gate when no finding is active), emitted by the iac management-status handler from finding.SafetyGate.",
                      "properties": {
                        "outcome": {"type": "string", "description": "Safety outcome emitted by packet.ManagementSafetyGate.Outcome."},
                        "read_only": {"type": "boolean", "description": "Whether the finding is read-only, emitted by packet.ManagementSafetyGate.ReadOnly."},
                        "review_required": {"type": "boolean", "description": "Whether human review is required, emitted by packet.ManagementSafetyGate.ReviewRequired."},
                        "refused_actions": {"type": "array", "items": {"type": "string"}, "description": "Actions the gate refuses, emitted by packet.ManagementSafetyGate.RefusedActions."},
                        "warnings": {"type": "array", "items": {"type": "string"}, "description": "Gate warnings emitted by packet.ManagementSafetyGate.Warnings."},
                        "redactions": {"type": "array", "items": {"type": "string"}, "description": "Fields the gate redacted, emitted by packet.ManagementSafetyGate.Redactions."},
                        "audit_expectation": {"type": "string", "description": "Audit expectation set by the gate, emitted by packet.ManagementSafetyGate.AuditExpectation."}
                      },
                      "required": ["outcome", "read_only", "review_required", "audit_expectation"]
                    },
                    "total_findings_count": {"type": "integer", "description": "Total active findings for the exact selector (0 or 1), emitted by the iac management-status handler from loadExactIaCManagementFinding."},
                    "limitations": {"type": "array", "items": {"type": "string"}, "description": "Fixed limitation strings for this read, emitted by iac.managementStatusLimitations."}
                  }
                }
              }
            }
          },
          "400": {"$ref": "#/components/responses/BadRequest"},
          "403": {"$ref": "#/components/responses/Forbidden"},
          "501": {"$ref": "#/components/responses/NotImplemented"},
          "503": {"$ref": "#/components/responses/ServiceUnavailable"},
          "500": {"$ref": "#/components/responses/InternalError"}
        }
      }
    },
`

// ManagementExplanation documents the exact IaC management status
// explanation read. It is split from Routes to keep that file under the
// source size cap.
const ManagementExplanation = `
    "/api/v0/iac/management-status/explain": {
      "post": {
        "tags": ["iac"],
        "summary": "Explain IaC management status evidence",
        "description": "Explains one exact AWS IaC management status with grouped reducer evidence rows, redacted sensitive values, and the safety gate that applies before import-plan use.",
        "operationId": "explainIaCManagementStatus",
        "x-scoped-token-support": true,
        "requestBody": {
          "required": true,
          "content": {
            "application/json": {
              "schema": {
                "type": "object",
                "properties": {
                  "scope_id": {"type": "string"},
                  "account_id": {"type": "string"},
                  "region": {"type": "string"},
                  "arn": {"type": "string"},
                  "resource_id": {"type": "string"},
                  "finding_kinds": {"type": "array", "items": {"type": "string"}}
                },
                "anyOf": [
                  {"required": ["scope_id", "arn"]},
                  {"required": ["scope_id", "resource_id"]},
                  {"required": ["account_id", "arn"]},
                  {"required": ["account_id", "resource_id"]}
                ]
              }
            }
          }
        },
        "responses": {
          "200": {
            "description": "IaC management status explanation",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "story": {"type": "string", "description": "One-line read summary rendered by iac.managementStatusStory from the exact selector and the loaded finding."},
                    "arn": {"type": "string", "description": "Echo of the caller's exact ARN selector, emitted by the iac management-explanation handler from the normalized filter."},
                    "scope_id": {"type": "string", "description": "Echo of the caller's scope_id selector, emitted by the iac management-explanation handler from the normalized filter."},
                    "account_id": {"type": "string", "description": "Echo of the caller's account_id selector, emitted by the iac management-explanation handler from the normalized filter."},
                    "region": {"type": "string", "description": "Echo of the caller's region selector, emitted by the iac management-explanation handler from the normalized filter."},
                    "finding": {` + managementFindingShapeBody + `},
                    "evidence_groups": {
                      "type": "array",
                      "description": "Reducer evidence atoms grouped by evidence layer, emitted by iac.managementEvidenceGroups.",
                      "items": {
                        "type": "object",
                        "description": "One evidence layer group. Owner: iac.managementEvidenceGroup.",
                        "properties": {
                          "layer": {"type": "string", "description": "Evidence layer bucket emitted by iac.managementEvidenceGroup.Layer."},
                          "count": {"type": "integer", "description": "Atoms in this layer, emitted by iac.managementEvidenceGroup.Count."},
                          "evidence": {
                            "type": "array",
                            "description": "Evidence atoms in this layer with sensitive values redacted, emitted by iac.managementEvidenceGroup.Evidence via sanitizeIaCManagementEvidence.",
                            "items": {
                              "type": "object",
                              "description": "One evidence atom. Owner: iac.ManagementEvidenceRow.",
                              "properties": {
                                "id": {"type": "string", "description": "Evidence atom identity emitted by iac.ManagementEvidenceRow.ID."},
                                "source_system": {"type": "string", "description": "Source system of the atom, emitted by iac.ManagementEvidenceRow.SourceSystem."},
                                "evidence_type": {"type": "string", "description": "Atom type emitted by iac.ManagementEvidenceRow.EvidenceType."},
                                "scope_id": {"type": "string", "description": "Scope the atom's data came from, emitted by iac.ManagementEvidenceRow.ScopeID."},
                                "key": {"type": "string", "description": "Evidence key emitted by iac.ManagementEvidenceRow.Key."},
                                "value": {"type": "string", "description": "Evidence value with sensitive values redacted, emitted by iac.ManagementEvidenceRow.Value via sanitizeIaCManagementEvidence."},
                                "confidence": {"type": "number", "description": "Atom confidence emitted by iac.ManagementEvidenceRow.Confidence."},
                                "provenance_only": {"type": "boolean", "description": "Whether the atom is provenance-only, emitted by iac.ManagementEvidenceRow.ProvenanceOnly."}
                              },
                              "required": ["id", "source_system", "evidence_type", "scope_id", "key", "value", "confidence", "provenance_only"]
                            }
                          }
                        },
                        "required": ["layer", "count", "evidence"]
                      }
                    },
                    "safety_gate": {
                      "type": "object",
                      "description": "Safety decision for the exact finding (the empty gate when no finding is active), emitted by iac.managementSafetyGateForExactFinding.",
                      "properties": {
                        "outcome": {"type": "string", "description": "Safety outcome emitted by packet.ManagementSafetyGate.Outcome."},
                        "read_only": {"type": "boolean", "description": "Whether the finding is read-only, emitted by packet.ManagementSafetyGate.ReadOnly."},
                        "review_required": {"type": "boolean", "description": "Whether human review is required, emitted by packet.ManagementSafetyGate.ReviewRequired."},
                        "refused_actions": {"type": "array", "items": {"type": "string"}, "description": "Actions the gate refuses, emitted by packet.ManagementSafetyGate.RefusedActions."},
                        "warnings": {"type": "array", "items": {"type": "string"}, "description": "Gate warnings emitted by packet.ManagementSafetyGate.Warnings."},
                        "redactions": {"type": "array", "items": {"type": "string"}, "description": "Fields the gate redacted, emitted by packet.ManagementSafetyGate.Redactions."},
                        "audit_expectation": {"type": "string", "description": "Audit expectation set by the gate, emitted by packet.ManagementSafetyGate.AuditExpectation."}
                      },
                      "required": ["outcome", "read_only", "review_required", "audit_expectation"]
                    },
                    "total_findings_count": {"type": "integer", "description": "Total active findings for the exact selector (0 or 1), emitted by the iac management-explanation handler from loadExactIaCManagementFinding."},
                    "limitations": {"type": "array", "items": {"type": "string"}, "description": "Fixed limitation strings for this read, emitted by iac.managementStatusLimitations."}
                  }
                }
              }
            }
          },
          "400": {"$ref": "#/components/responses/BadRequest"},
          "403": {"$ref": "#/components/responses/Forbidden"},
          "501": {"$ref": "#/components/responses/NotImplemented"},
          "503": {"$ref": "#/components/responses/ServiceUnavailable"},
          "500": {"$ref": "#/components/responses/InternalError"}
        }
      }
    },
`
