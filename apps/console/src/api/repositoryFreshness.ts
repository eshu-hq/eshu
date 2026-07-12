// api/repositoryFreshness.ts
// Per-repository commit-receipt and build-completeness read model (issue
// #5143) backing GET /api/v0/repositories/{id}/freshness. Answers, in plain
// language, the two questions an engineer asks after pushing: "did eshu pick
// up my latest commit" and "is the evidence fully built for that commit, or
// still converging". This module owns the wire -> UI mapping and the
// verdict -> {tone, headline, detail} copy; components only render the
// result.
//
// The read fails closed like operationsBoard.ts: a missing endpoint, a
// non-2xx response, or a thrown network/timeout error all degrade to an
// explicit "unavailable" freshness rather than throwing, so a page keeps
// rendering everything else instead of breaking on one repo's freshness
// chip.
//
// observed_commit may be legitimately empty (non-git scopes, or pre-delta-
// baseline git generations) — this module never fabricates a short SHA for
// an empty commit; callers render the honest "no commit receipt" copy
// instead (see freshnessCopy's "unknown" branch).

import type { EshuApiClient } from "./client";

export type FreshnessVerdict = "current" | "building" | "behind" | "unobserved" | "unknown";
export type FreshnessTone = "teal" | "violet" | "warn" | "neutral";

export interface RepositoryFreshnessCopy {
  readonly tone: FreshnessTone;
  readonly headline: string;
  readonly detail: string;
}

export interface RepositoryFreshnessGeneration {
  readonly id: string;
  readonly status: string;
  readonly triggerKind: string;
  readonly isDelta: boolean;
  readonly activatedAt: string | null;
}

export interface RepositoryFreshnessStages {
  readonly collected: boolean;
  readonly reduced: boolean;
  readonly projected: boolean;
  readonly materialized: boolean;
}

export interface RepositoryFreshnessOutstanding {
  readonly stage: string;
  readonly status: string;
  readonly count: number;
}

export interface RepositoryFreshnessPendingDomain {
  readonly domain: string;
  readonly count: number;
}

export interface RepositoryFreshnessSharedEnrichment {
  readonly pending: boolean;
  readonly pendingDomains: readonly RepositoryFreshnessPendingDomain[];
}

export interface RepositoryFreshnessUnobservedPush {
  readonly targetSha: string;
  readonly ref: string;
  readonly receivedAt: string | null;
}

export interface RepositoryFreshness {
  readonly verdict: FreshnessVerdict;
  readonly observedCommit: string;
  readonly observedAt: string | null;
  readonly generation: RepositoryFreshnessGeneration | null;
  readonly stages: RepositoryFreshnessStages;
  readonly outstandingByStage: readonly RepositoryFreshnessOutstanding[];
  readonly sharedEnrichment: RepositoryFreshnessSharedEnrichment;
  readonly unobservedPush: RepositoryFreshnessUnobservedPush | null;
  readonly asOf: string | null;
  readonly scoped: boolean;
  readonly expectedCommit: string;
  readonly copy: RepositoryFreshnessCopy;
  readonly provenance: "live" | "unavailable";
}

// ---- wire shapes (GET /api/v0/repositories/{id}/freshness) ----
interface FreshnessGenerationWire {
  readonly id?: string;
  readonly status?: string;
  readonly trigger_kind?: string;
  readonly is_delta?: boolean;
  readonly activated_at?: string | null;
}
interface FreshnessStagesWire {
  readonly collected?: boolean;
  readonly reduced?: boolean;
  readonly projected?: boolean;
  readonly materialized?: boolean;
}
interface FreshnessOutstandingWire {
  readonly stage?: string;
  readonly status?: string;
  readonly count?: number;
}
interface FreshnessPendingDomainWire {
  readonly domain?: string;
  readonly count?: number;
}
interface FreshnessSharedEnrichmentWire {
  readonly pending?: boolean;
  readonly pending_domains?: readonly FreshnessPendingDomainWire[];
}
interface FreshnessUnobservedPushWire {
  readonly target_sha?: string;
  readonly ref?: string;
  readonly received_at?: string | null;
}
interface FreshnessWire {
  readonly scope_id?: string;
  readonly verdict?: string;
  readonly observed_commit?: string;
  readonly observed_at?: string | null;
  readonly generation?: FreshnessGenerationWire | null;
  readonly stages?: FreshnessStagesWire;
  readonly outstanding_by_stage?: readonly FreshnessOutstandingWire[];
  readonly shared_enrichment?: FreshnessSharedEnrichmentWire;
  readonly unobserved_push?: FreshnessUnobservedPushWire | null;
  readonly as_of?: string;
  readonly scoped?: boolean;
}

// Clock lets tests inject a fixed "now" so relative-time copy is deterministic.
type Clock = () => number;

function unavailableFreshness(expectedCommit: string): RepositoryFreshness {
  return {
    verdict: "unknown",
    observedCommit: "",
    observedAt: null,
    generation: null,
    stages: { collected: false, reduced: false, projected: false, materialized: false },
    outstandingByStage: [],
    sharedEnrichment: { pending: false, pendingDomains: [] },
    unobservedPush: null,
    asOf: null,
    scoped: false,
    expectedCommit,
    copy: {
      tone: "neutral",
      headline: "Freshness unavailable",
      detail: "Freshness is unavailable from this source.",
    },
    provenance: "unavailable",
  };
}

// loadRepositoryFreshness fetches GET /api/v0/repositories/{id}/freshness for
// one repository. `expectedCommit` maps to the optional ?expected_commit=
// query parameter (#5143): passing the caller's local HEAD lets the backend
// answer "is *my* SHA current" and drives the "behind" verdict
// deterministically. The backend never echoes expected_commit back on the
// wire, so it is threaded through here to render the "behind" copy.
export async function loadRepositoryFreshness(
  client: EshuApiClient,
  repoId: string,
  options?: { readonly expectedCommit?: string; readonly clock?: Clock },
): Promise<RepositoryFreshness> {
  const expectedCommit = clean(options?.expectedCommit);
  const clock = options?.clock ?? Date.now;
  const path = expectedCommit
    ? `/api/v0/repositories/${encodeURIComponent(repoId)}/freshness?expected_commit=${encodeURIComponent(expectedCommit)}`
    : `/api/v0/repositories/${encodeURIComponent(repoId)}/freshness`;
  const result = await client.get<FreshnessWire>(path).catch(() => null);
  if (!result || result.error || !result.data) return unavailableFreshness(expectedCommit);
  return freshnessFromWire(result.data, expectedCommit, clock());
}

function freshnessFromWire(
  wire: FreshnessWire,
  expectedCommit: string,
  now: number,
): RepositoryFreshness {
  const verdict = verdictFromWire(wire.verdict);
  const observedCommit = clean(wire.observed_commit);
  const observedAt = clean(wire.observed_at) || null;
  const outstandingByStage = (wire.outstanding_by_stage ?? []).map(outstandingFromWire);
  const sharedEnrichment = sharedEnrichmentFromWire(wire.shared_enrichment);
  const unobservedPush = unobservedPushFromWire(wire.unobserved_push);
  return {
    verdict,
    observedCommit,
    observedAt,
    generation: generationFromWire(wire.generation),
    stages: stagesFromWire(wire.stages),
    outstandingByStage,
    sharedEnrichment,
    unobservedPush,
    asOf: clean(wire.as_of) || null,
    scoped: wire.scoped === true,
    expectedCommit,
    copy: freshnessCopy(
      verdict,
      { observedCommit, observedAt, outstandingByStage, sharedEnrichment, unobservedPush },
      expectedCommit,
      now,
    ),
    provenance: "live",
  };
}

function verdictFromWire(value: string | undefined): FreshnessVerdict {
  if (
    value === "current" ||
    value === "building" ||
    value === "behind" ||
    value === "unobserved" ||
    value === "unknown"
  ) {
    return value;
  }
  return "unknown";
}

function generationFromWire(
  wire: FreshnessGenerationWire | null | undefined,
): RepositoryFreshnessGeneration | null {
  if (!wire) return null;
  return {
    id: clean(wire.id),
    status: clean(wire.status),
    triggerKind: clean(wire.trigger_kind),
    isDelta: wire.is_delta === true,
    activatedAt: clean(wire.activated_at) || null,
  };
}

function stagesFromWire(wire: FreshnessStagesWire | undefined): RepositoryFreshnessStages {
  return {
    collected: wire?.collected === true,
    reduced: wire?.reduced === true,
    projected: wire?.projected === true,
    materialized: wire?.materialized === true,
  };
}

function outstandingFromWire(wire: FreshnessOutstandingWire): RepositoryFreshnessOutstanding {
  return {
    stage: clean(wire.stage) || "unknown",
    status: clean(wire.status) || "unknown",
    count: finite(wire.count),
  };
}

function sharedEnrichmentFromWire(
  wire: FreshnessSharedEnrichmentWire | undefined,
): RepositoryFreshnessSharedEnrichment {
  return {
    pending: wire?.pending === true,
    pendingDomains: (wire?.pending_domains ?? []).map((domain) => ({
      domain: clean(domain.domain) || "unknown",
      count: finite(domain.count),
    })),
  };
}

function unobservedPushFromWire(
  wire: FreshnessUnobservedPushWire | null | undefined,
): RepositoryFreshnessUnobservedPush | null {
  if (!wire) return null;
  return {
    targetSha: clean(wire.target_sha),
    ref: clean(wire.ref),
    receivedAt: clean(wire.received_at) || null,
  };
}

// shortSha renders a short, honest commit reference: an em dash when the
// commit is empty (never a fabricated SHA — see the module doc comment),
// otherwise the first 10 characters, matching RepoSourcePage's "Indexed ref"
// badge convention.
export function shortSha(sha: string): string {
  const trimmed = sha.trim();
  return trimmed === "" ? "—" : trimmed.slice(0, 10);
}

// relativeFromNow renders an ISO timestamp as a compact age ("2m ago", "1h 5m
// ago"), mirroring statusOverview.ts's relativeAge shape.
function relativeFromNow(iso: string | null, now: number): string {
  if (iso === null) return "just now";
  const at = Date.parse(iso);
  if (!Number.isFinite(at)) return "just now";
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

const STAGE_VERB: readonly (readonly [string, string])[] = [
  ["collect", "collecting"],
  ["reduce", "reducing"],
  ["project", "projecting"],
  ["materialize", "materializing"],
];

// stageVerb maps a stage name (e.g. "reduce") to its present-participle verb
// for end-user copy ("reducing"). Falls back to the raw stage name for a
// stage this module does not recognize, so a future stage still renders
// something readable instead of nothing.
function stageVerb(stage: string): string {
  const key = stage.trim().toLowerCase();
  for (const [match, verb] of STAGE_VERB) {
    if (key.includes(match)) return verb;
  }
  return key || "processing";
}

function buildingDetail(
  outstandingByStage: readonly RepositoryFreshnessOutstanding[],
  sharedEnrichment: RepositoryFreshnessSharedEnrichment,
): string {
  const first = outstandingByStage.find((row) => row.count > 0);
  if (first) {
    return `${stageVerb(first.stage)} — ${first.count} item${first.count === 1 ? "" : "s"} left`;
  }
  if (sharedEnrichment.pending) {
    return "your repo is done; cross-repo enrichment still running";
  }
  return "indexing in progress";
}

interface FreshnessSnapshotForCopy {
  readonly observedCommit: string;
  readonly observedAt: string | null;
  readonly outstandingByStage: readonly RepositoryFreshnessOutstanding[];
  readonly sharedEnrichment: RepositoryFreshnessSharedEnrichment;
  readonly unobservedPush: RepositoryFreshnessUnobservedPush | null;
}

// freshnessCopy renders the end-user-language {tone, headline, detail} for
// one verdict (issue #5143). Copy speaks user outcomes ("answers include
// your latest push"), never pipeline jargon, and never fabricates a commit
// SHA for an empty observed_commit (non-git scopes, or pre-delta-baseline
// git generations render the honest "unknown" copy instead).
export function freshnessCopy(
  verdict: FreshnessVerdict,
  snapshot: FreshnessSnapshotForCopy,
  expectedCommit: string,
  now: number,
): RepositoryFreshnessCopy {
  switch (verdict) {
    case "current":
      return {
        tone: "teal",
        headline: `Current through ${shortSha(snapshot.observedCommit)}`,
        detail: `Answers include your latest indexed push (${relativeFromNow(snapshot.observedAt, now)}).`,
      };
    case "building":
      return {
        tone: "violet",
        headline: `Indexing ${shortSha(snapshot.observedCommit)}`,
        detail: buildingDetail(snapshot.outstandingByStage, snapshot.sharedEnrichment),
      };
    case "behind":
      return {
        tone: "warn",
        headline: "Behind your commit",
        detail: `eshu has ${shortSha(snapshot.observedCommit)}; expected ${shortSha(expectedCommit)} not indexed yet.`,
      };
    case "unobserved":
      return {
        tone: "warn",
        headline: "Push not picked up",
        detail: snapshot.unobservedPush
          ? `A push to ${snapshot.unobservedPush.ref || "—"} (${shortSha(snapshot.unobservedPush.targetSha)}) was received but no indexing has started.`
          : "A push was received but no indexing has started.",
      };
    case "unknown":
    default:
      return {
        tone: "neutral",
        headline: "No commit receipt",
        detail:
          snapshot.observedCommit === ""
            ? "This scope has no recorded commit — not a git repository, or indexing has not produced one yet."
            : "Freshness could not be determined for this repository.",
      };
  }
}

function clean(value: string | null | undefined): string {
  return value?.trim() ?? "";
}

function finite(value: number | undefined): number {
  return typeof value === "number" && Number.isFinite(value) && value > 0 ? value : 0;
}
