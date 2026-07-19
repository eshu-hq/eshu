// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

const openAPIPathsKubernetes = `
    "/api/v0/kubernetes/correlations": {
      "get": {
        "tags": ["kubernetes"],
        "summary": "List Kubernetes correlations",
        "description": "Lists reducer-owned Kubernetes workload ownership and drift correlation rows. A live workload stays provenance-only unless its image digest or owner edge resolves exactly to deployment-source evidence. Scoped tokens receive rows intersected with the caller's granted repositories/ingestion scopes; a scoped caller with no grants receives an empty page without a query.",
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
                      "items": {
                        "type": "object",
                        "properties": {
                          "correlation_id": {"type": "string"},
                          "cluster_id": {"type": "string"},
                          "workload_object_id": {"type": "string"},
                          "namespace": {"type": "string"},
                          "workload_name": {"type": "string"},
                          "workload_uid": {"type": "string"},
                          "image_ref": {"type": "string"},
                          "source_digest": {"type": "string"},
                          "join_mode": {"type": "string"},
                          "identity_edge_key": {"type": "string"},
                          "relationship_type": {"type": "string"},
                          "outcome": {"type": "string"},
                          "drift_kind": {"type": "string"},
                          "reason": {"type": "string"},
                          "non_promotion": {"type": "string"},
                          "provenance_only": {"type": "boolean"},
                          "candidate_source_digests": {"type": "array", "items": {"type": "string"}},
                          "warnings": {"type": "array", "items": {"type": "string"}},
                          "evidence_fact_ids": {"type": "array", "items": {"type": "string"}}
                        },
                        "required": ["correlation_id", "outcome", "provenance_only"]
                      }
                    },
                    "count": {"type": "integer"},
                    "limit": {"type": "integer"},
                    "truncated": {"type": "boolean"},
                    "next_cursor": {
                      "type": "object",
                      "properties": {
                        "after_correlation_id": {"type": "string"}
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
          "500": {"$ref": "#/components/responses/InternalError"},
          "501": {"$ref": "#/components/responses/NotImplemented"},
          "503": {"$ref": "#/components/responses/ServiceUnavailable"}
        }
      }
    },
`
