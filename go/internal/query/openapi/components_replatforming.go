// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package openapi

const componentsReplatforming = `      "ReplatformingReadinessCounts": {
        "type": "object",
        "description": "Bounded import-readiness view for one replatforming rollup bucket. import_ready, needs_review, and refused stay separate so a refused or unproven item is never presented as ready.",
        "properties": {
          "import_ready": {"type": "integer", "description": "Findings in this bucket whose import plan is clean. Owner: iac/replatforming_rollups.go replatformingReadinessCounts.ImportReady."},
          "needs_review": {"type": "integer", "description": "Findings in this bucket that need human review before import. Owner: iac/replatforming_rollups.go replatformingReadinessCounts.NeedsReview."},
          "refused": {"type": "integer", "description": "Findings in this bucket a safety gate refused; never presented as ready. Owner: iac/replatforming_rollups.go replatformingReadinessCounts.Refused."}
        }
      },
      "ReplatformingRollupBucket": {
        "type": "object",
        "description": "One replatforming rollup group (an account ID, environment name, or service name) with per-source-state counts and the readiness view. Source states are preserved; unsupported, stale, and unavailable are never flattened into a clean total.",
        "properties": {
          "key": {"type": "string", "description": "Group key. The explicit __ambiguous__ and __unattributed__ keys hold contested and missing attribution and are never resolved to a guessed owner."},
          "total": {"type": "integer", "description": "Findings folded into this bucket. Owner: iac/replatforming_rollups.go replatformingRollupBucket.Total."},
          "source_state_counts": {"type": "object", "additionalProperties": {"type": "integer"}, "description": "Count per source-state taxonomy value: exact, derived, partial, ambiguous, stale, unavailable, unsupported, unknown, rejected. Owner: iac/replatforming_rollups.go replatformingRollupBucket.SourceStateCounts."},
          "readiness": {"$ref": "#/components/schemas/ReplatformingReadinessCounts", "description": "Import-readiness view for this bucket. Owner: iac/replatforming_rollups.go replatformingRollupBucket.Readiness."}
        }
      },
      "ReplatformingOwnerCandidate": {
        "type": "object",
        "description": "One candidate owner, repository, module, service, or environment attribution for a drift finding. A single candidate is derived, never exact; conflicting candidates of the same kind each carry explicit ambiguity_reasons. Raw tags are provenance-only and never appear here.",
        "properties": {
          "kind": {"type": "string", "description": "Candidate kind: account, repository, module, service, or environment."},
          "value": {"type": "string", "description": "Candidate owner value (account id, repository, module, service, or environment name). Owner: iac/replatforming_plan_contract.go ReplatformingOwnerCandidate value, built by ownershipCandidates."},
          "confidence": {"type": "string", "description": "exact, derived, or ambiguous. exact is reserved for a reducer-proved match such as a matched Terraform state address; a reducer candidate is at most derived."},
          "ambiguity_reasons": {"type": "array", "items": {"type": "string"}, "description": "Why the candidate is contested. Non-empty only when more than one deterministic candidate of this kind conflicts."}
        }
      },
      "ReplatformingOwnershipPacket": {
        "type": "object",
        "description": "Bounded ownership view for one AWS drift finding. Composes owner/repository/module/service/environment candidates from reducer-owned fields, preserves the read-only safety gate and per-item freshness, and records every missing attribution layer explicitly. Candidates are never collapsed to a single guessed owner.",
        "properties": {
          "item_id": {"type": "string", "description": "Stable finding identity the packet drills into. Owner: iac/replatforming_ownership.go replatformingOwnershipPacket.ItemID (the finding ID)."},
          "provider": {"type": "string", "description": "Cloud provider; AWS for this surface. Owner: iac/replatforming_ownership.go replatformingOwnershipPacket.Provider."},
          "account_id": {"type": "string", "description": "Resolved AWS account, empty when unattributed. Owner: iac/replatforming_ownership.go replatformingOwnershipPacket.AccountID."},
          "region": {"type": "string", "description": "Resolved AWS region, empty when unknown. Owner: iac/replatforming_ownership.go replatformingOwnershipPacket.Region."},
          "resource_type": {"type": "string", "description": "AWS resource family parsed from the ARN. Owner: iac/replatforming_ownership.go replatformingOwnershipPacket.ResourceType."},
          "stable_id": {"type": "string", "description": "Finding stable identity (the ARN for AWS). Owner: iac/replatforming_ownership.go replatformingOwnershipPacket.StableID."},
          "finding_kind": {"type": "string", "description": "Drift finding kind the packet explains. Owner: iac/replatforming_ownership.go replatformingOwnershipPacket.FindingKind."},
          "management_status": {"type": "string", "description": "Reducer-derived IaC management status. Owner: iac/replatforming_ownership.go replatformingOwnershipPacket.ManagementStatus."},
          "source_state": {"type": "string", "description": "Effective provider-neutral source state after the safety gate; a rejected finding is never reported as ready. Owner: iac/replatforming_ownership.go replatformingOwnershipPacket.SourceState."},
          "matched_terraform_state_address": {"type": "string", "description": "Matched Terraform state address, when the reducer proved one. Owner: iac/replatforming_ownership.go replatformingOwnershipPacket.MatchedTerraformStateAddress."},
          "matched_terraform_config_file": {"type": "string", "description": "Matched Terraform config file, when known. Owner: iac/replatforming_ownership.go replatformingOwnershipPacket.MatchedTerraformConfigFile."},
          "matched_terraform_module_path": {"type": "string", "description": "Matched Terraform module path, when known. Owner: iac/replatforming_ownership.go replatformingOwnershipPacket.MatchedTerraformModulePath."},
          "owner_candidates": {"type": "array", "description": "Every candidate attribution with explicit confidence; never collapsed to a single guessed owner. Owner: iac/replatforming_ownership.go replatformingOwnershipPacket.OwnerCandidates.", "items": {"$ref": "#/components/schemas/ReplatformingOwnerCandidate"}},
          "safety_gate": {"type": "object", "description": "Read-only safety decision carried verbatim from the finding. Owner: querycontract/packet/drift.go ManagementSafetyGate.", "properties": {
            "outcome": {"type": "string", "description": "Safety decision outcome. Owner: querycontract/packet/drift.go ManagementSafetyGate.Outcome."},
            "read_only": {"type": "boolean", "description": "Whether the finding is read-only. Owner: querycontract/packet/drift.go ManagementSafetyGate.ReadOnly."},
            "review_required": {"type": "boolean", "description": "Whether a human review is required before import. Owner: querycontract/packet/drift.go ManagementSafetyGate.ReviewRequired."},
            "refused_actions": {"type": "array", "description": "Actions the gate refused. Owner: querycontract/packet/drift.go ManagementSafetyGate.RefusedActions.", "items": {"type": "string"}},
            "warnings": {"type": "array", "description": "Safety warnings. Owner: querycontract/packet/drift.go ManagementSafetyGate.Warnings.", "items": {"type": "string"}},
            "redactions": {"type": "array", "description": "Redacted fields. Owner: querycontract/packet/drift.go ManagementSafetyGate.Redactions.", "items": {"type": "string"}},
            "audit_expectation": {"type": "string", "description": "Audit expectation for this finding. Owner: querycontract/packet/drift.go ManagementSafetyGate.AuditExpectation."}
          }},
          "freshness": {"type": "object", "description": "Per-item freshness derived from the finding source state; a stale or unavailable finding is visibly not fresh. Owner: querycontract/truth.go TruthFreshness.", "properties": {
            "state": {"type": "string", "description": "Freshness state. Owner: querycontract/truth.go TruthFreshness.State."},
            "observed_at": {"type": "string", "description": "When the freshness state was observed. Owner: querycontract/truth.go TruthFreshness.ObservedAt."},
            "detail": {"type": "string", "description": "Human-readable freshness detail. Owner: querycontract/truth.go TruthFreshness.Detail."},
            "cause": {"type": "string", "description": "Closed freshness-cause vocabulary naming why the answer is not fresh. Owner: querycontract/truth.go TruthFreshness.Cause."},
            "next_check": {"type": "object", "description": "Bounded follow-up call that drills into the cause. Owner: querycontract/freshness.go FreshnessNextCheck.", "properties": {
              "tool": {"type": "string", "description": "MCP tool a consumer can call to drill into the cause. Owner: querycontract/freshness.go FreshnessNextCheck.Tool."},
              "route": {"type": "string", "description": "HTTP route a consumer can call to drill into the cause. Owner: querycontract/freshness.go FreshnessNextCheck.Route."},
              "reason": {"type": "string", "description": "Short human-readable explanation of what the next check resolves. Owner: querycontract/freshness.go FreshnessNextCheck.Reason."},
              "params": {"type": "object", "additionalProperties": {"type": "string"}, "description": "Bounded query parameters for the next check. Owner: querycontract/freshness.go FreshnessNextCheck.Params."}
            }}
          }},
          "missing_evidence": {"type": "array", "items": {"type": "string"}, "description": "Attribution layers that resolved nothing, surfaced explicitly rather than read as agreement. Owner: iac/replatforming_ownership.go replatformingOwnershipPacket.MissingEvidence."},
          "recommended_next_calls": {"type": "array", "description": "Bounded follow-up collector or query calls to resolve a missing or contested attribution. Owner: iac/replatforming_ownership.go ownershipRecommendedNextCalls.", "items": {"type": "object", "properties": {
            "tool": {"type": "string", "description": "MCP tool a consumer can call to resolve the gap. Owner: iac/replatforming_ownership.go ownershipRecommendedNextCalls."},
            "route": {"type": "string", "description": "HTTP route a consumer can call to resolve the gap. Owner: iac/replatforming_ownership.go ownershipRecommendedNextCalls."},
            "reason": {"type": "string", "description": "Why this call resolves the gap. Owner: iac/replatforming_ownership.go ownershipRecommendedNextCalls."}
          }}},
          "limitations": {"type": "array", "items": {"type": "string"}, "description": "Bounded human-readable caveats for this item. Owner: iac/replatforming_ownership.go ownershipPacketLimitations."}
        }
      },
`
