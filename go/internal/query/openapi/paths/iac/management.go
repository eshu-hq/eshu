// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package iac

// ManagementStatus documents the exact IaC management status read. It is
// split from Routes to keep that file under the source size cap.
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
                    "finding": {"type": ["object", "null"], "description": "The exact active management finding as an iac.ManagementFindingRow (null when no active finding), loaded by iac.loadExactIaCManagementFinding."},
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
                    "finding": {"type": ["object", "null"], "description": "The exact active management finding as an iac.ManagementFindingRow (null when no active finding), loaded by iac.loadExactIaCManagementFinding."},
                    "evidence_groups": {
                      "type": "array",
                      "description": "Reducer evidence atoms grouped by evidence layer, emitted by iac.managementEvidenceGroups.",
                      "items": {
                        "type": "object",
                        "properties": {
                          "layer": {"type": "string", "description": "Evidence layer bucket emitted by iac.managementEvidenceGroup.Layer."},
                          "count": {"type": "integer", "description": "Atoms in this layer, emitted by iac.managementEvidenceGroup.Count."},
                          "evidence": {
                            "type": "array",
                            "description": "Evidence atoms in this layer with sensitive values redacted, emitted by iac.managementEvidenceGroup.Evidence via sanitizeIaCManagementEvidence.",
                            "items": {
                              "type": "object",
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
