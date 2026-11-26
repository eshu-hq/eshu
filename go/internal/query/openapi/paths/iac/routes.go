// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package iac

// Routes is the OpenAPI path fragment documenting the `/api/v0/iac/*`
// routes. openapi.Spec concatenates it into the published document; keep it in
// lockstep with the handlers and docs/public/reference/http-api.md.
const Routes = `
    "/api/v0/iac/dead": {
      "post": {
        "tags": ["iac"],
        "summary": "Find dead IaC candidates",
        "description": "Finds bounded, content-derived dead-IaC candidates for explicit repository scopes. Dynamic references are returned as ambiguous until reducer-materialized usage rows make the result exact.",
        "operationId": "findDeadIaC",
        "x-scoped-token-support": true,
        "requestBody": {
          "required": true,
          "content": {
            "application/json": {
              "schema": {
                "type": "object",
                "properties": {
                  "repo_id": {"type": "string", "description": "Single repository ID to analyze."},
                  "repo_ids": {
                    "type": "array",
                    "description": "Explicit bounded repository scope to analyze.",
                    "items": {"type": "string"}
                  },
                  "families": {
                    "type": "array",
                    "description": "Optional IaC families to include: terraform, helm, kustomize, ansible, compose.",
                    "items": {"type": "string"}
                  },
                  "include_ambiguous": {"type": "boolean", "description": "Include dynamic-reference candidates.", "default": false},
                  "limit": {"type": "integer", "description": "Maximum findings to return (default 100, max 500).", "default": 100},
                  "offset": {"type": "integer", "description": "Zero-based result offset for paging findings.", "default": 0}
                }
              }
            }
          }
        },
        "responses": {
          "200": {
            "description": "Dead-IaC candidate findings",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "repo_ids": {"type": "array", "items": {"type": "string"}, "description": "Echo of the bounded repository scope analyzed, emitted by the iac dead-content handler from the caller request."},
                    "findings_count": {"type": "integer", "description": "Number of findings in this page, emitted by the iac dead-content handler as len(findings)."},
                    "total_findings_count": {"type": "integer", "description": "Total findings matching the scope, emitted by the iac dead-content handler from the store count (content-derived count for the content path)."},
                    "limit": {"type": "integer", "description": "Echo of the caller's page size, emitted by the iac dead-content handler from deadIaCRequest.Limit."},
                    "offset": {"type": "integer", "description": "Echo of the caller's result offset, emitted by the iac dead-content handler from deadIaCRequest.Offset."},
                    "truncated": {"type": "boolean", "description": "True when more findings exist past this page, emitted by iac.deadIaCTruncated."},
                    "next_offset": {"type": ["integer", "null"], "description": "Offset to fetch the next page (0 when not truncated), emitted by iac.deadIaCNextOffset."},
                    "truth_basis": {"type": "string", "description": "Truth-basis label for this readback, emitted by the iac dead-content handler (materialized_reducer_rows for the reachability path, content_scope for the content path)."},
                    "analysis_status": {"type": "string", "description": "Read-model status label for this readback, emitted by the iac dead-content handler (materialized_reachability or derived_candidate_analysis)."},
                    "limitations": {"type": "array", "items": {"type": "string"}, "description": "Fixed limitation strings for this readback, emitted by the iac dead-content handler."},
                    "findings": {
                      "type": "array",
                      "description": "Page of dead-IaC candidate findings, emitted by iac.writeMaterializedDeadIaC or the content fallback from analyzeDeadIaC as []iac.deadIaCFinding.",
                      "items": {
                        "type": "object",
                        "properties": {
                          "id": {"type": "string", "description": "Finding identity emitted by iac.deadIaCFinding.ID."},
                          "family": {"type": "string", "description": "IaC family of the candidate (terraform, helm, kustomize, ansible, compose), emitted by iac.deadIaCFinding.Family."},
                          "repo_id": {"type": "string", "description": "Repository under analysis, emitted by iac.deadIaCFinding.RepoID."},
                          "repo_name": {"type": "string", "description": "Human-readable repository name resolved by iac.enrichDeadIaCRepoNames, emitted by iac.deadIaCFinding.RepoName (omitted when unresolvable)."},
                          "artifact": {"type": "string", "description": "IaC artifact path or address of the candidate, emitted by iac.deadIaCFinding.Artifact."},
                          "reachability": {"type": "string", "enum": ["unused", "ambiguous"], "description": "Reachability verdict emitted by iac.deadIaCFinding.Reachability: unused is content-proved dead, ambiguous needs reducer-materialized usage rows."},
                          "finding": {"type": "string", "description": "Finding statement emitted by iac.deadIaCFinding.Finding."},
                          "confidence": {"type": "number", "description": "Finding confidence emitted by iac.deadIaCFinding.Confidence."},
                          "evidence": {"type": "array", "items": {"type": "string"}, "description": "Content-derived evidence strings for the candidate, emitted by iac.deadIaCFinding.Evidence."},
                          "limitations": {"type": "array", "items": {"type": "string"}, "description": "Per-finding limitation strings, emitted by iac.deadIaCFinding.Limitations (omitted when empty)."}
                        },
                        "required": ["id", "family", "repo_id", "artifact", "reachability", "finding", "confidence", "evidence"]
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
    "/api/v0/iac/unmanaged-resources": {
      "post": {
        "tags": ["iac"],
        "summary": "Find unmanaged cloud resources",
        "description": "Finds AWS cloud resources whose active reducer drift facts show no Terraform config owner or only Terraform state ownership. Requests must be bounded by scope_id or account_id. Responses include safety gates, redacted sensitive evidence values, and refused import-plan actions for resources requiring security review.",
        "operationId": "findUnmanagedResources",
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
                    "description": "Optional finding kinds: orphaned_cloud_resource, unmanaged_cloud_resource, unknown_cloud_resource, or ambiguous_cloud_resource.",
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
            "description": "Unmanaged cloud resource findings",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "scope_id": {"type": "string", "description": "Echo of the caller's scope_id filter, emitted by the iac unmanaged-resources handler from ManagementFilter.ScopeID."},
                    "account_id": {"type": "string", "description": "Echo of the caller's account_id filter, emitted by the iac unmanaged-resources handler from ManagementFilter.AccountID."},
                    "region": {"type": "string", "description": "Echo of the caller's region filter, emitted by the iac unmanaged-resources handler from ManagementFilter.Region."},
                    "arn": {"type": "string", "description": "Echo of the caller's arn filter, emitted by the iac unmanaged-resources handler from ManagementFilter.ARN."},
                    "story": {"type": "string", "description": "One-line page summary rendered by iac.managementListStory from the caller's filter and the page counts."},
                    "finding_groups": {
                      "type": "array",
                      "description": "Finding counts grouped by management status and finding kind, emitted by iac.managementFindingGroups.",
                      "items": {
                        "type": "object",
                        "properties": {
                          "management_status": {"type": "string", "description": "Management-status bucket emitted by iac.managementFindingGroup.ManagementStatus."},
                          "finding_kind": {"type": "string", "description": "Finding-kind bucket emitted by iac.managementFindingGroup.FindingKind."},
                          "count": {"type": "integer", "description": "Number of page findings in this group, emitted by iac.managementFindingGroup.Count."},
                          "arns": {"type": "array", "items": {"type": "string"}, "description": "Sorted ARNs of the page findings in this group, emitted by iac.managementFindingGroup.ARNs."}
                        },
                        "required": ["management_status", "finding_kind", "count"]
                      }
                    },
                    "safety_summary": {
                      "type": "object",
                      "description": "Page-wide safety rollup, emitted by iac.managementSafetySummary.",
                      "properties": {
                        "total_findings": {"type": "integer", "description": "Page finding count summarized by iac.managementSafetySummaryRow.TotalFindings."},
                        "review_required_count": {"type": "integer", "description": "Findings requiring security review, counted by iac.managementSafetySummaryRow.ReviewRequiredCount."},
                        "redacted_findings_count": {"type": "integer", "description": "Findings with redacted sensitive evidence values, counted by iac.managementSafetySummaryRow.RedactedFindingsCount."},
                        "refused_actions": {"type": "array", "items": {"type": "string"}, "description": "Distinct refused actions across the page, collected by iac.managementSafetySummaryRow.RefusedActions (omitted when empty)."}
                      },
                      "required": ["total_findings", "review_required_count", "redacted_findings_count"]
                    },
                    "finding_kinds": {"type": "array", "items": {"type": "string"}, "description": "Echo of the caller's normalized finding-kind filter, emitted by the iac unmanaged-resources handler from ManagementFilter.FindingKinds."},
                    "findings_count": {"type": "integer", "description": "Number of findings in this page, emitted by the iac unmanaged-resources handler as len(findings)."},
                    "total_findings_count": {"type": "integer", "description": "Total active findings matching the filter, emitted by the iac unmanaged-resources handler from the store count."},
                    "limit": {"type": "integer", "description": "Echo of the caller's page size, emitted by the iac unmanaged-resources handler from ManagementFilter.Limit."},
                    "offset": {"type": "integer", "description": "Echo of the caller's result offset, emitted by the iac unmanaged-resources handler from ManagementFilter.Offset."},
                    "truncated": {"type": "boolean", "description": "True when more findings exist past this page, emitted by iac.ManagementTruncated."},
                    "next_offset": {"type": ["integer", "null"], "description": "Offset to fetch the next page (0 when not truncated), emitted by iac.ManagementNextOffset."},
                    "truth_basis": {"type": "string", "description": "Truth-basis label for this readback, emitted by the iac unmanaged-resources handler (always materialized_reducer_rows)."},
                    "analysis_status": {"type": "string", "description": "Read-model status label for this readback, emitted by the iac unmanaged-resources handler (always materialized_aws_runtime_drift)."},
                    "graph_projection_note": {"type": "string", "description": "Read-model projection note emitted by the iac unmanaged-resources handler: this surface reads the reducer read model and the graph projection stays gated."},
                    "limitations": {"type": "array", "items": {"type": "string"}, "description": "Fixed limitation strings for this readback, emitted by the iac unmanaged-resources handler."},
                    "findings": {
                      "type": "array",
                      "description": "Page of unmanaged-resource management findings, emitted by the iac unmanaged-resources handler from normalizeIaCManagementFindingsSafety as []iac.ManagementFindingRow.",
                      "items": {
                        "type": "object",
                        "properties": {
                          "id": {"type": "string", "description": "Finding row identity emitted by iac.ManagementFindingRow.ID."},
                          "provider": {"type": "string", "description": "Cloud provider of the finding, emitted by iac.ManagementFindingRow.Provider."},
                          "account_id": {"type": "string", "description": "AWS account owning the resource, emitted by iac.ManagementFindingRow.AccountID."},
                          "region": {"type": "string", "description": "AWS region of the resource, emitted by iac.ManagementFindingRow.Region."},
                          "resource_type": {"type": "string", "description": "Provider resource type, emitted by iac.ManagementFindingRow.ResourceType."},
                          "resource_id": {"type": "string", "description": "Provider resource identity, emitted by iac.ManagementFindingRow.ResourceID."},
                          "arn": {"type": "string", "description": "AWS ARN under review, emitted by iac.ManagementFindingRow.ARN."},
                          "tags": {"type": "object", "additionalProperties": {"type": "string"}, "description": "Provenance-only tag map emitted by iac.ManagementFindingRow.Tags. Keys are the reducer-admitted tag keys and values are the tag values (sensitive values redacted); tags never infer environment or ownership truth."},
                          "finding_kind": {"type": "string", "description": "Drift finding kind emitted by iac.ManagementFindingRow.FindingKind."},
                          "management_status": {
                            "type": "string",
                            "enum": [
                              "managed_by_terraform",
                              "terraform_state_only",
                              "terraform_config_only",
                              "cloud_only",
                              "managed_by_other_iac",
                              "ambiguous_management",
                              "unknown_management",
                              "stale_iac_candidate"
                            ],
                            "description": "IaC management status of the resource, emitted by iac.ManagementFindingRow.ManagementStatus."
                          },
                          "confidence": {"type": "number", "description": "Finding confidence emitted by iac.ManagementFindingRow.Confidence."},
                          "scope_id": {"type": "string", "description": "Canonical ingestion scope of the finding, emitted by iac.ManagementFindingRow.ScopeID."},
                          "generation_id": {"type": "string", "description": "Reducer generation that produced the finding, emitted by iac.ManagementFindingRow.GenerationID."},
                          "source_system": {"type": "string", "description": "Source system that admitted the finding, emitted by iac.ManagementFindingRow.SourceSystem."},
                          "candidate_id": {"type": "string", "description": "Ownership candidate identity when one is tracked, emitted by iac.ManagementFindingRow.CandidateID."},
                          "matched_terraform_state_address": {"type": "string", "description": "Matched Terraform state address, emitted by iac.ManagementFindingRow.MatchedTerraformStateAddress."},
                          "matched_terraform_config_file": {"type": "string", "description": "Matched Terraform config file, emitted by iac.ManagementFindingRow.MatchedTerraformConfigFile."},
                          "matched_terraform_module_path": {"type": "string", "description": "Matched Terraform module path, emitted by iac.ManagementFindingRow.MatchedTerraformModulePath."},
                          "matched_other_iac_source": {"type": "string", "description": "Matched non-Terraform IaC source, emitted by iac.ManagementFindingRow.MatchedOtherIaCSource."},
                          "service_candidates": {"type": "array", "items": {"type": "string"}, "description": "Candidate owning services emitted by iac.ManagementFindingRow.ServiceCandidates."},
                          "environment_candidates": {"type": "array", "items": {"type": "string"}, "description": "Candidate environments emitted by iac.ManagementFindingRow.EnvironmentCandidates."},
                          "dependency_paths": {"type": "array", "items": {"type": "string"}, "description": "Dependency paths attached to the finding, emitted by iac.ManagementFindingRow.DependencyPaths."},
                          "recommended_action": {"type": "string", "description": "Read-only recommended next action, emitted by iac.ManagementFindingRow.RecommendedAction."},
                          "missing_evidence": {"type": "array", "items": {"type": "string"}, "description": "Evidence the read could not produce, emitted by iac.ManagementFindingRow.MissingEvidence."},
                          "warning_flags": {"type": "array", "items": {"type": "string"}, "description": "Non-fatal warning flags for the finding, emitted by iac.ManagementFindingRow.WarningFlags."},
                          "safety_gate": {
                            "type": "object",
                            "description": "Safety decision before a caller turns this read-only finding into a task, emitted by iac.ManagementFindingRow.SafetyGate via normalizeIaCManagementFindingsSafety.",
                            "properties": {
                              "outcome": {"type": "string", "enum": ["read_only_allowed", "security_review_required"], "description": "Safety outcome emitted by packet.ManagementSafetyGate.Outcome on this surface."},
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
                            "description": "Evidence atoms explaining the finding with sensitive values redacted, emitted by iac.ManagementFindingRow.Evidence via sanitizeIaCManagementEvidence.",
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
                                "provenance_only": {"type": "boolean", "description": "Whether the atom is provenance-only (true for raw tag atoms), emitted by iac.ManagementEvidenceRow.ProvenanceOnly."}
                              },
                              "required": ["id", "source_system", "evidence_type", "scope_id", "key", "value", "confidence", "provenance_only"]
                            }
                          },
                          "drifted_attributes": {
                            "type": "array",
                            "description": "Bounded declared/observed value pairs for an image_version_drift finding, paired by iac.DriftedAttributesFromAWSEvidence and emitted by iac.ManagementFindingRow.DriftedAttributes (omitted for every other kind).",
                            "items": {
                              "type": "object",
                              "properties": {
                                "attribute": {"type": "string", "description": "Allowlisted comparable attribute name emitted by querycontract.DriftedAttributeView.Attribute."},
                                "declared_value": {"type": "string", "description": "Terraform-state value emitted by querycontract.DriftedAttributeView.Declared."},
                                "observed_value": {"type": "string", "description": "AWS-observed cloud value emitted by querycontract.DriftedAttributeView.Observed."}
                              },
                              "required": ["attribute", "declared_value", "observed_value"]
                            }
                          }
                        },
                        "required": ["id", "provider", "finding_kind", "management_status", "confidence", "scope_id", "generation_id", "source_system", "recommended_action", "safety_gate", "evidence"]
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
    "/api/v0/iac/terraform-import-plan/candidates": {
      "post": {
        "tags": ["iac"],
        "summary": "Propose Terraform import-plan candidates",
        "description": "Generates read-only Terraform import-plan candidates from active AWS IaC management findings. The route never runs Terraform, imports resources, or mutates cloud state. Safety-gated, ambiguous, unknown, stale, state-only, and unsupported findings are returned as refused candidates with reasons.",
        "operationId": "proposeTerraformImportPlanCandidates",
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
                  "resource_id": {"type": "string", "description": "Optional alias for arn; for AWS this must be the full ARN, not a provider-local ID such as an S3 bucket name or Lambda function name."},
                  "finding_kinds": {
                    "type": "array",
                    "description": "Optional finding kinds: orphaned_cloud_resource, unmanaged_cloud_resource, unknown_cloud_resource, or ambiguous_cloud_resource.",
                    "items": {"type": "string"}
                  },
                  "limit": {"type": "integer", "description": "Maximum findings to inspect (default 100, max 500).", "default": 100},
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
            "description": "Terraform import-plan candidates",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "story": {"type": "string", "description": "One-line page summary rendered by iac.terraformImportPlanStory from the caller's filter and the candidate counts."},
                    "scope_id": {"type": "string", "description": "Echo of the caller's scope_id filter, emitted by iac.handleTerraformImportPlanCandidates from ManagementFilter.ScopeID."},
                    "account_id": {"type": "string", "description": "Echo of the caller's account_id filter, emitted by iac.handleTerraformImportPlanCandidates from ManagementFilter.AccountID."},
                    "region": {"type": "string", "description": "Echo of the caller's region filter, emitted by iac.handleTerraformImportPlanCandidates from ManagementFilter.Region."},
                    "arn": {"type": "string", "description": "Echo of the caller's arn filter, emitted by iac.handleTerraformImportPlanCandidates from ManagementFilter.ARN."},
                    "finding_kinds": {"type": "array", "items": {"type": "string"}, "description": "Echo of the caller's normalized finding-kind filter, emitted by iac.handleTerraformImportPlanCandidates from ManagementFilter.FindingKinds."},
                    "candidates_count": {"type": "integer", "description": "Number of candidates in this page, emitted by iac.handleTerraformImportPlanCandidates as len(summary.Candidates)."},
                    "ready_count": {"type": "integer", "description": "Candidates with a ready import block, counted by iac.terraformImportPlanSummary.Ready."},
                    "refused_count": {"type": "integer", "description": "Candidates refused with reasons, counted by iac.terraformImportPlanSummary.Refused."},
                    "total_findings_count": {"type": "integer", "description": "Total active findings inspected for this page, emitted by iac.handleTerraformImportPlanCandidates from the store count."},
                    "limit": {"type": "integer", "description": "Echo of the caller's page size, emitted by iac.handleTerraformImportPlanCandidates from ManagementFilter.Limit."},
                    "offset": {"type": "integer", "description": "Echo of the caller's result offset, emitted by iac.handleTerraformImportPlanCandidates from ManagementFilter.Offset."},
                    "truncated": {"type": "boolean", "description": "True when more findings exist past this page, emitted by iac.ManagementTruncated."},
                    "next_offset": {"type": ["integer", "null"], "description": "Offset to fetch the next page (0 when not truncated), emitted by iac.ManagementNextOffset."},
                    "truth_basis": {"type": "string", "description": "Truth-basis label for this readback, emitted by iac.handleTerraformImportPlanCandidates (always materialized_reducer_rows)."},
                    "analysis_status": {"type": "string", "description": "Read-model status label for this readback, emitted by iac.handleTerraformImportPlanCandidates (always terraform_import_plan_candidates)."},
                    "limitations": {"type": "array", "items": {"type": "string"}, "description": "Fixed limitation strings for this readback, emitted by iac.handleTerraformImportPlanCandidates."},
                    "terraform_import_plan": {
                      "type": "object",
                      "description": "Concatenated ready import blocks, emitted by iac.terraformImportPlanSummary.Artifact via buildTerraformImportPlanCandidates.",
                      "properties": {
                        "format": {"type": "string", "description": "Artifact format emitted by iac.terraformImportPlanArtifact.Format (always terraform_import_blocks)."},
                        "hcl": {"type": "string", "description": "Concatenated ready import blocks emitted by iac.terraformImportPlanArtifact.HCL (empty when no candidate is ready)."}
                      },
                      "required": ["format", "hcl"]
                    },
                    "candidates": {
                      "type": "array",
                      "description": "Page of import-plan candidates, emitted by iac.buildTerraformImportPlanCandidates as []iac.terraformImportPlanCandidate.",
                      "items": {
                        "type": "object",
                        "properties": {
                          "id": {"type": "string", "description": "Candidate identity emitted by iac.terraformImportPlanCandidate.ID."},
                          "finding_id": {"type": "string", "description": "Source finding identity emitted by iac.terraformImportPlanCandidate.FindingID."},
                          "status": {"type": "string", "enum": ["ready", "refused"], "description": "Candidate verdict emitted by iac.terraformImportPlanCandidate.Status: ready carries an import block, refused carries refusal reasons."},
                          "provider": {"type": "string", "description": "Cloud provider emitted by iac.terraformImportPlanCandidate.Provider."},
                          "account_id": {"type": "string", "description": "AWS account of the candidate, emitted by iac.terraformImportPlanCandidate.AccountID."},
                          "region": {"type": "string", "description": "AWS region of the candidate, emitted by iac.terraformImportPlanCandidate.Region."},
                          "arn": {"type": "string", "description": "AWS ARN of the candidate, emitted by iac.terraformImportPlanCandidate.ARN."},
                          "cloud_resource_type": {"type": "string", "description": "Provider resource type emitted by iac.terraformImportPlanCandidate.CloudResourceType."},
                          "terraform_resource_type": {"type": "string", "description": "Terraform resource type for the import, emitted by iac.terraformImportPlanCandidate.TerraformResourceType."},
                          "import_id": {"type": "string", "description": "Terraform import ID emitted by iac.terraformImportPlanCandidate.ImportID."},
                          "suggested_resource_address": {"type": "string", "description": "Suggested Terraform resource address emitted by iac.terraformImportPlanCandidate.SuggestedResourceAddress."},
                          "destination_hint": {"type": "string", "description": "Human-readable destination hint emitted by iac.terraformImportPlanCandidate.DestinationHint."},
                          "configuration_shape": {"type": "string", "description": "Configuration shape label emitted by iac.terraformImportPlanCandidate.ConfigurationShape."},
                          "provider_hint": {
                            "type": "object",
                            "description": "Provider block hint for the import, emitted by iac.terraformImportPlanCandidate.ProviderHint.",
                            "properties": {
                              "provider": {"type": "string", "description": "Provider name emitted by iac.terraformProviderHint.Provider."},
                              "account_id": {"type": "string", "description": "Provider account emitted by iac.terraformProviderHint.AccountID."},
                              "region": {"type": "string", "description": "Provider region emitted by iac.terraformProviderHint.Region."},
                              "alias": {"type": "string", "description": "Provider alias emitted by iac.terraformProviderHint.Alias."}
                            },
                            "required": ["provider"]
                          },
                          "warnings": {"type": "array", "items": {"type": "string"}, "description": "Non-fatal candidate warnings emitted by iac.terraformImportPlanCandidate.Warnings."},
                          "refusal_reasons": {"type": "array", "items": {"type": "string"}, "description": "Machine-readable refusal reasons on refused candidates, emitted by iac.terraformImportPlanCandidate.RefusalReasons."},
                          "import_block": {"type": "string", "description": "Ready import block HCL emitted by iac.terraformImportPlanCandidate.ImportBlock (omitted on refused candidates)."},
                          "config_shape_hint": {
                            "type": "object",
                            "description": "Read-only structural skeleton for a ready candidate: argument NAMES and placeholders only, never values. Absent on refused candidates. Emitted by iac.terraformImportPlanCandidate.ConfigShapeHint.",
                            "properties": {
                              "format": {"type": "string", "description": "Hint format emitted by iac.terraformConfigShapeHint.Format (a structural skeleton, never generated config)."},
                              "resource_address": {"type": "string", "description": "Resource address mirrored from the candidate, emitted by iac.terraformConfigShapeHint.ResourceAddress."},
                              "provider_alias": {"type": "string", "description": "Provider alias mirrored from the candidate hint, emitted by iac.terraformConfigShapeHint.ProviderAlias."},
                              "required_arguments": {"type": "array", "items": {"type": "string"}, "description": "Required argument NAMES for the resource type (operator supplies every value), emitted by iac.terraformConfigShapeHint.RequiredArguments."},
                              "notable_optional_arguments": {"type": "array", "items": {"type": "string"}, "description": "Commonly authored optional argument names (never populated from finding data), emitted by iac.terraformConfigShapeHint.NotableOptionalArguments."},
                              "omitted_sensitive_arguments": {"type": "array", "items": {"type": "string"}, "description": "Argument names left for manual out-of-band authoring (sensitive, data-plane, or policy), emitted by iac.terraformConfigShapeHint.OmittedSensitiveArguments."},
                              "hcl_skeleton": {"type": "string", "description": "HCL skeleton with placeholders only, emitted by iac.terraformConfigShapeHint.HCLSkeleton."},
                              "manual_fill_warnings": {"type": "array", "items": {"type": "string"}, "description": "Warnings about values the operator must fill manually, emitted by iac.terraformConfigShapeHint.ManualFillWarnings."}
                            },
                            "required": ["format", "resource_address", "required_arguments", "hcl_skeleton", "manual_fill_warnings"]
                          },
                          "evidence_refs": {"type": "array", "items": {"type": "string"}, "description": "Evidence references backing the candidate, emitted by iac.terraformImportPlanCandidate.EvidenceRefs."},
                          "safety_gate": {
                            "type": "object",
                            "description": "Safety decision carried from the source finding, emitted by iac.terraformImportPlanCandidate.SafetyGate.",
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
                          }
                        },
                        "required": ["id", "finding_id", "status", "provider", "destination_hint", "configuration_shape", "provider_hint", "safety_gate"]
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
` + ManagementStatus + ManagementExplanation + `
`
