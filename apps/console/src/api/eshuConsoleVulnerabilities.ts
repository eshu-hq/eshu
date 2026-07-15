import type { EshuApiClient } from "./client";
import { EshuEnvelopeError } from "./envelope";
import type { VulnRow } from "./eshuConsoleLive";
import type { SectionContext } from "./eshuConsoleSections";

interface ImpactFindings {
  readonly findings?: readonly Record<string, unknown>[];
  readonly results?: readonly Record<string, unknown>[];
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

// loadVulnerabilities reads exact and derived affected impact findings. It
// preserves the production finding_id and service_ids fields, merging service
// evidence only when the same finding appears in both bounded status responses.
export async function loadVulnerabilities(
  client: EshuApiClient,
  ctx: SectionContext,
): Promise<readonly VulnRow[] | null> {
  const rows: VulnRow[] = [];
  const rowIndexByFindingID = new Map<string, number>();
  for (const status of ["affected_exact", "affected_derived"]) {
    const env = await client.get<ImpactFindings>(
      `/api/v0/supply-chain/impact/findings?limit=100&impact_status=${status}`,
    );
    if (env.error) throw new EshuEnvelopeError(env.error);
    for (const finding of env.data?.findings ?? env.data?.results ?? []) {
      const id =
        firstString(finding.advisory_id, finding.cve_id, finding.id) || `adv-${rows.length}`;
      const findingID = firstString(finding.finding_id) || fallbackFindingIdentity(finding, id);
      const affectedServices = findingAffectedServices(finding);
      const serviceIds = findingServiceIDs(finding);
      const services = findingServices(finding, ctx.repoNames);
      const existingIndex = rowIndexByFindingID.get(findingID);
      if (existingIndex !== undefined) {
        const existing = rows[existingIndex];
        rows[existingIndex] = {
          ...existing,
          affectedServices: [
            ...new Set([...(existing.affectedServices ?? []), ...affectedServices]),
          ],
          serviceIds: [...new Set([...(existing.serviceIds ?? []), ...serviceIds])],
          services: [...new Set([...existing.services, ...services])],
        };
        continue;
      }
      const cvss = Number(finding.cvss ?? finding.cvss_score ?? 0);
      const severity = (
        finding.severity ? asString(finding.severity) : severityFromCvss(cvss)
      ).toLowerCase();
      rowIndexByFindingID.set(findingID, rows.length);
      rows.push({
        findingId: findingID,
        id,
        package: firstString(finding.package, finding.package_name, finding.subject) || "—",
        severity,
        cvss,
        kev: Boolean(finding.kev ?? finding.known_exploited),
        fixedVersion: asString(finding.fixed_version) || null,
        affectedServices,
        serviceIds,
        services,
      });
    }
  }
  return rows.length > 0 ? rows : null;
}
