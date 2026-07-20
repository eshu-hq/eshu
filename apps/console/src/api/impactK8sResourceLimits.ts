import type { KubernetesResourceLimits } from "./impactReviewTypes";

/**
 * Validates the merged Kubernetes evidence bound. The aggregate count can be
 * smaller than the constituent sum because the server deduplicates rows.
 */
export function normalizeK8sResourceLimits(
  value: unknown,
  expectedReturnedCount: number,
): KubernetesResourceLimits | null {
  if (!isRecord(value)) return null;
  const limit = positiveInteger(value.limit);
  const querySentinelLimit = positiveInteger(value.query_sentinel_limit);
  const deploymentSourceQuerySentinelLimit = positiveInteger(
    value.deployment_source_query_sentinel_limit,
  );
  const returnedCount = nonNegativeInteger(value.returned_count);
  const observedCount = nonNegativeInteger(value.observed_count);
  const observedCountIsLowerBound = booleanValue(value.observed_count_is_lower_bound);
  const contentObservedCount = nonNegativeInteger(value.content_observed_count);
  const contentObservedCountIsLowerBound = booleanValue(
    value.content_observed_count_is_lower_bound,
  );
  const deploymentSourceObservedCount = nonNegativeInteger(value.deployment_source_observed_count);
  const deploymentSourceObservedCountIsLowerBound = booleanValue(
    value.deployment_source_observed_count_is_lower_bound,
  );
  const truncated = booleanValue(value.truncated);
  const ordering = stringArray(value.ordering);
  if (
    limit === null ||
    querySentinelLimit === null ||
    deploymentSourceQuerySentinelLimit === null ||
    returnedCount === null ||
    observedCount === null ||
    observedCountIsLowerBound === null ||
    contentObservedCount === null ||
    contentObservedCountIsLowerBound === null ||
    deploymentSourceObservedCount === null ||
    deploymentSourceObservedCountIsLowerBound === null ||
    truncated === null ||
    ordering === null
  )
    return null;

  const constituentLowerBound =
    contentObservedCountIsLowerBound || deploymentSourceObservedCountIsLowerBound;
  if (
    returnedCount !== expectedReturnedCount ||
    returnedCount > limit ||
    returnedCount > observedCount ||
    observedCount > contentObservedCount + deploymentSourceObservedCount ||
    querySentinelLimit !== limit + 1 ||
    observedCountIsLowerBound !== constituentLowerBound ||
    truncated !== (observedCountIsLowerBound || observedCount > returnedCount)
  )
    return null;

  return {
    contentObservedCount,
    contentObservedCountIsLowerBound,
    deploymentSourceObservedCount,
    deploymentSourceObservedCountIsLowerBound,
    deploymentSourceQuerySentinelLimit,
    limit,
    observedCount,
    observedCountIsLowerBound,
    ordering,
    querySentinelLimit,
    returnedCount,
    truncated,
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

function stringArray(value: unknown): readonly string[] | null {
  if (!Array.isArray(value)) return null;
  const normalized = value.map((item) => (typeof item === "string" ? item.trim() : ""));
  return normalized.length > 0 && normalized.every((item) => item.length > 0) ? normalized : null;
}
