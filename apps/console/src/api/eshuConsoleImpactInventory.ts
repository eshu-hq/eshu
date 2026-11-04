import type { EshuApiClient, EshuApiRequestOptions } from "./client";
import { EshuEnvelopeError } from "./envelope";
import type { EshuTruth } from "./envelope";
import { verifyEchoedProfile, type DetectionProfile } from "./impactDetectionProfile";

// Grouped impact inventory: GET /api/v0/supply-chain/impact/inventory. One
// bounded read that answers "which services (or workloads, or environments)
// carry the most vulnerability impact" without paging through every finding.

export type InventoryDimension = "service_id" | "workload_id" | "environment";

export type InventoryProfile = DetectionProfile;

// InventoryCompleteness is the closed per-bucket completeness set. It is not the
// six-state finding readiness envelope.
export type InventoryCompleteness =
  | "complete"
  | "partial"
  | "unattributed"
  | "read_model_unavailable";

export interface InventoryBucket {
  readonly dimension: string;
  readonly value: string;
  readonly unattributed: boolean;
  readonly count: number;
  readonly byImpactStatus: Readonly<Record<string, number>>;
  readonly bySeverity: Readonly<Record<string, number>>;
  readonly maxPriorityScore: number | null;
  readonly completeness: InventoryCompleteness;
  readonly missingEvidence: readonly string[];
}

export interface InventoryPage {
  readonly buckets: readonly InventoryBucket[];
  readonly count: number;
  readonly limit: number;
  readonly offset: number;
  readonly groupBy: string;
  // detectionProfile is the profile the server echoed; the loader rejects a
  // page whose echo differs from the request.
  readonly detectionProfile: string;
  readonly distinctFindings: number;
  readonly truncated: boolean;
  readonly nextOffset: number | null;
  readonly truth: EshuTruth | null;
}

export interface InventoryQuery {
  readonly groupBy: InventoryDimension;
  readonly profile: InventoryProfile;
  readonly limit?: number;
  readonly offset?: number;
}

export const inventoryDefaultLimit = 50;

const completenessValues: ReadonlySet<string> = new Set([
  "complete",
  "partial",
  "unattributed",
  "read_model_unavailable",
]);

function asRecord(value: unknown): Readonly<Record<string, unknown>> | null {
  return value !== null && typeof value === "object" && !Array.isArray(value)
    ? (value as Readonly<Record<string, unknown>>)
    : null;
}

function asNumber(value: unknown, fallback: number): number {
  return typeof value === "number" && Number.isFinite(value) ? value : fallback;
}

function asText(value: unknown): string {
  return typeof value === "string" ? value : "";
}

function countMap(value: unknown): Readonly<Record<string, number>> {
  const source = asRecord(value);
  if (source === null) return {};
  const out: Record<string, number> = {};
  for (const [key, count] of Object.entries(source)) {
    if (typeof count === "number" && Number.isFinite(count)) out[key] = count;
  }
  return out;
}

function parseCompleteness(raw: unknown, unattributed: boolean): InventoryCompleteness {
  // An unattributed bucket is never complete, whatever the wire says.
  if (unattributed) return "unattributed";
  const text = asText(raw);
  // An unknown state reads as partial so a new server value cannot look safe.
  return completenessValues.has(text) ? (text as InventoryCompleteness) : "partial";
}

function parseBucket(raw: unknown): InventoryBucket | null {
  const item = asRecord(raw);
  if (item === null) return null;
  const unattributed = item.unattributed === true;
  const score = item.max_priority_score;
  return {
    dimension: asText(item.dimension),
    value: asText(item.value),
    unattributed,
    count: asNumber(item.count, 0),
    byImpactStatus: countMap(item.by_impact_status),
    bySeverity: countMap(item.by_severity),
    maxPriorityScore: typeof score === "number" && Number.isFinite(score) ? score : null,
    completeness: parseCompleteness(item.completeness, unattributed),
    missingEvidence: Array.isArray(item.missing_evidence)
      ? item.missing_evidence.filter((entry): entry is string => typeof entry === "string")
      : [],
  };
}

// sortInventoryBuckets orders groups by highest priority first, then by finding
// count, then by value for a stable order. Buckets without a score sort last.
export function sortInventoryBuckets(
  buckets: readonly InventoryBucket[],
): readonly InventoryBucket[] {
  return [...buckets].sort((a, b) => {
    const scoreA = a.maxPriorityScore ?? Number.NEGATIVE_INFINITY;
    const scoreB = b.maxPriorityScore ?? Number.NEGATIVE_INFINITY;
    if (scoreA !== scoreB) return scoreB > scoreA ? 1 : -1;
    if (a.count !== b.count) return b.count - a.count;
    return a.value.localeCompare(b.value);
  });
}

const dimensionNoun: Readonly<Record<InventoryDimension, string>> = {
  service_id: "service",
  workload_id: "workload",
  environment: "environment",
};

export function inventoryDimensionNoun(dimension: InventoryDimension): string {
  return dimensionNoun[dimension];
}

// inventoryMultiGroupNote explains why group counts can add up to more than the
// number of distinct findings: a finding that touches several groups is counted
// in each of them. It returns null when the counts do not overlap.
export function inventoryMultiGroupNote(
  buckets: readonly InventoryBucket[],
  distinctFindings: number,
  dimension: InventoryDimension,
): string | null {
  const total = buckets.reduce((sum, item) => sum + item.count, 0);
  if (total <= distinctFindings) return null;
  const noun = inventoryDimensionNoun(dimension);
  return (
    `The group counts add up to ${total}, but there are only ${distinctFindings} distinct findings. ` +
    `A finding that touches several ${noun}s is counted in each ${noun}.`
  );
}

// loadImpactInventory reads one page of grouped impact inventory. An envelope
// error is thrown so the caller never renders an empty page as "no impact".
export async function loadImpactInventory(
  client: EshuApiClient,
  query: InventoryQuery,
  options: EshuApiRequestOptions = {},
): Promise<InventoryPage> {
  const params = new URLSearchParams({
    group_by: query.groupBy,
    profile: query.profile,
    limit: String(query.limit ?? inventoryDefaultLimit),
  });
  if (query.offset !== undefined && query.offset > 0) params.set("offset", String(query.offset));
  const env = await client.get<Record<string, unknown>>(
    `/api/v0/supply-chain/impact/inventory?${params.toString()}`,
    options,
  );
  if (env.error) throw new EshuEnvelopeError(env.error);
  const data = env.data ?? {};
  // The inventory always echoes its profile; a missing or different echo is an
  // error so a precise answer is never shown as the comprehensive one.
  const detectionProfile = verifyEchoedProfile(query.profile, data.detection_profile, true);
  const buckets = (Array.isArray(data.buckets) ? data.buckets : [])
    .map(parseBucket)
    .filter((item): item is InventoryBucket => item !== null);
  const nextOffset = data.next_offset;
  return {
    buckets,
    count: asNumber(data.count, buckets.length),
    limit: asNumber(data.limit, query.limit ?? inventoryDefaultLimit),
    offset: asNumber(data.offset, query.offset ?? 0),
    groupBy: asText(data.group_by) || query.groupBy,
    detectionProfile: detectionProfile ?? query.profile,
    distinctFindings: asNumber(data.distinct_findings, 0),
    truncated: data.truncated === true,
    nextOffset: typeof nextOffset === "number" && Number.isFinite(nextOffset) ? nextOffset : null,
    truth: env.truth,
  };
}
