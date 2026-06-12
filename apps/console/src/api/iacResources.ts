// api/iacResources.ts
// Bounded Terraform/IaC inventory loader for GET /api/v0/iac/resources. The
// endpoint is keyset-paginated by (name, id); callers must pass both cursor
// fields together and must not use offsets.

import type { EshuApiClient } from "./client";
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
  readonly type?: string;
  readonly provider?: string;
  readonly module?: string;
  readonly cursor?: IacResourceCursor | null;
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
  readonly truth: IacResourcePageTruth;
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
  readonly truncated?: boolean;
}

/**
 * Builds the GET /api/v0/iac/resources URL using the endpoint's keyset cursor
 * contract. It never emits offset pagination because the server ignores offset.
 */
export function iacResourcesPath(query: IacResourceQuery): string {
  const params = new URLSearchParams();
  params.set("limit", String(query.limit));
  if (query.kind) params.set("kind", query.kind);
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
  data: { readonly resources?: readonly IacResourceRecord[] } | null | undefined
): IacResourceRow[] {
  return (data?.resources ?? []).map(iacResourceRowFromRecord).filter((row) => row.id !== "");
}

/**
 * Loads one bounded page of Terraform/IaC resources and preserves the server
 * truth and keyset continuation metadata.
 */
export async function loadIacResourcesPage(
  client: EshuApiClient,
  query: IacResourceQuery
): Promise<IacResourcePage> {
  const env = await client.get<IacResourcesResponse>(iacResourcesPath(query));
  if (env.error) throw new EshuEnvelopeError(env.error);
  const data = env.data ?? {};
  const next = data.next_cursor;
  return {
    rows: iacResourceRowsFromResponse(data),
    count: typeof data.count === "number" ? data.count : 0,
    kind: iacResourceKind(data.kind ?? query.kind),
    limit: typeof data.limit === "number" ? data.limit : query.limit,
    nextCursor: data.truncated === true && next?.after_name && next.after_id
      ? { afterName: next.after_name, afterId: next.after_id }
      : null,
    truncated: data.truncated === true,
    truth: iacPageTruth(env.truth)
  };
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
    type: str(record.type)
  };
}

function iacPageTruth(truth: EshuTruth | null): IacResourcePageTruth {
  return {
    freshness: truth?.freshness.state ?? "fresh",
    level: truth?.level ?? "exact",
    profile: truth?.profile ?? "unknown"
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
