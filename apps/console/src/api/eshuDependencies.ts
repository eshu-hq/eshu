// api/eshuDependencies.ts
// Bounded dependency inventory loader for GET /api/v0/dependencies. The endpoint
// answers two questions over the package-native dependency graph: forward ("what
// does package X depend on") and reverse ("who depends on package X"). Reverse
// requires a package anchor; forward may browse. Live-API data only — no
// fabricated rows. The row/page/query view models live in eshuConsoleLive.ts so
// the snapshot can surface a default forward browse alongside this interactive
// loader.

import type { EshuApiClient } from "./client";
import type { DependencyRow, DependencyQuery, DependencyPage } from "./eshuConsoleLive";

interface DependencyRecord {
  readonly direction?: string;
  readonly anchor_package?: string;
  readonly anchor_package_id?: string;
  readonly declaring_version?: string;
  readonly related_package?: string;
  readonly related_package_id?: string;
  readonly related_ecosystem?: string;
  readonly dependency_range?: string;
  readonly dependency_type?: string;
  readonly optional?: boolean;
  readonly edge_id?: string;
}

interface DependenciesResponse {
  readonly dependencies?: readonly DependencyRecord[];
  readonly direction?: string;
  readonly truncated?: boolean;
  readonly next_cursor?: { readonly after_name?: string; readonly after_edge?: string };
}

// dependencyRowFromRecord maps one API dependency record to the view model,
// preferring the related_package label and falling back to the related package
// id when the registry did not record a clean name (some versions report the
// version string as the name).
export function dependencyRowFromRecord(record: DependencyRecord, index: number): DependencyRow {
  const direction = record.direction === "reverse" ? "reverse" : "forward";
  return {
    direction,
    anchorPackage: record.anchor_package ?? "",
    anchorPackageId: record.anchor_package_id ?? "",
    declaringVersion: record.declaring_version ?? "",
    relatedPackage: record.related_package || record.related_package_id || "—",
    relatedPackageId: record.related_package_id ?? "",
    ecosystem: record.related_ecosystem ?? "",
    range: record.dependency_range ?? "",
    dependencyType: record.dependency_type ?? "",
    optional: Boolean(record.optional),
    edgeId: record.edge_id || `dep-${index}`
  };
}

// dependenciesPath builds the GET /api/v0/dependencies query string from a
// bounded dependency lookup.
export function dependenciesPath(query: DependencyQuery): string {
  const params = new URLSearchParams();
  params.set("direction", query.direction);
  params.set("limit", String(query.limit ?? 50));
  if (query.pkg) params.set("package", query.pkg);
  if (query.ecosystem) params.set("ecosystem", query.ecosystem);
  if (query.afterName && query.afterEdge) {
    params.set("after_name", query.afterName);
    params.set("after_edge", query.afterEdge);
  }
  return `/api/v0/dependencies?${params.toString()}`;
}

// loadDependencies runs one bounded dependency lookup and returns a typed page
// with paging and truth metadata. Used by the Dependencies page for interactive
// forward/reverse lookups.
export async function loadDependencies(
  client: EshuApiClient,
  query: DependencyQuery
): Promise<DependencyPage> {
  const env = await client.get<DependenciesResponse>(dependenciesPath(query));
  const records = env.data?.dependencies ?? [];
  const rows = records.map(dependencyRowFromRecord);
  const cursor = env.data?.next_cursor;
  const direction = env.data?.direction === "reverse" ? "reverse" : query.direction;
  return {
    rows,
    direction,
    truncated: Boolean(env.data?.truncated),
    nextCursor: cursor?.after_name && cursor?.after_edge
      ? { afterName: cursor.after_name, afterEdge: cursor.after_edge }
      : null,
    truth: env.truth
  };
}
