// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package infrastructure

// Kubernetes is the OpenAPI path fragment documenting the
// `/api/v0/kubernetes/correlations` route. openapi.Spec concatenates it into
// the published document; keep it in lockstep with the handlers and
// docs/public/reference/http-api.md.
const Kubernetes = `
    "/api/v0/kubernetes/correlations": {
      "get": {
        "tags": ["kubernetes"],
        "summary": "List Kubernetes correlations",
        "description": "Lists reducer-owned Kubernetes workload ownership and drift correlation rows. A live workload stays provenance-only unless its image digest or owner edge resolves exactly to deployment-source evidence. At least one anchor (scope_id, cluster_id, workload_object_id, namespace, image_ref, or source_digest) is required; a request with none returns 400. Scoped tokens receive rows intersected with the caller's granted repositories/ingestion scopes; a scoped caller with no grants receives an empty page without a query.",
        "operationId": "listKubernetesCorrelations",
        "x-scoped-token-support": true,
        "parameters": [
          {"name": "scope_id", "in": "query", "schema": {"type": "string"}, "description": "Reducer scope ID to anchor lookup."},
          {"name": "cluster_id", "in": "query", "schema": {"type": "string"}, "description": "Cluster ID to anchor lookup."},
          {"name": "workload_object_id", "in": "query", "schema": {"type": "string"}, "description": "Durable workload object ID emitted by the Kubernetes live collector (an opaque deterministic identifier, not a deployment/namespace/name shorthand) to anchor lookup."},
          {"name": "namespace", "in": "query", "schema": {"type": "string"}, "description": "Kubernetes namespace to anchor lookup."},
          {"name": "image_ref", "in": "query", "schema": {"type": "string"}, "description": "Live container image reference to anchor lookup."},
          {"name": "source_digest", "in": "query", "schema": {"type": "string"}, "description": "Deployment-source image digest to anchor lookup."},
          {"name": "outcome", "in": "query", "schema": {"type": "string", "enum": ["exact", "derived", "ambiguous", "unresolved", "stale", "rejected"]}},
          {"name": "drift_kind", "in": "query", "schema": {"type": "string"}, "description": "Optional drift kind filter such as in_sync, image_drift, missing_source, or stale_source."},
          {"name": "after_correlation_id", "in": "query", "schema": {"type": "string"}, "description": "Correlation ID from next_cursor when continuing a truncated page."},
          {"name": "limit", "in": "query", "required": true, "schema": {"type": "integer", "minimum": 1, "maximum": 200}}
        ],
        "responses": {
          "200": {
            "description": "Kubernetes correlation rows",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "correlations": {
                      "type": "array",
                      "description": "Page of Kubernetes workload ownership and drift correlation rows, emitted by the kubernetes handler from the reducer read model as []kubernetes.CorrelationResult.",
                      "items": {
                        "type": "object",
                        "description": "One Kubernetes workload ownership and drift correlation row. Owner: kubernetes handler reducer read model.",
                        "properties": {
                          "correlation_id": {"type": "string", "description": "Correlation identity emitted by kubernetes.CorrelationResult.CorrelationID."},
                          "cluster_id": {"type": "string", "description": "Cluster anchoring the lookup, emitted by kubernetes.CorrelationResult.ClusterID."},
                          "workload_object_id": {"type": "string", "description": "Durable workload object identity emitted by kubernetes.CorrelationResult.WorkloadObjectID."},
                          "namespace": {"type": "string", "description": "Kubernetes namespace emitted by kubernetes.CorrelationResult.Namespace."},
                          "workload_name": {"type": "string", "description": "Workload name emitted by kubernetes.CorrelationResult.WorkloadName."},
                          "workload_uid": {"type": "string", "description": "Workload UID emitted by kubernetes.CorrelationResult.WorkloadUID."},
                          "image_ref": {"type": "string", "description": "Live container image reference emitted by kubernetes.CorrelationResult.ImageRef."},
                          "source_digest": {"type": "string", "description": "Deployment-source image digest emitted by kubernetes.CorrelationResult.SourceDigest."},
                          "join_mode": {"type": "string", "description": "Join mode that produced the row, emitted by kubernetes.CorrelationResult.JoinMode."},
                          "identity_edge_key": {"type": "string", "description": "Identity edge key for the join, emitted by kubernetes.CorrelationResult.IdentityEdgeKey."},
                          "relationship_type": {"type": "string", "description": "Relationship type of the join, emitted by kubernetes.CorrelationResult.RelationshipType."},
                          "outcome": {"type": "string", "description": "Correlation outcome emitted by kubernetes.CorrelationResult.Outcome."},
                          "drift_kind": {"type": "string", "description": "Drift kind (in_sync, image_drift, missing_source, stale_source), emitted by kubernetes.CorrelationResult.DriftKind."},
                          "reason": {"type": "string", "description": "Machine-readable reason emitted by kubernetes.CorrelationResult.Reason."},
                          "non_promotion": {"type": "string", "description": "Why the live workload was not promoted, emitted by kubernetes.CorrelationResult.NonPromotion."},
                          "provenance_only": {"type": "boolean", "description": "Whether the row is provenance-only, emitted by kubernetes.CorrelationResult.ProvenanceOnly."},
                          "candidate_source_digests": {"type": "array", "items": {"type": "string"}, "description": "Candidate deployment-source digests emitted by kubernetes.CorrelationResult.CandidateSourceDigests."},
                          "warnings": {"type": "array", "items": {"type": "string"}, "description": "Non-fatal row warnings emitted by kubernetes.CorrelationResult.Warnings."},
                          "evidence_fact_ids": {"type": "array", "items": {"type": "string"}, "description": "Backing evidence fact ids emitted by kubernetes.CorrelationResult.EvidenceFactIDs."}
                        },
                        "required": ["correlation_id", "outcome", "provenance_only"]
                      }
                    },
                    "count": {"type": "integer", "description": "Rows returned in this page, emitted by the kubernetes handler as len(results)."},
                    "limit": {"type": "integer", "description": "Echo of the caller's page size, emitted by the kubernetes handler from the normalized limit."},
                    "truncated": {"type": "boolean", "description": "True when more rows exist past this page, emitted by the kubernetes handler from the lookahead row."},
                    "next_cursor": {
                      "type": "object",
                      "description": "Continuation cursor for the next page (present only when truncated), emitted by the kubernetes handler from the last row.",
                      "properties": {
                        "after_correlation_id": {"type": "string", "description": "Correlation id of the last row, emitted by the kubernetes handler from the last CorrelationResult.CorrelationID."}
                      },
                      "required": ["after_correlation_id"]
                    }
                  },
                  "required": ["correlations", "count", "limit", "truncated"]
                }
              }
            }
          },
          "400": {"$ref": "#/components/responses/BadRequest"},
          "403": {"$ref": "#/components/responses/Forbidden"},
          "500": {"$ref": "#/components/responses/InternalError"},
          "501": {"$ref": "#/components/responses/NotImplemented"},
          "503": {"$ref": "#/components/responses/ServiceUnavailable"}
        }
      }
    },
`
