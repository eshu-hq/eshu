import type { CloudDriftProvider, CloudDriftQuery } from "../api/cloudDrift";

const PAGE_LIMIT = 50;

export interface DriftFilters {
  readonly accountId: string;
  readonly provider: CloudDriftProvider;
  readonly region: string;
  readonly scopeId: string;
}

export const EMPTY_FILTERS: DriftFilters = {
  accountId: "",
  provider: "",
  region: "",
  scopeId: "",
};

export function filtersFromSearch(
  search: string,
  defaults: DriftFilters | undefined,
): DriftFilters {
  const params = new URLSearchParams(search);
  const provider = params.get("provider") ?? "";
  return {
    accountId: params.get("account_id") ?? defaults?.accountId ?? "",
    provider:
      provider === "aws" || provider === "gcp" || provider === "azure"
        ? provider
        : (defaults?.provider ?? ""),
    region: params.get("region") ?? defaults?.region ?? "",
    scopeId: params.get("scope_id") ?? defaults?.scopeId ?? "",
  };
}

export function queryFor(filters: DriftFilters, offset: number): CloudDriftQuery {
  return {
    accountId: cleanFilter(filters.accountId),
    limit: PAGE_LIMIT,
    offset,
    provider: filters.provider,
    region: cleanFilter(filters.region),
    scopeId: cleanFilter(filters.scopeId),
  };
}

export function cleanFilter(value: string): string | undefined {
  const trimmed = value.trim();
  return trimmed.length === 0 ? undefined : trimmed;
}

export function hasBoundedScope(filters: DriftFilters): boolean {
  return filters.scopeId.trim() !== "" || filters.accountId.trim() !== "";
}

export function shouldLoadAwsSurfaces(filters: DriftFilters): boolean {
  return (
    filters.provider === "aws" ||
    filters.accountId.trim() !== "" ||
    filters.scopeId.trim().startsWith("aws:")
  );
}
