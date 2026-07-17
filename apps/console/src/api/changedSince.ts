import type { EshuApiClient } from "./client";
import { EshuEnvelopeError, unwrapEnvelope, type EshuTruth } from "./envelope";

export type ChangedSinceMode = "repository" | "service";
export type ChangeClassification = "added" | "updated" | "unchanged" | "retired" | "superseded";

export interface ChangeCounts {
  readonly added: number;
  readonly updated: number;
  readonly unchanged: number;
  readonly retired: number;
  readonly superseded: number;
}

export interface ChangeSample {
  readonly factKind: string;
  readonly stableFactKey: string;
}

export type ChangeSamples = Record<ChangeClassification, readonly ChangeSample[]>;
export type ChangeTruncation = Record<ChangeClassification, boolean>;

export interface ChangedSinceCategory {
  readonly category: string;
  readonly changedCount: number;
  readonly counts: ChangeCounts;
  readonly samples: ChangeSamples;
  readonly truncated: ChangeTruncation;
  readonly unavailable: boolean;
}

export interface ChangedSincePageData {
  readonly categories: readonly ChangedSinceCategory[];
  readonly changedCount: number;
  readonly currentActiveGenerationId: string;
  readonly currentObservedAt: string | null;
  readonly mode: ChangedSinceMode;
  readonly sampleLimit: number;
  readonly scopeId: string;
  readonly scopeKind: string;
  readonly scopeLabel: string;
  readonly sinceGenerationId: string;
  readonly sinceObservedAt: string | null;
  readonly truth: EshuTruth;
  readonly unchangedCount: number;
  readonly unavailable: boolean;
  readonly unavailableReason: string;
}

export interface RepositoryChangedSinceQuery {
  readonly repository?: string;
  readonly sampleLimit?: number;
  readonly scopeId?: string;
  readonly sinceGenerationId?: string;
  readonly sinceObservedAt?: string;
}

export interface ServiceChangedSinceQuery {
  readonly sampleLimit?: number;
  readonly serviceId: string;
  readonly sinceGenerationId: string;
}

export interface GenerationLifecycleQuery {
  readonly collectorKind?: string;
  readonly generationId?: string;
  readonly limit?: number;
  readonly repository?: string;
  readonly scopeId?: string;
  readonly sourceSystem?: string;
  readonly status?: string;
}

export interface GenerationLifecycleLoadOptions {
  readonly signal?: AbortSignal;
}

export interface GenerationLifecycleRow {
  readonly activatedAt: string | null;
  readonly collectorKind: string;
  readonly currentActiveGenerationId: string;
  readonly freshnessHint: string;
  readonly generationId: string;
  readonly ingestedAt: string | null;
  readonly isActive: boolean;
  readonly latestFailure: string;
  readonly observedAt: string | null;
  readonly queueDeadLetter: number;
  readonly queueFailed: number;
  readonly queueInFlight: number;
  readonly queueOutstanding: number;
  readonly queueRetrying: number;
  readonly queueSucceeded: number;
  readonly queueTotal: number;
  readonly scopeId: string;
  readonly scopeKind: string;
  readonly sourceSystem: string;
  readonly status: string;
  readonly supersededAt: string | null;
  readonly triggerKind: string;
}

export interface GenerationLifecyclePage {
  readonly count: number;
  readonly generations: readonly GenerationLifecycleRow[];
  readonly limit: number;
  readonly truncated: boolean;
  readonly truth: EshuTruth;
}

interface WireChangedSinceResponse {
  readonly categories?: readonly WireChangedSinceCategory[];
  readonly current_active_generation_id?: string;
  readonly current_observed_at?: string;
  readonly repository?: string;
  readonly sample_limit?: number;
  readonly scope_id?: string;
  readonly scope_kind?: string;
  readonly service_id?: string;
  readonly since_generation_id?: string;
  readonly since_observed_at?: string;
  readonly unavailable?: boolean;
  readonly unavailable_reason?: string;
}

interface WireChangedSinceCategory {
  readonly category?: string;
  readonly counts?: Partial<Record<ChangeClassification, number>>;
  readonly samples?: Partial<Record<ChangeClassification, readonly WireChangeSample[]>>;
  readonly truncated?: Partial<Record<ChangeClassification, boolean>>;
  readonly unavailable?: boolean;
}

interface WireChangeSample {
  readonly fact_kind?: string;
  readonly stable_fact_key?: string;
}

interface WireGenerationLifecycleResponse {
  readonly count?: number;
  readonly generations?: readonly WireGenerationLifecycleRow[];
  readonly limit?: number;
  readonly truncated?: boolean;
}

interface WireGenerationLifecycleRow {
  readonly activated_at?: string;
  readonly collector_kind?: string;
  readonly current_active_generation_id?: string;
  readonly freshness_hint?: string;
  readonly generation_id?: string;
  readonly ingested_at?: string;
  readonly is_active?: boolean;
  readonly latest_failure?: WireGenerationLifecycleFailure | null;
  readonly observed_at?: string;
  readonly queue_status?: Partial<
    Record<
      "dead_letter" | "failed" | "in_flight" | "outstanding" | "retrying" | "succeeded" | "total",
      number
    >
  >;
  readonly scope_id?: string;
  readonly scope_kind?: string;
  readonly source_system?: string;
  readonly status?: string;
  readonly superseded_at?: string;
  readonly trigger_kind?: string;
}

interface WireGenerationLifecycleFailure {
  readonly failure_class?: string;
  readonly failure_message?: string;
  readonly observed_at?: string;
  readonly work_item_status?: string;
}

const classifications: readonly ChangeClassification[] = [
  "added",
  "updated",
  "unchanged",
  "retired",
  "superseded",
];

export async function loadRepositoryChangedSince(
  client: EshuApiClient,
  query: RepositoryChangedSinceQuery,
): Promise<ChangedSincePageData> {
  const envelope = await client.get<WireChangedSinceResponse>(repositoryChangedSincePath(query));
  if (envelope.error) throw new EshuEnvelopeError(envelope.error);
  const { data, truth } = unwrapEnvelope(envelope);
  return normalizeChangedSince(data, truth, "repository");
}

export async function loadServiceChangedSince(
  client: EshuApiClient,
  query: ServiceChangedSinceQuery,
): Promise<ChangedSincePageData> {
  const envelope = await client.get<WireChangedSinceResponse>(serviceChangedSincePath(query));
  if (envelope.error) throw new EshuEnvelopeError(envelope.error);
  const { data, truth } = unwrapEnvelope(envelope);
  return normalizeChangedSince(data, truth, "service");
}

export async function loadGenerationLifecycle(
  client: EshuApiClient,
  query: GenerationLifecycleQuery,
  options: GenerationLifecycleLoadOptions = {},
): Promise<GenerationLifecyclePage> {
  const envelope = await client.get<WireGenerationLifecycleResponse>(
    generationLifecyclePath(query),
    { signal: options.signal },
  );
  if (envelope.error) throw new EshuEnvelopeError(envelope.error);
  const { data, truth } = unwrapEnvelope(envelope);
  return {
    count: numberOrZero(data.count),
    generations: (data.generations ?? []).map(normalizeGeneration),
    limit: numberOrZero(data.limit),
    truncated: data.truncated === true,
    truth,
  };
}

export function repositoryChangedSincePath(query: RepositoryChangedSinceQuery): string {
  if ((query.repository?.trim() ?? "") !== "" && (query.scopeId?.trim() ?? "") !== "") {
    throw new Error("repository and scopeId are mutually exclusive");
  }
  const params = new URLSearchParams();
  addParam(params, "repository", query.repository);
  addParam(params, "scope_id", query.scopeId);
  addParam(params, "since_generation_id", query.sinceGenerationId);
  addParam(params, "since_observed_at", query.sinceObservedAt);
  addNumberParam(params, "sample_limit", query.sampleLimit);
  return `/api/v0/freshness/changed-since?${params.toString()}`;
}

export function serviceChangedSincePath(query: ServiceChangedSinceQuery): string {
  const params = new URLSearchParams();
  addParam(params, "service_id", query.serviceId);
  addParam(params, "since_generation_id", query.sinceGenerationId);
  addNumberParam(params, "sample_limit", query.sampleLimit);
  return `/api/v0/freshness/services/changed-since?${params.toString()}`;
}

export function generationLifecyclePath(query: GenerationLifecycleQuery): string {
  const params = new URLSearchParams();
  addParam(params, "scope_id", query.scopeId);
  addParam(params, "repository", query.repository);
  addParam(params, "collector_kind", query.collectorKind);
  addParam(params, "source_system", query.sourceSystem);
  addParam(params, "generation_id", query.generationId);
  addParam(params, "status", query.status);
  addNumberParam(params, "limit", query.limit);
  return `/api/v0/freshness/generations?${params.toString()}`;
}

function normalizeChangedSince(
  data: WireChangedSinceResponse,
  truth: EshuTruth,
  mode: ChangedSinceMode,
): ChangedSincePageData {
  const categories = (data.categories ?? []).map(normalizeCategory);
  return {
    categories,
    changedCount: categories.reduce((sum, category) => sum + category.changedCount, 0),
    currentActiveGenerationId: str(data.current_active_generation_id),
    currentObservedAt: strOrNull(data.current_observed_at),
    mode,
    sampleLimit: numberOrZero(data.sample_limit),
    scopeId: str(data.scope_id),
    scopeKind: mode === "service" ? "service" : str(data.scope_kind),
    scopeLabel: scopeLabel(data, mode),
    sinceGenerationId: str(data.since_generation_id),
    sinceObservedAt: strOrNull(data.since_observed_at),
    truth,
    unchangedCount: categories.reduce((sum, category) => sum + category.counts.unchanged, 0),
    unavailable: data.unavailable === true,
    unavailableReason: str(data.unavailable_reason),
  };
}

function normalizeCategory(category: WireChangedSinceCategory): ChangedSinceCategory {
  const counts = normalizeCounts(category.counts);
  return {
    category: str(category.category),
    changedCount: counts.added + counts.updated + counts.retired + counts.superseded,
    counts,
    samples: normalizeSamples(category.samples),
    truncated: normalizeTruncated(category.truncated),
    unavailable: category.unavailable === true,
  };
}

function normalizeCounts(counts: WireChangedSinceCategory["counts"]): ChangeCounts {
  return {
    added: numberOrZero(counts?.added),
    updated: numberOrZero(counts?.updated),
    unchanged: numberOrZero(counts?.unchanged),
    retired: numberOrZero(counts?.retired),
    superseded: numberOrZero(counts?.superseded),
  };
}

function normalizeSamples(samples: WireChangedSinceCategory["samples"]): ChangeSamples {
  return classifications.reduce(
    (acc, classification) => {
      acc[classification] = (samples?.[classification] ?? []).map(normalizeSample);
      return acc;
    },
    {} as Record<ChangeClassification, readonly ChangeSample[]>,
  );
}

function normalizeSample(sample: WireChangeSample): ChangeSample {
  return {
    factKind: str(sample.fact_kind),
    stableFactKey: str(sample.stable_fact_key),
  };
}

function normalizeTruncated(truncated: WireChangedSinceCategory["truncated"]): ChangeTruncation {
  return Object.fromEntries(
    classifications.map((classification) => [classification, truncated?.[classification] === true]),
  ) as ChangeTruncation;
}

function normalizeGeneration(row: WireGenerationLifecycleRow): GenerationLifecycleRow {
  const queue = row.queue_status ?? {};
  return {
    activatedAt: strOrNull(row.activated_at),
    collectorKind: str(row.collector_kind),
    currentActiveGenerationId: str(row.current_active_generation_id),
    freshnessHint: str(row.freshness_hint),
    generationId: str(row.generation_id),
    ingestedAt: strOrNull(row.ingested_at),
    isActive: row.is_active === true,
    latestFailure: generationFailureMessage(row.latest_failure),
    observedAt: strOrNull(row.observed_at),
    queueDeadLetter: numberOrZero(queue.dead_letter),
    queueFailed: numberOrZero(queue.failed),
    queueInFlight: numberOrZero(queue.in_flight),
    queueOutstanding: numberOrZero(queue.outstanding),
    queueRetrying: numberOrZero(queue.retrying),
    queueSucceeded: numberOrZero(queue.succeeded),
    queueTotal: numberOrZero(queue.total),
    scopeId: str(row.scope_id),
    scopeKind: str(row.scope_kind),
    sourceSystem: str(row.source_system),
    status: str(row.status),
    supersededAt: strOrNull(row.superseded_at),
    triggerKind: str(row.trigger_kind),
  };
}

function generationFailureMessage(
  failure: WireGenerationLifecycleFailure | null | undefined,
): string {
  if (!failure) return "";
  return str(failure.failure_message) || str(failure.failure_class);
}

function scopeLabel(data: WireChangedSinceResponse, mode: ChangedSinceMode): string {
  if (mode === "service") return str(data.service_id);
  return str(data.repository) || str(data.scope_id);
}

function addParam(params: URLSearchParams, name: string, value: string | undefined): void {
  const trimmed = value?.trim() ?? "";
  if (trimmed.length > 0) params.set(name, trimmed);
}

function addNumberParam(params: URLSearchParams, name: string, value: number | undefined): void {
  if (typeof value === "number" && Number.isFinite(value)) params.set(name, String(value));
}

function str(value: string | undefined): string {
  return value?.trim() ?? "";
}

function strOrNull(value: string | undefined): string | null {
  const trimmed = str(value);
  return trimmed.length > 0 ? trimmed : null;
}

function numberOrZero(value: number | undefined): number {
  return typeof value === "number" && Number.isFinite(value) ? value : 0;
}
