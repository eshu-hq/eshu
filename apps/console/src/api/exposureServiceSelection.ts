import { EshuApiHttpError, type EshuApiClient } from "./client";
import { resolveEntity } from "./entityResolution";

const DEFAULT_RESOLUTION_LIMIT = 10;

export interface ExposureServiceRecord {
  readonly id: string;
  readonly kind: string;
  readonly name: string;
  readonly repo: string;
}

export interface ExposureServiceOption {
  readonly aliases: readonly string[];
  readonly canonicalId: string;
  readonly displayName: string;
  readonly kind: string;
  readonly repoName: string;
}

export type ExposureServiceSelectionResult =
  | {
      readonly option: ExposureServiceOption;
      readonly source: "canonical_handle" | "catalog_alias" | "resolver";
      readonly status: "resolved";
    }
  | {
      readonly candidates: readonly ExposureServiceOption[];
      readonly query: string;
      readonly status: "ambiguous";
      readonly truncated: boolean;
    }
  | {
      readonly query: string;
      readonly status: "not_authorized" | "not_found" | "unavailable";
    };

interface ResolveExposureServiceSelectionOptions {
  readonly client: EshuApiClient;
  readonly limit?: number;
  readonly options?: readonly ExposureServiceOption[];
  readonly query: string;
  readonly signal?: AbortSignal;
}

export function exposureServiceOptions(
  services: readonly ExposureServiceRecord[],
): readonly ExposureServiceOption[] {
  const byID = new Map<string, ExposureServiceOption>();
  for (const service of services) {
    const canonicalId = service.id.trim();
    const displayName = service.name.trim();
    if (canonicalId.length === 0 || displayName.length === 0 || byID.has(canonicalId)) {
      continue;
    }
    const handleName = canonicalWorkloadName(canonicalId);
    byID.set(canonicalId, {
      aliases: handleName !== displayName ? [handleName] : [],
      canonicalId,
      displayName,
      kind: service.kind.trim(),
      repoName: service.repo.trim(),
    });
  }
  return [...byID.values()].sort(compareExposureServiceOptions);
}

export function filterExposureServiceOptions(
  options: readonly ExposureServiceOption[],
  query: string,
): readonly ExposureServiceOption[] {
  const needle = normalized(query);
  if (needle.length === 0) {
    return options;
  }
  return options.filter((option) =>
    [option.displayName, option.canonicalId, option.repoName, option.kind, ...option.aliases].some(
      (value) => normalized(value).includes(needle),
    ),
  );
}

export async function resolveExposureServiceSelection({
  client,
  limit = DEFAULT_RESOLUTION_LIMIT,
  options = [],
  query,
  signal,
}: ResolveExposureServiceSelectionOptions): Promise<ExposureServiceSelectionResult> {
  const trimmed = query.trim();
  if (isCanonicalWorkloadHandle(trimmed)) {
    const catalogOption = options.find((option) => option.canonicalId === trimmed);
    return {
      option: catalogOption ?? {
        aliases: [],
        canonicalId: trimmed,
        displayName: canonicalWorkloadName(trimmed),
        kind: "workload",
        repoName: "",
      },
      source: "canonical_handle",
      status: "resolved",
    };
  }

  const exactAliasMatches = options.filter((option) =>
    option.aliases.some((alias) => normalized(alias) === normalized(trimmed)),
  );
  if (exactAliasMatches.length === 1 && exactAliasMatches[0]) {
    return { option: exactAliasMatches[0], source: "catalog_alias", status: "resolved" };
  }
  if (exactAliasMatches.length > 1) {
    return {
      candidates: exactAliasMatches,
      query: trimmed,
      status: "ambiguous",
      truncated: false,
    };
  }

  try {
    const resolution = await resolveEntity({
      client,
      limit,
      name: trimmed,
      signal,
      type: "workload",
    });
    const candidates = resolution.candidates
      .filter(isWorkloadCandidate)
      .map((candidate) => ({
        aliases: [],
        canonicalId: candidate.id,
        displayName: candidate.name,
        kind: "workload",
        repoName: candidate.repoName,
      }))
      .filter((candidate) => isCanonicalWorkloadHandle(candidate.canonicalId))
      .sort(compareExposureServiceOptions);
    if (candidates.length === 1 && candidates[0] && !resolution.truncated) {
      return { option: candidates[0], source: "resolver", status: "resolved" };
    }
    if (candidates.length > 1 || resolution.truncated) {
      return { candidates, query: trimmed, status: "ambiguous", truncated: resolution.truncated };
    }
    return { query: trimmed, status: "not_found" };
  } catch (error) {
    if (error instanceof EshuApiHttpError) {
      if (error.status === 401 || error.status === 403) {
        return { query: trimmed, status: "not_authorized" };
      }
      if (error.status === 404) {
        return { query: trimmed, status: "not_found" };
      }
    }
    return { query: trimmed, status: "unavailable" };
  }
}

function isWorkloadCandidate(candidate: {
  readonly labels: readonly string[];
  readonly type: string;
}): boolean {
  return candidate.type === "Workload" || candidate.labels.includes("Workload");
}

function isCanonicalWorkloadHandle(value: string): boolean {
  return /^workload:[^\s:][^\s]*$/u.test(value);
}

function canonicalWorkloadName(value: string): string {
  return value.startsWith("workload:") ? value.slice("workload:".length) : value;
}

function compareExposureServiceOptions(
  left: ExposureServiceOption,
  right: ExposureServiceOption,
): number {
  return (
    left.displayName.localeCompare(right.displayName) ||
    left.canonicalId.localeCompare(right.canonicalId)
  );
}

function normalized(value: string): string {
  return value.trim().toLocaleLowerCase();
}
