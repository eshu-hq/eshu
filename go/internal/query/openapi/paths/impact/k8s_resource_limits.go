// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

// K8sResourceLimits is the shared OpenAPI schema fragment spliced into
// every path fragment that references it, so those routes document one
// identical shape rather than drifting copies.
const K8sResourceLimits = `
                    "k8s_resource_limits": {
                      "type": "object",
                      "description": "Deterministic bound and sentinel completeness metadata for k8s_resources after content and deployment-source rows are merged and deduplicated.",
                      "required": ["limit", "query_sentinel_limit", "deployment_source_query_sentinel_limit", "returned_count", "observed_count", "observed_count_is_lower_bound", "content_observed_count", "content_observed_count_is_lower_bound", "deployment_source_observed_count", "deployment_source_observed_count_is_lower_bound", "truncated", "ordering", "k8s_select_candidate_sentinel_limit", "k8s_relationships_complete"],
                      "properties": {
                        "limit": {"type": "integer", "description": "Public response cap the Kubernetes rows were cut to (the service story item limit). Owner: impact trace_deployment_resources.go k8s resource result."},
                        "query_sentinel_limit": {"type": "integer", "description": "Service-repository content entity search limit, including the one-row truncation probe."},
                        "deployment_source_query_sentinel_limit": {"type": "integer", "description": "Per deployment-source repository entity scan limit, including the one-row truncation probe."},
                        "returned_count": {"type": "integer", "description": "Kubernetes rows returned in this response after the public response cap. Owner: impact trace_deployment_resources.go k8s resource result."},
                        "observed_count": {"type": "integer", "description": "Distinct Kubernetes rows observed across the bounded content and deployment-source inputs before the public response cap."},
                        "observed_count_is_lower_bound": {"type": "boolean", "description": "True when either bounded entity input reached its sentinel, so additional matching Kubernetes rows may exist."},
                        "content_observed_count": {"type": "integer", "description": "Kubernetes rows observed from the content input. Owner: impact trace_deployment_resources.go k8s resource result."},
                        "content_observed_count_is_lower_bound": {"type": "boolean", "description": "True when the content input reached its sentinel. Owner: impact trace_deployment_resources.go k8s resource result."},
                        "deployment_source_observed_count": {"type": "integer", "description": "Kubernetes rows observed from the deployment-source input. Owner: impact trace_deployment_resources.go k8s resource result."},
                        "deployment_source_observed_count_is_lower_bound": {"type": "boolean", "description": "True when the deployment-source input reached its sentinel. Owner: impact trace_deployment_resources.go k8s resource result."},
                        "truncated": {"type": "boolean", "description": "True when a lower-bound input fired or the merged rows were cut at the cap. Owner: impact trace_deployment_resources.go k8s resource result."},
                        "ordering": {"type": "array", "description": "Deterministic row ordering (repo_id, relative_path, entity_id). Owner: impact trace_deployment_resources.go k8s resource result.", "items": {"type": "string"}},
                        "k8s_select_candidate_sentinel_limit": {"type": "integer", "description": "Directed SELECTS candidate-scan limit, including the one-row truncation probe (issue #5363). Always present."},
                        "k8s_relationships_complete": {"type": "boolean", "description": "Always present. False only when the directed SELECTS candidate scan hit the repository entity ceiling (issue #5363 follow-up #5367), so some selector-matching Services may be missing from the surfaced pool; true otherwise."},
                        "k8s_relationships_incomplete_reason": {"type": "string", "description": "Machine-readable reason paired with k8s_relationships_complete=false. Present only when the candidate scan truncated.", "enum": ["k8s_select_candidate_pool_truncated"]}
                      }
                    },`
