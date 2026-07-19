import type { IacResourceKind } from "../api/iacResources";
import type { IacResourceRow } from "../console/types";

export interface IacFilters {
  readonly kind: IacResourceKind;
  readonly type: string;
  readonly provider: string;
  readonly module: string;
  readonly repository: string;
}

export interface IacViewState {
  readonly filters: IacFilters;
  readonly query: string;
}

export function iacViewFromSearch(params: URLSearchParams): IacViewState {
  const rawKind = params.get("kind");
  const kind: IacResourceKind =
    rawKind === "module" || rawKind === "data-source" ? rawKind : "resource";
  return {
    filters: {
      kind,
      module: params.get("module") ?? "",
      provider: params.get("provider") ?? "",
      repository: params.get("repository") ?? "",
      type: params.get("type") ?? "",
    },
    query: params.get("q") ?? "",
  };
}

export function iacSearchFromView(filters: IacFilters, query: string): URLSearchParams {
  const params = new URLSearchParams();
  addIacSearchParam(params, "q", query);
  params.set("kind", filters.kind);
  addIacSearchParam(params, "type", filters.type);
  addIacSearchParam(params, "provider", filters.provider);
  addIacSearchParam(params, "module", filters.module);
  addIacSearchParam(params, "repository", filters.repository);
  return params;
}

function addIacSearchParam(params: URLSearchParams, key: string, value: string): void {
  const trimmed = value.trim();
  if (trimmed !== "") params.set(key, trimmed);
}

export function distinctIacValues(values: readonly string[]): readonly string[] {
  return [...new Set(values.filter((value) => value !== ""))].sort();
}

export function matchesIacRow(
  row: IacResourceRow,
  query: string,
  filters: IacFilters,
  serverSideFilters: boolean,
): boolean {
  if (!serverSideFilters) {
    if (row.kind !== filters.kind) return false;
    if (filters.type !== "" && row.type !== filters.type) return false;
    if (filters.provider !== "" && row.provider !== filters.provider) return false;
    if (filters.module !== "" && row.module !== filters.module) return false;
    if (filters.repository !== "" && row.repoId !== filters.repository) return false;
  }
  if (query === "") return true;
  const needle = query.toLowerCase();
  const haystack = [
    row.name,
    row.resourceName,
    row.type,
    row.provider,
    row.service,
    row.category,
    row.module,
    row.relativePath,
    row.repoId,
    row.kind,
  ]
    .join(" ")
    .toLowerCase();
  return haystack.includes(needle);
}

export function IacSourceLocation({ row }: { readonly row: IacResourceRow }): React.JSX.Element {
  if (!row.repoId || !row.relativePath) return <span className="t-mut">Source unavailable</span>;
  const params = new URLSearchParams({ path: row.relativePath });
  if (row.lineNumber !== null) params.set("lineStart", String(row.lineNumber));
  const label =
    row.lineNumber === null ? row.relativePath : `${row.relativePath}:${row.lineNumber}`;
  return (
    <a href={`/repositories/${encodeURIComponent(row.repoId)}/source?${params.toString()}`}>
      {label}
    </a>
  );
}
