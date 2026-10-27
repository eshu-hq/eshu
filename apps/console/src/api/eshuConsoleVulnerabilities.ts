import type { EshuApiClient, EshuApiRequestOptions } from "./client";
import { EshuEnvelopeError } from "./envelope";
import type { EshuTruth } from "./envelope";
import type { VulnRow } from "./eshuConsoleLive";
import type { SectionContext } from "./eshuConsoleSections";

export type VulnerabilityStatus = "affected_exact" | "affected_derived";

export interface VulnerabilityPage {
  readonly rows: readonly VulnRow[];
  readonly count: number;
  readonly limit: number;
  readonly truncated: boolean;
  readonly nextCursor: string | null;
  readonly truth: EshuTruth | null;
}

export interface VulnerabilitySnapshot {
  readonly rows: readonly VulnRow[];
  readonly pages: Readonly<Record<VulnerabilityStatus, VulnerabilityPage>>;
}

interface ImpactFindings {
  readonly findings?: readonly Record<string, unknown>[];
  readonly results?: readonly Record<string, unknown>[];
  readonly truncated?: boolean;
  readonly next_cursor?: { readonly after_finding_id?: string };
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

function asString(value: unknown): string {
  return typeof value === "string" ? value : "";
}

function firstString(...values: readonly unknown[]): string {
  for (const value of values) {
    const text = asString(value).trim();
    if (text !== "") return text;
  }
  return "";
}

function stringValues(value: unknown): readonly string[] {
  if (!Array.isArray(value)) return [];
  return value
    .filter((item): item is string => typeof item === "string")
    .map((item) => item.trim())
    .filter((item) => item !== "");
}

function serviceLabel(id: string, repoNames: ReadonlyMap<string, string>): string {
  const trimmed = id.trim();
  const mapped = repoNames.get(trimmed);
  if (mapped) return mapped;
  return trimmed.replace(/^(?:repository|workload|service)[:_]/i, "");
}

function findingServices(
  finding: Readonly<Record<string, unknown>>,
  repoNames: ReadonlyMap<string, string>,
): readonly string[] {
  const labels = [
    ...stringValues(finding.affected_services),
    ...stringValues(finding.service_ids).map((id) => serviceLabel(id, repoNames)),
  ];
  const singularService = asString(finding.service_id);
  if (singularService !== "") labels.push(serviceLabel(singularService, repoNames));
  return [...new Set(labels.filter((label) => label !== ""))];
}

function findingAffectedServices(finding: Readonly<Record<string, unknown>>): readonly string[] {
  return [...new Set(stringValues(finding.affected_services))];
}

function findingServiceIDs(finding: Readonly<Record<string, unknown>>): readonly string[] {
  const ids = [...stringValues(finding.service_ids)];
  const singularService = asString(finding.service_id).trim();
  if (singularService !== "") ids.push(singularService);
  return [...new Set(ids)];
}

function fallbackFindingIdentity(
  finding: Readonly<Record<string, unknown>>,
  advisoryID: string,
): string {
  const parts = [
    advisoryID,
    firstString(
      finding.package_id,
      finding.purl,
      finding.package,
      finding.package_name,
      finding.subject,
    ),
    firstString(finding.observed_version, finding.version),
    firstString(finding.repository_id, finding.subject_digest, finding.image_ref),
  ];
  return `legacy:${parts.map((part) => encodeURIComponent(part)).join(":")}`;
}

// vulnerabilityRowKey keeps React and worklist identities aligned with the
// reducer finding. Legacy/demo rows receive a deterministic composite key,
// while their advisory id remains available for detail URLs.
export function vulnerabilityRowKey(row: VulnRow): string {
  const findingID = row.findingId?.trim();
  if (findingID) return findingID;
  return `legacy:${JSON.stringify([
    row.id,
    row.package,
    row.fixedVersion,
    [...row.services].sort(),
  ])}`;
}

function record(value: unknown): Readonly<Record<string, unknown>> | null {
  return value !== null && typeof value === "object" && !Array.isArray(value)
    ? (value as Readonly<Record<string, unknown>>)
    : null;
}

function advisoryObservations(
  finding: Readonly<Record<string, unknown>>,
): readonly Readonly<Record<string, unknown>>[] {
  const values = record(finding.provenance)?.advisory_sources;
  return Array.isArray(values)
    ? values
        .map(record)
        .filter((value): value is Readonly<Record<string, unknown>> => value !== null)
    : [];
}

function epssProbability(value: unknown): number | null {
  if (typeof value !== "number" && typeof value !== "string") return null;
  if (typeof value === "string" && value.trim() === "") return null;
  const number = Number(value);
  return Number.isFinite(number) && number >= 0 && number <= 1 ? number : null;
}

function findingRow(
  finding: Readonly<Record<string, unknown>>,
  repoNames: ReadonlyMap<string, string>,
  index: number,
): VulnRow {
  const rawCVE = firstString(finding.cve_id);
  const cveId = /^CVE-/i.test(rawCVE) ? rawCVE : "";
  const id = cveId || firstString(finding.advisory_id, rawCVE, finding.id) || `adv-${index}`;
  const observations = advisoryObservations(finding);
  const ghsaIds = [
    firstString(finding.advisory_id),
    rawCVE,
    ...observations.map((item) => firstString(item.advisory_id)),
  ].filter((value) => /^GHSA-/i.test(value));
  const sources = observations
    .map((item) => firstString(item.source))
    .filter((value) => value !== "");
  if (sources.length === 0) {
    const provenance = record(finding.provenance);
    const selected = firstString(
      provenance?.selected_severity_source,
      provenance?.selected_fixed_version_source,
    );
    if (selected !== "") sources.push(selected);
  }
  const cvss = Number(finding.cvss ?? finding.cvss_score ?? 0);
  const severity = (
    finding.severity ? asString(finding.severity) : severityFromCvss(cvss)
  ).toLowerCase();
  return {
    findingId: firstString(finding.finding_id) || fallbackFindingIdentity(finding, id),
    id,
    cveId: cveId || undefined,
    ghsaIds: [...new Set(ghsaIds.filter((value) => value !== id))],
    sources: [...new Set(sources)],
    epssProbability: epssProbability(finding.epss_probability),
    package: firstString(finding.package, finding.package_name, finding.subject) || "—",
    severity,
    cvss,
    kev: finding.known_exploited === true || finding.kev === true,
    fixedVersion: asString(finding.fixed_version) || null,
    affectedServices: findingAffectedServices(finding),
    serviceIds: findingServiceIDs(finding),
    services: findingServices(finding, repoNames),
  };
}

function mergeFindingRows(rows: readonly VulnRow[]): readonly VulnRow[] {
  const byID = new Map<string, VulnRow>();
  for (const row of rows) {
    const key = row.findingId ?? vulnerabilityRowKey(row);
    const existing = byID.get(key);
    byID.set(
      key,
      existing
        ? {
            ...existing,
            affectedServices: [
              ...new Set([...(existing.affectedServices ?? []), ...(row.affectedServices ?? [])]),
            ],
            serviceIds: [...new Set([...(existing.serviceIds ?? []), ...(row.serviceIds ?? [])])],
            services: [...new Set([...existing.services, ...row.services])],
          }
        : row,
    );
  }
  return [...byID.values()];
}

// loadVulnerabilityStatusPage reads one bounded status page in server finding-id order.
// A truncated response needs a usable cursor; otherwise the UI cannot truthfully
// offer access to later findings.
export async function loadVulnerabilityStatusPage(
  client: EshuApiClient,
  repoNames: ReadonlyMap<string, string>,
  status: VulnerabilityStatus,
  cursor: string | null = null,
  options: EshuApiRequestOptions = {},
): Promise<VulnerabilityPage> {
  const query = new URLSearchParams({ limit: "100", impact_status: status });
  if (cursor) query.set("after_finding_id", cursor);
  const env = await client.get<ImpactFindings>(
    `/api/v0/supply-chain/impact/findings?${query.toString()}`,
    options,
  );
  if (env.error) throw new EshuEnvelopeError(env.error);
  const findings = env.data?.findings ?? env.data?.results ?? [];
  const rows = mergeFindingRows(
    findings.map((finding, index) => findingRow(finding, repoNames, index)),
  );
  const truncated = env.data?.truncated === true;
  const nextCursor = firstString(env.data?.next_cursor?.after_finding_id) || null;
  if (truncated && (!nextCursor || nextCursor === cursor)) {
    throw new Error("truncated impact findings page has no advancing cursor");
  }
  return {
    rows,
    count: rows.length,
    limit: 100,
    truncated,
    nextCursor: truncated ? nextCursor : null,
    truth: env.truth,
  };
}

// loadVulnerabilities seeds the shared console snapshot with one bounded page
// per affected status. The route owns subsequent cursor navigation.
export async function loadVulnerabilities(
  client: EshuApiClient,
  ctx: SectionContext,
): Promise<VulnerabilitySnapshot | null> {
  const [affectedExact, affectedDerived] = await Promise.all([
    loadVulnerabilityStatusPage(client, ctx.repoNames, "affected_exact"),
    loadVulnerabilityStatusPage(client, ctx.repoNames, "affected_derived"),
  ]);
  const rows = mergeFindingRows([...affectedExact.rows, ...affectedDerived.rows]);
  if (rows.length === 0 && !affectedExact.truncated && !affectedDerived.truncated) return null;
  return {
    rows,
    pages: { affected_exact: affectedExact, affected_derived: affectedDerived },
  };
}
