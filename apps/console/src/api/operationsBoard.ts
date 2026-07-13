// operationsBoard.ts
// Bounded live operations board read model for the console Operations page
// (issue #5137). It wraps the single bounded read GET /api/v0/status/operations
// -- health, collector heartbeats, per-stage queue summaries, queue pressure,
// and a bounded live_activity list of in-flight work items -- into a typed UI
// model. This is the one backing read for the page's live sections; it never
// issues an unbounded catalog/aggregate read.
//
// The read fails closed: a missing endpoint, a non-2xx response, or a network
// error all degrade to an explicit "unavailable" board rather than throwing,
// so the page keeps rendering its other sections instead of going blank.

import type { EshuApiClient } from "./client";
import type { UiFresh } from "../console/types";

export type OperationsHealthState = "healthy" | "progressing" | "degraded" | "stalled" | "unknown";

export interface OperationsHealth {
  readonly state: OperationsHealthState;
  readonly reasons: readonly string[];
}

export interface OperationsStageSummary {
  readonly stage: string;
  readonly pending: number;
  readonly claimed: number;
  readonly running: number;
  readonly retrying: number;
  readonly succeeded: number;
  readonly failed: number;
  readonly deadLetter: number;
}

export interface OperationsQueue {
  readonly outstanding: number;
  readonly inFlight: number;
  readonly retrying: number;
  readonly succeeded: number;
  readonly deadLetter: number;
  readonly failed: number;
  readonly overdueClaims: number;
}

export interface OperationsCollectorRow {
  readonly instanceId: string;
  readonly kind: string;
  readonly displayName: string;
  readonly mode: string;
  readonly health: string;
  readonly lastObservedAt: string | null;
  // freshness is a heartbeat-age classification (fresh <2min / lagging <10min /
  // stale otherwise), distinct from the collector's own reported `health`.
  readonly freshness: UiFresh;
}

export interface OperationsActivityRow {
  readonly workItemId: string;
  readonly stage: string;
  readonly status: string;
  readonly domain: string;
  // leaseOwner, sourceKey, and sourceDisplay are null when the route is
  // scoped (redacted worker/repo identity for a tenant-scoped token) or
  // absent on the wire.
  readonly leaseOwner: string | null;
  readonly claimUntil: string | null;
  readonly attemptCount: number;
  readonly updatedAt: string | null;
  readonly createdAt: string | null;
  readonly ageSeconds: number;
  readonly scopeKind: string;
  readonly collectorKind: string;
  readonly sourceSystem: string;
  // sourceKey is the raw, possibly-opaque repo identity (e.g.
  // "repository:r_ea78e8bb" for git scopes). Kept for a secondary/tooltip
  // use; prefer repoLabel() for the primary display value.
  readonly sourceKey: string | null;
  // sourceDisplay is the operator-facing repo name (#5137 follow-up), e.g.
  // "acme/orders-api", resolved server-side from the scope payload.
  readonly sourceDisplay: string | null;
  // generationState (#5138) is "stale" when this is a retrying row belonging
  // to a superseded generation -- the console renders it dimmed with a badge
  // rather than as ordinary in-flight work. claimed/running rows are always
  // "active" regardless of generation. Defaults to "active" when absent so a
  // row never renders as stale by omission.
  readonly generationState: "active" | "stale";
}

// OperationsDomainBacklog is one row of the server's already-sorted,
// already-bounded top-N reducer/projection domain backlog list (issue
// #5172): go/internal/status's topDomainBacklogs sorts by outstanding desc
// (then oldest age desc) and caps to status.Options.DomainLimit before the
// operations endpoint ever serializes it, so the adapter trusts wire order
// rather than re-sorting or re-limiting client-side.
export interface OperationsDomainBacklog {
  readonly domain: string;
  readonly outstanding: number;
  readonly pending: number;
  readonly inFlight: number;
  readonly blocked: number;
  readonly retrying: number;
  readonly deadLetter: number;
  readonly failed: number;
  readonly oldestAgeSeconds: number;
}

export interface OperationsBoard {
  readonly asOf: string | null;
  readonly scoped: boolean;
  readonly health: OperationsHealth;
  readonly collectors: readonly OperationsCollectorRow[];
  readonly stageSummaries: readonly OperationsStageSummary[];
  readonly queue: OperationsQueue;
  readonly domainBacklogs: readonly OperationsDomainBacklog[];
  readonly liveActivity: readonly OperationsActivityRow[];
  readonly truncated: boolean;
  readonly limit: number;
  readonly provenance: "live" | "unavailable";
}

// ---- wire shapes (GET /api/v0/status/operations) ----
interface HealthWire {
  readonly state?: string;
  readonly reasons?: readonly string[];
}
interface StageSummaryWire {
  readonly stage?: string;
  readonly pending?: number;
  readonly claimed?: number;
  readonly running?: number;
  readonly retrying?: number;
  readonly succeeded?: number;
  readonly failed?: number;
  readonly dead_letter?: number;
}
interface QueueWire {
  readonly outstanding?: number;
  readonly in_flight?: number;
  readonly retrying?: number;
  readonly succeeded?: number;
  readonly dead_letter?: number;
  readonly failed?: number;
  readonly overdue_claims?: number;
}
interface CollectorRuntimeWire {
  readonly instance_id?: string;
  readonly collector_kind?: string;
  readonly mode?: string;
  readonly display_name?: string;
  readonly health?: string;
  readonly last_observed_at?: string | null;
}
// DomainBacklogWire mirrors go/internal/query/aws_materialization_status.go's
// domainBacklogToMap (issue #5172).
interface DomainBacklogWire {
  readonly domain?: string;
  readonly outstanding?: number;
  readonly pending?: number;
  readonly in_flight?: number;
  readonly blocked?: number;
  readonly retrying?: number;
  readonly dead_letter?: number;
  readonly failed?: number;
  readonly oldest_age?: number;
}
interface LiveActivityWire {
  readonly work_item_id?: string;
  readonly stage?: string;
  readonly status?: string;
  readonly domain?: string;
  readonly lease_owner?: string | null;
  readonly claim_until?: string | null;
  readonly attempt_count?: number;
  readonly updated_at?: string | null;
  readonly created_at?: string | null;
  readonly age_seconds?: number;
  readonly scope_kind?: string;
  readonly collector_kind?: string;
  readonly source_system?: string;
  readonly source_key?: string | null;
  readonly source_display?: string | null;
  readonly generation_state?: string;
}
interface OperationsWire {
  readonly version?: string;
  readonly as_of?: string;
  readonly scoped?: boolean;
  readonly health?: HealthWire;
  readonly collectors?: readonly CollectorRuntimeWire[];
  readonly stage_summaries?: readonly StageSummaryWire[];
  readonly queue?: QueueWire;
  readonly domain_backlogs?: readonly DomainBacklogWire[];
  readonly live_activity?: readonly LiveActivityWire[];
  readonly truncated?: boolean;
  readonly limit?: number;
}

// Clock lets tests inject a fixed "now" so heartbeat freshness is deterministic.
type Clock = () => number;

const unavailableBoard: OperationsBoard = {
  asOf: null,
  scoped: false,
  health: { state: "unknown", reasons: [] },
  collectors: [],
  stageSummaries: [],
  queue: {
    outstanding: 0,
    inFlight: 0,
    retrying: 0,
    succeeded: 0,
    deadLetter: 0,
    failed: 0,
    overdueClaims: 0,
  },
  domainBacklogs: [],
  liveActivity: [],
  truncated: false,
  limit: 0,
  provenance: "unavailable",
};

// loadOperationsBoard fetches the bounded live operations board read model. A
// missing endpoint, an envelope error, or a thrown network/timeout error all
// degrade to unavailableBoard rather than throwing, so the page keeps
// rendering its other (model-driven) sections.
export async function loadOperationsBoard(
  client: EshuApiClient,
  limit?: number,
  clock: Clock = Date.now,
): Promise<OperationsBoard> {
  const path =
    limit !== undefined && limit > 0
      ? `/api/v0/status/operations?limit=${limit}`
      : "/api/v0/status/operations";
  const result = await client.get<OperationsWire>(path).catch(() => null);
  if (!result || result.error || !result.data) return unavailableBoard;
  const wire = result.data;
  const now = clock();
  return {
    asOf: clean(wire.as_of) || null,
    scoped: wire.scoped === true,
    health: healthFromWire(wire.health),
    collectors: (wire.collectors ?? []).map((row) => collectorRowFromWire(row, now)),
    stageSummaries: (wire.stage_summaries ?? []).map(stageSummaryFromWire),
    queue: queueFromWire(wire.queue),
    domainBacklogs: (wire.domain_backlogs ?? []).map(domainBacklogFromWire),
    liveActivity: (wire.live_activity ?? []).map(activityRowFromWire),
    truncated: wire.truncated === true,
    limit: finite(wire.limit),
    provenance: "live",
  };
}

function healthFromWire(health: HealthWire | undefined): OperationsHealth {
  return {
    state: healthState(health?.state),
    reasons: (health?.reasons ?? []).filter((r) => typeof r === "string" && r.trim() !== ""),
  };
}

function healthState(value: string | undefined): OperationsHealthState {
  if (value === "healthy" || value === "progressing" || value === "degraded" || value === "stalled")
    return value;
  return "unknown";
}

function stageSummaryFromWire(row: StageSummaryWire): OperationsStageSummary {
  return {
    stage: clean(row.stage) || "unknown",
    pending: finite(row.pending),
    claimed: finite(row.claimed),
    running: finite(row.running),
    retrying: finite(row.retrying),
    succeeded: finite(row.succeeded),
    failed: finite(row.failed),
    deadLetter: finite(row.dead_letter),
  };
}

function queueFromWire(queue: QueueWire | undefined): OperationsQueue {
  return {
    outstanding: finite(queue?.outstanding),
    inFlight: finite(queue?.in_flight),
    retrying: finite(queue?.retrying),
    succeeded: finite(queue?.succeeded),
    deadLetter: finite(queue?.dead_letter),
    failed: finite(queue?.failed),
    overdueClaims: finite(queue?.overdue_claims),
  };
}

function domainBacklogFromWire(row: DomainBacklogWire): OperationsDomainBacklog {
  return {
    domain: clean(row.domain) || "—",
    outstanding: finite(row.outstanding),
    pending: finite(row.pending),
    inFlight: finite(row.in_flight),
    blocked: finite(row.blocked),
    retrying: finite(row.retrying),
    deadLetter: finite(row.dead_letter),
    failed: finite(row.failed),
    oldestAgeSeconds:
      typeof row.oldest_age === "number" && Number.isFinite(row.oldest_age) ? row.oldest_age : 0,
  };
}

function collectorRowFromWire(row: CollectorRuntimeWire, now: number): OperationsCollectorRow {
  const kind = clean(row.collector_kind) || "collector";
  const lastObservedAt = clean(row.last_observed_at ?? undefined) || null;
  return {
    instanceId: clean(row.instance_id) || kind,
    kind,
    displayName: clean(row.display_name) || titleCase(kind),
    mode: clean(row.mode) || "—",
    health: clean(row.health) || "unknown",
    lastObservedAt,
    freshness: heartbeatFreshness(lastObservedAt, now),
  };
}

// heartbeatFreshness classifies a collector's last_observed_at age: fresh
// under 2 minutes, lagging under 10 minutes, stale beyond that or when the
// timestamp is missing/unparseable.
function heartbeatFreshness(lastObservedAt: string | null, now: number): UiFresh {
  if (lastObservedAt === null) return "stale";
  const at = Date.parse(lastObservedAt);
  if (!Number.isFinite(at)) return "stale";
  const ageMs = now - at;
  if (ageMs < 2 * 60 * 1000) return "fresh";
  if (ageMs < 10 * 60 * 1000) return "lagging";
  return "stale";
}

function activityRowFromWire(row: LiveActivityWire): OperationsActivityRow {
  return {
    workItemId: clean(row.work_item_id) || "—",
    stage: clean(row.stage) || "unknown",
    status: clean(row.status) || "unknown",
    domain: clean(row.domain) || "—",
    leaseOwner: clean(row.lease_owner ?? undefined) || null,
    claimUntil: clean(row.claim_until ?? undefined) || null,
    attemptCount: finite(row.attempt_count),
    updatedAt: clean(row.updated_at ?? undefined) || null,
    createdAt: clean(row.created_at ?? undefined) || null,
    ageSeconds:
      typeof row.age_seconds === "number" && Number.isFinite(row.age_seconds) ? row.age_seconds : 0,
    scopeKind: clean(row.scope_kind) || "—",
    collectorKind: clean(row.collector_kind) || "—",
    sourceSystem: clean(row.source_system) || "—",
    sourceKey: clean(row.source_key ?? undefined) || null,
    sourceDisplay: clean(row.source_display ?? undefined) || null,
    generationState: row.generation_state === "stale" ? "stale" : "active",
  };
}

// repoLabel resolves the "Now processing" repo column: the operator-facing
// source_display when present, falling back to the raw source_key, and
// finally an em dash when both are redacted (scoped token) or absent.
export function repoLabel(row: {
  readonly sourceDisplay: string | null;
  readonly sourceKey: string | null;
}): string {
  return row.sourceDisplay ?? row.sourceKey ?? "—";
}

// repositorySourceHref resolves a "Now processing" row to the repository
// freshness route (issue #5171): the same /repositories/:id/source
// destination RepositoriesPage links to (RepositoryFreshnessSection lives on
// RepoSourcePage). It is only resolvable for a git repository scope
// (scope_kind "repository") -- that scope's source_key IS the repository
// catalog id GET /api/v0/repositories returns for it. Both values derive from
// the same repositoryidentity.CanonicalRepositoryID output
// ("repository:r_<hash8>", go/internal/repositoryidentity/identity.go): the
// git collector stores it as both ingestion_scopes.source_key and
// payload->>'repo_id' (go/internal/collector/git_source_processing.go's
// buildScope), and the repository catalog reads the id back from
// payload->>'repo_id' first (go/internal/query/content_reader_repository_
// catalog.go's repositoryCatalogIDExpr). Every other collector's scope_kind
// has no such relationship to the repository catalog, and a scoped caller's
// source_key is redacted to null -- both cases return null so the row stays
// plain text rather than linking to a wrong or dead destination.
export function repositorySourceHref(row: {
  readonly scopeKind: string;
  readonly sourceKey: string | null;
}): string | null {
  if (row.scopeKind !== "repository") return null;
  if (row.sourceKey === null || row.sourceKey === "") return null;
  return `/repositories/${encodeURIComponent(row.sourceKey)}/source`;
}

// humanizeAge renders a work item's age_seconds into a compact duration such
// as "40s", "3m", or "2h 5m". Mirrors statusOverview.ts's relativeAge shape
// but operates on a duration in seconds rather than an ISO timestamp.
export function humanizeAge(seconds: number): string {
  const secs = Math.max(0, Math.floor(seconds));
  if (secs < 60) return `${secs}s`;
  const mins = Math.floor(secs / 60);
  if (mins < 60) return `${mins}m`;
  const hours = Math.floor(mins / 60);
  const remMins = mins % 60;
  if (hours < 24) return remMins > 0 ? `${hours}h ${remMins}m` : `${hours}h`;
  const days = Math.floor(hours / 24);
  return `${days}d`;
}

function titleCase(kind: string): string {
  return kind
    .split(/[_-]/)
    .filter(Boolean)
    .map((part) => `${part.slice(0, 1).toUpperCase()}${part.slice(1)}`)
    .join(" ");
}

function clean(value: string | null | undefined): string {
  return value?.trim() ?? "";
}

function finite(value: number | undefined): number {
  return typeof value === "number" && Number.isFinite(value) && value > 0 ? value : 0;
}
