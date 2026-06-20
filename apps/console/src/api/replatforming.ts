import type { EshuApiClient } from "./client";
import { EshuEnvelopeError, type EshuTruth } from "./envelope";
import type {
  ReplatformingBlastRadiusSummaryWire,
  ReplatformingImportCandidateWire,
  ReplatformingOwnerCandidateWire,
  ReplatformingOwnershipPacketWire,
  ReplatformingOwnershipWire,
  ReplatformingPlanItemWire,
  ReplatformingPlanWire,
  ReplatformingReadinessWire,
  ReplatformingRollupBucketWire,
  ReplatformingRollupsWire,
  ReplatformingSafetyGateWire,
  ReplatformingWaveSummaryWire
} from "./replatformingWireTypes";

export type ReplatformingScopeKind = "account" | "region" | "service" | "workload" | "repository" | "environment" | "resource";

export interface ReplatformingInput {
  readonly accountId?: string;
  readonly arn?: string;
  readonly environment?: string;
  readonly findingKinds?: readonly string[];
  readonly limit?: number;
  readonly offset?: number;
  readonly region?: string;
  readonly repoId?: string;
  readonly resourceId?: string;
  readonly scopeId?: string;
  readonly scopeKind?: ReplatformingScopeKind;
  readonly serviceName?: string;
  readonly workloadId?: string;
}

export interface ReplatformingReview {
  readonly input: Required<ReplatformingInput>;
  readonly ownership: ReplatformingSection<ReplatformingOwnership> | ReplatformingSkippedSection;
  readonly plan: ReplatformingSection<ReplatformingPlan> | ReplatformingSkippedSection;
  readonly rollups: ReplatformingSection<ReplatformingRollups> | ReplatformingSkippedSection;
}

export type ReplatformingSection<TData> =
  | { readonly data: TData; readonly source: string; readonly status: "ready"; readonly truth: EshuTruth | null }
  | { readonly error: string; readonly source: string; readonly status: "unavailable" };

export interface ReplatformingSkippedSection {
  readonly reason: string;
  readonly source: string;
  readonly status: "skipped";
}

export interface ReplatformingReadiness {
  readonly importReady: number;
  readonly needsReview: number;
  readonly refused: number;
}

export interface ReplatformingRollups {
  readonly dimensions: {
    readonly account: readonly ReplatformingRollupBucket[];
    readonly environment: readonly ReplatformingRollupBucket[];
    readonly service: readonly ReplatformingRollupBucket[];
  };
  readonly limit: number;
  readonly nextOffset: number | null;
  readonly offset: number;
  readonly readinessTotals: ReplatformingReadiness;
  readonly rollupFindingsCount: number;
  readonly sourceStateTotals: Record<string, number>;
  readonly story: string;
  readonly totalFindingsCount: number;
  readonly truncated: boolean;
}

export interface ReplatformingRollupBucket {
  readonly key: string;
  readonly readiness: ReplatformingReadiness;
  readonly sourceStateCounts: Record<string, number>;
  readonly total: number;
}

export interface ReplatformingPlan {
  readonly blastRadiusSummaries: readonly ReplatformingBlastRadiusSummary[];
  readonly items: readonly ReplatformingPlanItem[];
  readonly itemsCount: number;
  readonly limit: number;
  readonly nextOffset: number | null;
  readonly nonGoals: readonly string[];
  readonly offset: number;
  readonly readyImportCount: number;
  readonly refusedImportCount: number;
  readonly story: string;
  readonly totalFindingsCount: number;
  readonly truncated: boolean;
  readonly waveSummaries: readonly ReplatformingWaveSummary[];
}

export interface ReplatformingPlanItem {
  readonly blastRadiusGroup: string;
  readonly importCandidate: ReplatformingImportCandidate;
  readonly itemId: string;
  readonly ownerCandidates: readonly ReplatformingOwnerCandidate[];
  readonly provider: string;
  readonly resourceType: string;
  readonly safetyGate: ReplatformingSafetyGate;
  readonly sourceState: string;
  readonly stableId: string;
  readonly waveId: string;
}

export interface ReplatformingImportCandidate {
  readonly refusalReasons: readonly string[];
  readonly status: string;
}

export interface ReplatformingOwnerCandidate {
  readonly ambiguityReasons: readonly string[];
  readonly confidence: string;
  readonly kind: string;
  readonly value: string;
}

export interface ReplatformingSafetyGate {
  readonly outcome: string;
  readonly refusedActions: readonly string[];
  readonly reviewRequired: boolean;
}

export interface ReplatformingWaveSummary {
  readonly itemCount: number;
  readonly order: number;
  readonly waveId: string;
}

export interface ReplatformingBlastRadiusSummary {
  readonly groupId: string;
  readonly itemCount: number;
  readonly severity: string;
}

export interface ReplatformingOwnership {
  readonly ambiguousCount: number;
  readonly limit: number;
  readonly nextOffset: number | null;
  readonly offset: number;
  readonly packets: readonly ReplatformingOwnershipPacket[];
  readonly packetsCount: number;
  readonly rejectedCount: number;
  readonly story: string;
  readonly totalFindingsCount: number;
  readonly truncated: boolean;
  readonly unattributedCount: number;
}

export interface ReplatformingOwnershipPacket {
  readonly freshnessState: string;
  readonly itemId: string;
  readonly missingEvidence: readonly string[];
  readonly ownerCandidates: readonly ReplatformingOwnerCandidate[];
  readonly provider: string;
  readonly safetyGate: ReplatformingSafetyGate;
  readonly sourceState: string;
  readonly stableId: string;
}

const sourcePaths = {
  ownership: "/api/v0/replatforming/ownership-packets",
  plan: "/api/v0/replatforming/plans",
  rollups: "/api/v0/replatforming/rollups"
} as const;

export async function loadReplatformingReview(
  client: EshuApiClient,
  rawInput: ReplatformingInput
): Promise<ReplatformingReview> {
  const input = normalizeInput(rawInput);
  if (input.scopeId.length === 0 && input.accountId.length === 0) {
    const reason = "Add scope_id or account_id to load replatforming planning data.";
    return {
      input,
      ownership: skipped(sourcePaths.ownership, reason),
      plan: skipped(sourcePaths.plan, reason),
      rollups: skipped(sourcePaths.rollups, reason)
    };
  }
  const [rollups, plan, ownership] = await Promise.all([
    loadSection(sourcePaths.rollups, () => client.post<ReplatformingRollupsWire>(sourcePaths.rollups, baseBody(input)), normalizeRollups),
    loadSection(sourcePaths.plan, () => client.post<ReplatformingPlanWire>(sourcePaths.plan, planBody(input)), normalizePlan),
    loadSection(sourcePaths.ownership, () => client.post<ReplatformingOwnershipWire>(sourcePaths.ownership, baseBody(input)), normalizeOwnership)
  ]);
  return { input, ownership, plan, rollups };
}

async function loadSection<TWire, TData>(
  source: string,
  load: () => Promise<{
    readonly data: TWire | null;
    readonly error: { readonly code: string; readonly message: string } | null;
    readonly truth: EshuTruth | null;
  }>,
  normalize: (wire: TWire) => TData
): Promise<ReplatformingSection<TData>> {
  try {
    const envelope = await load();
    if (envelope.error !== null) throw new EshuEnvelopeError(envelope.error);
    if (envelope.data === null) throw new Error("Eshu envelope success response is missing data");
    return { data: normalize(envelope.data), source, status: "ready", truth: envelope.truth };
  } catch (error) {
    return { error: error instanceof Error ? error.message : "request failed", source, status: "unavailable" };
  }
}

function skipped(source: string, reason: string): ReplatformingSkippedSection {
  return { reason, source, status: "skipped" };
}

function baseBody(input: Required<ReplatformingInput>): Record<string, unknown> {
  const body: Record<string, unknown> = {};
  add(body, "scope_id", input.scopeId);
  add(body, "account_id", input.accountId);
  add(body, "region", input.region);
  add(body, "arn", input.arn);
  add(body, "resource_id", input.resourceId);
  if (input.findingKinds.length > 0) body.finding_kinds = input.findingKinds;
  body.limit = input.limit;
  body.offset = input.offset;
  return body;
}

function planBody(input: Required<ReplatformingInput>): Record<string, unknown> {
  const body = baseBody(input);
  body.scope_kind = input.scopeKind;
  add(body, "service_name", input.serviceName);
  add(body, "workload_id", input.workloadId);
  add(body, "repo_id", input.repoId);
  add(body, "environment", input.environment);
  return body;
}

function add(body: Record<string, unknown>, key: string, value: string): void {
  if (value.length > 0) body[key] = value;
}

function normalizeInput(input: ReplatformingInput): Required<ReplatformingInput> {
  return {
    accountId: str(input.accountId),
    arn: str(input.arn),
    environment: str(input.environment),
    findingKinds: (input.findingKinds ?? []).map((value) => str(value)).filter((value) => value.length > 0),
    limit: clampInt(input.limit, 100, 1, 500),
    offset: clampInt(input.offset, 0, 0, 100000),
    region: str(input.region),
    repoId: str(input.repoId),
    resourceId: str(input.resourceId),
    scopeId: str(input.scopeId),
    scopeKind: input.scopeKind ?? "account",
    serviceName: str(input.serviceName),
    workloadId: str(input.workloadId)
  };
}

function normalizeRollups(wire: ReplatformingRollupsWire): ReplatformingRollups {
  return {
    dimensions: {
      account: normalizeBuckets(wire.dimensions?.account),
      environment: normalizeBuckets(wire.dimensions?.environment),
      service: normalizeBuckets(wire.dimensions?.service)
    },
    limit: wire.limit ?? 100,
    nextOffset: wire.next_offset ?? null,
    offset: wire.offset ?? 0,
    readinessTotals: normalizeReadiness(wire.readiness_totals),
    rollupFindingsCount: wire.rollup_findings_count ?? 0,
    sourceStateTotals: wire.source_state_totals ?? {},
    story: str(wire.story),
    totalFindingsCount: wire.total_findings_count ?? 0,
    truncated: wire.truncated ?? false
  };
}

function normalizeBuckets(wire: readonly ReplatformingRollupBucketWire[] | undefined): ReplatformingRollupBucket[] {
  return (wire ?? []).map((bucket) => ({
    key: str(bucket.key, "unattributed"),
    readiness: normalizeReadiness(bucket.readiness),
    sourceStateCounts: bucket.source_state_counts ?? {},
    total: bucket.total ?? 0
  }));
}

function normalizeReadiness(wire: ReplatformingReadinessWire | undefined): ReplatformingReadiness {
  return {
    importReady: wire?.import_ready ?? 0,
    needsReview: wire?.needs_review ?? 0,
    refused: wire?.refused ?? 0
  };
}

function normalizePlan(wire: ReplatformingPlanWire): ReplatformingPlan {
  const plan = wire.plan;
  return {
    blastRadiusSummaries: (wire.blast_radius_summaries ?? []).map(normalizeBlastRadiusSummary),
    items: (plan?.items ?? []).map(normalizePlanItem),
    itemsCount: wire.items_count ?? plan?.items?.length ?? 0,
    limit: wire.limit ?? 100,
    nextOffset: wire.next_offset ?? null,
    nonGoals: plan?.non_goals ?? [],
    offset: wire.offset ?? 0,
    readyImportCount: wire.ready_import_count ?? 0,
    refusedImportCount: wire.refused_import_count ?? 0,
    story: str(wire.story),
    totalFindingsCount: wire.total_findings_count ?? 0,
    truncated: wire.truncated ?? false,
    waveSummaries: (wire.wave_summaries ?? []).map(normalizeWaveSummary)
  };
}

function normalizePlanItem(wire: ReplatformingPlanItemWire): ReplatformingPlanItem {
  return {
    blastRadiusGroup: str(wire.blast_radius_group),
    importCandidate: normalizeImportCandidate(wire.import_candidate),
    itemId: str(wire.item_id, "item"),
    ownerCandidates: (wire.owner_candidates ?? []).map(normalizeOwnerCandidate),
    provider: str(wire.provider),
    resourceType: str(wire.resource_type),
    safetyGate: normalizeSafetyGate(wire.safety_gate),
    sourceState: str(wire.source_state, "unknown"),
    stableId: str(wire.stable_id),
    waveId: str(wire.wave_id)
  };
}

function normalizeImportCandidate(wire: ReplatformingImportCandidateWire | undefined): ReplatformingImportCandidate {
  return {
    refusalReasons: wire?.refusal_reasons ?? [],
    status: str(wire?.status, "unknown")
  };
}

function normalizeOwnerCandidate(wire: ReplatformingOwnerCandidateWire): ReplatformingOwnerCandidate {
  return {
    ambiguityReasons: wire.ambiguity_reasons ?? [],
    confidence: str(wire.confidence),
    kind: str(wire.kind),
    value: str(wire.value)
  };
}

function normalizeSafetyGate(wire: ReplatformingSafetyGateWire | undefined): ReplatformingSafetyGate {
  return {
    outcome: str(wire?.outcome),
    refusedActions: wire?.refused_actions ?? [],
    reviewRequired: wire?.review_required ?? false
  };
}

function normalizeWaveSummary(wire: ReplatformingWaveSummaryWire): ReplatformingWaveSummary {
  return {
    itemCount: wire.item_count ?? 0,
    order: wire.order ?? 0,
    waveId: str(wire.wave_id)
  };
}

function normalizeBlastRadiusSummary(wire: ReplatformingBlastRadiusSummaryWire): ReplatformingBlastRadiusSummary {
  return {
    groupId: str(wire.group_id),
    itemCount: wire.item_count ?? 0,
    severity: str(wire.severity)
  };
}

function normalizeOwnership(wire: ReplatformingOwnershipWire): ReplatformingOwnership {
  return {
    ambiguousCount: wire.ambiguous_count ?? 0,
    limit: wire.limit ?? 100,
    nextOffset: wire.next_offset ?? null,
    offset: wire.offset ?? 0,
    packets: (wire.ownership_packets ?? []).map(normalizeOwnershipPacket),
    packetsCount: wire.packets_count ?? wire.ownership_packets?.length ?? 0,
    rejectedCount: wire.rejected_count ?? 0,
    story: str(wire.story),
    totalFindingsCount: wire.total_findings_count ?? 0,
    truncated: wire.truncated ?? false,
    unattributedCount: wire.unattributed_count ?? 0
  };
}

function normalizeOwnershipPacket(wire: ReplatformingOwnershipPacketWire): ReplatformingOwnershipPacket {
  return {
    freshnessState: str(wire.freshness?.state, "fresh"),
    itemId: str(wire.item_id, "packet"),
    missingEvidence: wire.missing_evidence ?? [],
    ownerCandidates: (wire.owner_candidates ?? []).map(normalizeOwnerCandidate),
    provider: str(wire.provider),
    safetyGate: normalizeSafetyGate(wire.safety_gate),
    sourceState: str(wire.source_state, "unknown"),
    stableId: str(wire.stable_id)
  };
}

function clampInt(value: number | undefined, fallback: number, min: number, max: number): number {
  if (value === undefined || !Number.isFinite(value)) return fallback;
  return Math.max(min, Math.min(max, Math.trunc(value)));
}

function str(...values: readonly (string | undefined)[]): string {
  return values.find((value) => value !== undefined && value.trim().length > 0)?.trim() ?? "";
}
