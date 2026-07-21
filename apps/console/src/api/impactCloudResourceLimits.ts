import {
  boundedCollectionGraphAccounting,
  normalizeBoundedCollectionLimits,
} from "./impactBoundedCollectionLimits";
import type { CloudResourceLimits } from "./impactReviewTypes";

export function normalizeCloudResourceLimits(
  value: unknown,
  expectedReturnedCount: number,
): CloudResourceLimits | null {
  const resourceLimits = normalizeBoundedCollectionLimits(value, expectedReturnedCount);
  if (resourceLimits === null || !isRecord(value)) return null;

  const observationCount = nonNegativeInteger(value.observation_count);
  const observationCountIsLowerBound = booleanValue(value.observation_count_is_lower_bound);
  const observationLimit = positiveInteger(value.observation_limit);
  const observationQuerySentinelLimit = positiveInteger(value.observation_query_sentinel_limit);
  if (
    observationCount === null ||
    observationCountIsLowerBound === null ||
    observationLimit === null ||
    observationQuerySentinelLimit === null ||
    observationQuerySentinelLimit !== observationLimit + 1 ||
    observationCount < resourceLimits.observedCount ||
    (!observationCountIsLowerBound && observationCount > observationLimit) ||
    (observationCountIsLowerBound && !resourceLimits.truncated)
  ) {
    return null;
  }
  return {
    ...resourceLimits,
    observationCount,
    observationCountIsLowerBound,
    observationLimit,
    observationQuerySentinelLimit,
  };
}

export function cloudResourceGraphAccounting(
  limits: CloudResourceLimits | null,
  options: Parameters<typeof boundedCollectionGraphAccounting>[1],
): ReturnType<typeof boundedCollectionGraphAccounting> {
  const accounting = boundedCollectionGraphAccounting(limits, options);
  if (limits === null || !limits.truncated) {
    return accounting;
  }
  if (limits.observedCount > limits.returnedCount) {
    return limits.observationCountIsLowerBound
      ? {
          ...accounting,
          limitations: [
            ...accounting.limitations,
            `cloud-resource relationship-observation count is a lower bound at ${limits.observationCount}; additional observations may exist`,
          ],
        }
      : accounting;
  }
  if (!limits.observedCountIsLowerBound) return accounting;

  return {
    limitations: [
      limits.observationCountIsLowerBound
        ? "cloud-resource relationship observations truncated upstream; additional observations or resource identities may be omitted, but their count is unknown"
        : "cloud-resource identity coverage is a lower bound; additional resource identities may be omitted, but their count is unknown",
    ],
    omittedEdges: 0,
    omittedNodes: 0,
    truncated: true,
  };
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
