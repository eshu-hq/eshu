// statusOverview.ts
// Bounded operator-status read model for the console Status page (issue #3400).
// It joins three already-bounded status surfaces — collector-readiness (the
// generated collector roster + schedule), index-status (coordinator collector
// instances, per-collector backpressure, and the work queue), and
// freshness-causality (pipeline generation lifecycle) — into a single glanceable
// "what is Eshu doing right now" view. Every backing call is one of the bounded
// status read paths (#3368/#3373/#3390); this module never issues full-table
// catalog or aggregate reads, so it holds the few-seconds SLA at 900-repo scale.
//
// Each sub-read fails closed: a failed collector-readiness read yields an
// explicit unavailable overview rather than throwing, and the optional
// index-status / ingester / freshness reads degrade to neutral defaults so the
// page shows "—" instead of fabricated progress.

import type { EshuApiClient } from "./client";

// StatusCollectorState is the live catch-up classification the issue calls for:
// stalled (red), catching up (amber), or up to date (teal).
export type StatusCollectorState = "stalled" | "catching_up" | "up_to_date";

// StatusPipelineState mirrors the freshness-causality overall state used to
// describe the ingest -> reduce -> project -> query pipeline.
export type StatusPipelineState = "fresh" | "building" | "stale" | "unknown";

export interface StatusCollectorRow {
  readonly instanceId: string;
  readonly kind: string;
  readonly displayName: string;
  readonly schedule: string;
  readonly state: StatusCollectorState;
  // progress is the catch-up fraction in [0,1]: 1 means up to date.
  readonly progress: number;
  readonly workItems: number;
  // volume is the facts/edges processed by this collector, when an ingester
  // fact count is available; null when the source carries no count.
  readonly volume: number | null;
  readonly lastRunLabel: string;
}

export interface StatusPipeline {
  readonly state: StatusPipelineState;
  readonly activeGenerations: number;
  readonly pendingGenerations: number;
  readonly pendingProjection: number;
  readonly deadLetters: number;
}

export interface StatusOverview {
  // indexingPercent is the hero number: ~100 when caught up, lower under live
  // backlog. Always within [0,100].
  readonly indexingPercent: number;
  readonly indexStatusLabel: string;
  readonly repositories: number;
  readonly collectors: readonly StatusCollectorRow[];
  readonly pipeline: StatusPipeline;
  readonly queue: StatusQueue;
  readonly provenance: "live" | "unavailable";
}

export interface StatusQueue {
  readonly outstanding: number;
  readonly inFlight: number;
  readonly deadLetter: number;
  readonly succeeded: number;
}

// ---- wire shapes (subset of the bounded status payloads) ----
interface ReadinessWireRow {
  readonly collector_kind?: string;
  readonly display_name?: string;
  readonly instance_id?: string;
  readonly claim_state?: string;
  readonly promotion_state?: string;
}
interface ReadinessWire {
  readonly readiness?: readonly ReadinessWireRow[];
}
interface CoordinatorInstanceWire {
  readonly instance_id?: string;
  readonly collector_kind?: string;
  readonly enabled?: boolean;
  readonly mode?: string;
  readonly last_observed_at?: string | null;
  readonly deactivated_at?: string | null;
}
interface BackpressureWire {
  readonly collector_kind?: string;
  readonly collector_instance_id?: string;
  readonly pending?: number;
  readonly claimed?: number;
  readonly retrying?: number;
  readonly dead_letter?: number;
}
interface QueueWire {
  readonly outstanding?: number;
  readonly pending?: number;
  readonly in_flight?: number;
  readonly dead_letter?: number;
  readonly succeeded?: number;
}
interface IndexStatusWire {
  readonly status?: string;
  readonly repository_count?: number;
  readonly queue?: QueueWire;
  readonly coordinator?: {
    readonly collector_instances?: readonly CoordinatorInstanceWire[];
    readonly collector_backpressure?: readonly BackpressureWire[];
  };
}
interface IngesterWire {
  readonly ingesters?: readonly Record<string, unknown>[];
}
interface FreshnessWire {
  readonly state?: string;
  readonly generations?: { readonly active?: number; readonly pending?: number; readonly superseded?: number; readonly failed?: number };
  readonly pending_projection?: { readonly outstanding?: number; readonly dead_letter?: number };
}

// Clock lets tests inject a fixed "now" so relative ages are deterministic.
type Clock = () => number;

const unavailableOverview: StatusOverview = {
  indexingPercent: 0,
  indexStatusLabel: "unavailable",
  repositories: 0,
  collectors: [],
  pipeline: { state: "unknown", activeGenerations: 0, pendingGenerations: 0, pendingProjection: 0, deadLetters: 0 },
  queue: { outstanding: 0, inFlight: 0, deadLetter: 0, succeeded: 0 },
  provenance: "unavailable"
};

// loadStatusOverview fetches all four bounded status surfaces concurrently and
// joins them. collector-readiness is the required spine (the collector roster);
// if it is unreachable the whole overview is unavailable. The other reads are
// optional and degrade to neutral defaults.
//
// All four reads are issued in parallel (Promise.all) so the total wall time is
// bounded by the slowest single read (~2.1s) rather than their serial sum
// (~6.3s). This is the fix for issue #3441.
export async function loadStatusOverview(client: EshuApiClient, clock: Clock = Date.now): Promise<StatusOverview> {
  const [readinessResult, indexStatus, freshness, ingesters] = await Promise.all([
    client.get<ReadinessWire>("/api/v0/status/collector-readiness").catch(() => null),
    optionalJson<IndexStatusWire>(client, "/api/v0/index-status"),
    optionalFreshness(client),
    optionalJson<IngesterWire>(client, "/api/v0/status/ingesters")
  ]);

  if (!readinessResult || readinessResult.error || !readinessResult.data) return unavailableOverview;
  const readiness = readinessResult.data;

  const now = clock();
  const instances = indexStatus?.coordinator?.collector_instances ?? [];
  const backpressure = indexStatus?.coordinator?.collector_backpressure ?? [];
  const volumes = ingesterVolumes(ingesters);

  const collectors = (readiness.readiness ?? [])
    .map((row) => collectorRow(row, instances, backpressure, volumes, now))
    .sort(byStateThenName);

  const queue = queueFromWire(indexStatus?.queue);
  const pipeline = pipelineFromWire(freshness);
  return {
    indexingPercent: indexingPercent(collectors, queue, pipeline),
    indexStatusLabel: clean(indexStatus?.status) || pipeline.state,
    repositories: finite(indexStatus?.repository_count),
    collectors,
    pipeline,
    queue,
    provenance: "live"
  };
}

function collectorRow(
  row: ReadinessWireRow,
  instances: readonly CoordinatorInstanceWire[],
  backpressure: readonly BackpressureWire[],
  volumes: ReadonlyMap<string, number>,
  now: number
): StatusCollectorRow {
  const kind = clean(row.collector_kind) || "collector";
  const instanceId = clean(row.instance_id) || kind;
  const instance = instances.find((i) => clean(i.instance_id) === instanceId)
    ?? instances.find((i) => clean(i.collector_kind) === kind);
  const bp = backpressure.find((b) => clean(b.collector_instance_id) === instanceId)
    ?? backpressure.find((b) => clean(b.collector_kind) === kind);

  const pending = finite(bp?.pending);
  const claimed = finite(bp?.claimed) + finite(bp?.retrying);
  const deadLetter = finite(bp?.dead_letter);
  const workItems = pending + claimed;
  const disabled = instance ? instance.enabled === false || clean(instance.deactivated_at) !== "" : false;
  // Any promotion_state that is not an actively-running collector is treated as
  // stalled.  "implemented" and "partial" mean the family is live; everything
  // else (failed, stale, gated, disabled, unsupported, permission_hidden, or
  // any unknown future value) means it is not making forward progress.
  const ACTIVE_PROMOTION_STATES = new Set(["implemented", "partial"]);
  const failedPromotion =
    typeof row.promotion_state === "string" &&
    row.promotion_state !== "" &&
    !ACTIVE_PROMOTION_STATES.has(row.promotion_state);

  const state = collectorState({ deadLetter, disabled, failedPromotion, workItems });
  return {
    instanceId,
    kind,
    displayName: clean(row.display_name) || titleCase(kind),
    schedule: scheduleLabel(kind, instance?.mode, row.claim_state),
    state,
    progress: catchUpProgress(state, workItems, deadLetter),
    workItems,
    volume: volumes.get(instanceId) ?? volumes.get(kind) ?? null,
    lastRunLabel: relativeAge(instance?.last_observed_at, now)
  };
}

// collectorState is the core "what is Eshu doing right now" classifier:
// - stalled when work is dead-lettered, the instance is disabled/deactivated, or
//   readiness reports a failed/stale promotion (nothing is making progress);
// - catching up when there is outstanding work but no hard failure;
// - up to date otherwise.
function collectorState(input: {
  readonly deadLetter: number;
  readonly disabled: boolean;
  readonly failedPromotion: boolean;
  readonly workItems: number;
}): StatusCollectorState {
  if (input.deadLetter > 0 || input.disabled || input.failedPromotion) return "stalled";
  if (input.workItems > 0) return "catching_up";
  return "up_to_date";
}

// catchUpProgress maps a collector to a [0,1] bar. Up-to-date is full; stalled is
// empty; catching-up shrinks as outstanding work grows (a soft, bounded curve so
// a large backlog never reads as "almost done").
function catchUpProgress(state: StatusCollectorState, workItems: number, deadLetter: number): number {
  if (state === "up_to_date") return 1;
  if (state === "stalled") return deadLetter > 0 ? 0.05 : 0;
  // catching up: 1/(1+work/50) keeps small backlogs near full and large ones low.
  return Math.max(0.05, Math.min(0.95, 1 / (1 + workItems / 50)));
}

// indexingPercent is the hero number. It blends queue drain progress with the
// pipeline generation backlog so it reads ~100% when everything is caught up and
// drops under live indexing. Bounded to [0,100].
function indexingPercent(collectors: readonly StatusCollectorRow[], queue: StatusQueue, pipeline: StatusPipeline): number {
  const queueTotal = queue.outstanding + queue.inFlight + queue.succeeded;
  const queueRatio = queueTotal > 0 ? queue.succeeded / queueTotal : 1;
  const genTotal = pipeline.activeGenerations + pipeline.pendingGenerations;
  const genRatio = genTotal > 0 ? pipeline.activeGenerations / genTotal : 1;
  const collectorRatio = collectors.length > 0
    ? collectors.filter((c) => c.state === "up_to_date").length / collectors.length
    : 1;
  const blended = (queueRatio + genRatio + collectorRatio) / 3;
  return Math.round(Math.max(0, Math.min(1, blended)) * 100);
}

function queueFromWire(queue: QueueWire | undefined): StatusQueue {
  return {
    outstanding: finite(queue?.outstanding) || finite(queue?.pending),
    inFlight: finite(queue?.in_flight),
    deadLetter: finite(queue?.dead_letter),
    succeeded: finite(queue?.succeeded)
  };
}

function pipelineFromWire(freshness: FreshnessWire | null): StatusPipeline {
  if (!freshness) {
    return { state: "unknown", activeGenerations: 0, pendingGenerations: 0, pendingProjection: 0, deadLetters: 0 };
  }
  return {
    state: pipelineState(freshness.state),
    activeGenerations: finite(freshness.generations?.active),
    pendingGenerations: finite(freshness.generations?.pending),
    pendingProjection: finite(freshness.pending_projection?.outstanding),
    deadLetters: finite(freshness.pending_projection?.dead_letter)
  };
}

function pipelineState(value: string | undefined): StatusPipelineState {
  if (value === "fresh" || value === "building" || value === "stale") return value;
  return "unknown";
}

function ingesterVolumes(ingesters: IngesterWire | null): ReadonlyMap<string, number> {
  const volumes = new Map<string, number>();
  for (const g of ingesters?.ingesters ?? []) {
    const id = String(g.name ?? g.id ?? g.ingester ?? "");
    const kind = String(g.runtime_family ?? g.kind ?? "");
    const facts = Number(g.fact_count ?? g.facts ?? 0);
    if (!Number.isFinite(facts) || facts <= 0) continue;
    if (id) volumes.set(id, facts);
    if (kind && !volumes.has(kind)) volumes.set(kind, facts);
  }
  return volumes;
}

// scheduleLabel maps a collector kind (and its coordinator mode / claim state)
// to the human cadence the operator recognises, e.g. "5m poll" or "stream".
function scheduleLabel(kind: string, mode: string | undefined, claimState: string | undefined): string {
  const cleanMode = clean(mode);
  if (cleanMode === "stream" || cleanMode === "watch") return cleanMode;
  const known = SCHEDULE_BY_KIND[kind];
  if (known) return known;
  if (claimState === "claim_driven") return "claim";
  return cleanMode || "poll";
}

const SCHEDULE_BY_KIND: Readonly<Record<string, string>> = {
  git: "webhook + 10m poll",
  github: "webhook + 10m poll",
  aws: "5m poll",
  azure: "5m poll",
  gcp: "5m poll",
  kubernetes: "watch",
  kubernetes_live: "watch",
  terraform_state: "on-apply + 1h",
  oci_registry: "on-publish",
  ecr_registry: "on-publish",
  pagerduty: "stream",
  prometheus_mimir: "30m claim",
  tempo: "30m claim",
  grafana: "30m claim",
  loki: "30m claim",
  sbom_attestation: "on-publish",
  synthetics: "5m poll"
};

// relativeAge renders a coordinator last_observed_at into a compact age such as
// "40s ago", "1m ago", or "5h 12m ago". Empty/zero timestamps read "never".
function relativeAge(iso: string | null | undefined, now: number): string {
  const ts = clean(iso);
  if (ts === "") return "never";
  const at = Date.parse(ts);
  if (!Number.isFinite(at)) return "never";
  // The collector_instances timestamp defaults to the zero time when unset.
  if (at <= 0 || ts.startsWith("0001-01-01")) return "never";
  const secs = Math.max(0, Math.floor((now - at) / 1000));
  if (secs < 60) return `${secs}s ago`;
  const mins = Math.floor(secs / 60);
  if (mins < 60) return `${mins}m ago`;
  const hours = Math.floor(mins / 60);
  const remMins = mins % 60;
  if (hours < 24) return remMins > 0 ? `${hours}h ${remMins}m ago` : `${hours}h ago`;
  const days = Math.floor(hours / 24);
  return `${days}d ago`;
}

function byStateThenName(a: StatusCollectorRow, b: StatusCollectorRow): number {
  return stateRank(a.state) - stateRank(b.state) || a.displayName.localeCompare(b.displayName);
}

function stateRank(state: StatusCollectorState): number {
  if (state === "stalled") return 0;
  if (state === "catching_up") return 1;
  return 2;
}

async function optionalJson<T>(client: EshuApiClient, path: string): Promise<T | null> {
  try {
    return await client.getJson<T>(path);
  } catch {
    return null;
  }
}

async function optionalFreshness(client: EshuApiClient): Promise<FreshnessWire | null> {
  try {
    const env = await client.get<FreshnessWire>("/api/v0/status/freshness-causality");
    if (env.error || !env.data) return null;
    return env.data;
  } catch {
    return null;
  }
}

function titleCase(kind: string): string {
  return kind.split(/[_-]/).filter(Boolean).map((part) => `${part.slice(0, 1).toUpperCase()}${part.slice(1)}`).join(" ");
}

function clean(value: string | null | undefined): string {
  return value?.trim() ?? "";
}

function finite(value: number | undefined): number {
  return typeof value === "number" && Number.isFinite(value) && value > 0 ? value : 0;
}
