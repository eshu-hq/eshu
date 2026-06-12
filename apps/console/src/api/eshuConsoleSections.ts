// eshuConsoleSections.ts
// Per-section loaders for the console snapshot (see eshuConsoleLive.ts). Each
// loader maps one Eshu HTTP API surface into a console view-model slice and
// returns null for "empty" so the orchestrator's section() wrapper can record
// accurate provenance. Splitting these out of eshuConsoleLive.ts keeps both
// files under the repo 500-line cap while preserving the same truth-capture and
// degradation semantics. Live-API data only — nothing here fabricates rows.

import type { EshuApiClient } from "./client";
import type { EshuTruth, FreshnessState } from "./envelope";
import { loadDependencies } from "./eshuDependencies";
import { fetchAdvisoryCatalogPage } from "./eshuConsoleAdvisories";
import { imageRowsFromResponse } from "./imageInventory";
import type {
  AdvisoryRow,
  ConsoleSnapshot,
  DependencyRow,
  FindingRow,
  IacResourceRow,
  IngesterRow,
  LanguageRow,
  RuntimeSummary,
  SbomEvidenceRow,
  SeriesBundle,
  ServiceRow,
  VulnRow
} from "./eshuConsoleLive";

// SectionContext carries the cross-section mutable state the snapshot threads
// through every loader: the per-key truth capture and the catalog-derived
// repo_id -> human name map. repoNames is populated by loadServices and read by
// loadVulnerabilities (serviceLabel), so the orchestrator MUST await services
// before launching vulnerabilities.
export interface SectionContext {
  readonly truth: Partial<Record<keyof ConsoleSnapshot, EshuTruth>>;
  readonly repoNames: Map<string, string>;
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
interface DeadCodeResponse {
  readonly results?: readonly {
    readonly name?: string;
    readonly file_path?: string;
    readonly repo_name?: string;
    readonly repo_id?: string;
    readonly classification?: string;
    readonly entity_id?: string;
    readonly start_line?: number;
    readonly end_line?: number;
    readonly language?: string;
    readonly labels?: readonly string[];
  }[];
}
interface ImpactFindings { readonly findings?: readonly Record<string, unknown>[]; readonly results?: readonly Record<string, unknown>[]; }
interface MetricsTimeSeriesResponse { readonly points?: readonly { readonly t?: string; readonly v?: number }[]; }
interface SBOMAttachmentCount {
  readonly total_attachments?: number;
  readonly by_attachment_status?: Readonly<Record<string, number>>;
  readonly by_artifact_kind?: Readonly<Record<string, number>>;
}
interface IacResourcesResponse {
  readonly resources?: readonly {
    readonly id?: string; readonly kind?: string; readonly name?: string;
    readonly type?: string; readonly provider?: string; readonly resource_service?: string;
    readonly module?: string; readonly repo_id?: string; readonly relative_path?: string;
  }[];
}

// Impact findings carry a CVSS score but no severity label; derive the standard
// CVSS v3 qualitative band so the vulnerability list can colour-rank rows.
export function severityFromCvss(cvss: number): string {
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

// emptyRuntime is the unknown-status baseline the runtime section degrades to
// when both the ecosystem overview and index-status sub-fetches are unavailable.
export function emptyRuntime(): RuntimeSummary {
  return {
    indexStatus: "unavailable", repositories: 0, workloads: 0, platforms: 0, instances: 0,
    queueOutstanding: 0, inFlight: 0, deadLetters: 0, succeeded: 0, profile: "unknown"
  };
}

// loadRuntime reads the ecosystem overview (enveloped) and index-status (raw
// JSON) into the runtime summary. Each sub-fetch is optional and swallows its
// own failure so the snapshot degrades to the unknown baseline rather than
// throwing.
export async function loadRuntime(client: EshuApiClient, ctx: SectionContext): Promise<RuntimeSummary | null> {
  let overview: EcosystemOverview = {};
  let profile = "unknown";
  try {
    const eco = await client.get<EcosystemOverview>("/api/v0/ecosystem/overview");
    overview = eco.data ?? {};
    if (eco.truth) { ctx.truth.runtime = eco.truth; profile = eco.truth.profile ?? profile; }
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
}

// loadServices reads the catalog into the service/workload roster and harvests
// the repo_id -> human name map used by downstream sections (vulnerabilities).
// Because it mutates ctx.repoNames, the orchestrator awaits it before launching
// the vulnerabilities section.
export async function loadServices(client: EshuApiClient, ctx: SectionContext): Promise<readonly ServiceRow[] | null> {
  const env = await client.get<CatalogResponse>("/api/v0/catalog?limit=2000&offset=0");
  const c = env.data ?? {};
  if (env.truth) ctx.truth.services = env.truth;
  const lvl = env.truth?.level ?? "exact";
  const fresh = env.truth?.freshness.state ?? "fresh";
  // services and workloads can overlap (a workload promoted to a service, or
  // the same workload listed across environments); dedup by id so the catalog
  // list has unique React keys and no duplicated rows.
  const byId = new Map<string, ServiceRow>();
  for (const w of [...(c.services ?? []), ...(c.workloads ?? [])]) {
    const repoId = w.repo_id?.trim();
    const friendly = w.repo_name?.trim() || w.name?.trim();
    if (repoId && friendly && !ctx.repoNames.has(repoId)) ctx.repoNames.set(repoId, friendly);
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
}

// loadLanguages reads the language-inventory aggregate. by-language requires a
// specific ?language= and 400s without it, so it is intentionally not used.
export async function loadLanguages(client: EshuApiClient): Promise<readonly LanguageRow[] | null> {
  const env = await client.get<LanguageInventory>("/api/v0/repositories/language-inventory?limit=100&offset=0");
  const rows = (env.data?.languages ?? []).map((l) => ({ language: l.language, count: l.repository_count ?? l.count ?? l.file_count ?? 0 }));
  return rows.length > 0 ? rows : null;
}

// loadIngesters merges two raw status payloads: status/ingesters (the repository
// ingester) and index-status.coordinator.collector_instances (every configured
// collector), deduped by id, so the operator sees the full fact-source roster.
export async function loadIngesters(client: EshuApiClient): Promise<readonly IngesterRow[] | null> {
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
}

// loadFindings reads the dead-code analysis into finding rows. When a catalog
// context is available, repo_id is resolved to the human repository name so
// code-analysis surfaces do not expose internal graph ids.
export async function loadFindings(client: EshuApiClient, ctx?: SectionContext): Promise<readonly FindingRow[] | null> {
  const env = await client.post<DeadCodeResponse>("/api/v0/code/dead-code", { limit: 25 });
  const lvl = env.truth?.level ?? "derived";
  const rows = (env.data?.results ?? []).map((r, i) => ({
    id: r.entity_id ?? `dead-code-${i}`, type: "Dead code",
    entity: deadCodeRepositoryLabel(r, ctx?.repoNames),
    title: `Unreferenced symbol ${nonEmpty(r.name, "candidate")}`,
    detail: `${nonEmpty(r.file_path, "unknown")}${r.classification ? ` · ${r.classification}` : ""}`,
    truth: lvl,
    entityId: r.entity_id,
    repoId: r.repo_id,
    filePath: r.file_path,
    startLine: r.start_line,
    endLine: r.end_line,
    language: r.language,
    labels: r.labels,
    classification: r.classification
  }));
  return rows.length > 0 ? rows : null;
}

function deadCodeRepositoryLabel(
  row: NonNullable<DeadCodeResponse["results"]>[number],
  repoNames?: ReadonlyMap<string, string>
): string {
  const explicitName = row.repo_name?.trim();
  if (explicitName) return explicitName;
  const repoId = row.repo_id?.trim();
  if (repoId && repoNames?.has(repoId)) return repoNames.get(repoId) ?? repoId;
  return nonEmpty(repoId, "repository");
}

function nonEmpty(...values: readonly (string | undefined)[]): string {
  return values.find((value) => value !== undefined && value.trim().length > 0)?.trim() ?? "";
}

// loadVulnerabilities reads the affected impact findings (reachable in indexed
// services) and resolves raw service/repository ids to catalog names via
// ctx.repoNames, which loadServices must have populated first.
export async function loadVulnerabilities(client: EshuApiClient, ctx: SectionContext): Promise<readonly VulnRow[] | null> {
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
          v.service_id ? [serviceLabel(String(v.service_id), ctx.repoNames)]
            : v.repository_id ? [serviceLabel(String(v.repository_id), ctx.repoNames)] : []
        )
      });
    }
  }
  return rows.length > 0 ? rows : null;
}

// loadSbom reads the cheap SBOM/attestation count rollup. The endpoint requires
// no scope and stays bounded; the full subject browse lives on the SBOM page.
export async function loadSbom(client: EshuApiClient, ctx: SectionContext): Promise<SbomEvidenceRow | null> {
  const env = await client.get<SBOMAttachmentCount>("/api/v0/supply-chain/sbom-attestations/attachments/count");
  const data = env.data ?? {};
  if (env.truth) ctx.truth.sbom = env.truth;
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
}

// loadDependenciesSection reads the default forward dependency browse.
export async function loadDependenciesSection(client: EshuApiClient, ctx: SectionContext): Promise<readonly DependencyRow[] | null> {
  const page = await loadDependencies(client, { direction: "forward", limit: 50 });
  if (page.truth) ctx.truth.dependencies = page.truth;
  return page.rows.length > 0 ? page.rows : null;
}

// loadImagesSection reads the head page of the bounded container-image
// inventory. The Images page paginates further via next_cursor; the snapshot
// only needs the head page to know the section is live vs empty/unavailable.
export async function loadImagesSection(client: EshuApiClient, ctx: SectionContext): Promise<ReturnType<typeof imageRowsFromResponse> | null> {
  const env = await client.get<{ images?: readonly Record<string, unknown>[] }>("/api/v0/images?limit=50&offset=0");
  if (env.truth) ctx.truth.images = env.truth;
  const rows = imageRowsFromResponse(env.data);
  return rows.length > 0 ? rows : null;
}

// loadIacResources reads the bounded IaC inventory list. The endpoint requires
// the authoritative graph; on lower profiles it 501s and the section is reported
// unavailable rather than failing the whole snapshot.
export async function loadIacResources(client: EshuApiClient, ctx: SectionContext): Promise<readonly IacResourceRow[] | null> {
  const env = await client.get<IacResourcesResponse>("/api/v0/iac/resources?limit=200");
  if (env.truth) ctx.truth.iacResources = env.truth;
  const rows: IacResourceRow[] = (env.data?.resources ?? []).map((r) => ({
    id: String(r.id ?? ""),
    kind: String(r.kind ?? "resource"),
    name: String(r.name ?? ""),
    type: String(r.type ?? ""),
    provider: String(r.provider ?? ""),
    service: String(r.resource_service ?? ""),
    module: String(r.module ?? ""),
    repoId: String(r.repo_id ?? ""),
    relativePath: String(r.relative_path ?? "")
  }));
  return rows.length > 0 ? rows : null;
}

// loadAdvisories reads the browsable CVE-intelligence catalog (known
// intelligence, not service reachability), seeding only the first highest-CVSS
// page; the Advisories page paginates further.
export async function loadAdvisories(client: EshuApiClient, ctx: SectionContext): Promise<readonly AdvisoryRow[] | null> {
  const page = await fetchAdvisoryCatalogPage(client, { limit: 50 });
  if (page.truth) ctx.truth.advisories = page.truth;
  return page.rows.length > 0 ? [...page.rows] : null;
}

// emptySeries is the all-empty trend baseline the series bundle starts from so
// any unavailable metric reports an empty series rather than fabricated points.
export const emptySeries: SeriesBundle = {
  ingestRate: [], queueDepth: [], graphNodes: [], graphEdges: [], queryP99: [], newVulns: []
};

// loadSeriesBundle fetches every dashboard trend metric concurrently and folds
// them into the series bundle. section() (passed in) records per-metric
// provenance; failures degrade to an empty series.
export async function loadSeriesBundle(
  client: EshuApiClient,
  section: <T>(key: string, load: () => Promise<T | null>) => Promise<T | null>
): Promise<SeriesBundle> {
  const [ingestRate, queueDepth, graphNodes, graphEdges, queryP99] = await Promise.all([
    loadMetricSeries(client, section, "ingestRate", "ingest_rate"),
    loadMetricSeries(client, section, "queueDepth", "queue_depth"),
    loadMetricSeries(client, section, "graphNodes", "graph_nodes"),
    loadMetricSeries(client, section, "graphEdges", "graph_edges"),
    loadMetricSeries(client, section, "queryP99", "query_p99")
  ]);
  return { ...emptySeries, ingestRate, queueDepth, graphNodes, graphEdges, queryP99 };
}

async function loadMetricSeries(
  client: EshuApiClient,
  section: <T>(key: string, load: () => Promise<T | null>) => Promise<T | null>,
  key: keyof Omit<SeriesBundle, "newVulns">,
  metric: string
): Promise<readonly number[]> {
  const values = await section(`series.${key}`, async () => {
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
