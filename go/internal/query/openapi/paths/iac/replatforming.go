// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package iac

// Replatforming documents the service-scoped replatforming plan
// compose route. The route composes a provider-neutral ReplatformingPlan over
// reducer-owned IaC management and runtime-drift evidence and is read-only: it
// never runs Terraform, imports resources, or mutates cloud or repository state.
const Replatforming = `
    "/api/v0/replatforming/plans": {
      "post": {
        "tags": ["iac"],
        "summary": "Compose a service-scoped replatforming plan",
        "description": "Composes one bounded, truth-labeled replatforming plan over active AWS IaC management and runtime-drift findings. Each migration packet item carries its management status, finding kind, provider-neutral source state, safety gate, source layers, owner candidates with ambiguity reasons, and a ready or refused Terraform import candidate. The route is read-only: it never runs Terraform, imports resources, or mutates cloud or repository state. Lightweight local profiles return unsupported_capability.",
        "operationId": "composeReplatformingPlan",
        "x-scoped-token-support": true,
        "requestBody": {
          "required": true,
          "content": {
            "application/json": {
              "schema": {
                "type": "object",
                "properties": {
                  "scope_kind": {"type": "string", "description": "Primary plan scope dimension: account, region, service, workload, repository, environment, or resource.", "enum": ["account", "region", "service", "workload", "repository", "environment", "resource"]},
                  "scope_id": {"type": "string", "description": "Exact AWS collector scope, for example aws:123456789012:us-east-1:lambda."},
                  "account_id": {"type": "string", "description": "AWS account ID used to bound the active finding read."},
                  "region": {"type": "string", "description": "Optional AWS region when account_id is supplied."},
                  "service_name": {"type": "string", "description": "Optional service name that narrows the plan scope."},
                  "workload_id": {"type": "string", "description": "Optional deployable workload identity that narrows the plan scope."},
                  "repo_id": {"type": "string", "description": "Optional source repository identity that narrows the plan scope."},
                  "environment": {"type": "string", "description": "Optional environment that narrows the plan scope."},
                  "arn": {"type": "string", "description": "Optional exact AWS ARN to inspect."},
                  "resource_id": {"type": "string", "description": "Optional alias for arn; for AWS this must be the full ARN, not a provider-local ID such as an S3 bucket name or Lambda function name."},
                  "finding_kinds": {
                    "type": "array",
                    "description": "Optional finding kinds: orphaned_cloud_resource, unmanaged_cloud_resource, unknown_cloud_resource, or ambiguous_cloud_resource.",
                    "items": {"type": "string"}
                  },
                  "limit": {"type": "integer", "description": "Maximum migration packet items to compose (default 100, max 500).", "default": 100},
                  "offset": {"type": "integer", "description": "Zero-based result offset for paging migration packet items.", "default": 0}
                },
                "required": ["scope_kind"],
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
            "description": "Composed replatforming plan",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "story": {"type": "string", "description": "One-line composition summary rendered by iac.replatformingPlanStory from the plan scope, the inspected findings, and the composed item counts."},
                    "scope_kind": {"type": "string", "description": "Echo of the caller's primary plan scope dimension, emitted by the iac replatforming-plan handler from ReplatformingPlanScope.Kind."},
                    "account_id": {"type": "string", "description": "Echo of the caller's account_id bound, emitted by the iac replatforming-plan handler from ManagementFilter.AccountID."},
                    "scope_id": {"type": "string", "description": "Echo of the caller's scope_id bound, emitted by the iac replatforming-plan handler from ManagementFilter.ScopeID."},
                    "region": {"type": "string", "description": "Echo of the caller's region bound, emitted by the iac replatforming-plan handler from ManagementFilter.Region."},
                    "arn": {"type": "string", "description": "Echo of the caller's arn bound, emitted by the iac replatforming-plan handler from ManagementFilter.ARN."},
                    "finding_kinds": {"type": "array", "items": {"type": "string"}, "description": "Echo of the caller's normalized finding-kind filter, emitted by the iac replatforming-plan handler from ManagementFilter.FindingKinds."},
                    "plan": {
                      "type": "object",
                      "description": "Versioned replatforming plan / migration packet, emitted by iac.composeReplatformingPlan as iac.ReplatformingPlan.",
                      "properties": {
                        "contract_version": {"type": "string", "description": "Plan contract version emitted by iac.ReplatformingPlan.ContractVersion."},
                        "scope": {
                          "type": "object",
                          "description": "Normalized plan scope, emitted by iac.ReplatformingPlan.Scope via normalizeReplatformingScope as iac.ReplatformingPlanScope.",
                          "properties": {
                            "kind": {"type": "string", "description": "Primary scope dimension emitted by iac.ReplatformingPlanScope.Kind."},
                            "account": {"type": "string", "description": "Account narrowing emitted by iac.ReplatformingPlanScope.Account."},
                            "region": {"type": "string", "description": "Region narrowing emitted by iac.ReplatformingPlanScope.Region."},
                            "service": {"type": "string", "description": "Service narrowing emitted by iac.ReplatformingPlanScope.Service."},
                            "workload": {"type": "string", "description": "Workload narrowing emitted by iac.ReplatformingPlanScope.Workload."},
                            "repository": {"type": "string", "description": "Repository narrowing emitted by iac.ReplatformingPlanScope.Repository."},
                            "environment": {"type": "string", "description": "Environment narrowing emitted by iac.ReplatformingPlanScope.Environment."},
                            "resource": {"type": "string", "description": "Resource narrowing emitted by iac.ReplatformingPlanScope.Resource."}
                          },
                          "required": ["kind"]
                        },
                        "items": {
                          "type": "array",
                          "description": "Migration packet items, emitted by iac.ReplatformingPlan.Items via composeReplatformingPlan as []iac.MigrationPacketItem.",
                          "items": {
                            "type": "object",
                            "properties": {
                              "item_id": {"type": "string", "description": "Packet item identity emitted by iac.MigrationPacketItem.ItemID."},
                              "provider": {"type": "string", "description": "Cloud provider emitted by iac.MigrationPacketItem.Provider."},
                              "resource_type": {"type": "string", "description": "Provider resource type emitted by iac.MigrationPacketItem.ResourceType."},
                              "stable_id": {"type": "string", "description": "Stable cross-read identity emitted by iac.MigrationPacketItem.StableID."},
                              "source_state": {"type": "string", "description": "Provider-neutral source state after the safety gate, emitted by iac.MigrationPacketItem.SourceState."},
                              "management_status": {"type": "string", "description": "IaC management status emitted by iac.MigrationPacketItem.ManagementStatus."},
                              "finding_kind": {"type": "string", "description": "Drift finding kind emitted by iac.MigrationPacketItem.FindingKind."},
                              "confidence": {"type": "string", "description": "Finding confidence label emitted by iac.MigrationPacketItem.Confidence."},
                              "freshness": {
                                "type": "object",
                                "description": "Per-item truth freshness emitted by iac.MigrationPacketItem.Freshness as a querycontract.TruthFreshness view.",
                                "properties": {
                                  "state": {"type": "string", "description": "Freshness state emitted by querycontract.TruthFreshness.State."},
                                  "observed_at": {"type": "string", "description": "Observation timestamp emitted by querycontract.TruthFreshness.ObservedAt."},
                                  "detail": {"type": "string", "description": "Freshness detail emitted by querycontract.TruthFreshness.Detail."},
                                  "cause": {"type": "string", "description": "Machine-readable lag cause from the closed FreshnessCause enumeration, emitted by querycontract.TruthFreshness.Cause (set only when the handler holds the evidence)."},
                                  "next_check": {
                                    "type": "object",
                                    "description": "Bounded follow-up call that drills into the cause, emitted by querycontract.TruthFreshness.NextCheck as a FreshnessNextCheck view (present only alongside cause).",
                                    "properties": {
                                      "tool": {"type": "string", "description": "MCP tool to drill into the cause, emitted by querycontract.FreshnessNextCheck.Tool."},
                                      "route": {"type": "string", "description": "HTTP route to drill into the cause, emitted by querycontract.FreshnessNextCheck.Route."},
                                      "reason": {"type": "string", "description": "What the next check resolves, emitted by querycontract.FreshnessNextCheck.Reason."},
                                      "params": {"type": "object", "additionalProperties": {"type": "string"}, "description": "Bounded query params for the next check, emitted by querycontract.FreshnessNextCheck.Params. Keys are the check's query parameter names."}
                                    }
                                  }
                                },
                                "required": ["state"]
                              },
                              "safety_gate": {
                                "type": "object",
                                "description": "Safety decision carried from the source finding, emitted by iac.MigrationPacketItem.SafetyGate.",
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
                              "source_layers": {
                                "type": "array",
                                "description": "Evidence-layer statuses the item carries or misses, emitted by iac.MigrationPacketItem.SourceLayers as []iac.ReplatformingSourceLayerStatus.",
                                "items": {
                                  "type": "object",
                                  "properties": {
                                    "layer": {"type": "string", "description": "Evidence layer emitted by iac.ReplatformingSourceLayerStatus.Layer."},
                                    "state": {"type": "string", "description": "Layer state emitted by iac.ReplatformingSourceLayerStatus.State."},
                                    "detail": {"type": "string", "description": "Layer detail emitted by iac.ReplatformingSourceLayerStatus.Detail."}
                                  },
                                  "required": ["layer", "state"]
                                }
                              },
                              "owner_candidates": {
                                "type": "array",
                                "description": "Owner, repository, module, service, or environment attributions with explicit ambiguity reasons (never collapsed to a single guessed owner), emitted by iac.MigrationPacketItem.OwnerCandidates as []iac.ReplatformingOwnerCandidate.",
                                "items": {
                                  "type": "object",
                                  "properties": {
                                    "kind": {"type": "string", "description": "Candidate kind emitted by iac.ReplatformingOwnerCandidate.Kind."},
                                    "value": {"type": "string", "description": "Candidate value emitted by iac.ReplatformingOwnerCandidate.Value."},
                                    "confidence": {"type": "string", "description": "Candidate confidence (at most derived) emitted by iac.ReplatformingOwnerCandidate.Confidence."},
                                    "ambiguity_reasons": {"type": "array", "items": {"type": "string"}, "description": "Why the candidate is contested, emitted by iac.ReplatformingOwnerCandidate.AmbiguityReasons (non-empty when candidates conflict)."}
                                  },
                                  "required": ["kind", "value"]
                                }
                              },
                              "import_candidate": {
                                "type": "object",
                                "description": "Read-only Terraform import candidate for the packet item (a refused candidate carries refusal reasons and never an import block), emitted by iac.MigrationPacketItem.ImportCandidate as iac.ReplatformingImportCandidate.",
                                "properties": {
                                  "status": {"type": "string", "description": "Candidate verdict emitted by iac.ReplatformingImportCandidate.Status (ready or refused)."},
                                  "resource_type": {"type": "string", "description": "Terraform resource type emitted by iac.ReplatformingImportCandidate.ResourceType."},
                                  "import_block": {"type": "string", "description": "Ready import block emitted by iac.ReplatformingImportCandidate.ImportBlock (absent on refused candidates)."},
                                  "refusal_reasons": {"type": "array", "items": {"type": "string"}, "description": "Refusal reasons emitted by iac.ReplatformingImportCandidate.RefusalReasons."}
                                },
                                "required": ["status"]
                              },
                              "wave_id": {"type": "string", "description": "Assigned migration wave emitted by iac.MigrationPacketItem.WaveID."},
                              "blast_radius_group": {"type": "string", "description": "Assigned blast-radius group emitted by iac.MigrationPacketItem.BlastRadiusGroup."}
                            },
                            "required": ["item_id", "provider", "resource_type", "stable_id", "source_state", "safety_gate"]
                          }
                        },
                        "waves": {"type": "array", "description": "Ordered migration waves for staged migration, emitted by iac.ReplatformingPlan.Waves as []iac.MigrationWave. wave-1-early-safe holds import-ready, low-blast-radius, non-gated items; wave-2-review holds non-gated items needing review; wave-3-blocked holds safety-gated, rejected, or ambiguously owned items and is always last.", "items": {"type": "object", "properties": {"id": {"type": "string", "description": "Wave identity emitted by iac.MigrationWave.ID."}, "order": {"type": "integer", "description": "Staging order emitted by iac.MigrationWave.Order."}, "rationale": {"type": "string", "description": "Wave rationale emitted by iac.MigrationWave.Rationale."}, "item_ids": {"type": "array", "items": {"type": "string"}, "description": "Packet item ids in this wave, emitted by iac.MigrationWave.ItemIDs."}}, "required": ["id", "order"]}},
                        "blast_radius_groups": {"type": "array", "description": "Dependency and risk groups in ascending severity (none, low, medium, high, blocked), emitted by iac.ReplatformingPlan.BlastRadiusGroups as []iac.BlastRadiusGroup. Severity is driven by recorded dependency paths and missing-evidence counts; ambiguous, rejected, or safety-gated items are grouped as blocked regardless of footprint.", "items": {"type": "object", "properties": {"id": {"type": "string", "description": "Group identity emitted by iac.BlastRadiusGroup.ID."}, "severity": {"type": "string", "description": "Group severity emitted by iac.BlastRadiusGroup.Severity."}, "reason": {"type": "string", "description": "Group reason emitted by iac.BlastRadiusGroup.Reason."}, "item_ids": {"type": "array", "items": {"type": "string"}, "description": "Packet item ids in this group, emitted by iac.BlastRadiusGroup.ItemIDs."}}, "required": ["id"]}},
                        "recommended_next_calls": {
                          "type": "array",
                          "description": "Follow-up route hints for this plan, emitted by iac.ReplatformingPlan.RecommendedNextCalls via replatformingPlanNextCalls.",
                          "items": {
                            "type": "object",
                            "properties": {
                              "route": {"type": "string", "description": "Route to call next, emitted by iac.replatformingPlanNextCalls."},
                              "reason": {"type": "string", "description": "Why to call it, emitted by iac.replatformingPlanNextCalls."},
                              "params": {"type": "object", "additionalProperties": true, "description": "Paging params echo for the next plan page (present only on the paging entry), emitted by iac.replatformingPlanNextCalls. Keys are the echoed filter params (account_id, scope_id, region, offset, limit)."}
                            },
                            "required": ["route", "reason"]
                          }
                        },
                        "limitations": {"type": "array", "items": {"type": "string"}, "description": "Plan limitations emitted by iac.ReplatformingPlan.Limitations via replatformingPlanLimitations."},
                        "non_goals": {"type": "array", "items": {"type": "string"}, "description": "Fixed safety boundaries every plan repeats (never execute a migration), emitted by iac.ReplatformingPlan.NonGoals via ReplatformingPlanNonGoals."}
                      },
                      "required": ["contract_version", "scope", "items", "non_goals"]
                    },
                    "plan_truth_level": {"type": "string", "description": "Plan truth-level label emitted by the iac replatforming-plan handler from plan.RollupTruthLevel."},
                    "items_count": {"type": "integer", "description": "Packet items composed, emitted by the iac replatforming-plan handler as len(plan.Items)."},
                    "ready_import_count": {"type": "integer", "description": "Items with a ready import candidate, counted by iac.replatformingReadyImportCount."},
                    "refused_import_count": {"type": "integer", "description": "Items with a refused import candidate, counted by iac.replatformingRefusedImportCount."},
                    "wave_summaries": {"type": "array", "description": "Per-wave item counts in staging order, emitted by iac.replatformingPlanWaveSummaries.", "items": {"type": "object", "properties": {"wave_id": {"type": "string", "description": "Wave identity emitted by the wave summary builder from MigrationWave.ID."}, "order": {"type": "integer", "description": "Staging order emitted by the wave summary builder from MigrationWave.Order."}, "item_count": {"type": "integer", "description": "Items in this wave, counted by the wave summary builder from len(MigrationWave.ItemIDs)."}}, "required": ["wave_id", "order", "item_count"]}},
                    "blast_radius_summaries": {"type": "array", "description": "Per-blast-radius-group item counts in ascending severity order, emitted by iac.replatformingPlanBlastRadiusSummaries.", "items": {"type": "object", "properties": {"group_id": {"type": "string", "description": "Group identity emitted by the blast-radius summary builder from BlastRadiusGroup.ID."}, "severity": {"type": "string", "description": "Group severity emitted by the blast-radius summary builder from BlastRadiusGroup.Severity."}, "item_count": {"type": "integer", "description": "Items in this group, counted by the blast-radius summary builder from len(BlastRadiusGroup.ItemIDs)."}}, "required": ["group_id", "severity", "item_count"]}},
                    "total_findings_count": {"type": "integer", "description": "Total active findings inspected for this plan, emitted by the iac replatforming-plan handler from the store count."},
                    "limit": {"type": "integer", "description": "Echo of the caller's page size, emitted by the iac replatforming-plan handler from ManagementFilter.Limit."},
                    "offset": {"type": "integer", "description": "Echo of the caller's result offset, emitted by the iac replatforming-plan handler from ManagementFilter.Offset."},
                    "truncated": {"type": "boolean", "description": "True when more findings exist past this page, emitted by iac.ManagementTruncated."},
                    "next_offset": {"type": ["integer", "null"], "description": "Offset to fetch the next page (0 when not truncated), emitted by iac.ManagementNextOffset."},
                    "recommended_next_calls": {
                      "type": "array",
                      "description": "Follow-up route hints for this response, emitted by the iac replatforming-plan handler from plan.RecommendedNextCalls via replatformingPlanNextCalls.",
                      "items": {
                        "type": "object",
                        "properties": {
                          "route": {"type": "string", "description": "Route to call next, emitted by iac.replatformingPlanNextCalls."},
                          "reason": {"type": "string", "description": "Why to call it, emitted by iac.replatformingPlanNextCalls."},
                          "params": {"type": "object", "additionalProperties": true, "description": "Paging params echo for the next plan page (present only on the paging entry), emitted by iac.replatformingPlanNextCalls. Keys are the echoed filter params (account_id, scope_id, region, offset, limit)."}
                        },
                        "required": ["route", "reason"]
                      }
                    },
                    "truth_basis": {"type": "string", "description": "Truth-basis label for this composition, emitted by the iac replatforming-plan handler (always materialized_reducer_rows)."},
                    "analysis_status": {"type": "string", "description": "Composition status label emitted by the iac replatforming-plan handler (always replatforming_plan_composition)."},
                    "limitations": {"type": "array", "items": {"type": "string"}, "description": "Plan limitations emitted by the iac replatforming-plan handler from plan.Limitations via replatformingPlanLimitations."}
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
