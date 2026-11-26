// api/knowledgeHome.ts
// Loads the reads the Home page renders. Each read is independent: one slow,
// forbidden or failing endpoint degrades only its own panel, never the page.
// Wire shapes come from the generated contract types (api/contracts/wire), so
// demo fixtures and live responses share one type.
import { EshuApiHttpError, type EshuApiClient } from "./client";
import type { CatalogWire } from "./contracts/wire/CatalogWire";
import type { DocumentationFindingsCountWire } from "./contracts/wire/DocumentationFindingsCountWire";
import type { EcosystemOverviewWire } from "./contracts/wire/EcosystemOverviewWire";
import type { IndexStatusWire } from "./contracts/wire/IndexStatusWire";
import type { InvestigationWorkflowsWire } from "./contracts/wire/InvestigationWorkflowsWire";
import type { StatusCollectorReadinessWire } from "./contracts/wire/StatusCollectorReadinessWire";
import type { SupplyChainImpactFindingsCountWire } from "./contracts/wire/SupplyChainImpactFindingsCountWire";
import type { EshuEnvelope, EshuTruth } from "./envelope";

// Why a panel has no data, in terms a reader can act on.
export type UnavailableReason = "forbidden" | "slow" | "not_ready" | "failed";

export type Section<T> =
  | { readonly status: "loading" }
  | { readonly status: "ready"; readonly data: T; readonly truth: EshuTruth | null }
  | { readonly status: "unavailable"; readonly reason: UnavailableReason; readonly detail: string };

export interface KnowledgeHomeData {
  readonly overview: Section<EcosystemOverviewWire>;
  readonly index: Section<IndexStatusWire>;
  readonly collectors: Section<StatusCollectorReadinessWire>;
  readonly vulnerabilities: Section<SupplyChainImpactFindingsCountWire>;
  readonly docs: Section<DocumentationFindingsCountWire>;
  readonly workflows: Section<InvestigationWorkflowsWire>;
  readonly catalog: Section<CatalogWire>;
}

export const HOME_READS = {
  overview: "/api/v0/ecosystem/overview",
  index: "/api/v0/index-status",
  collectors: "/api/v0/status/collector-readiness",
  vulnerabilities: "/api/v0/supply-chain/impact/findings/count",
  docs: "/api/v0/documentation/findings/count",
  workflows: "/api/v0/investigation-workflows?view=full&limit=50",
  catalog: "/api/v0/catalog?limit=200&offset=0",
} as const satisfies Record<keyof KnowledgeHomeData, string>;

export const loadingHome: KnowledgeHomeData = {
  overview: { status: "loading" },
  index: { status: "loading" },
  collectors: { status: "loading" },
  vulnerabilities: { status: "loading" },
  docs: { status: "loading" },
  workflows: { status: "loading" },
  catalog: { status: "loading" },
};

function isEnvelope(value: unknown): value is EshuEnvelope<unknown> {
  return typeof value === "object" && value !== null && "data" in value && "error" in value && "truth" in value;
}

// readSection reads one route and folds every failure into an unavailable
// section. Some reads are enveloped and some (index-status) are raw payloads;
// both are accepted.
export async function readSection<T>(client: EshuApiClient, path: string, signal?: AbortSignal): Promise<Section<T>> {
  try {
    const body: unknown = await client.get<T>(path, { signal });
    if (!isEnvelope(body)) {
      return { status: "ready", data: body as T, truth: null };
    }
    if (body.error !== null) {
      return { status: "unavailable", reason: "not_ready", detail: body.error.message };
    }
    if (body.data === null) {
      return { status: "unavailable", reason: "failed", detail: "The response had no data." };
    }
    return { status: "ready", data: body.data as T, truth: body.truth };
  } catch (error) {
    return unavailableFrom(error);
  }
}

export function unavailableFrom(error: unknown): Section<never> {
  if (error instanceof EshuApiHttpError) {
    const detail = error.error?.message ?? `HTTP ${error.status}`;
    if (error.status === 401 || error.status === 403) return { status: "unavailable", reason: "forbidden", detail };
    if (error.status === 504 || error.status === 408) return { status: "unavailable", reason: "slow", detail };
    if (error.status === 503) return { status: "unavailable", reason: "not_ready", detail };
    return { status: "unavailable", reason: "failed", detail };
  }
  if (error instanceof DOMException && error.name === "TimeoutError") {
    return { status: "unavailable", reason: "slow", detail: error.message };
  }
  return { status: "unavailable", reason: "failed", detail: error instanceof Error ? error.message : String(error) };
}

// loadKnowledgeHome fires every Home read in parallel and reports each section
// as soon as it settles, so fast panels render while slow ones are pending.
export async function loadKnowledgeHome(
  client: EshuApiClient,
  onSection: <K extends keyof KnowledgeHomeData>(key: K, section: KnowledgeHomeData[K]) => void,
  signal?: AbortSignal,
): Promise<void> {
  const keys = Object.keys(HOME_READS) as (keyof KnowledgeHomeData)[];
  await Promise.all(
    keys.map(async (key) => {
      const section = await readSection<never>(client, HOME_READS[key], signal);
      if (!signal?.aborted) onSection(key, section as KnowledgeHomeData[typeof key]);
    }),
  );
}
