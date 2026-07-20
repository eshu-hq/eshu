import type { BoundedCollectionLimits } from "./impactReviewTypes";

export interface BoundedCollectionGraphAccounting {
  readonly limitations: readonly string[];
  readonly omittedEdges: number;
  readonly omittedNodes: number;
  readonly truncated: boolean;
}

interface CollectionAccountingOptions {
  readonly edgeMultiplier: number;
  readonly family: string;
  readonly familyPlural?: string;
  readonly item: string;
  readonly missingMetadataLimitation: string;
  readonly nodeMultiplier: number;
}

export function normalizeBoundedCollectionLimits(
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

export function boundedCollectionGraphAccounting(
  limits: BoundedCollectionLimits | null,
  options: CollectionAccountingOptions,
): BoundedCollectionGraphAccounting {
  if (limits === null) {
    return {
      limitations: [options.missingMetadataLimitation],
      omittedEdges: 0,
      omittedNodes: 0,
      truncated: false,
    };
  }
  if (!limits.truncated) {
    return { limitations: [], omittedEdges: 0, omittedNodes: 0, truncated: false };
  }
  const omitted = Math.max(0, limits.observedCount - limits.returnedCount);
  const observed = limits.observedCountIsLowerBound
    ? `at least ${limits.observedCount}`
    : String(limits.observedCount);
  if (limits.observedCountIsLowerBound && omitted === 0) {
    return {
      limitations: [
        `${options.family} input truncated upstream; showing ${limits.returnedCount} of ${observed} observed ${limits.observedCount === 1 ? options.family : (options.familyPlural ?? plural(options.family, limits.observedCount))}; additional ${plural(options.item, 2)} may exist, but their count is unknown`,
      ],
      omittedEdges: 0,
      omittedNodes: 0,
      truncated: true,
    };
  }
  const missing = limits.observedCountIsLowerBound ? `at least ${omitted}` : String(omitted);
  return {
    limitations: [
      `${options.family} input truncated upstream; showing ${limits.returnedCount} of ${observed} observed ${limits.observedCount === 1 ? options.family : (options.familyPlural ?? plural(options.family, limits.observedCount))}; ${missing} ${plural(options.item, omitted)} ${omitted === 1 ? "was" : "were"} not returned`,
    ],
    omittedEdges: omitted * options.edgeMultiplier,
    omittedNodes: omitted * options.nodeMultiplier,
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
