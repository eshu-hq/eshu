// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package schema

const boundedCollectionLimits = `{
  "type": "object",
  "description": "Bounded-collection completeness metadata. Owner: querycontract/story_collection_helpers.go BoundedCollectionMetadata.",
  "required": ["limit", "query_sentinel_limit", "returned_count", "observed_count", "observed_count_is_lower_bound", "truncated", "ordering"],
  "properties": {
    "limit": {"type": "integer", "description": "Requested row limit. Owner: querycontract/story_collection_helpers.go BoundedCollectionMetadata limit."},
    "query_sentinel_limit": {"type": "integer", "description": "Rows the reader asked for (limit + 1 sentinel) to detect truncation. Owner: querycontract/story_collection_helpers.go BoundedCollectionMetadata queryLimit."},
    "returned_count": {"type": "integer", "description": "Rows returned in this block. Owner: querycontract/story_collection_helpers.go BoundedCollectionMetadata returned."},
    "observed_count": {"type": "integer", "description": "Rows observed by the reader. Owner: querycontract/story_collection_helpers.go BoundedCollectionMetadata observed."},
    "observed_count_is_lower_bound": {"type": "boolean", "description": "True when truncation cut the observation short, so observed_count is a lower bound. Owner: querycontract/story_collection_helpers.go BoundedCollectionMetadata (reports truncated)."},
    "truncated": {"type": "boolean", "description": "True when more rows exist past this block. Owner: querycontract/story_collection_helpers.go BoundedCollectionMetadata truncated."},
    "ordering": {"type": "array", "items": {"type": "string"}, "description": "Key order the rows are served in. Owner: querycontract/story_collection_helpers.go BoundedCollectionMetadata ordering."}
  }
}`

// ImpactRuntimeTopologyLimits is the shared OpenAPI schema fragment spliced
// into every path fragment that references it, so those routes document one
// identical shape rather than drifting copies.
const ImpactRuntimeTopologyLimits = `{
  "type": "object",
  "description": "Completeness metadata for bounded instance, direct RUNS_ON edge, and provisioned-platform reads. Owner: entity/workload_runtime_topology.go boundedCollectionMetadata over querycontract.BoundedCollectionMetadata.",
  "required": ["instances", "platform_edges", "provisioned_platforms"],
  "properties": {
    "instances": ` + boundedCollectionLimits + `,
    "platform_edges": ` + boundedCollectionLimits + `,
    "provisioned_platforms": ` + boundedCollectionLimits + `
  }
}`
