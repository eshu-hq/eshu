// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

const openAPIComponentsReplatforming = `      "ReplatformingReadinessCounts": {
        "type": "object",
        "description": "Bounded import-readiness view for one replatforming rollup bucket. import_ready, needs_review, and refused stay separate so a refused or unproven item is never presented as ready.",
        "properties": {
          "import_ready": {"type": "integer"},
          "needs_review": {"type": "integer"},
          "refused": {"type": "integer"}
        }
      },
      "ReplatformingRollupBucket": {
        "type": "object",
        "description": "One replatforming rollup group (an account ID, environment name, or service name) with per-source-state counts and the readiness view. Source states are preserved; unsupported, stale, and unavailable are never flattened into a clean total.",
        "properties": {
          "key": {"type": "string", "description": "Group key. The explicit __ambiguous__ and __unattributed__ keys hold contested and missing attribution and are never resolved to a guessed owner."},
          "total": {"type": "integer"},
          "source_state_counts": {"type": "object", "additionalProperties": {"type": "integer"}, "description": "Count per source-state taxonomy value: exact, derived, partial, ambiguous, stale, unavailable, unsupported, unknown, rejected."},
          "readiness": {"$ref": "#/components/schemas/ReplatformingReadinessCounts"}
        }
      },
      "ReplatformingOwnerCandidate": {
        "type": "object",
        "description": "One candidate owner, repository, module, service, or environment attribution for a drift finding. A single candidate is derived, never exact; conflicting candidates of the same kind each carry explicit ambiguity_reasons. Raw tags are provenance-only and never appear here.",
        "properties": {
          "kind": {"type": "string", "description": "Candidate kind: account, repository, module, service, or environment."},
          "value": {"type": "string"},
          "confidence": {"type": "string", "description": "exact, derived, or ambiguous. exact is reserved for a reducer-proved match such as a matched Terraform state address; a reducer candidate is at most derived."},
          "ambiguity_reasons": {"type": "array", "items": {"type": "string"}, "description": "Why the candidate is contested. Non-empty only when more than one deterministic candidate of this kind conflicts."}
        }
      },
      "ReplatformingOwnershipPacket": {
        "type": "object",
        "description": "Bounded ownership view for one AWS drift finding. Composes owner/repository/module/service/environment candidates from reducer-owned fields, preserves the read-only safety gate and per-item freshness, and records every missing attribution layer explicitly. Candidates are never collapsed to a single guessed owner.",
        "properties": {
          "item_id": {"type": "string"},
          "provider": {"type": "string"},
          "account_id": {"type": "string"},
          "region": {"type": "string"},
          "resource_type": {"type": "string"},
          "stable_id": {"type": "string"},
          "finding_kind": {"type": "string"},
          "management_status": {"type": "string"},
          "source_state": {"type": "string", "description": "Effective provider-neutral source state after the safety gate; a rejected finding is never reported as ready."},
          "matched_terraform_state_address": {"type": "string"},
          "matched_terraform_config_file": {"type": "string"},
          "matched_terraform_module_path": {"type": "string"},
          "owner_candidates": {"type": "array", "items": {"$ref": "#/components/schemas/ReplatformingOwnerCandidate"}},
          "safety_gate": {"type": "object", "description": "Read-only safety decision carried verbatim from the finding."},
          "freshness": {"type": "object", "description": "Per-item freshness; a stale or unavailable finding is visibly not fresh."},
          "missing_evidence": {"type": "array", "items": {"type": "string"}, "description": "Attribution layers that resolved nothing, surfaced explicitly rather than read as agreement."},
          "recommended_next_calls": {"type": "array", "items": {"type": "object"}},
          "limitations": {"type": "array", "items": {"type": "string"}}
        }
      },
`
