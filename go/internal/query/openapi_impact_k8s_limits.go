// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

const openAPIImpactK8sResourceLimits = `
                    "k8s_resource_limits": {
                      "type": "object",
                      "description": "Deterministic bound and sentinel completeness metadata for k8s_resources after content and deployment-source rows are merged and deduplicated.",
                      "properties": {
                        "limit": {"type": "integer"},
                        "query_sentinel_limit": {"type": "integer", "description": "Service-repository content entity search limit, including the one-row truncation probe."},
                        "deployment_source_query_sentinel_limit": {"type": "integer", "description": "Per deployment-source repository entity scan limit, including the one-row truncation probe."},
                        "returned_count": {"type": "integer"},
                        "observed_count": {"type": "integer", "description": "Distinct Kubernetes rows observed across the bounded content and deployment-source inputs before the public response cap."},
                        "observed_count_is_lower_bound": {"type": "boolean", "description": "True when either bounded entity input reached its sentinel, so additional matching Kubernetes rows may exist."},
                        "content_observed_count": {"type": "integer"},
                        "content_observed_count_is_lower_bound": {"type": "boolean"},
                        "deployment_source_observed_count": {"type": "integer"},
                        "deployment_source_observed_count_is_lower_bound": {"type": "boolean"},
                        "truncated": {"type": "boolean"},
                        "ordering": {"type": "array", "items": {"type": "string"}}
                      }
                    },`
