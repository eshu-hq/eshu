// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package chain

// RuntimeContext keeps the list and explain response
// contracts identical without relying on permissive additional properties.
const RuntimeContext = `{
  "type": "object",
  "description": "Read-time-resolved runtime context (#5746). Workloads, services, deployments, and catalog refs are current repository mappings. Environment corroboration additionally confirms already-visible finding environment names against current accepted correlations for the finding's exact subject digest, mirroring the reducer's strong digest match across builder/deployer repository seams; it is artifact deployment context, not repository ownership. Populated on findings list and impact explain responses; the transformed investigation packet omits it. truth_basis is always read_time_resolved. The workload_id/service_id/environment filters use current active repository mappings (#5747); stale baked values cannot satisfy them.",
  "properties": {
    "truth_basis": {"type": "string", "enum": ["read_time_resolved"], "description": "Resolution path label emitted by the read-time runtime context resolver (packet.RuntimeContextResult.TruthBasis); always read_time_resolved."},
    "workload_ids": {"type": "array", "items": {"type": "string"}, "description": "Current runtime workload ids resolved from the finding's repository at query time (packet.RuntimeContextResult.WorkloadIDs); an honest empty list when no runtime facts landed yet."},
    "service_ids": {"type": "array", "items": {"type": "string"}, "description": "Current runtime service ids resolved from the finding's repository at query time (packet.RuntimeContextResult.ServiceIDs)."},
    "deployment_ids": {"type": "array", "items": {"type": "string"}, "description": "Current runtime deployment ids resolved from the finding's repository at query time (packet.RuntimeContextResult.DeploymentIDs)."},
    "environments": {"type": "array", "items": {"type": "string"}, "description": "Current runtime environments resolved from the finding's repository at query time (packet.RuntimeContextResult.Environments)."},
    "environment_evidence": {"type": "object", "additionalProperties": {"type": "string", "enum": ["deploy_event", "declared"]}, "description": "Per-environment corroboration admitted only when a current authorized cicd_run_correlation matches the finding's exact subject digest and candidate environment. Repository context contributes candidate names only; repository or baked evidence values are never copied, and unconfirmed names are omitted. deploy_event wins over declared; missing or unknown producer values on an admitted matching row normalize to declared."},
    "environment_evidence_probe": {"type": "object", "description": "Page-weighted exact-digest environment confirmation metadata. candidate_limit is the number of this finding's already-visible environment candidates checked. candidates_truncated is true only when those visible candidates exceeded the allocated quota; it reveals nothing about hidden facts. This is confirmation, not discovery of environments absent from the finding.", "properties": {"candidate_limit": {"type": "integer", "minimum": 1, "maximum": 200, "description": "Environment candidates checked, emitted by the read-time runtime context resolver (packet.RuntimeEnvironmentEvidenceProbe.CandidateLimit)."}, "candidates_truncated": {"type": "boolean", "description": "Whether visible candidates exceeded the quota, emitted by the read-time runtime context resolver (packet.RuntimeEnvironmentEvidenceProbe.CandidatesTruncated)."}}, "required": ["candidate_limit", "candidates_truncated"]},
    "catalog_entity_refs": {"type": "array", "items": {"type": "string"}, "description": "Current service-catalog entity references resolved at query time (packet.RuntimeContextResult.CatalogEntityRefs)."},
    "catalog_owner_refs": {"type": "array", "items": {"type": "string"}, "description": "Current service-catalog owner references resolved at query time (packet.RuntimeContextResult.CatalogOwnerRefs)."}
  },
  "required": ["truth_basis"]
}`
