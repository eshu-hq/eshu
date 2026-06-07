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
import type { EshuTruth, TruthLevel, FreshnessState } from "./envelope";
import { loadDependencies } from "./eshuDependencies";
import { imageRowsFromResponse, loadImages } from "./imageInventory";
import type { ImagePage, ImageRow } from "./imageInventory";

export type SectionProvenance = "live" | "empty" | "unavailable";

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
export interface SeriesBundle {
  readonly ingestRate: readonly number[];
  readonly queueDepth: readonly number[];
  readonly graphNodes: readonly number[];
  readonly graphEdges: readonly number[];
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

// ---- endpoint response shapes (partial; see GET /api/v0/openapi.json) ----
interface EcosystemOverview {
  // The API field is repo_count; repository_count is kept as a defensive alias.
  readonly repo_count?: number;
  readonly repository_count?: number;
  readonly workload_count?: number;
  readonly platform_count?: number;
  readonly instance_count?: number;
}
interface CollectorInstanceStatus {
  readonly collector_kind?: string;
  readonly instance_id?: string;
  readonly enabled?: boolean;
  readonly mode?: string;
  readonly last_observed_at?: string | null;
  readonly deactivated_at?: string | null;
}
interface IndexStatus {
  readonly status?: string;
  readonly repository_count?: number;
  readonly queue?: {
    readonly outstanding?: number; readonly pending?: number;
    readonly in_flight?: number; readonly dead_letter?: number; readonly succeeded?: number;
  };
  readonly coordinator?: {
    readonly collector_instances?: readonly CollectorInstanceStatus[];
  };
}
interface CatalogResponse {
  readonly repositories?: readonly CatalogRecord[];
  readonly services?: readonly CatalogRecord[];
  readonly workloads?: readonly CatalogRecord[];
}
interface CatalogRecord {
  readonly id?: string; readonly name?: string; readonly kind?: string;
  readonly repo_name?: string; readonly repo_id?: string; readonly repo_slug?: string;
  readonly environments?: readonly string[]; readonly materialization_status?: string;
}
interface LanguageInventory { readonly languages?: readonly { language: string; count?: number; repository_count?: number; file_count?: number }[]; }
interface IngesterStatus { readonly ingesters?: readonly Record<string, unknown>[]; }
interface DeadCodeResponse { readonly results?: readonly { name?: string; file_path?: string; repo_name?: string; repo_id?: string; classification?: string }[]; }
interface ImpactFindings { readonly findings?: readonly Record<string, unknown>[]; readonly results?: readonly Record<string, unknown>[]; }
interface MetricsTimeSeriesResponse { readonly points?: readonly { readonly t?: string; readonly v?: number }[]; }
interface SBOMAttachmentCount {
  readonly total_attachments?: number;
  readonly by_attachment_status?: Readonly<Record<string, number>>;
  readonly by_artifact_kind?: Readonly<Record<string, number>>;
}

// Impact findings carry a CVSS score but no severity label; derive the standard
// CVSS v3 qualitative band so the vulnerability list can colour-rank rows.
function severityFromCvss(cvss: number): string {
  if (cvss >= 9) return "critical";
  if (cvss >= 7) return "high";
  if (cvss >= 4) return "medium";
  if (cvss > 0) return "low";
  return "unknown";
}

// serviceLabel turns a raw graph id (e.g. repository:r_f9600c28 or
// repository_r_f9600c28) into a human label. It prefers the catalog repo name
// and, failing that, strips the internal entity-type prefix (repository:,
// workload:, service:, with `:` or `_`) so the UI never shows the bare graph id.
function serviceLabel(id: string, repoNames: ReadonlyMap<string, string>): string {
  const trimmed = id.trim();
  const mapped = repoNames.get(trimmed);
  if (mapped) return mapped;
  return trimmed.replace(/^(?:repository|workload|service)[:_]/i, "");
}

async function section<T>(
  prov: Record<string, SectionProvenance>,
  key: string,
  load: () => Promise<T | null>
): Promise<T | null> {
  try {
    const value = await load();
    prov[key] = value === null ? "empty" : "live";
    return value;
  } catch {
    prov[key] = "unavailable";
    return null;
  }
}

export async function loadConsoleSnapshot(client: EshuApiClient): Promise<ConsoleSnapshot> {
  const prov: Record<string, SectionProvenance> = {};
  const truth: Partial<Record<keyof ConsoleSnapshot, EshuTruth>> = {};

  const runtime = (await section(prov, "runtime", async () => {
    let overview: EcosystemOverview = {};
    let profile = "unknown";
    try {
      const eco = await client.get<EcosystemOverview>("/api/v0/ecosystem/overview");
      overview = eco.data ?? {};
      if (eco.truth) { truth.runtime = eco.truth; profile = eco.truth.profile ?? profile; }
    } catch { /* optional */ }
    // index-status is a raw status payload, not the eshu envelope, so read it as
    // plain JSON (client.get would unwrap a non-existent `data` field to nothing).
    let st: IndexStatus = {};
    try { st = await client.getJson<IndexStatus>("/api/v0/index-status"); } catch { /* optional */ }
    const q = st.queue ?? {};
    return {
      indexStatus: st.status ?? "unknown",
      repositories: st.repository_count ?? overview.repo_count ?? overview.repository_count ?? 0,
      workloads: overview.workload_count ?? 0,
      platforms: overview.platform_count ?? 0,
      instances: overview.instance_count ?? 0,
      queueOutstanding: q.outstanding ?? q.pending ?? 0,
      inFlight: q.in_flight ?? 0,
      deadLetters: q.dead_letter ?? 0,
      succeeded: q.succeeded ?? 0,
      profile
    } satisfies RuntimeSummary;
  })) ?? emptyRuntime();

  // repo_id -> human name, harvested from the catalog so downstream sections
  // (e.g. vulnerabilities, which carry only repository_id) can show a readable
  // service/repo name instead of the raw graph id.
  const repoNames = new Map<string, string>();

  const services = (await section(prov, "services", async () => {
    const env = await client.get<CatalogResponse>("/api/v0/catalog?limit=2000&offset=0");
    const c = env.data ?? {};
    if (env.truth) truth.services = env.truth;
    const lvl = env.truth?.level ?? "exact";
    const fresh = env.truth?.freshness.state ?? "fresh";
    // services and workloads can overlap (a workload promoted to a service, or
    // the same workload listed across environments); dedup by id so the catalog
    // list has unique React keys and no duplicated rows.
    const byId = new Map<string, ServiceRow>();
    for (const w of [...(c.services ?? []), ...(c.workloads ?? [])]) {
      const repoId = w.repo_id?.trim();
      const friendly = w.repo_name?.trim() || w.name?.trim();
      if (repoId && friendly && !repoNames.has(repoId)) repoNames.set(repoId, friendly);
      const id = w.id ?? w.name ?? "";
      if (id === "" || byId.has(id)) continue;
      byId.set(id, {
        id, name: w.name ?? w.id ?? "", kind: w.kind ?? "service",
        repo: w.repo_name ?? w.repo_id ?? "", environments: w.environments ?? [],
        truth: lvl, freshness: fresh
      });
    }
    const rows = [...byId.values()];
    return rows.length > 0 ? rows : null;
  })) ?? [];

  const languages = (await section(prov, "languages", async () => {
    // language-inventory is the "what languages exist" aggregate; by-language
    // requires a specific ?language= and 400s without it.
    const env = await client.get<LanguageInventory>("/api/v0/repositories/language-inventory?limit=100&offset=0");
    const rows = (env.data?.languages ?? []).map((l) => ({ language: l.language, count: l.repository_count ?? l.count ?? l.file_count ?? 0 }));
    return rows.length > 0 ? rows : null;
  })) ?? [];

  const ingesters = (await section(prov, "ingesters", async () => {
    // Two raw status payloads (not the eshu envelope): status/ingesters reports
    // the repository ingester; index-status.coordinator.collector_instances
    // reports every configured collector. Merge both so the operator sees the
    // full fact-source roster, deduped by id.
    const rows: IngesterRow[] = [];
    try {
      const data = await client.getJson<IngesterStatus>("/api/v0/status/ingesters");
      for (const [i, g] of (data?.ingesters ?? []).entries()) {
        rows.push({
          id: String(g.name ?? g.id ?? g.ingester ?? `ingester-${i}`),
          kind: String(g.runtime_family ?? g.kind ?? g.name ?? g.ingester ?? "ingester"),
          state: String(g.health ?? g.state ?? g.status ?? "healthy"),
          facts: Number(g.fact_count ?? g.facts ?? 0),
          freshness: (g.freshness as FreshnessState) ?? "fresh"
        });
      }
    } catch { /* ingester status optional */ }
    try {
      const st = await client.getJson<IndexStatus>("/api/v0/index-status");
      for (const c of st.coordinator?.collector_instances ?? []) {
        const id = String(c.instance_id ?? c.collector_kind ?? "");
        if (id === "" || rows.some((r) => r.id === id)) continue;
        const deactivated = c.deactivated_at != null;
        const enabled = c.enabled !== false;
        rows.push({
          id,
          kind: String(c.collector_kind ?? id),
          state: deactivated ? "deactivated" : enabled ? "active" : "disabled",
          // collector instances carry no fact count in this payload.
          facts: null,
          freshness: deactivated || !enabled ? "stale" : c.last_observed_at ? "fresh" : "building"
        });
      }
    } catch { /* coordinator status optional */ }
    return rows.length > 0 ? rows : null;
  })) ?? [];

  const findings = (await section(prov, "findings", async () => {
    const env = await client.post<DeadCodeResponse>("/api/v0/code/dead-code", { limit: 25 });
    const lvl = env.truth?.level ?? "derived";
    const rows = (env.data?.results ?? []).map((r, i) => ({
      id: `dead-code-${i}`, type: "Dead code",
      entity: r.repo_name ?? r.repo_id ?? "repository",
      title: `Unreferenced symbol ${r.name ?? "candidate"}`,
      detail: `${r.file_path ?? "unknown"}${r.classification ? ` · ${r.classification}` : ""}`,
      truth: lvl
    }));
    return rows.length > 0 ? rows : null;
  })) ?? [];

  const vulnerabilities = (await section(prov, "vulnerabilities", async () => {
    // impact/findings requires an anchor; query the affected impact statuses
    // (vulnerabilities reachable in indexed services) and merge them.
    const rows: VulnRow[] = [];
    const seen = new Set<string>();
    for (const status of ["affected_exact", "affected_derived"]) {
      let env;
      try {
        env = await client.get<ImpactFindings>(`/api/v0/supply-chain/impact/findings?limit=100&impact_status=${status}`);
      } catch { continue; }
      for (const v of (env.data?.findings ?? env.data?.results ?? [])) {
        const id = String(v.advisory_id ?? v.cve_id ?? v.id ?? `adv-${rows.length}`);
        if (seen.has(id)) continue;
        seen.add(id);
        const cvss = Number(v.cvss ?? v.cvss_score ?? 0);
        const sev = (v.severity ? String(v.severity) : severityFromCvss(cvss)).toLowerCase();
        rows.push({
          id,
          package: String(v.package ?? v.package_name ?? v.subject ?? "—"),
          severity: sev,
          cvss,
          kev: Boolean(v.kev ?? v.known_exploited),
          fixedVersion: (v.fixed_version as string) ?? null,
          // affected_services is already human-readable; service_id/repository_id
          // are raw graph ids, so resolve them to catalog names before display.
          services: (v.affected_services as string[]) ?? (
            v.service_id ? [serviceLabel(String(v.service_id), repoNames)]
              : v.repository_id ? [serviceLabel(String(v.repository_id), repoNames)] : []
          )
        });
      }
    }
    return rows.length > 0 ? rows : null;
  })) ?? [];

  // SBOM/attestation evidence: a cheap count rollup for the snapshot. The
  // count endpoint requires no scope and stays bounded; the full subject browse
  // and per-subject provenance are loaded on demand by the SBOM page. The
  // `attached_verified` status names the trusted-attestation count.
  const sbom = await section(prov, "sbom", async () => {
    const env = await client.get<SBOMAttachmentCount>("/api/v0/supply-chain/sbom-attestations/attachments/count");
    const data = env.data ?? {};
    if (env.truth) truth.sbom = env.truth;
    const total = Number(data.total_attachments ?? 0);
    if (total <= 0) return null;
    const byStatus = data.by_attachment_status ?? {};
    const byKind = data.by_artifact_kind ?? {};
    return {
      total,
      verified: Number(byStatus.attached_verified ?? 0),
      sbomCount: Number(byKind.sbom ?? 0),
      attestationCount: Number(byKind.attestation ?? 0)
    } satisfies SbomEvidenceRow;
  });

  const dependencies = (await section(prov, "dependencies", async () => {
    const page = await loadDependencies(client, { direction: "forward", limit: 50 });
    if (page.truth) truth.dependencies = page.truth;
    return page.rows.length > 0 ? page.rows : null;
  })) ?? [];

  const images = (await section(prov, "images", async () => {
    // First page of the bounded (:ContainerImage) inventory. The page itself
    // paginates via next_cursor on the Images page; the snapshot only needs the
    // head page to know the section is live vs empty/unavailable.
    const env = await client.get<{ images?: readonly Record<string, unknown>[] }>("/api/v0/images?limit=50&offset=0");
    if (env.truth) truth.images = env.truth;
    const rows = imageRowsFromResponse(env.data);
    return rows.length > 0 ? rows : null;
  })) ?? [];

  const series = await loadSeriesBundle(client, prov);

  return {
    runtime,
    services,
    languages,
    ingesters,
    findings,
    vulnerabilities,
    sbom,
    dependencies,
    images,
    series,
    truth,
    provenance: prov
  };
}

function emptyRuntime(): RuntimeSummary {
  return {
    indexStatus: "unavailable", repositories: 0, workloads: 0, platforms: 0, instances: 0,
    queueOutstanding: 0, inFlight: 0, deadLetters: 0, succeeded: 0, profile: "unknown"
  };
}

const emptySeries: SeriesBundle = {
  ingestRate: [], queueDepth: [], graphNodes: [], graphEdges: [], queryP99: [], newVulns: []
};

async function loadSeriesBundle(
  client: EshuApiClient,
  prov: Record<string, SectionProvenance>
): Promise<SeriesBundle> {
  const [ingestRate, queueDepth, graphNodes, graphEdges, queryP99] = await Promise.all([
    loadMetricSeries(client, prov, "ingestRate", "ingest_rate"),
    loadMetricSeries(client, prov, "queueDepth", "queue_depth"),
    loadMetricSeries(client, prov, "graphNodes", "graph_nodes"),
    loadMetricSeries(client, prov, "graphEdges", "graph_edges"),
    loadMetricSeries(client, prov, "queryP99", "query_p99")
  ]);
  return { ...emptySeries, ingestRate, queueDepth, graphNodes, graphEdges, queryP99 };
}

async function loadMetricSeries(
  client: EshuApiClient,
  prov: Record<string, SectionProvenance>,
  key: keyof Omit<SeriesBundle, "newVulns">,
  metric: string
): Promise<readonly number[]> {
  const values = await section(prov, `series.${key}`, async () => {
    const env = await client.get<MetricsTimeSeriesResponse>(
      `/api/v0/metrics/timeseries?metric=${metric}&window=24h&step=30m`
    );
    const points = (env.data?.points ?? []).map((point) => point.v).filter(isFiniteNumber);
    return points.length > 0 ? points : null;
  });
  return values ?? [];
}

function isFiniteNumber(value: number | undefined): value is number {
  return typeof value === "number" && Number.isFinite(value);
}
