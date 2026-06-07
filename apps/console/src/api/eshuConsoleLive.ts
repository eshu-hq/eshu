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

export type SectionProvenance = "live" | "empty" | "unavailable";

export interface ConsoleSnapshot {
  readonly runtime: RuntimeSummary;
  readonly services: readonly ServiceRow[];
  readonly languages: readonly LanguageRow[];
  readonly ingesters: readonly IngesterRow[];
  readonly findings: readonly FindingRow[];
  readonly vulnerabilities: readonly VulnRow[];
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

// Impact findings carry a CVSS score but no severity label; derive the standard
// CVSS v3 qualitative band so the vulnerability list can colour-rank rows.
function severityFromCvss(cvss: number): string {
  if (cvss >= 9) return "critical";
  if (cvss >= 7) return "high";
  if (cvss >= 4) return "medium";
  if (cvss > 0) return "low";
  return "unknown";
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
          services: (v.affected_services as string[]) ?? (v.service_id ? [String(v.service_id)] : v.repository_id ? [String(v.repository_id)] : [])
        });
      }
    }
    return rows.length > 0 ? rows : null;
  })) ?? [];

  return { runtime, services, languages, ingesters, findings, vulnerabilities, truth, provenance: prov };
}

function emptyRuntime(): RuntimeSummary {
  return {
    indexStatus: "unavailable", repositories: 0, workloads: 0, platforms: 0, instances: 0,
    queueOutstanding: 0, inFlight: 0, deadLetters: 0, succeeded: 0, profile: "unknown"
  };
}
