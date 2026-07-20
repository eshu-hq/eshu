// api/iacResources.ts
// Bounded Terraform/IaC inventory loader for GET /api/v0/iac/resources. The
// endpoint is keyset-paginated by (name, id); callers must pass both cursor
// fields together and must not use offsets.

import type { EshuApiClient } from "./client";
import type { EshuApiRequestOptions } from "./client";
import type { EshuTruth, FreshnessState, TruthLevel } from "./envelope";
import { EshuEnvelopeError } from "./envelope";
import type { IacResourceRow } from "./eshuConsoleLive";

/** IaC graph node family accepted by GET /api/v0/iac/resources. */
export type IacResourceKind = "resource" | "module" | "data-source";

/** Keyset cursor returned by the IaC resource endpoint for the next page. */
export interface IacResourceCursor {
  readonly afterName: string;
  readonly afterId: string;
}

/** Bounded IaC resource query with server-side filters and optional cursor. */
export interface IacResourceQuery {
  readonly limit: number;
  readonly kind?: IacResourceKind;
  readonly query?: string;
  readonly type?: string;
  readonly provider?: string;
  readonly module?: string;
  readonly repository?: string;
  readonly includeFacets?: boolean;
  readonly cursor?: IacResourceCursor | null;
}

/** One authoritative bounded selector value. */
export interface IacInventoryFacet {
  readonly kind?: IacResourceKind;
  readonly value: string;
  readonly count: number;
}

/** Current caller-authorized inventory totals and selector facets. */
export interface IacInventorySummary {
  readonly total: number;
  readonly byKind: Readonly<Record<IacResourceKind, number>>;
  readonly types: readonly IacInventoryFacet[];
  readonly providers: readonly IacInventoryFacet[];
  readonly modules: readonly IacInventoryFacet[];
  readonly repositories: readonly IacInventoryFacet[];
  readonly facetLimit: number;
  readonly truncated: Readonly<Record<string, boolean>>;
}

/** Normalized truth metadata rendered by the IaC inventory page. */
export interface IacResourcePageTruth {
  readonly freshness: FreshnessState;
  readonly level: TruthLevel;
  readonly profile: string;
}

/** One keyset-paged IaC inventory response ready for console rendering. */
export interface IacResourcePage {
  readonly rows: readonly IacResourceRow[];
  readonly count: number;
  readonly kind: IacResourceKind;
  readonly limit: number;
  readonly truncated: boolean;
  readonly nextCursor: IacResourceCursor | null;
  readonly summary: IacInventorySummary | null;
  readonly truth: IacResourcePageTruth | null;
}

interface IacResourceRecord {
  readonly id?: string;
  readonly kind?: string;
  readonly name?: string;
  readonly resource_name?: string;
  readonly type?: string;
  readonly provider?: string;
  readonly resource_service?: string;
  readonly resource_category?: string;
  readonly module?: string;
  readonly repo_id?: string;
  readonly relative_path?: string;
  readonly line_number?: number;
}

interface IacResourcesResponse {
  readonly count?: number;
  readonly kind?: string;
  readonly limit?: number;
  readonly next_cursor?: { readonly after_name?: string; readonly after_id?: string };
  readonly resources?: readonly IacResourceRecord[];
  readonly summary?: IacInventorySummaryRecord;
  readonly truncated?: boolean;
}

interface IacInventoryFacetRecord {
  readonly kind?: string;
  readonly value?: string;
  readonly count?: number;
}

interface IacInventorySummaryRecord {
  readonly total?: number;
  readonly by_kind?: Readonly<Record<string, number>>;
  readonly types?: readonly IacInventoryFacetRecord[];
  readonly providers?: readonly IacInventoryFacetRecord[];
  readonly modules?: readonly IacInventoryFacetRecord[];
  readonly repositories?: readonly IacInventoryFacetRecord[];
  readonly facet_limit?: number;
  readonly truncated?: Readonly<Record<string, boolean>>;
}

/**
 * Builds the GET /api/v0/iac/resources URL using the endpoint's keyset cursor
 * contract. It never emits offset pagination because the server ignores offset.
 */
export function iacResourcesPath(query: IacResourceQuery): string {
  const params = new URLSearchParams();
  params.set("limit", String(query.limit));
  if (query.kind) params.set("kind", query.kind);
  if (query.query) params.set("q", query.query);
  if (query.repository) params.set("repository", query.repository);
  if (query.includeFacets) params.set("include_facets", "true");
  if (query.type) params.set("type", query.type);
  if (query.provider) params.set("provider", query.provider);
  if (query.module) params.set("module", query.module);
  if (query.cursor?.afterName && query.cursor.afterId) {
    params.set("after_name", query.cursor.afterName);
    params.set("after_id", query.cursor.afterId);
  }
  return `/api/v0/iac/resources?${params.toString()}`;
}

/** Maps a raw IaC resources payload into id-bearing console rows. */
export function iacResourceRowsFromResponse(
  data: { readonly resources?: readonly IacResourceRecord[] } | null | undefined,
): IacResourceRow[] {
  return (data?.resources ?? []).map(iacResourceRowFromRecord).filter((row) => row.id !== "");
}

/**
 * Loads one bounded page of Terraform/IaC resources and preserves the server
 * truth and keyset continuation metadata.
 */
export async function loadIacResourcesPage(
  client: EshuApiClient,
  query: IacResourceQuery,
  options: EshuApiRequestOptions = {},
): Promise<IacResourcePage> {
  const path = iacResourcesPath(query);
  const env = options.signal
    ? await client.get<IacResourcesResponse>(path, options)
    : await client.get<IacResourcesResponse>(path);
  if (env.error) throw new EshuEnvelopeError(env.error);
  const data = env.data ?? {};
  const next = data.next_cursor;
  return {
    rows: iacResourceRowsFromResponse(data),
    count: typeof data.count === "number" ? data.count : 0,
    kind: iacResourceKind(data.kind ?? query.kind),
    limit: typeof data.limit === "number" ? data.limit : query.limit,
    nextCursor:
      data.truncated === true && next?.after_name && next.after_id
        ? { afterName: next.after_name, afterId: next.after_id }
        : null,
    summary: iacInventorySummary(data.summary),
    truncated: data.truncated === true,
    truth: iacPageTruth(env.truth),
  };
}

function iacInventorySummary(
  record: IacInventorySummaryRecord | undefined,
): IacInventorySummary | null {
  if (
    !record ||
    !isInventoryCount(record.total) ||
    !isInventoryCount(record.facet_limit) ||
    !record.by_kind ||
    !isInventoryCount(record.by_kind.resource) ||
    !isInventoryCount(record.by_kind.module) ||
    !isInventoryCount(record.by_kind["data-source"]) ||
    !record.truncated ||
    !validIacInventoryFacets(record.types) ||
    !validIacInventoryFacets(record.providers) ||
    !validIacInventoryFacets(record.modules) ||
    !validIacInventoryFacets(record.repositories)
  ) {
    return null;
  }
  return {
    total: record.total,
    byKind: {
      "data-source": record.by_kind["data-source"],
      module: record.by_kind.module,
      resource: record.by_kind.resource,
    },
    types: iacInventoryFacets(record.types),
    providers: iacInventoryFacets(record.providers),
    modules: iacInventoryFacets(record.modules),
    repositories: iacInventoryFacets(record.repositories),
    facetLimit: record.facet_limit,
    truncated: record.truncated,
  };
}

function validIacInventoryFacets(
  records: readonly IacInventoryFacetRecord[] | undefined,
): records is readonly IacInventoryFacetRecord[] {
  return (
    Array.isArray(records) &&
    records.every(
      (record) =>
        typeof record.value === "string" &&
        record.value !== "" &&
        isInventoryCount(record.count) &&
        (record.kind === undefined ||
          record.kind === "resource" ||
          record.kind === "module" ||
          record.kind === "data-source"),
    )
  );
}

function iacInventoryFacets(records: readonly IacInventoryFacetRecord[]): IacInventoryFacet[] {
  return records.map((record) => {
    const rawKind = record.kind;
    const kind =
      rawKind === "resource" || rawKind === "module" || rawKind === "data-source"
        ? rawKind
        : undefined;
    return { count: record.count as number, kind, value: record.value as string };
  });
}

function iacResourceRowFromRecord(record: IacResourceRecord): IacResourceRow {
  return {
    category: str(record.resource_category),
    id: str(record.id),
    kind: str(record.kind) || "resource",
    lineNumber: numberOrNull(record.line_number),
    module: str(record.module),
    name: str(record.name),
    provider: str(record.provider),
    relativePath: str(record.relative_path),
    repoId: str(record.repo_id),
    resourceName: str(record.resource_name),
    service: str(record.resource_service),
    type: str(record.type),
  };
}

function iacPageTruth(truth: EshuTruth | null): IacResourcePageTruth | null {
  if (!truth) return null;
  return {
    freshness: truth.freshness.state,
    level: truth.level,
    profile: truth.profile,
  };
}

function iacResourceKind(value: string | undefined): IacResourceKind {
  if (value === "module" || value === "data-source") return value;
  return "resource";
}

function str(value: string | undefined): string {
  return value?.trim() ?? "";
}

function numberOrNull(value: number | undefined): number | null {
  return typeof value === "number" && Number.isFinite(value) ? value : null;
}

function isInventoryCount(value: number | undefined): value is number {
  return typeof value === "number" && Number.isInteger(value) && value >= 0;
}
