// console/liveModel.ts
// Private/API model construction. This module owns (a) the snapshot -> UI model
// lift and (b) an empty model used for loading / needs-connection / error
// states. There is no demo fallback here: an empty model carries real
// "unavailable" provenance, never fabricated numbers. Route pages hydrate their graph views directly
// from graph-specific APIs, so this snapshot shell stays graph-empty and never
// invents topology. Metrics series come from the live time-series API when
// available.

import type {
  ConsoleModel,
  ConsoleSnapshot,
  RuntimeSummary,
  SeriesBundle,
  SectionProvenance,
} from "./types";

export const emptySeries: SeriesBundle = {
  ingestRate: [],
  queueDepth: [],
  deadLetters: [],
  graphNodes: [],
  graphEdges: [],
  queryP50: [],
  queryP95: [],
  queryP99: [],
  newVulns: [],
  metricsConfigured: true,
};

const SNAPSHOT_SECTIONS = [
  "runtime",
  "services",
  "languages",
  "ingesters",
  "findings",
  "vulnerabilities",
  "sbom",
  "dependencies",
  "images",
  "iacResources",
  "advisories",
  "collectorReadiness",
  "argoCDApps",
] as const;

function emptyRuntime(): RuntimeSummary {
  return {
    indexStatus: "unavailable",
    repositories: 0,
    workloads: 0,
    platforms: 0,
    instances: 0,
    queueOutstanding: 0,
    inFlight: 0,
    deadLetters: 0,
    succeeded: 0,
    profile: "unknown",
  };
}

// emptySnapshot builds a live snapshot with no rows. Pass a provenance state
// (e.g. "unavailable" after a failed connection) to stamp every section so panels
// can render an explicit empty/unavailable state instead of a fabricated zero.
export function emptySnapshot(provenance: SectionProvenance | null = null): ConsoleSnapshot {
  const prov: Record<string, SectionProvenance> = {};
  if (provenance) for (const section of SNAPSHOT_SECTIONS) prov[section] = provenance;
  return {
    runtime: emptyRuntime(),
    services: [],
    languages: [],
    ingesters: [],
    findings: [],
    vulnerabilities: [],
    sbom: null,
    dependencies: [],
    images: [],
    iacResources: [],
    advisories: [],
    advisoryCatalogSummary: null,
    advisoryCatalogNextCursor: null,
    collectorReadiness: [],
    argoCDApps: [],
    series: emptySeries,
    truth: {},
    provenance: prov,
  };
}

// modelFromSnapshot lifts a live snapshot into the UI model. Route-level pages
// own graph/API fan-out (Dashboard, Explorer, Code Graph, Topology, Workspace),
// so the shared snapshot model stays graph-empty instead of caching partial
// topology globally.
export function modelFromSnapshot(snap: ConsoleSnapshot): ConsoleModel {
  return { ...snap, source: "live", graph: { nodes: [], edges: [] }, relationships: [] };
}

// emptyConsoleModel is the model used before/while connecting and after a failed
// connection. It is always source: "live"; explicit demo mode uses demoModel
// instead of this unavailable private/API model.
export function emptyConsoleModel(provenance: SectionProvenance | null = null): ConsoleModel {
  return modelFromSnapshot(emptySnapshot(provenance));
}
