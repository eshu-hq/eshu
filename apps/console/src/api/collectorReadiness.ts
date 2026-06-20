import type { EshuApiClient } from "./client";
import type { EshuTruth } from "./envelope";
import { EshuEnvelopeError } from "./envelope";

export type CollectorReadinessState =
  | "disabled"
  | "failed"
  | "gated"
  | "implemented"
  | "partial"
  | "permission_hidden"
  | "stale"
  | "unsupported";

export interface CollectorReadinessRow {
  readonly blockingGate: string;
  readonly claimDriven: boolean;
  readonly claimState: string;
  readonly displayName: string;
  readonly evidence: readonly string[];
  readonly family: string;
  readonly health: string;
  readonly instanceId: string;
  readonly kind: string;
  readonly lastProof: string;
  readonly reducerReadback: string;
  readonly sourceScope: string;
  readonly state: CollectorReadinessState;
  readonly stateLabel: string;
}

export interface CollectorReadinessPageData {
  readonly rows: readonly CollectorReadinessRow[];
  readonly truth: EshuTruth | null;
}

interface CollectorReadinessResponse {
  readonly readiness?: readonly CollectorReadinessWireRow[];
}

interface CollectorReadinessWireRow {
  readonly blockers?: readonly string[];
  readonly claim_driven?: boolean;
  readonly claim_state?: string;
  readonly collector_kind?: string;
  readonly display_name?: string;
  readonly evidence_sources?: readonly string[];
  readonly health?: string;
  readonly instance_id?: string;
  readonly last_proof_at?: string;
  readonly observation_count?: number;
  readonly promotion_state?: string;
  readonly recommended_next_action?: string;
  readonly reducer_readback?: string;
  readonly source_scope?: string;
  readonly source_systems?: readonly string[];
}

export async function loadCollectorReadiness(client: EshuApiClient): Promise<CollectorReadinessPageData> {
  const env = await client.get<CollectorReadinessResponse>("/api/v0/status/collector-readiness");
  if (env.error) throw new EshuEnvelopeError(env.error);
  return {
    rows: (env.data?.readiness ?? []).map(readinessRowFromWire),
    truth: env.truth ?? null
  };
}

function readinessRowFromWire(row: CollectorReadinessWireRow): CollectorReadinessRow {
  const kind = clean(row.collector_kind) || "collector";
  const state = readinessState(row.promotion_state);
  const observationCount = finiteCount(row.observation_count);
  return {
    blockingGate: firstClean(row.blockers) || clean(row.recommended_next_action) || "none",
    claimDriven: row.claim_driven === true,
    claimState: clean(row.claim_state) || "none",
    displayName: clean(row.display_name) || displayName(kind),
    evidence: evidenceLabels(row),
    family: collectorFamily(kind),
    health: clean(row.health) || stateLabel(state),
    instanceId: clean(row.instance_id),
    kind,
    lastProof: lastProofLabel(observationCount, row.last_proof_at),
    reducerReadback: clean(row.reducer_readback) || "unavailable",
    sourceScope: clean(row.source_scope) || kind,
    state,
    stateLabel: stateLabel(state)
  };
}

function evidenceLabels(row: CollectorReadinessWireRow): readonly string[] {
  const labels = new Set<string>();
  for (const source of row.evidence_sources ?? []) {
    const normalized = clean(source);
    if (normalized === "source_facts") labels.add("source facts");
    else if (normalized === "reducer_facts") labels.add("reducer facts");
    else if (normalized === "workflow_coordinator") labels.add("queue");
    else if (normalized !== "") labels.add(normalized.replace(/_/g, " "));
  }
  if (row.claim_state === "claim_driven") labels.add("queue");
  if ((row.reducer_readback ?? "") === "available") labels.add("reducer facts");
  labels.add("API/MCP evidence");
  return [...labels];
}

function readinessState(value: string | undefined): CollectorReadinessState {
  switch (value) {
    case "disabled":
    case "failed":
    case "gated":
    case "implemented":
    case "partial":
    case "permission_hidden":
    case "stale":
    case "unsupported":
      return value;
    default:
      return "partial";
  }
}

function collectorFamily(kind: string): string {
  if (["aws", "azure", "gcp", "kubernetes_live", "oci_registry", "terraform_state"].includes(kind)) return "Cloud and runtime";
  if (["pagerduty", "jira", "prometheus_mimir", "tempo", "grafana", "loki"].includes(kind)) return "Operations evidence";
  if (["sbom_attestation", "security_alert", "scanner_worker", "vault_live", "vulnerability_intelligence"].includes(kind)) return "Security evidence";
  return "Source collection";
}

function stateLabel(state: CollectorReadinessState): string {
  return state.replace(/_/g, " ");
}

function lastProofLabel(observationCount: number, lastProofAt: string | undefined): string {
  const proofAt = clean(lastProofAt);
  if (observationCount > 0 && proofAt !== "") return `${observationCount} observations at ${proofAt}`;
  if (observationCount > 0) return `${observationCount} observations`;
  return proofAt || "not observed";
}

function displayName(kind: string): string {
  return kind.split("_").filter(Boolean).map((part) => `${part.slice(0, 1).toUpperCase()}${part.slice(1)}`).join(" ");
}

function clean(value: string | undefined): string {
  return value?.trim() ?? "";
}

function firstClean(values: readonly string[] | undefined): string {
  return (values ?? []).map(clean).find((value) => value.length > 0) ?? "";
}

function finiteCount(value: number | undefined): number {
  return typeof value === "number" && Number.isFinite(value) && value > 0 ? value : 0;
}
