import type { EshuApiClient } from "./client";
import type { FreshnessState, TruthLevel } from "./envelope";
import { EshuEnvelopeError } from "./envelope";

export interface CloudInventoryEvidence {
  readonly declared: boolean;
  readonly applied: boolean;
  readonly observed: boolean;
}

export interface CloudInventoryRow {
  readonly cloudResourceUid: string;
  readonly provider: string;
  readonly resourceType: string;
  readonly managementOrigin: string;
  readonly scopeId: string;
  readonly generationId: string;
  readonly sourceState: string;
  readonly evidence: CloudInventoryEvidence;
  readonly tagValueFingerprints: Readonly<Record<string, string>>;
}

export interface CloudInventoryQuery {
  readonly limit: number;
  readonly provider?: string;
  readonly scopeId?: string;
  readonly accountId?: string;
  readonly projectId?: string;
  readonly subscriptionId?: string;
  readonly managementOrigin?: string;
  readonly cursor?: string;
}

export interface CloudInventoryTruth {
  readonly level: TruthLevel;
  readonly freshness: FreshnessState;
  readonly profile: string;
}

export interface CloudInventoryPage {
  readonly rows: readonly CloudInventoryRow[];
  readonly count: number;
  readonly limit: number;
  readonly truncated: boolean;
  readonly nextCursor: string;
  readonly truth: CloudInventoryTruth;
}

interface CloudInventoryRecord {
  readonly cloud_resource_uid?: string;
  readonly provider?: string;
  readonly resource_type?: string;
  readonly management_origin?: string;
  readonly scope_id?: string;
  readonly generation_id?: string;
  readonly source_state?: string;
  readonly evidence?: Partial<Record<keyof CloudInventoryEvidence, unknown>>;
  readonly tag_value_fingerprints?: Readonly<Record<string, unknown>>;
}

interface CloudInventoryResponse {
  readonly resources?: readonly CloudInventoryRecord[];
  readonly count?: number;
  readonly limit?: number;
  readonly truncated?: boolean;
  readonly next_cursor?: string;
}

function str(value: unknown): string {
  return typeof value === "string" ? value : "";
}

function bool(value: unknown): boolean {
  return value === true;
}

function stringRecord(value: unknown): Readonly<Record<string, string>> {
  if (typeof value !== "object" || value === null) return {};
  const out: Record<string, string> = {};
  for (const [key, raw] of Object.entries(value)) {
    if (key.trim() !== "" && typeof raw === "string" && raw !== "") {
      out[key] = raw;
    }
  }
  return out;
}

function buildPath(query: CloudInventoryQuery): string {
  const params = new URLSearchParams();
  params.set("limit", String(query.limit));
  if (query.provider) params.set("provider", query.provider);
  if (query.scopeId) params.set("scope_id", query.scopeId);
  if (query.accountId) params.set("account_id", query.accountId);
  if (query.projectId) params.set("project_id", query.projectId);
  if (query.subscriptionId) params.set("subscription_id", query.subscriptionId);
  if (query.managementOrigin) params.set("management_origin", query.managementOrigin);
  if (query.cursor) params.set("cursor", query.cursor);
  return `/api/v0/cloud/inventory?${params.toString()}`;
}

function rowFromRecord(record: CloudInventoryRecord): CloudInventoryRow {
  return {
    cloudResourceUid: str(record.cloud_resource_uid),
    provider: str(record.provider),
    resourceType: str(record.resource_type),
    managementOrigin: str(record.management_origin),
    scopeId: str(record.scope_id),
    generationId: str(record.generation_id),
    sourceState: str(record.source_state),
    evidence: {
      declared: bool(record.evidence?.declared),
      applied: bool(record.evidence?.applied),
      observed: bool(record.evidence?.observed)
    },
    tagValueFingerprints: stringRecord(record.tag_value_fingerprints)
  };
}

export async function loadCloudInventory(
  client: EshuApiClient,
  query: CloudInventoryQuery
): Promise<CloudInventoryPage> {
  const env = await client.get<CloudInventoryResponse>(buildPath(query));
  if (env.error) throw new EshuEnvelopeError(env.error);
  const data = env.data ?? {};
  const rows = (data.resources ?? [])
    .map(rowFromRecord)
    .filter((row) => row.cloudResourceUid !== "");
  return {
    rows,
    count: typeof data.count === "number" ? data.count : rows.length,
    limit: typeof data.limit === "number" ? data.limit : query.limit,
    truncated: data.truncated === true,
    nextCursor: str(data.next_cursor),
    truth: {
      level: env.truth?.level ?? "exact",
      freshness: env.truth?.freshness.state ?? "fresh",
      profile: env.truth?.profile ?? "unknown"
    }
  };
}
