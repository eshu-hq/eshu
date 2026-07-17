import type { EshuApiClient } from "./client";
import { EshuEnvelopeError, type EshuTruth } from "./envelope";
import type { ReplatformingScopeKind } from "./replatforming";

export type ReplatformingSelectorScopeKind = Extract<
  ReplatformingScopeKind,
  "account" | "region" | "service"
>;

export interface ReplatformingSelectorScope {
  readonly accountId: string;
  readonly findingCount: number;
  readonly label: string;
  readonly region: string;
  readonly scopeId: string;
  readonly service: string;
}

export interface ReplatformingSelectorReadiness {
  readonly detail: string;
  readonly nextAction: string;
  readonly state: "collector_evidence_absent" | "ready";
}

export interface ReplatformingSelectorInventory {
  readonly count: number;
  readonly emptyScopeCount: number;
  readonly findingKinds: readonly string[];
  readonly limit: number;
  readonly pageSizes: readonly number[];
  readonly readiness: ReplatformingSelectorReadiness;
  readonly scopes: readonly ReplatformingSelectorScope[];
  readonly supportedScopeKinds: readonly ReplatformingSelectorScopeKind[];
  readonly truncated: boolean;
  readonly truth: EshuTruth | null;
}

interface ReplatformingSelectorScopeWire {
  readonly account_id?: string;
  readonly finding_count?: number;
  readonly label?: string;
  readonly region?: string;
  readonly scope_id?: string;
  readonly service?: string;
}

interface ReplatformingSelectorReadinessWire {
  readonly detail?: string;
  readonly next_action?: string;
  readonly state?: string;
}

interface ReplatformingSelectorInventoryWire {
  readonly count?: number;
  readonly empty_scope_count?: number;
  readonly finding_kinds?: readonly unknown[];
  readonly limit?: number;
  readonly page_sizes?: readonly unknown[];
  readonly readiness?: ReplatformingSelectorReadinessWire;
  readonly scopes?: readonly ReplatformingSelectorScopeWire[];
  readonly supported_scope_kinds?: readonly unknown[];
  readonly truncated?: boolean;
}

const supportedScopeKinds = new Set<ReplatformingSelectorScopeKind>([
  "account",
  "region",
  "service",
]);

export async function loadReplatformingSelectors(
  client: EshuApiClient,
): Promise<ReplatformingSelectorInventory> {
  const envelope = await client.get<ReplatformingSelectorInventoryWire>(
    "/api/v0/replatforming/selectors?limit=200",
  );
  if (envelope.error) throw new EshuEnvelopeError(envelope.error);
  const data = envelope.data ?? {};
  const scopes = (data.scopes ?? []).map(scopeFromWire).filter(isSelectableScope);
  return {
    count: numberOr(data.count, scopes.length),
    emptyScopeCount: numberOr(
      data.empty_scope_count,
      scopes.filter((scope) => scope.findingCount === 0).length,
    ),
    findingKinds: stringList(data.finding_kinds),
    limit: numberOr(data.limit, 200),
    pageSizes: numberList(data.page_sizes),
    readiness: readinessFromWire(data.readiness, scopes.length),
    scopes,
    supportedScopeKinds: (data.supported_scope_kinds ?? []).filter(
      (value): value is ReplatformingSelectorScopeKind =>
        typeof value === "string" &&
        supportedScopeKinds.has(value as ReplatformingSelectorScopeKind),
    ),
    truncated: data.truncated === true,
    truth: envelope.truth,
  };
}

function scopeFromWire(scope: ReplatformingSelectorScopeWire): ReplatformingSelectorScope {
  return {
    accountId: stringOrEmpty(scope.account_id),
    findingCount: numberOr(scope.finding_count, 0),
    label: stringOrEmpty(scope.label),
    region: stringOrEmpty(scope.region),
    scopeId: stringOrEmpty(scope.scope_id),
    service: stringOrEmpty(scope.service),
  };
}

function isSelectableScope(scope: ReplatformingSelectorScope): boolean {
  return scope.scopeId !== "" && scope.accountId !== "" && scope.region !== "";
}

function readinessFromWire(
  readiness: ReplatformingSelectorReadinessWire | undefined,
  scopeCount: number,
): ReplatformingSelectorReadiness {
  const state =
    readiness?.state === "ready" && scopeCount > 0 ? "ready" : "collector_evidence_absent";
  return {
    detail: stringOrEmpty(readiness?.detail),
    nextAction: stringOrEmpty(readiness?.next_action),
    state,
  };
}

function stringList(values: readonly unknown[] | undefined): readonly string[] {
  return (values ?? []).filter(
    (value): value is string => typeof value === "string" && value.trim() !== "",
  );
}

function numberList(values: readonly unknown[] | undefined): readonly number[] {
  return (values ?? []).filter(
    (value): value is number => Number.isInteger(value) && (value as number) > 0,
  );
}

function stringOrEmpty(value: unknown): string {
  return typeof value === "string" ? value : "";
}

function numberOr(value: unknown, fallback: number): number {
  return typeof value === "number" && Number.isFinite(value) ? value : fallback;
}
