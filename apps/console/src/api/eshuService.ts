// api/eshuService.ts
// Service-spotlight loader. Pulls the story + context for one service from the
// API and maps it into a drawer view-model. Defensive over response shape.

import type { EshuApiClient } from "./client";
import { EshuEnvelopeError } from "./envelope";
import type { ServiceRow, UiTruth, UiFresh } from "../console/types";
import { uiTruth, uiFresh } from "../console/types";

export interface ServiceSpotlight {
  readonly name: string;
  readonly repo: string;
  readonly story: string;
  readonly environments: readonly string[];
  readonly dependencies: readonly string[];
  readonly deploymentPath: readonly string[];
  readonly stats: readonly { readonly label: string; readonly value: string }[];
  readonly truth: UiTruth;
  readonly freshness: UiFresh;
  readonly source: "live" | "demo";
}

interface StoryResponse {
  readonly story?: string; readonly summary?: string;
  readonly repository?: { readonly name?: string };
  readonly deployment_overview?: {
    readonly workloads?: readonly string[];
    readonly path?: readonly string[];
    readonly environments?: readonly string[];
  };
  readonly dependencies?: readonly (string | { name?: string })[];
}
interface ContextResponse {
  readonly environments?: readonly string[];
  readonly counts?: Readonly<Record<string, number>>;
  readonly dependencies?: readonly (string | { name?: string })[];
}

function names(list: readonly (string | { name?: string })[] | undefined): string[] {
  return (list ?? []).map((d) => (typeof d === "string" ? d : d.name ?? "")).filter((s) => s.length > 0);
}

export async function loadServiceSpotlight(client: EshuApiClient, name: string): Promise<ServiceSpotlight> {
  const enc = encodeURIComponent(name);
  const storyEnv = await client.get<StoryResponse>(`/api/v0/services/${enc}/story`);
  if (storyEnv.error) throw new EshuEnvelopeError(storyEnv.error);
  const story = storyEnv.data ?? {};
  let ctx: ContextResponse = {};
  try { ctx = (await client.get<ContextResponse>(`/api/v0/services/${enc}/context`)).data ?? {}; } catch { /* optional */ }

  const deps = [...new Set([...names(story.dependencies), ...names(ctx.dependencies)])];
  const environments = story.deployment_overview?.environments ?? ctx.environments ?? [];
  const path = story.deployment_overview?.path
    ?? ["Source", "Build", "Workload", ...(environments.length ? ["Runtime"] : [])];
  const counts = ctx.counts ?? {};
  const stats = Object.entries(counts).slice(0, 4).map(([label, value]) => ({ label, value: String(value) }));

  return {
    name,
    repo: story.repository?.name ?? "",
    story: story.story ?? story.summary ?? `${name} resolved from the Eshu graph.`,
    environments,
    dependencies: deps,
    deploymentPath: path,
    stats: stats.length ? stats : [{ label: "Environments", value: String(environments.length) }, { label: "Dependencies", value: String(deps.length) }],
    truth: uiTruth(storyEnv.truth?.level),
    freshness: uiFresh(storyEnv.truth?.freshness.state),
    source: "live"
  };
}

// Build a spotlight from an already-loaded catalog row (demo / offline).
export function spotlightFromRow(row: ServiceRow): ServiceSpotlight {
  return {
    name: row.name,
    repo: row.repo,
    story: `${row.name} (${row.kind}) from ${row.repo || "the indexed workspace"}.`,
    environments: row.environments,
    dependencies: [],
    deploymentPath: ["Source", "Build", "Workload", ...(row.environments.length ? ["Runtime"] : [])],
    stats: [{ label: "Environments", value: String(row.environments.length) }, { label: "Kind", value: row.kind }],
    truth: uiTruth(row.truth),
    freshness: uiFresh(row.freshness),
    source: "demo"
  };
}
