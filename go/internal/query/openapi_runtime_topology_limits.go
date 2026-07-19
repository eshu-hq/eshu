// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

const openAPIBoundedCollectionLimits = `{
  "type": "object",
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

const openAPIImpactRuntimeTopologyLimits = `{
  "type": "object",
  "description": "Completeness metadata for bounded instance, direct RUNS_ON edge, and provisioned-platform reads.",
  "required": ["instances", "platform_edges", "provisioned_platforms"],
  "properties": {
    "instances": ` + openAPIBoundedCollectionLimits + `,
    "platform_edges": ` + openAPIBoundedCollectionLimits + `,
    "provisioned_platforms": ` + openAPIBoundedCollectionLimits + `
  }
}`
