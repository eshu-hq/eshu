import type { BoundedCollectionLimits, RuntimeTopologyLimits } from "./impactReviewTypes";

interface RuntimeTopologyCounts {
  readonly instances: number;
  readonly platformEdges: number;
  readonly provisionedPlatforms: number;
}

export interface RuntimeTopologyGraphAccounting {
  readonly limitations: readonly string[];
  readonly omittedEdges: number;
  readonly omittedNodes: number;
  readonly truncated: boolean;
}

export function normalizeRuntimeTopologyLimits(
  value: unknown,
  counts: RuntimeTopologyCounts,
): RuntimeTopologyLimits | null {
  if (!isRecord(value)) return null;
  const instances = normalizeCollectionLimits(value.instances, counts.instances);
  const platformEdges = normalizeCollectionLimits(value.platform_edges, counts.platformEdges);
  const provisionedPlatforms = normalizeCollectionLimits(
    value.provisioned_platforms,
    counts.provisionedPlatforms,
  );
  if (instances === null || platformEdges === null || provisionedPlatforms === null) return null;
  return { instances, platformEdges, provisionedPlatforms };
}

export function runtimeTopologyGraphAccounting(
  limits: RuntimeTopologyLimits | null,
): RuntimeTopologyGraphAccounting {
  if (limits === null) {
    return {
      limitations: [
        "runtime-topology completeness unverified because collection metadata is unavailable",
      ],
      omittedEdges: 0,
      omittedNodes: 0,
      truncated: false,
    };
  }

  const instances = collectionAccounting(limits.instances, "runtime instance", "instance", 1, 1);
  const placements = collectionAccounting(
    limits.platformEdges,
    "direct placement",
    "relationship",
    0,
    1,
  );
  const provisioned = collectionAccounting(
    limits.provisionedPlatforms,
    "provisioned platform",
    "platform record",
    0,
    2,
  );
  return {
    limitations: [...instances.limitations, ...placements.limitations, ...provisioned.limitations],
    omittedEdges: instances.omittedEdges + placements.omittedEdges + provisioned.omittedEdges,
    omittedNodes: instances.omittedNodes + placements.omittedNodes + provisioned.omittedNodes,
    truncated: instances.truncated || placements.truncated || provisioned.truncated,
  };
}

function normalizeCollectionLimits(
  value: unknown,
  expectedReturnedCount: number,
): BoundedCollectionLimits | null {
  if (!isRecord(value)) return null;
  const limit = positiveInteger(value.limit);
  const observedCount = nonNegativeInteger(value.observed_count);
  const observedCountIsLowerBound = booleanValue(value.observed_count_is_lower_bound);
  const ordering = stringArray(value.ordering);
  const querySentinelLimit = positiveInteger(value.query_sentinel_limit);
  const returnedCount = nonNegativeInteger(value.returned_count);
  const truncated = booleanValue(value.truncated);
  if (
    limit === null ||
    observedCount === null ||
    observedCountIsLowerBound === null ||
    ordering === null ||
    querySentinelLimit === null ||
    returnedCount === null ||
    truncated === null
  ) {
    return null;
  }
  if (
    returnedCount !== expectedReturnedCount ||
    returnedCount > limit ||
    returnedCount > observedCount ||
    querySentinelLimit !== limit + 1 ||
    truncated !== (observedCountIsLowerBound || observedCount > returnedCount)
  ) {
    return null;
  }
  return {
    limit,
    observedCount,
    observedCountIsLowerBound,
    ordering,
    querySentinelLimit,
    returnedCount,
    truncated,
  };
}

function collectionAccounting(
  limits: BoundedCollectionLimits,
  family: string,
  item: string,
  nodeMultiplier: number,
  edgeMultiplier: number,
): RuntimeTopologyGraphAccounting {
  if (!limits.truncated) {
    return { limitations: [], omittedEdges: 0, omittedNodes: 0, truncated: false };
  }
  const omitted = Math.max(1, limits.observedCount - limits.returnedCount);
  const observed = limits.observedCountIsLowerBound
    ? `at least ${limits.observedCount}`
    : String(limits.observedCount);
  const missing = limits.observedCountIsLowerBound ? `at least ${omitted}` : String(omitted);
  return {
    limitations: [
      `${family} input truncated upstream; showing ${limits.returnedCount} of ${observed} observed ${plural(family, limits.observedCount)}; ${missing} ${plural(item, omitted)} ${omitted === 1 ? "was" : "were"} not returned`,
    ],
    omittedEdges: omitted * edgeMultiplier,
    omittedNodes: omitted * nodeMultiplier,
    truncated: true,
  };
}

function plural(value: string, count: number): string {
  return count === 1 ? value : `${value}s`;
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function nonNegativeInteger(value: unknown): number | null {
  return typeof value === "number" && Number.isInteger(value) && value >= 0 ? value : null;
}

function positiveInteger(value: unknown): number | null {
  const normalized = nonNegativeInteger(value);
  return normalized !== null && normalized > 0 ? normalized : null;
}

function booleanValue(value: unknown): boolean | null {
  return typeof value === "boolean" ? value : null;
}

function stringArray(value: unknown): readonly string[] | null {
  if (!Array.isArray(value)) return null;
  const normalized = value.map((item) => (typeof item === "string" ? item.trim() : ""));
  return normalized.length > 0 && normalized.every((item) => item.length > 0) ? normalized : null;
}
