// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package schema

const boundedCollectionLimits = `{
  "type": "object",
  "required": ["limit", "query_sentinel_limit", "returned_count", "observed_count", "observed_count_is_lower_bound", "truncated", "ordering"],
  "properties": {
    "limit": {"type": "integer"},
    "query_sentinel_limit": {"type": "integer"},
    "returned_count": {"type": "integer"},
    "observed_count": {"type": "integer"},
    "observed_count_is_lower_bound": {"type": "boolean"},
    "truncated": {"type": "boolean"},
    "ordering": {"type": "array", "items": {"type": "string"}}
  }
}`

// ImpactRuntimeTopologyLimits is the shared OpenAPI schema fragment spliced
// into every path fragment that references it, so those routes document one
// identical shape rather than drifting copies.
const ImpactRuntimeTopologyLimits = `{
  "type": "object",
  "description": "Completeness metadata for bounded instance, direct RUNS_ON edge, and provisioned-platform reads.",
  "required": ["instances", "platform_edges", "provisioned_platforms"],
  "properties": {
    "instances": ` + boundedCollectionLimits + `,
    "platform_edges": ` + boundedCollectionLimits + `,
    "provisioned_platforms": ` + boundedCollectionLimits + `
  }
}`
