import type { EshuApiClient } from "./client";
import type { EshuTruth } from "./envelope";

// Wire shape returned by GET /api/v0/status/freshness-causality (snake_case).
interface FreshnessNextCheckWire {
  readonly tool?: string;
  readonly route?: string;
  readonly reason?: string;
  readonly params?: Readonly<Record<string, string>>;
}

interface FreshnessCauseWire {
  readonly cause: string;
  readonly observed: boolean;
  readonly observability: string;
  readonly detail: string;
  readonly next_check?: FreshnessNextCheckWire;
}

interface FreshnessGenerationsWire {
  readonly active: number;
  readonly pending: number;
  readonly completed: number;
  readonly superseded: number;
  readonly failed: number;
}

interface FreshnessPendingProjectionWire {
  readonly outstanding: number;
  readonly dead_letter: number;
  readonly domains: number;
}

interface FreshnessTransitionWire {
  readonly status: string;
  readonly trigger_kind?: string;
  readonly freshness_hint?: string;
  readonly observed_at?: string | null;
  readonly superseded_at?: string | null;
  readonly scope_id?: string;
  readonly generation_id?: string;
}

interface FreshnessCausalityWire {
  readonly state: string;
  readonly scoped: boolean;
  readonly causes: readonly FreshnessCauseWire[];
  readonly generations: FreshnessGenerationsWire;
  readonly pending_projection: FreshnessPendingProjectionWire;
  readonly recent_transitions: readonly FreshnessTransitionWire[];
}

// Domain types consumed by the page (camelCase).
export type FreshnessOverallState = "fresh" | "building" | "stale";

export interface FreshnessCause {
  readonly cause: string;
  readonly observed: boolean;
  readonly observability: "runtime" | "per_answer";
  readonly detail: string;
  readonly nextCheckReason: string;
}

export interface FreshnessTransition {
  readonly status: string;
  readonly triggerKind: string;
  readonly freshnessHint: string;
  readonly supersededAt: string | null;
  readonly scopeId: string | null;
  readonly generationId: string | null;
}

export interface FreshnessCausalityPage {
  readonly state: FreshnessOverallState | "unknown";
  readonly scoped: boolean;
  readonly causes: readonly FreshnessCause[];
  readonly generations: FreshnessGenerationsWire;
  readonly pending: FreshnessPendingProjectionWire;
  readonly transitions: readonly FreshnessTransition[];
  readonly truth: EshuTruth | null;
  readonly provenance: "live" | "unavailable";
}

const emptyGenerations: FreshnessGenerationsWire = {
  active: 0,
  pending: 0,
  completed: 0,
  superseded: 0,
  failed: 0,
};

const emptyPending: FreshnessPendingProjectionWire = {
  outstanding: 0,
  dead_letter: 0,
  domains: 0,
};

function normalizeObservability(value: string): "runtime" | "per_answer" {
  return value === "per_answer" ? "per_answer" : "runtime";
}

function normalizeState(value: string): FreshnessOverallState | "unknown" {
  if (value === "fresh" || value === "building" || value === "stale") {
    return value;
  }
  return "unknown";
}

function causeFromWire(wire: FreshnessCauseWire): FreshnessCause {
  return {
    cause: wire.cause,
    observed: wire.observed,
    observability: normalizeObservability(wire.observability),
    detail: wire.detail,
    nextCheckReason: wire.next_check?.reason ?? "",
  };
}

function transitionFromWire(wire: FreshnessTransitionWire): FreshnessTransition {
  return {
    status: wire.status,
    triggerKind: wire.trigger_kind ?? "",
    freshnessHint: wire.freshness_hint ?? "",
    supersededAt: wire.superseded_at ?? null,
    scopeId: wire.scope_id ?? null,
    generationId: wire.generation_id ?? null,
  };
}

const unavailablePage: FreshnessCausalityPage = {
  state: "unknown",
  scoped: false,
  causes: [],
  generations: emptyGenerations,
  pending: emptyPending,
  transitions: [],
  truth: null,
  provenance: "unavailable",
};

// loadFreshnessCausality fetches and normalizes the freshness causality read
// model. Any error yields a fail-closed unavailable page rather than throwing.
export async function loadFreshnessCausality(
  client: EshuApiClient,
): Promise<FreshnessCausalityPage> {
  try {
    const env = await client.get<FreshnessCausalityWire>("/api/v0/status/freshness-causality");
    if (env.error || !env.data) {
      return unavailablePage;
    }
    const data = env.data;
    return {
      state: normalizeState(data.state),
      scoped: Boolean(data.scoped),
      causes: (data.causes ?? []).map(causeFromWire),
      generations: data.generations ?? emptyGenerations,
      pending: data.pending_projection ?? emptyPending,
      transitions: (data.recent_transitions ?? []).map(transitionFromWire),
      truth: env.truth ?? null,
      provenance: "live",
    };
  } catch {
    return unavailablePage;
  }
}
