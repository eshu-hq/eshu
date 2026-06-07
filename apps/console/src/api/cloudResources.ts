// api/cloudResources.ts
// Cloud inventory loader. Browses cloud-provider resources from the bounded,
// keyset-paged GET /api/v0/cloud/resources endpoint (see #1643). The graph holds
// ~17k CloudResource nodes, so this never loads the whole set: each call returns
// one bounded page plus a next_cursor the page component pages through. Field
// names mirror the real API wire contract (see go/internal/query/cloud_resources.go
// and GET /api/v0/openapi.json); nothing here fabricates values.

import type { EshuApiClient } from "./client";
import type { TruthLevel, FreshnessState } from "./envelope";

// CloudResourceRow is one cloud-provider resource projected from a CloudResource
// graph node. Optional fields are empty strings when the node does not populate
// them; provider mirrors the collector kind because the AWS collector does not
// set a separate provider property.
export interface CloudResourceRow {
  readonly id: string;
  readonly resourceType: string;
  readonly name: string;
  readonly provider: string;
  readonly region: string;
  readonly accountId: string;
  readonly arn: string;
  readonly serviceName: string;
  readonly state: string;
}

// CloudResourceCursor is the keyset continuation anchor returned by the API. Both
// halves must be passed back together to resume the next page deterministically.
export interface CloudResourceCursor {
  readonly afterResourceType: string;
  readonly afterId: string;
}

// CloudResourceQuery holds the optional bounded filters and page controls. Empty
// filters are omitted from the request so the server treats them as "no filter".
export interface CloudResourceQuery {
  readonly limit: number;
  readonly provider?: string;
  readonly resourceType?: string;
  readonly region?: string;
  readonly accountId?: string;
  readonly cursor?: CloudResourceCursor;
}

// CloudResourcePageTruth carries the truth-envelope signals the page renders as
// chips, normalized to the fields the console UI consumes.
export interface CloudResourcePageTruth {
  readonly level: TruthLevel;
  readonly freshness: FreshnessState;
  readonly profile: string;
}

// CloudResourcePage is one bounded page of cloud resources plus the pagination
// and truth metadata needed to fetch the next page and render freshness.
export interface CloudResourcePage {
  readonly rows: readonly CloudResourceRow[];
  readonly count: number;
  readonly limit: number;
  readonly truncated: boolean;
  readonly nextCursor: CloudResourceCursor | null;
  readonly truth: CloudResourcePageTruth;
}

// ---- wire shapes (partial; see GET /api/v0/openapi.json) ----
interface CloudResourceRecord {
  readonly id?: string;
  readonly resource_type?: string;
  readonly name?: string;
  readonly provider?: string;
  readonly region?: string;
  readonly account_id?: string;
  readonly arn?: string;
  readonly service_name?: string;
  readonly state?: string;
}
interface CloudResourceListResponse {
  readonly resources?: readonly CloudResourceRecord[];
  readonly count?: number;
  readonly limit?: number;
  readonly truncated?: boolean;
  readonly next_cursor?: { readonly after_resource_type?: string; readonly after_id?: string };
}

function str(value: unknown): string {
  return typeof value === "string" ? value : "";
}

function buildPath(query: CloudResourceQuery): string {
  const params = new URLSearchParams();
  params.set("limit", String(query.limit));
  if (query.provider) params.set("provider", query.provider);
  if (query.resourceType) params.set("resource_type", query.resourceType);
  if (query.region) params.set("region", query.region);
  if (query.accountId) params.set("account_id", query.accountId);
  // Keyset pagination only: both cursor halves must be present, and id alone is
  // enough for the server to apply the predicate, so guard on afterId.
  if (query.cursor && query.cursor.afterId) {
    params.set("after_resource_type", query.cursor.afterResourceType);
    params.set("after_id", query.cursor.afterId);
  }
  return `/api/v0/cloud/resources?${params.toString()}`;
}

function rowFromRecord(record: CloudResourceRecord): CloudResourceRow {
  return {
    id: str(record.id),
    resourceType: str(record.resource_type),
    name: str(record.name),
    provider: str(record.provider),
    region: str(record.region),
    accountId: str(record.account_id),
    arn: str(record.arn),
    serviceName: str(record.service_name),
    state: str(record.state)
  };
}

// loadCloudResources fetches one bounded page of cloud resources. It forwards the
// optional filters and keyset cursor, maps the enveloped response into typed
// rows, and captures truth/freshness. It does not swallow failures: a missing or
// erroring endpoint throws so the page can show an explicit error state instead
// of a fabricated empty list.
export async function loadCloudResources(
  client: EshuApiClient,
  query: CloudResourceQuery
): Promise<CloudResourcePage> {
  const env = await client.get<CloudResourceListResponse>(buildPath(query));
  const data = env.data ?? {};
  const rows = (data.resources ?? [])
    .map(rowFromRecord)
    .filter((row) => row.id !== "");
  const next = data.next_cursor;
  const nextCursor: CloudResourceCursor | null =
    data.truncated && next && str(next.after_id) !== ""
      ? { afterResourceType: str(next.after_resource_type), afterId: str(next.after_id) }
      : null;
  return {
    rows,
    count: typeof data.count === "number" ? data.count : rows.length,
    limit: typeof data.limit === "number" ? data.limit : query.limit,
    truncated: data.truncated === true,
    nextCursor,
    truth: {
      level: env.truth?.level ?? "exact",
      freshness: env.truth?.freshness.state ?? "fresh",
      profile: env.truth?.profile ?? "unknown"
    }
  };
}
