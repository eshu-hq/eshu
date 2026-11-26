// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cloud

// RuntimeDrift documents the runtime drift readback route
// across all three providers (issues #1997, #1998, #5759 follow-up). It
// aggregates reducer_multi_cloud_runtime_drift_finding rows (gcp, azure; one
// per canonical cloud_resource_uid) with reducer_aws_cloud_runtime_drift_finding
// rows (aws) in one query, so provider=aws and an unfiltered query genuinely
// return AWS findings instead of the empty page this route returned for aws
// before the aggregation existed. Findings carry provider, normalized
// identity, finding_kind, management_status, provider-neutral source_state,
// and refusal-safety posture. The route is read-only, bounded, paginated, and
// truth-labeled; it never returns raw provider locators (including the AWS
// ARN) or raw evidence atoms, and refuses unsafe findings rather than
// omitting them. drifted_attributes (#5453) is a narrow exception: for an
// image_version_drift finding, it carries the bounded declared/observed value
// pairs (e.g. ami, image_uri, version) the finding is ABOUT -- a
// purpose-built projection of two evidence atoms per attribute, never the
// full raw evidence-atom list. An aws-origin finding's management_status,
// missing_evidence, and warning_flags (folded into safety_gate) are derived
// through the SAME classification list_aws_runtime_drift_findings uses
// (awsCloudRuntimeDriftDerivedStatus, #5759 follow-up P1-1), so the identical
// underlying reducer row never produces two different safety verdicts
// depending on which route reads it.
const RuntimeDrift = `
    "/api/v0/cloud/runtime-drift/findings": {
      "post": {
        "tags": ["cloud"],
        "summary": "List runtime drift findings across aws, gcp, and azure",
        "description": "Lists active reducer-materialized runtime drift findings for a bounded canonical scope, aggregated across all three providers: reducer_multi_cloud_runtime_drift_finding (gcp, azure) and reducer_aws_cloud_runtime_drift_finding (aws) in one query. Filterable by provider, canonical cloud_resource_uid, and finding_kind; cloud_resource_uid filtering matches only gcp/azure findings (an AWS finding's canonical identity is resolved for display but is not a stored, filterable column on its fact kind). Each finding carries its provider-neutral source_state and safety gate; unsafe findings are reported as rejected with a refused action rather than omitted. local_lightweight returns unsupported_capability. Scoped tokens must supply a scope_id (or account_id/project_id/subscription_id alias) that resolves to a granted repository or ingestion scope; a scoped caller with no grants or an out-of-grant scope_id receives an empty page. A 503 with Retry-After means a transient condition that is safe to retry: the drift finding count or page read hit a stale or timed-out PostgreSQL reader; this route has no configuration 503, so every 503 it answers carries Retry-After (a missing drift finding read model answers 501). No 5xx body carries backend error text. A graph or reader verdict 503 names capability cloud_runtime_drift.readback.list in its error envelope.",
        "operationId": "listCloudRuntimeDriftFindings",
        "x-scoped-token-support": true,
        "requestBody": {
          "required": true,
          "content": {
            "application/json": {
              "schema": {
                "type": "object",
                "properties": {
                  "scope_id": {"type": "string", "description": "Canonical ingestion scope, for example cloud-scope:gcp:project-synthetic."},
                  "account_id": {"type": "string", "description": "Alias for scope_id (AWS account scope)."},
                  "project_id": {"type": "string", "description": "Alias for scope_id (GCP project scope)."},
                  "subscription_id": {"type": "string", "description": "Alias for scope_id (Azure subscription scope)."},
                  "provider": {"type": "string", "description": "Optional provider filter: aws, gcp, or azure.", "enum": ["aws", "gcp", "azure"]},
                  "cloud_resource_uid": {"type": "string", "description": "Optional exact canonical resource uid to inspect."},
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
                  {"required": ["account_id"]},
                  {"required": ["project_id"]},
                  {"required": ["subscription_id"]}
                ]
              }
            }
          }
        },
        "responses": {
          "200": {
            "description": "Provider-neutral multi-cloud runtime drift findings",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "scope_id": {"type": "string", "description": "Echo of the caller's scope_id filter, emitted by query.writeCloudRuntimeDriftFindings from MultiCloudRuntimeDriftFilter.ScopeID."},
                    "provider": {"type": "string", "description": "Echo of the caller's provider filter (empty when unfiltered), emitted by query.writeCloudRuntimeDriftFindings from MultiCloudRuntimeDriftFilter.Provider."},
                    "cloud_resource_uid": {"type": "string", "description": "Echo of the caller's canonical resource uid filter (empty when unfiltered; matches only gcp/azure findings), emitted by query.writeCloudRuntimeDriftFindings from MultiCloudRuntimeDriftFilter.CloudResourceUID."},
                    "story": {"type": "string", "description": "One-line page summary rendered by query.cloudRuntimeDriftStory from the caller's filter and the page counts."},
                    "finding_kinds": {"type": "array", "items": {"type": "string"}, "description": "Echo of the caller's normalized finding-kind filter, emitted by query.writeCloudRuntimeDriftFindings from MultiCloudRuntimeDriftFilter.FindingKinds."},
                    "source_state_groups": {
                      "type": "array",
                      "description": "Finding counts grouped by provider-neutral source state with drilldown uids, emitted by query.cloudRuntimeDriftSourceStateGroups.",
                      "items": {
                        "type": "object",
                        "description": "One source-state group. Owner: query.cloudRuntimeDriftSourceStateGroups.",
                        "properties": {
                          "source_state": {"type": "string", "description": "Provider-neutral source-state bucket emitted by query.cloudRuntimeDriftSourceStateGroup.SourceState."},
                          "count": {"type": "integer", "description": "Number of page findings in this source-state bucket, emitted by query.cloudRuntimeDriftSourceStateGroup.Count."},
                          "cloud_resource_uids": {"type": "array", "items": {"type": "string"}, "description": "Sorted canonical uids of the page findings in this bucket, emitted by query.cloudRuntimeDriftSourceStateGroup.CloudResourceUIDs."}
                        },
                        "required": ["source_state", "count"]
                      }
                    },
                    "findings_count": {"type": "integer", "description": "Number of findings in this page, emitted by query.writeCloudRuntimeDriftFindings as len(views)."},
                    "total_findings_count": {"type": "integer", "description": "Total active findings matching the filter, emitted by query.writeCloudRuntimeDriftFindings from the store count."},
                    "limit": {"type": "integer", "description": "Echo of the caller's page size, emitted by query.writeCloudRuntimeDriftFindings from MultiCloudRuntimeDriftFilter.Limit."},
                    "offset": {"type": "integer", "description": "Echo of the caller's result offset, emitted by query.writeCloudRuntimeDriftFindings from MultiCloudRuntimeDriftFilter.Offset."},
                    "truncated": {"type": "boolean", "description": "True when more findings exist past this page, emitted by query.cloudRuntimeDriftTruncated."},
                    "next_offset": {"type": "integer", "description": "Offset to fetch the next page (0 when not truncated), emitted by query.cloudRuntimeDriftNextOffset."},
                    "truth_basis": {"type": "string", "description": "Truth-basis label for this readback, emitted by query.writeCloudRuntimeDriftFindings (always materialized_reducer_rows)."},
                    "analysis_status": {"type": "string", "description": "Read-model status label for this readback, emitted by query.writeCloudRuntimeDriftFindings (always materialized_multi_cloud_runtime_drift)."},
                    "limitations": {"type": "array", "items": {"type": "string"}, "description": "Fixed limitation strings for this readback, emitted by query.writeCloudRuntimeDriftFindings."},
                    "drift_findings": {
                      "type": "array",
                      "description": "Page of provider-neutral runtime drift finding views, emitted by query.cloudRuntimeDriftFindingViews.",
                      "items": {
                        "type": "object",
                        "description": "One runtime drift finding view. Owner: query.cloudRuntimeDriftFindingViews.",
                        "properties": {
                          "fact_id": {"type": "string", "description": "Reducer fact identity emitted by packet.CloudRuntimeDriftFindingView.FactID."},
                          "provider": {"type": "string", "enum": ["aws", "gcp", "azure"], "description": "Cloud provider of the finding, emitted by packet.CloudRuntimeDriftFindingView.Provider."},
                          "scope_id": {"type": "string", "description": "Canonical ingestion scope of the finding, emitted by packet.CloudRuntimeDriftFindingView.ScopeID."},
                          "generation_id": {"type": "string", "description": "Reducer generation that produced the finding, emitted by packet.CloudRuntimeDriftFindingView.GenerationID."},
                          "source_system": {"type": "string", "description": "Source system that admitted the finding, emitted by packet.CloudRuntimeDriftFindingView.SourceSystem."},
                          "cloud_resource_uid": {"type": "string", "description": "Canonical cross-provider resource identity, emitted by packet.CloudRuntimeDriftFindingView.CloudResourceUID."},
                          "finding_kind": {"type": "string", "description": "Drift finding kind emitted by packet.CloudRuntimeDriftFindingView.FindingKind."},
                          "management_status": {"type": "string", "description": "Normalized management status emitted by packet.CloudRuntimeDriftFindingView.ManagementStatus."},
                          "confidence": {"type": "number", "description": "Finding confidence emitted by packet.CloudRuntimeDriftFindingView.Confidence."},
                          "source_state": {"type": "string", "description": "Provider-neutral source-state taxonomy value derived from management_status and the safety gate, emitted by packet.CloudRuntimeDriftFindingView.SourceState via query.ResolveReplatformingSourceState."},
                          "matched_terraform_state_address": {"type": "string", "description": "Matched Terraform state address when the finding correlates to managed state, emitted by packet.CloudRuntimeDriftFindingView.MatchedTerraformStateAddress."},
                          "missing_evidence": {"type": "array", "items": {"type": "string"}, "description": "Evidence the read could not produce, emitted by packet.CloudRuntimeDriftFindingView.MissingEvidence."},
                          "recommended_action": {"type": "string", "description": "Read-only recommended next action, emitted by packet.CloudRuntimeDriftFindingView.RecommendedAction."},
                          "drifted_attributes": {
                            "type": "array",
                            "description": "Bounded declared/observed value pairs for an image_version_drift finding (ami, image_uri, version, or the ECS container image comparison), one pair per comparison that ran and disagreed. Empty for every other kind, including value_comparison_inconclusive, which reports that this pass cannot speak for the covered values -- either no comparison could be made at all, or the ones that ran agreed while another covered comparable was unreadable. Both kinds name any comparable they could not READ in missing_evidence as comparable_attribute:<key>, so an image_version_drift carrying one is a verdict reached on partial evidence. Emitted by packet.CloudRuntimeDriftFindingView.DriftedAttributes.",
                            "items": {
                              "type": "object",
                              "description": "One declared/observed value pair. Owner: querycontract.DriftedAttributeView.",
                              "properties": {
                                "attribute": {"type": "string", "description": "Allowlisted comparable attribute name emitted by querycontract.DriftedAttributeView.Attribute."},
                                "declared_value": {"type": "string", "description": "Terraform-state value emitted by querycontract.DriftedAttributeView.Declared."},
                                "observed_value": {"type": "string", "description": "Cloud-observed value emitted by querycontract.DriftedAttributeView.Observed."}
                              },
                              "required": ["attribute", "declared_value", "observed_value"]
                            }
                          },
                          "safety_gate": {
                            "type": "object",
                            "description": "Safety decision before a caller turns this read-only finding into a task, emitted by packet.CloudRuntimeDriftFindingView.SafetyGate via iacManagementSafetyGate.",
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
                        "required": ["fact_id", "provider", "scope_id", "generation_id", "cloud_resource_uid", "finding_kind", "management_status", "confidence", "source_state", "safety_gate"]
                      }
                    }
                  }
                }
              }
            }
          },
          "400": {"$ref": "#/components/responses/BadRequest"},
          "403": {"$ref": "#/components/responses/Forbidden"},
          "501": {
            "description": "Not implemented: the active query profile does not support cloud runtime drift readback (unsupported_capability), or the reducer drift finding read model is not wired (read_model_unavailable). Neither is a transient outage, so retrying does not help.",
            "content": {
              "application/json": {
                "schema": {"$ref": "#/components/schemas/ErrorResponse"}
              }
            }
          },
          "500": {"$ref": "#/components/responses/InternalError"},
          "503": {"$ref": "#/components/responses/ServiceUnavailable"}
        }
      }
    },
`
