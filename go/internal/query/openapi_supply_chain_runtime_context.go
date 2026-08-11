// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

// openAPISupplyChainRuntimeContextSchema keeps the list and explain response
// contracts identical without relying on permissive additional properties.
const openAPISupplyChainRuntimeContextSchema = `{
  "type": "object",
  "description": "Read-time-resolved runtime context (#5746). Workloads, services, deployments, and catalog refs are current repository mappings. Environment corroboration additionally confirms already-visible finding environment names against current accepted correlations for the finding's exact subject digest, mirroring the reducer's strong digest match across builder/deployer repository seams; it is artifact deployment context, not repository ownership. Populated on findings list and impact explain responses; the transformed investigation packet omits it. truth_basis is always read_time_resolved. The workload_id/service_id/environment filters use current active repository mappings (#5747); stale baked values cannot satisfy them.",
  "properties": {
    "truth_basis": {"type": "string", "enum": ["read_time_resolved"]},
    "workload_ids": {"type": "array", "items": {"type": "string"}},
    "service_ids": {"type": "array", "items": {"type": "string"}},
    "deployment_ids": {"type": "array", "items": {"type": "string"}},
    "environments": {"type": "array", "items": {"type": "string"}},
    "environment_evidence": {"type": "object", "additionalProperties": {"type": "string", "enum": ["deploy_event", "declared"]}, "description": "Per-environment corroboration resolved from current accepted cicd_run_correlation facts. Finding-bound candidates require an exact subject-digest and environment match; baked evidence values are never copied. deploy_event wins over declared; missing or unknown current producer values normalize to declared."},
    "environment_evidence_probe": {"type": "object", "description": "Page-weighted exact-digest environment confirmation metadata. candidate_limit is the number of this finding's already-visible environment candidates checked. candidates_truncated is true only when those visible candidates exceeded the allocated quota; it reveals nothing about hidden facts. This is confirmation, not discovery of environments absent from the finding.", "properties": {"candidate_limit": {"type": "integer", "minimum": 1, "maximum": 200}, "candidates_truncated": {"type": "boolean"}}, "required": ["candidate_limit", "candidates_truncated"]},
    "catalog_entity_refs": {"type": "array", "items": {"type": "string"}},
    "catalog_owner_refs": {"type": "array", "items": {"type": "string"}}
  },
  "required": ["truth_basis"]
}`
