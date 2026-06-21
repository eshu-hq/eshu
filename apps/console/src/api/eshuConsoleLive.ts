// eshuConsoleLive.ts
// Drop into apps/console/src/api/. Maps the real Eshu HTTP API (v0) into the
// view-models the redesigned console renders. Uses the existing EshuApiClient
// and envelope contract — nothing here flattens truth or freshness.
//
//   import { EshuApiClient } from "./client";
//   const client = new EshuApiClient({ baseUrl: "/eshu-api/", apiKey });
//   const snapshot = await loadConsoleSnapshot(client);
//
// Every section is independent: a missing/disabled endpoint (e.g. a collector
// that isn't enabled) is caught and reported in `provenance`, so the UI can show
// "live" vs "not available" per panel instead of failing the whole page.

import type { EshuApiClient } from "./client";
import { loadCollectorReadiness, type CollectorReadinessRow } from "./collectorReadiness";
import type { EshuTruth, TruthLevel, FreshnessState } from "./envelope";
import { loadImages } from "./imageInventory";
import type { ImagePage, ImageRow } from "./imageInventory";
import {
  emptyRuntime,
  loadAdvisories,
  loadDependenciesSection,
  loadFindings,
  loadIacResources,
  loadImagesSection,
  loadIngesters,
  loadLanguages,
  loadRuntime,
  loadSbom,
  loadSeriesBundle,
  loadServices,
  loadVulnerabilities
} from "./eshuConsoleSections";
import type { SectionContext } from "./eshuConsoleSections";
// severityFromCvss stays re-exported from this module for existing consumers.
export { severityFromCvss } from "./eshuConsoleSections";

// Cloud inventory rows are loaded live per page by api/cloudResources.ts (the
// graph holds ~17k CloudResource nodes, too many for the one-shot snapshot). The
// row type is re-exported here so console view-model consumers have a single
// import surface for live API row shapes.
export type { CloudResourceRow } from "./cloudResources";

export type SectionProvenance = "demo" | "live" | "empty" | "loading" | "unavailable";

export type { ImageRow, ImagePage };
export { loadImages };

export interface ConsoleSnapshot {
  readonly runtime: RuntimeSummary;
  readonly services: readonly ServiceRow[];
  readonly languages: readonly LanguageRow[];
  readonly ingesters: readonly IngesterRow[];
  readonly findings: readonly FindingRow[];
  readonly vulnerabilities: readonly VulnRow[];
  readonly sbom: SbomEvidenceRow | null;
  readonly dependencies: readonly DependencyRow[];
  readonly images: readonly ImageRow[];
  readonly iacResources: readonly IacResourceRow[];
  readonly advisories: readonly AdvisoryRow[];
  readonly collectorReadiness: readonly CollectorReadinessRow[];
  readonly series: SeriesBundle;
  readonly truth: Partial<Record<keyof ConsoleSnapshot, EshuTruth>>;
  readonly provenance: Record<string, SectionProvenance>;
}

export interface RuntimeSummary {
  readonly indexStatus: string;
  readonly repositories: number;
  readonly workloads: number;
  readonly platforms: number;
  readonly instances: number;
  readonly queueOutstanding: number;
  readonly inFlight: number;
  readonly deadLetters: number;
  readonly succeeded: number;
  readonly profile: string;
}

export interface ServiceRow {
  readonly id: string;
  readonly name: string;
  readonly kind: string;
  readonly repo: string;
  readonly environments: readonly string[];
  readonly truth: TruthLevel;
  readonly freshness: FreshnessState;
  // Service-catalog enrichment — present when the catalog API returns a
  // correlated manifest declaration; absent when no manifest is indexed.
  readonly tier?: string;
  readonly category?: string;
  readonly domain?: string;
  readonly language?: string;
}

export interface LanguageRow { readonly language: string; readonly count: number; }
export interface IngesterRow {
  readonly id: string;
  readonly kind: string;
  readonly state: string;
  // facts is null when the source does not report a fact count (e.g. coordinator
  // collector instances), so the UI can show "—" rather than a misleading 0.
  readonly facts: number | null;
  readonly freshness: FreshnessState;
}
export interface FindingRow {
  readonly id: string;
  readonly type: string;
  readonly entity: string;
  readonly title: string;
  readonly detail: string;
  readonly truth: TruthLevel;
  readonly entityId?: string;
  readonly repoId?: string;
  readonly filePath?: string;
  readonly startLine?: number;
  readonly endLine?: number;
  readonly language?: string;
  readonly labels?: readonly string[];
  readonly classification?: string;
}
export interface VulnRow {
  readonly id: string;
  readonly package: string;
  readonly severity: string;
  readonly cvss: number;
  readonly kev: boolean;
  readonly fixedVersion: string | null;
  readonly services: readonly string[];
}

// DependencyRow is one package dependency edge from GET /api/v0/dependencies.
// Forward rows describe what the anchor package depends on; reverse rows
// describe which package depends on the anchor. related identifies the other end
// of the edge for the active direction.
export interface DependencyRow {
  readonly direction: "forward" | "reverse";
  readonly anchorPackage: string;
  readonly anchorPackageId: string;
  readonly declaringVersion: string;
  readonly relatedPackage: string;
  readonly relatedPackageId: string;
  readonly ecosystem: string;
  readonly range: string;
  readonly dependencyType: string;
  readonly optional: boolean;
  readonly edgeId: string;
}

// DependencyQuery anchors a dependency lookup. package is required for reverse
// and optional (browse) for forward; cursor pages a prior truncated response.
export interface DependencyQuery {
  readonly direction: "forward" | "reverse";
  readonly pkg?: string;
  readonly ecosystem?: string;
  readonly afterName?: string;
  readonly afterEdge?: string;
  readonly limit?: number;
}

// DependencyPage is a bounded page of dependency rows plus paging and truth
// metadata, mirroring the GET /api/v0/dependencies envelope.
export interface DependencyPage {
  readonly rows: readonly DependencyRow[];
  readonly direction: "forward" | "reverse";
  readonly truncated: boolean;
  readonly nextCursor: { readonly afterName: string; readonly afterEdge: string } | null;
  readonly truth: EshuTruth | null;
}

// IacResourceRow is one Terraform/IaC node from GET /api/v0/iac/resources. The
// list defaults to Terraform resources; provider/service/category/module and
// source locations are present only on canonically attributed nodes, so they may
// be empty/null for tfstate-only rows.
export interface IacResourceRow {
  readonly category: string;
  readonly id: string;
  readonly kind: string;
  readonly lineNumber: number | null;
  readonly resourceName: string;
  readonly name: string;
  readonly type: string;
  readonly provider: string;
  readonly service: string;
  readonly module: string;
  readonly repoId: string;
  readonly relativePath: string;
}

// AdvisoryRow is one row of the browsable vulnerability-intelligence catalog
// (GET /api/v0/supply-chain/advisories). Unlike VulnRow it is known source
// intelligence only and does not imply service reachability or impact.
export interface AdvisoryRow {
  readonly id: string;
  readonly cveId: string;
  readonly ghsaId: string;
  readonly severity: string;
  readonly cvss: number;
  readonly kev: boolean;
  readonly ecosystems: readonly string[];
  readonly packageIds: readonly string[];
  readonly publishedAt: string;
}

export interface SeriesBundle {
  readonly ingestRate: readonly number[];
  readonly queueDepth: readonly number[];
  readonly deadLetters: readonly number[];
  readonly graphNodes: readonly number[];
  readonly graphEdges: readonly number[];
  readonly queryP50: readonly number[];
  readonly queryP95: readonly number[];
  readonly queryP99: readonly number[];
  readonly newVulns: readonly number[];
}

// SbomEvidenceRow is the cheap SBOM/attestation rollup the snapshot carries so
// the dashboard can show whether supply-chain attestation evidence exists at a
// glance. The full subject browse + per-subject provenance live on the SBOM
// page (see api/sbomEvidence.ts), which calls the same reducer-owned endpoints
// directly. total/verified are counts only; this row adds no new graph reads.
export interface SbomEvidenceRow {
  readonly total: number;
  readonly verified: number;
  readonly sbomCount: number;
  readonly attestationCount: number;
}

// isAbortError reports whether a thrown value is a request-abort (not a real
// endpoint failure). Under React StrictMode the boot effect re-runs and the
// browser aborts in-flight fetches, so a transient AbortError must be retried
// once rather than degrading the section to empty. A genuine 500/timeout is a
// plain Error / TimeoutError and is intentionally NOT treated as an abort, so
// real failures still degrade visibly. We match by name to cover both DOMException
// ("AbortError") and any host that surfaces the same name on a plain object.
function isAbortError(error: unknown): boolean {
  return (
    typeof error === "object" &&
    error !== null &&
    (error as { name?: unknown }).name === "AbortError"
  );
}

// section runs one snapshot section, records its provenance, and never lets a
// single failing endpoint fail the whole snapshot. A transient AbortError is
// retried exactly once (StrictMode re-run / browser abort); any other error,
// or a second abort, degrades the section to "unavailable".
async function section<T>(
  prov: Record<string, SectionProvenance>,
  key: string,
  load: () => Promise<T | null>
): Promise<T | null> {
  for (let attempt = 0; attempt < 2; attempt += 1) {
    try {
      const value = await load();
      prov[key] = value === null ? "empty" : "live";
      return value;
    } catch (error) {
      if (attempt === 0 && isAbortError(error)) {
        continue; // retry once on a transient request abort
      }
      prov[key] = "unavailable";
      return null;
    }
  }
  prov[key] = "unavailable";
  return null;
}

// loadConsoleSnapshot fans every independent snapshot section out concurrently
// so the cold first paint costs roughly the slowest single API call rather than
// the sum of ~15 serial calls (issue #1727). The only ordering dependency is
// repoNames: loadServices populates the catalog repo_id -> name map that
// vulnerabilities and dead-code findings read for human repository labels, so
// those sections start after services resolves. Every other section runs in
// parallel via the section() wrapper, which also retries a transient AbortError
// once so a StrictMode re-run / browser abort does not leave a populated
// endpoint blank.
export async function loadConsoleSnapshot(client: EshuApiClient): Promise<ConsoleSnapshot> {
  const prov: Record<string, SectionProvenance> = {};
  const truth: Partial<Record<keyof ConsoleSnapshot, EshuTruth>> = {};
  const ctx: SectionContext = { truth, repoNames: new Map<string, string>() };
  const runSection = <T>(key: string, load: () => Promise<T | null>): Promise<T | null> =>
    section(prov, key, load);

  // Launch every independent section immediately so the requests overlap.
  // services must resolve before downstream sections can resolve repository
  // labels, so its promise is captured and awaited before those fan-outs.
  const servicesPromise = runSection("services", () => loadServices(client, ctx));

  const runtimePromise = runSection("runtime", () => loadRuntime(client, ctx));
  const languagesPromise = runSection("languages", () => loadLanguages(client));
  const ingestersPromise = runSection("ingesters", () => loadIngesters(client));
  const sbomPromise = runSection("sbom", () => loadSbom(client, ctx));
  const dependenciesPromise = runSection("dependencies", () => loadDependenciesSection(client, ctx));
  const imagesPromise = runSection("images", () => loadImagesSection(client, ctx));
  const iacPromise = runSection("iacResources", () => loadIacResources(client, ctx));
  const advisoriesPromise = runSection("advisories", () => loadAdvisories(client, ctx));
  const collectorReadinessPromise = runSection("collectorReadiness", async () => {
    const page = await loadCollectorReadiness(client);
    if (page.truth) truth.collectorReadiness = page.truth;
    return page.rows.length > 0 ? page.rows : null;
  });
  const seriesPromise = loadSeriesBundle(client, runSection);

  // vulnerabilities and dead-code findings depend on catalog-derived repoNames,
  // so they only start after services resolves; they then run concurrently with
  // the rest.
  await servicesPromise;
  const findingsPromise = runSection("findings", () => loadFindings(client, ctx));
  const vulnerabilitiesPromise = runSection("vulnerabilities", () => loadVulnerabilities(client, ctx));

  const [
    runtime,
    services,
    languages,
    ingesters,
    findings,
    vulnerabilities,
    sbom,
    dependencies,
    images,
    iacResources,
    advisories,
    collectorReadiness,
    series
  ] = await Promise.all([
    runtimePromise,
    servicesPromise,
    languagesPromise,
    ingestersPromise,
    findingsPromise,
    vulnerabilitiesPromise,
    sbomPromise,
    dependenciesPromise,
    imagesPromise,
    iacPromise,
    advisoriesPromise,
    collectorReadinessPromise,
    seriesPromise
  ] as const);

  return {
    runtime: runtime ?? emptyRuntime(),
    services: services ?? [],
    languages: languages ?? [],
    ingesters: ingesters ?? [],
    findings: findings ?? [],
    vulnerabilities: vulnerabilities ?? [],
    sbom,
    dependencies: dependencies ?? [],
    images: images ?? [],
    iacResources: iacResources ?? [],
    advisories: advisories ?? [],
    collectorReadiness: collectorReadiness ?? [],
    series,
    truth,
    provenance: prov
  };
}
