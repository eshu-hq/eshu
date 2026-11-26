// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cloud

// AWSRuntimeDrift is the OpenAPI path fragment documenting the
// `/api/v0/aws/runtime-drift/findings` route. openapi.Spec concatenates it
// into the published document; keep it in lockstep with the handlers and
// docs/public/reference/http-api.md.
const AWSRuntimeDrift = `
    "/api/v0/aws/runtime-drift/findings": {
      "post": {
        "tags": ["aws"],
        "summary": "List AWS runtime drift findings",
        "description": "Lists active reducer-materialized AWS runtime drift findings for a bounded AWS scope or account. The response preserves evidence, missing/stale/ambiguous/unknown status, and rejected promotion state without exposing raw Cypher. Scoped tokens must supply an exact scope_id that resolves to a granted repository or ingestion scope; an account_id-only request (which fans out across every region/service scope under that account) or an out-of-grant scope_id receives an empty page.",
        "operationId": "listAWSRuntimeDriftFindings",
        "x-scoped-token-support": true,
        "requestBody": {
          "required": true,
          "content": {
            "application/json": {
              "schema": {
                "type": "object",
                "properties": {
                  "scope_id": {"type": "string", "description": "Exact AWS collector scope, for example aws:123456789012:us-east-1:lambda."},
                  "account_id": {"type": "string", "description": "AWS account ID used to bound the active finding read."},
                  "region": {"type": "string", "description": "Optional AWS region when account_id is supplied."},
                  "arn": {"type": "string", "description": "Optional exact AWS ARN to inspect."},
                  "finding_kinds": {
                    "type": "array",
                    "description": "Optional finding kinds: orphaned_cloud_resource, unmanaged_cloud_resource, unknown_cloud_resource, ambiguous_cloud_resource, image_version_drift, or value_comparison_inconclusive.",
                    "items": {"type": "string"}
                  },
                  "limit": {"type": "integer", "description": "Maximum findings to return (default 100, max 500).", "default": 100},
                  "offset": {"type": "integer", "description": "Zero-based result offset for paging findings.", "default": 0}
                },
                "anyOf": [
                  {"required": ["scope_id"]},
                  {"required": ["account_id"]}
                ]
              }
            }
          }
        },
        "responses": {
          "200": {
            "description": "AWS runtime drift findings",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "scope_id": {"type": "string", "description": "Echo of the caller's scope_id filter, emitted by iac.writeAWSRuntimeDriftFindings from ManagementFilter.ScopeID."},
                    "account_id": {"type": "string", "description": "Echo of the caller's account_id filter, emitted by iac.writeAWSRuntimeDriftFindings from ManagementFilter.AccountID."},
                    "region": {"type": "string", "description": "Echo of the caller's region filter, emitted by iac.writeAWSRuntimeDriftFindings from ManagementFilter.Region."},
                    "arn": {"type": "string", "description": "Echo of the caller's arn filter, emitted by iac.writeAWSRuntimeDriftFindings from ManagementFilter.ARN."},
                    "story": {"type": "string", "description": "One-line page summary rendered by iac.awsRuntimeDriftFindingsStory from the caller's filter and the page counts."},
                    "finding_kinds": {"type": "array", "items": {"type": "string"}, "description": "Echo of the caller's normalized finding-kind filter, emitted by iac.writeAWSRuntimeDriftFindings from ManagementFilter.FindingKinds."},
                    "outcome_groups": {
                      "type": "array",
                      "description": "Finding counts grouped by derived outcome, emitted by iac.awsRuntimeDriftOutcomeGroups.",
                      "items": {
                        "type": "object",
                      "description": "One outcome group. Owner: iac.awsRuntimeDriftOutcomeGroup.",
                        "properties": {
                          "outcome": {"type": "string", "description": "Derived outcome bucket emitted by iac.awsRuntimeDriftOutcomeGroup.Outcome (exact, derived, ambiguous, stale, or unknown)."},
                          "count": {"type": "integer", "description": "Number of page findings in this outcome bucket, emitted by iac.awsRuntimeDriftOutcomeGroup.Count."},
                          "arns": {"type": "array", "items": {"type": "string"}, "description": "Sorted ARNs of the page findings in this outcome bucket, emitted by iac.awsRuntimeDriftOutcomeGroup.ARNs."}
                        },
                        "required": ["outcome", "count"]
                      }
                    },
                    "findings_count": {"type": "integer", "description": "Number of findings in this page, emitted by iac.writeAWSRuntimeDriftFindings as len(driftFindings)."},
                    "total_findings_count": {"type": "integer", "description": "Total active findings matching the filter, emitted by iac.writeAWSRuntimeDriftFindings from the store count."},
                    "limit": {"type": "integer", "description": "Echo of the caller's page size, emitted by iac.writeAWSRuntimeDriftFindings from ManagementFilter.Limit."},
                    "offset": {"type": "integer", "description": "Echo of the caller's result offset, emitted by iac.writeAWSRuntimeDriftFindings from ManagementFilter.Offset."},
                    "truncated": {"type": "boolean", "description": "True when more findings exist past this page, emitted by iac.ManagementTruncated."},
                    "next_offset": {"type": ["integer", "null"], "description": "Offset to fetch the next page (0 when not truncated), emitted by iac.ManagementNextOffset."},
                    "truth_basis": {"type": "string", "description": "Truth-basis label for this readback, emitted by iac.writeAWSRuntimeDriftFindings (always materialized_reducer_rows)."},
                    "analysis_status": {"type": "string", "description": "Read-model status label for this readback, emitted by iac.writeAWSRuntimeDriftFindings (always materialized_aws_runtime_drift)."},
                    "graph_projection_note": {"type": "string", "description": "Read-model projection note emitted by iac.writeAWSRuntimeDriftFindings: this surface reads the reducer read model and the graph projection stays gated."},
                    "limitations": {"type": "array", "items": {"type": "string"}, "description": "Fixed limitation strings for this readback, emitted by iac.writeAWSRuntimeDriftFindings."},
                    "drift_findings": {
                      "type": "array",
                      "description": "Page of AWS runtime drift findings, emitted by iac.awsRuntimeDriftFindingRows.",
                      "items": {
                        "type": "object",
                        "description": "One AWS runtime drift finding. Owner: iac.awsRuntimeDriftFindingRows.",
                        "properties": {
                        "id": {"type": "string", "description": "Finding row identity emitted by iac.ManagementFindingRow.ID."},
                          "arn": {"type": "string", "description": "AWS ARN under review, emitted by iac.ManagementFindingRow.ARN."},
                          "provider": {"type": "string", "description": "Cloud provider of the finding, emitted by iac.ManagementFindingRow.Provider."},
                          "account_id": {"type": "string", "description": "AWS account owning the resource, emitted by iac.ManagementFindingRow.AccountID."},
                          "region": {"type": "string", "description": "AWS region of the resource, emitted by iac.ManagementFindingRow.Region."},
                          "finding_kind": {"type": "string", "description": "Drift finding kind emitted by iac.ManagementFindingRow.FindingKind."},
                          "management_status": {"type": "string", "description": "IaC management status of the resource, emitted by iac.ManagementFindingRow.ManagementStatus."},
                          "outcome": {"type": "string", "enum": ["exact", "derived", "ambiguous", "stale", "unknown"], "description": "Outcome derived from management status and evidence strength, emitted by iac.awsRuntimeDriftOutcome via AWSRuntimeDriftFindingRow.Outcome."},
                          "promotion_outcome": {"type": "string", "enum": ["not_promoted", "rejected"], "description": "Ownership-promotion verdict emitted by iac.awsRuntimeDriftPromotionOutcome via AWSRuntimeDriftFindingRow.PromotionOutcome."},
                          "promotion_reason": {"type": "string", "description": "Machine-readable promotion reason emitted by iac.awsRuntimeDriftPromotionReason via AWSRuntimeDriftFindingRow.PromotionReason."},
                          "confidence": {"type": "number", "description": "Finding confidence emitted by iac.ManagementFindingRow.Confidence."},
                          "scope_id": {"type": "string", "description": "Canonical ingestion scope of the finding, emitted by iac.ManagementFindingRow.ScopeID."},
                          "generation_id": {"type": "string", "description": "Reducer generation that produced the finding, emitted by iac.ManagementFindingRow.GenerationID."},
                          "source_system": {"type": "string", "description": "Source system that admitted the finding, emitted by iac.ManagementFindingRow.SourceSystem."},
                          "service_candidates": {"type": "array", "items": {"type": "string"}, "description": "Candidate owning services emitted by iac.ManagementFindingRow.ServiceCandidates."},
                          "environment_candidates": {"type": "array", "items": {"type": "string"}, "description": "Candidate environments emitted by iac.ManagementFindingRow.EnvironmentCandidates."},
                          "dependency_paths": {"type": "array", "items": {"type": "string"}, "description": "Dependency paths attached to the finding, emitted by iac.ManagementFindingRow.DependencyPaths."},
                          "missing_evidence": {"type": "array", "items": {"type": "string"}, "description": "Evidence the read could not produce, emitted by iac.ManagementFindingRow.MissingEvidence."},
                          "warning_flags": {"type": "array", "items": {"type": "string"}, "description": "Non-fatal warning flags for the finding, emitted by iac.ManagementFindingRow.WarningFlags."},
                          "recommended_action": {"type": "string", "description": "Read-only recommended next action, emitted by iac.ManagementFindingRow.RecommendedAction."},
                          "safety_gate": {
                            "type": "object",
                            "description": "Safety decision before a caller turns this read-only finding into a task, emitted by iac.ManagementFindingRow.SafetyGate via iacManagementSafetyGate.",
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
                          "evidence": {
                            "type": "array",
                            "description": "Evidence atoms explaining the finding, emitted by iac.ManagementFindingRow.Evidence.",
                            "items": {
                              "type": "object",
                            "description": "One evidence atom. Owner: iac.ManagementEvidenceRow.",
                            "properties": {
                              "id": {"type": "string", "description": "Evidence atom identity emitted by iac.ManagementEvidenceRow.ID."},
                                "source_system": {"type": "string", "description": "Source system of the atom, emitted by iac.ManagementEvidenceRow.SourceSystem."},
                                "evidence_type": {"type": "string", "description": "Atom type emitted by iac.ManagementEvidenceRow.EvidenceType."},
                                "scope_id": {"type": "string", "description": "Scope the atom's data came from, emitted by iac.ManagementEvidenceRow.ScopeID."},
                                "key": {"type": "string", "description": "Evidence key emitted by iac.ManagementEvidenceRow.Key."},
                                "value": {"type": "string", "description": "Evidence value emitted by iac.ManagementEvidenceRow.Value."},
                                "confidence": {"type": "number", "description": "Atom confidence emitted by iac.ManagementEvidenceRow.Confidence."},
                                "provenance_only": {"type": "boolean", "description": "Whether the atom is provenance-only, emitted by iac.ManagementEvidenceRow.ProvenanceOnly."}
                              },
                              "required": ["id", "source_system", "evidence_type", "scope_id", "key", "value", "confidence", "provenance_only"]
                            }
                          },
                          "drifted_attributes": {
                            "type": "array",
                            "description": "Bounded declared/observed value pairs for an image_version_drift finding (ami, image_uri, version, or the ECS container image comparison), one pair per comparison that ran and disagreed. Empty for every other kind, including value_comparison_inconclusive, which reports that this pass cannot speak for the covered values -- either no comparison could be made at all, or the ones that ran agreed while another covered comparable was unreadable. Both kinds name any comparable they could not READ in missing_evidence as comparable_attribute:<key>, so an image_version_drift carrying one is a verdict reached on partial evidence. Emitted by iac.ManagementFindingRow.DriftedAttributes.",
                            "items": {
                              "type": "object",
                            "description": "One declared/observed value pair. Owner: querycontract.DriftedAttributeView.",
                            "properties": {
                              "attribute": {"type": "string", "description": "Allowlisted comparable attribute name emitted by querycontract.DriftedAttributeView.Attribute."},
                                "declared_value": {"type": "string", "description": "Terraform-state value emitted by querycontract.DriftedAttributeView.Declared."},
                                "observed_value": {"type": "string", "description": "AWS-observed cloud value emitted by querycontract.DriftedAttributeView.Observed."}
                              },
                              "required": ["attribute", "declared_value", "observed_value"]
                            }
                          }
                        },
                        "required": ["id", "finding_kind", "management_status", "confidence", "safety_gate", "evidence"]
                      }
                    }
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
