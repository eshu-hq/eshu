// eshuConsoleSections.ts
// Per-section loaders for the console snapshot (see eshuConsoleLive.ts). Each
// loader maps one Eshu HTTP API surface into a console view-model slice and
// returns null for "empty" so the orchestrator's section() wrapper can record
// accurate provenance. Splitting these out of eshuConsoleLive.ts keeps both
// files under the repo 500-line cap while preserving the same truth-capture and
// degradation semantics. Live-API data only — nothing here fabricates rows.

import type { EshuApiClient } from "./client";
import { deadCodeRowsFromResponse, type DeadCodeResponse } from "./deadCode";
import { EshuEnvelopeError, type EshuTruth, type FreshnessState } from "./envelope";
import { fetchAdvisoryCatalogPage } from "./eshuConsoleAdvisories";
import type {
  AdvisoryRow,
  ArgoCDAppRow,
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
import { loadDependencies } from "./eshuDependencies";
import { iacResourceRowsFromResponse } from "./iacResources";
import { imageRowsFromResponse } from "./imageInventory";

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
// RepositoryListBrief is the minimal shape read by loadRuntime for the total
// field. It is intentionally separate from the full RepositoryListResponse in
// liveData.ts to keep each file's interface surface narrow.
interface RepositoryListBrief {
  readonly total?: number;
  readonly repository_count?: number; // legacy alias kept for forward-compat
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
  readonly tier?: string; readonly category?: string;
  readonly domain?: string; readonly language?: string;
}
interface LanguageInventory { readonly languages?: readonly { language: string; count?: number; repository_count?: number; file_count?: number }[]; }
interface IngesterStatus { readonly ingesters?: readonly Record<string, unknown>[]; }
interface ImpactFindings { readonly findings?: readonly Record<string, unknown>[]; readonly results?: readonly Record<string, unknown>[]; }
interface MetricsTimeSeriesResponse { readonly points?: readonly { readonly t?: string; readonly v?: number }[]; }
interface InfraSearchResponse {
  readonly results?: readonly {
    readonly id?: string;
    readonly name?: string;
    readonly kind?: string;
    readonly source?: string;
  }[];
}
interface SBOMAttachmentCount {
  readonly total_attachments?: number;
  readonly by_attachment_status?: Readonly<Record<string, number>>;
  readonly by_artifact_kind?: Readonly<Record<string, number>>;
}
interface IacResourcesResponse { readonly resources?: NonNullable<Parameters<typeof iacResourceRowsFromResponse>[0]>["resources"]; }

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

// asString narrows an `unknown` API value to a string. Anything that is not
// a string (including objects, which would otherwise stringify to
// "[object Object]") is treated as missing and the caller falls through to
// the next candidate or the final fallback.
function asString(value: unknown): string {
  return typeof value === "string" ? value : "";
}

// emptyRuntime is the unknown-status baseline the runtime section degrades to
// when both the ecosystem overview and index-status sub-fetches are unavailable.
export function emptyRuntime(): RuntimeSummary {
  return {
    indexStatus: "unavailable", repositories: 0, workloads: 0, platforms: 0, instances: 0,
    queueOutstanding: 0, inFlight: 0, deadLetters: 0, succeeded: 0, profile: "unknown"
  };
}

// loadRuntime reads the ecosystem overview (enveloped), index-status (raw
// JSON), and the repositories list total into the runtime summary. Each
// sub-fetch is optional and swallows its own failure so the snapshot degrades
// to the unknown baseline rather than throwing.
//
// Repository count priority (issue #3392): the repositories API total field is
// the authoritative source because it reflects a true graph COUNT independent
// of page size. index-status repository_count and ecosystem overview repo_count
// are kept as cascading fallbacks for backward compatibility with older server
// versions that predate the total field.
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
  // Probe the repositories list with limit=1 to get the total without fetching
  // the full page. The total field is the true graph COUNT added in issue #3392.
  let repoTotal: number | undefined;
  try {
    const repoList = await client.getJson<RepositoryListBrief>("/api/v0/repositories?limit=1&offset=0");
    repoTotal = repoList.total;
  } catch { /* optional */ }
  const q = st.queue ?? {};
  return {
    indexStatus: st.status ?? "unknown",
    repositories: repoTotal ?? st.repository_count ?? overview.repo_count ?? overview.repository_count ?? 0,
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
  if (env.error) throw new EshuEnvelopeError(env.error);
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
    const row: ServiceRow = {
      id, name: w.name ?? w.id ?? "", kind: w.kind ?? "service",
      repo: w.repo_name ?? w.repo_id ?? "", environments: w.environments ?? [],
      truth: lvl, freshness: fresh,
      ...(w.tier ? { tier: w.tier } : {}),
      ...(w.category ? { category: w.category } : {}),
      ...(w.domain ? { domain: w.domain } : {}),
      ...(w.language ? { language: w.language } : {}),
    };
    byId.set(id, row);
  }
  const rows = [...byId.values()];
  return rows.length > 0 ? rows : null;
}

// loadLanguages reads the language-inventory aggregate. by-language requires a
// specific ?language= and 400s without it, so it is intentionally not used.
export async function loadLanguages(client: EshuApiClient): Promise<readonly LanguageRow[] | null> {
  const env = await client.get<LanguageInventory>("/api/v0/repositories/language-inventory?limit=100&offset=0");
  if (env.error) throw new EshuEnvelopeError(env.error);
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
        id: asString(g.name) || asString(g.id) || asString(g.ingester) || `ingester-${i}`,
        kind: asString(g.runtime_family) || asString(g.kind) || asString(g.name) || asString(g.ingester) || "ingester",
        state: asString(g.health) || asString(g.state) || asString(g.status) || "healthy",
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
  if (env.error) throw new EshuEnvelopeError(env.error);
  const rows = deadCodeRowsFromResponse(env.data, env.truth?.level ?? "derived", ctx?.repoNames);
  return rows.length > 0 ? rows : null;
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
    if (env.error) throw new EshuEnvelopeError(env.error);
    for (const v of (env.data?.findings ?? env.data?.results ?? [])) {
      const id = asString(v.advisory_id) || asString(v.cve_id) || asString(v.id) || `adv-${rows.length}`;
      if (seen.has(id)) continue;
      seen.add(id);
      const cvss = Number(v.cvss ?? v.cvss_score ?? 0);
      const sev = (v.severity ? asString(v.severity) : severityFromCvss(cvss)).toLowerCase();
      rows.push({
        id,
        package: asString(v.package) || asString(v.package_name) || asString(v.subject) || "—",
        severity: sev,
        cvss,
        kev: Boolean(v.kev ?? v.known_exploited),
        fixedVersion: (v.fixed_version as string) ?? null,
        // affected_services is already human-readable; service_id/repository_id
        // are raw graph ids, so resolve them to catalog names before display.
        services: (v.affected_services as string[]) ?? (
          v.service_id ? [serviceLabel(asString(v.service_id), ctx.repoNames)]
            : v.repository_id ? [serviceLabel(asString(v.repository_id), ctx.repoNames)] : []
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
  if (env.error) throw new EshuEnvelopeError(env.error);
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
  if (env.error) throw new EshuEnvelopeError(env.error);
  if (env.truth) ctx.truth.images = env.truth;
  const rows = imageRowsFromResponse(env.data);
  return rows.length > 0 ? rows : null;
}

// loadIacResources reads the bounded IaC inventory list. The endpoint requires
// the authoritative graph; on lower profiles it 501s and the section is reported
// unavailable rather than failing the whole snapshot.
export async function loadIacResources(client: EshuApiClient, ctx: SectionContext): Promise<readonly IacResourceRow[] | null> {
  const env = await client.get<IacResourcesResponse>("/api/v0/iac/resources?limit=200");
  if (env.error) throw new EshuEnvelopeError(env.error);
  if (env.truth) ctx.truth.iacResources = env.truth;
  const rows: IacResourceRow[] = iacResourceRowsFromResponse(env.data);
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
  ingestRate: [], queueDepth: [], deadLetters: [],
  graphNodes: [], graphEdges: [], queryP50: [], queryP95: [], queryP99: [],
  newVulns: [], metricsConfigured: true
};

// MetricSeriesResult carries the point values and whether the metrics source
// is configured. The configured flag is derived from the truth envelope: a
// freshness state of "unavailable" means no Prometheus/Mimir collector is
// wired up, while "building" means the source exists but has no history yet.
interface MetricSeriesResult {
  readonly points: readonly number[];
  readonly configured: boolean;
}

// loadSeriesBundle fetches every dashboard trend metric concurrently and folds
// them into the series bundle. section() (passed in) records per-metric
// provenance; failures degrade to an empty series. metricsConfigured is
// derived from the queue_depth probe: if its truth freshness is "unavailable"
// then no Prometheus/Mimir source is wired up and all chart placeholders
// switch to an explicit "not configured" message.
export async function loadSeriesBundle(
  client: EshuApiClient,
  section: <T>(key: string, load: () => Promise<T | null>) => Promise<T | null>
): Promise<SeriesBundle> {
  const [ingestRate, queueDepth, deadLetters, graphNodes, graphEdges, queryP50, queryP95, queryP99] = await Promise.all([
    loadMetricSeries(client, section, "ingestRate", "ingest_rate"),
    loadMetricSeries(client, section, "queueDepth", "queue_depth"),
    loadMetricSeries(client, section, "deadLetters", "dead_letters"),
    loadMetricSeries(client, section, "graphNodes", "graph_nodes"),
    loadMetricSeries(client, section, "graphEdges", "graph_edges"),
    loadMetricSeries(client, section, "queryP50", "query_p50"),
    loadMetricSeries(client, section, "queryP95", "query_p95"),
    loadMetricSeries(client, section, "queryP99", "query_p99")
  ]);
  // Use the queue_depth probe to derive metricsConfigured. Any metric that
  // returns configured=false means the source is missing; all charts should
  // show "not configured" rather than "no history yet".
  const metricsConfigured = [ingestRate, queueDepth, deadLetters, graphNodes, graphEdges, queryP50, queryP95, queryP99]
    .every((r) => r.configured);
  return {
    ...emptySeries,
    ingestRate: ingestRate.points,
    queueDepth: queueDepth.points,
    deadLetters: deadLetters.points,
    graphNodes: graphNodes.points,
    graphEdges: graphEdges.points,
    queryP50: queryP50.points,
    queryP95: queryP95.points,
    queryP99: queryP99.points,
    metricsConfigured
  };
}

async function loadMetricSeries(
  client: EshuApiClient,
  section: <T>(key: string, load: () => Promise<T | null>) => Promise<T | null>,
  key: keyof Omit<SeriesBundle, "newVulns" | "metricsConfigured">,
  metric: string
): Promise<MetricSeriesResult> {
  let configured = true;
  const values = await section(`series.${key}`, async () => {
    const env = await client.get<MetricsTimeSeriesResponse>(
      `/api/v0/metrics/timeseries?metric=${metric}&window=24h&step=30m`
    );
    if (env.error) throw new EshuEnvelopeError(env.error);
    // A freshness state of "unavailable" means the Prometheus/Mimir source is
    // not configured; "building" means the source exists but has no samples yet.
    if (env.truth?.freshness.state === "unavailable") {
      configured = false;
    }
    const points = (env.data?.points ?? []).map((point) => point.v).filter(isFiniteNumber);
    return points.length > 0 ? points : null;
  });
  return { points: values ?? [], configured };
}

// loadArgoCDApps fetches ArgoCD Application and ApplicationSet nodes from the
// infra search. It uses ctx.repoNames (populated by loadServices) to mark each
// app as source-indexed when its source repository is already ingested by Eshu.
// The section degrades to empty rather than failing the snapshot when the
// authoritative graph is unavailable (non-production profile or no graph store).
export async function loadArgoCDApps(
  client: EshuApiClient,
  ctx: SectionContext
): Promise<readonly ArgoCDAppRow[] | null> {
  const env = await client.post<InfraSearchResponse>("/api/v0/infra/resources/search", {
    category: "argocd",
    limit: 200,
  });
  if (env.error) throw new EshuEnvelopeError(env.error);
  const results = env.data?.results ?? [];
  if (results.length === 0) return null;
  // Build a fast lookup: the catalog repoNames map is keyed by repo_id; the
  // ArgoCD app's source field typically contains a repo URL or short name.
  // We match when any known repo name appears in the source string.
  const knownNames = new Set(ctx.repoNames.values());
  return results.map((r): ArgoCDAppRow => {
    const source = r.source ?? "";
    const sourceIndexed = source !== "" && (
      knownNames.has(source) ||
      [...knownNames].some((name) => source.includes(name))
    );
    return {
      id: r.id ?? "",
      name: r.name ?? "",
      kind: r.kind ?? "",
      source,
      sourceIndexed,
    };
  });
}

function isFiniteNumber(value: number | undefined): value is number {
  return typeof value === "number" && Number.isFinite(value);
}
