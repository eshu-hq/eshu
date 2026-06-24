import type {
  CICDCountWire,
  CICDEvidenceBlockWire,
  CICDEvidenceSummaryWire,
  CICDInventoryWire,
  CICDListWire,
  CICDRunArtifactEvidenceWire,
  CICDRunCorrelationWire,
  CICDStaticWorkflowArtifactsWire
} from "./cicdRunCorrelationsWireTypes";
import type { EshuApiClient } from "./client";
import { EshuEnvelopeError, type EshuTruth } from "./envelope";

export type CICDRunCorrelationInventoryGroup = "environment" | "outcome" | "provider" | "repository_id";

export interface CICDRunCorrelationInput {
  readonly afterCorrelationId?: string;
  readonly artifactDigest?: string;
  readonly commitSha?: string;
  readonly environment?: string;
  readonly imageRef?: string;
  readonly inventoryGroup?: CICDRunCorrelationInventoryGroup;
  readonly inventoryLimit?: number;
  readonly limit?: number;
  readonly outcome?: string;
  readonly provider?: string;
  readonly providerRunId?: string;
  readonly repositoryId?: string;
  readonly scopeId?: string;
}

export interface CICDRunCorrelationReview {
  readonly count: CICDReviewSection<CICDRunCorrelationCount>;
  readonly input: Required<CICDRunCorrelationInput>;
  readonly inventory: CICDReviewSection<CICDRunCorrelationInventory>;
  readonly list: CICDReviewSection<CICDRunCorrelationPage> | CICDSkippedSection;
}

export type CICDReviewSection<TData> =
  | {
      readonly data: TData;
      readonly source: string;
      readonly status: "ready";
      readonly truth: EshuTruth | null;
    }
  | {
      readonly error: string;
      readonly source: string;
      readonly status: "unavailable";
    };

export interface CICDSkippedSection {
  readonly reason: string;
  readonly source: string;
  readonly status: "skipped";
}

export interface CICDRunCorrelationCount {
  readonly byEnvironment: Record<string, number>;
  readonly byOutcome: Record<string, number>;
  readonly byProvider: Record<string, number>;
  readonly scope: Record<string, string>;
  readonly totalCorrelations: number;
}

export interface CICDRunCorrelationInventory {
  readonly buckets: readonly CICDRunCorrelationBucket[];
  readonly count: number;
  readonly groupBy: CICDRunCorrelationInventoryGroup;
  readonly limit: number;
  readonly nextOffset: number | null;
  readonly offset: number;
  readonly scope: Record<string, string>;
  readonly truncated: boolean;
}

export interface CICDRunCorrelationBucket {
  readonly count: number;
  readonly dimension: string;
  readonly value: string;
}

export interface CICDRunCorrelationPage {
  readonly correlations: readonly CICDRunCorrelationRow[];
  readonly count: number;
  readonly evidenceSummary: CICDEvidenceSummary;
  readonly limit: number;
  readonly nextCursor: {
    readonly afterCorrelationId: string;
  } | null;
  readonly truncated: boolean;
}

export interface CICDRunCorrelationRow {
  readonly artifactDigest: string;
  readonly canonicalTarget: string;
  readonly canonicalWrites: number;
  readonly commitSha: string;
  readonly correlationId: string;
  readonly correlationKind: string;
  readonly environment: string;
  readonly evidenceFactIds: readonly string[];
  readonly imageRef: string;
  readonly outcome: string;
  readonly provider: string;
  readonly provenanceOnly: boolean;
  readonly reason: string;
  readonly repositoryId: string;
  readonly runAttempt: string;
  readonly runId: string;
}

export interface CICDEvidenceSummary {
  readonly liveRunCorrelations: CICDEvidenceBlock;
  readonly missingEvidence: readonly string[];
  readonly reason: string;
  readonly runArtifactEvidence: CICDRunArtifactEvidence;
  readonly staticWorkflowArtifacts: CICDStaticWorkflowArtifacts;
}

export interface CICDEvidenceBlock {
  readonly count: number;
  readonly reason: string;
  readonly state: string;
  readonly truncated: boolean;
}

export interface CICDRunArtifactEvidence extends CICDEvidenceBlock {
  readonly ambiguousCount: number;
  readonly artifactDigestCount: number;
  readonly imageRefCount: number;
}

export interface CICDStaticWorkflowArtifacts extends CICDEvidenceBlock {
  readonly ambiguousCount: number;
  readonly evidenceClass: string;
  readonly imageRefCount: number;
  readonly paths: readonly string[];
  readonly unresolvedCount: number;
}

const sourcePaths = {
  count: "/api/v0/ci-cd/run-correlations/count",
  inventory: "/api/v0/ci-cd/run-correlations/inventory",
  list: "/api/v0/ci-cd/run-correlations"
} as const;

export async function loadCICDRunCorrelationReview(
  client: EshuApiClient,
  rawInput: CICDRunCorrelationInput
): Promise<CICDRunCorrelationReview> {
  const input = normalizeInput(rawInput);
  const count = await loadSection(
    sourcePaths.count,
    () => client.get<CICDCountWire>(countPath(input)),
    normalizeCount
  );
  const inventory = await loadSection(
    sourcePaths.inventory,
    () => client.get<CICDInventoryWire>(inventoryPath(input)),
    normalizeInventory
  );
  const list = await loadListSection(client, input);
  return { count, input, inventory, list };
}

async function loadListSection(
  client: EshuApiClient,
  input: Required<CICDRunCorrelationInput>
): Promise<CICDReviewSection<CICDRunCorrelationPage> | CICDSkippedSection> {
  if (!hasListAnchor(input)) {
    return {
      reason: "Add a scope, repository, commit, run, artifact, image, or environment anchor to load row details.",
      source: sourcePaths.list,
      status: "skipped"
    };
  }
  if (input.providerRunId.length > 0 && input.provider.length === 0 && !hasProviderRunDisambiguator(input)) {
    return {
      reason: "Provider is required when provider run id is the only anchor.",
      source: sourcePaths.list,
      status: "skipped"
    };
  }
  return loadSection(
    sourcePaths.list,
    () => client.get<CICDListWire>(listPath(input)),
    normalizePage
  );
}

async function loadSection<TWire, TData>(
  source: string,
  load: () => Promise<{
    readonly data: TWire | null;
    readonly error: { readonly code: string; readonly message: string } | null;
    readonly truth: EshuTruth | null;
  }>,
  normalize: (wire: TWire) => TData
): Promise<CICDReviewSection<TData>> {
  try {
    const envelope = await load();
    if (envelope.error !== null) {
      throw new EshuEnvelopeError(envelope.error);
    }
    if (envelope.data === null) {
      throw new Error("Eshu envelope success response is missing data");
    }
    return {
      data: normalize(envelope.data),
      source,
      status: "ready",
      truth: envelope.truth
    };
  } catch (error) {
    return {
      error: error instanceof Error ? error.message : "request failed",
      source,
      status: "unavailable"
    };
  }
}

function normalizeInput(input: CICDRunCorrelationInput): Required<CICDRunCorrelationInput> {
  return {
    afterCorrelationId: nonEmpty(input.afterCorrelationId),
    artifactDigest: nonEmpty(input.artifactDigest),
    commitSha: nonEmpty(input.commitSha),
    environment: nonEmpty(input.environment),
    imageRef: nonEmpty(input.imageRef),
    inventoryGroup: input.inventoryGroup ?? "outcome",
    inventoryLimit: clampInt(input.inventoryLimit, 25, 1, 500),
    limit: clampInt(input.limit, 25, 1, 200),
    outcome: nonEmpty(input.outcome),
    provider: nonEmpty(input.provider),
    providerRunId: nonEmpty(input.providerRunId),
    repositoryId: nonEmpty(input.repositoryId),
    scopeId: nonEmpty(input.scopeId)
  };
}

function countPath(input: Required<CICDRunCorrelationInput>): string {
  const params = aggregateParams(input);
  return withQuery(sourcePaths.count, params);
}

function inventoryPath(input: Required<CICDRunCorrelationInput>): string {
  const params = new URLSearchParams();
  params.set("group_by", input.inventoryGroup);
  addCommonParams(params, input);
  params.set("limit", String(input.inventoryLimit));
  return withQuery(sourcePaths.inventory, params);
}

function listPath(input: Required<CICDRunCorrelationInput>): string {
  const params = new URLSearchParams();
  addCommonParams(params, input);
  if (input.providerRunId.length > 0) params.set("provider_run_id", input.providerRunId);
  if (input.afterCorrelationId.length > 0) params.set("after_correlation_id", input.afterCorrelationId);
  params.set("limit", String(input.limit));
  return withQuery(sourcePaths.list, params);
}

function aggregateParams(input: Required<CICDRunCorrelationInput>): URLSearchParams {
  const params = new URLSearchParams();
  addCommonParams(params, input);
  return params;
}

function addCommonParams(params: URLSearchParams, input: Required<CICDRunCorrelationInput>): void {
  if (input.scopeId.length > 0) params.set("scope_id", input.scopeId);
  if (input.repositoryId.length > 0) params.set("repository_id", input.repositoryId);
  if (input.commitSha.length > 0) params.set("commit_sha", input.commitSha);
  if (input.provider.length > 0) params.set("provider", input.provider);
  if (input.artifactDigest.length > 0) params.set("artifact_digest", input.artifactDigest);
  if (input.imageRef.length > 0) params.set("image_ref", input.imageRef);
  if (input.environment.length > 0) params.set("environment", input.environment);
  if (input.outcome.length > 0) params.set("outcome", input.outcome);
}

function withQuery(path: string, params: URLSearchParams): string {
  const query = params.toString();
  return query.length > 0 ? `${path}?${query}` : path;
}

function normalizeCount(wire: CICDCountWire): CICDRunCorrelationCount {
  return {
    byEnvironment: wire.by_environment ?? {},
    byOutcome: wire.by_outcome ?? {},
    byProvider: wire.by_provider ?? {},
    scope: wire.scope ?? {},
    totalCorrelations: wire.total_correlations ?? 0
  };
}

function normalizeInventory(wire: CICDInventoryWire): CICDRunCorrelationInventory {
  return {
    buckets: (wire.buckets ?? []).map((bucket) => ({
      count: bucket.count ?? 0,
      dimension: nonEmpty(bucket.dimension),
      value: nonEmpty(bucket.value, "unlabeled")
    })),
    count: wire.count ?? wire.buckets?.length ?? 0,
    groupBy: wire.group_by ?? "outcome",
    limit: wire.limit ?? 25,
    nextOffset: wire.next_offset ?? null,
    offset: wire.offset ?? 0,
    scope: wire.scope ?? {},
    truncated: wire.truncated ?? false
  };
}

function normalizePage(wire: CICDListWire): CICDRunCorrelationPage {
  return {
    correlations: (wire.correlations ?? []).map(normalizeRow),
    count: wire.count ?? wire.correlations?.length ?? 0,
    evidenceSummary: normalizeEvidenceSummary(wire.evidence_summary),
    limit: wire.limit ?? 25,
    nextCursor: wire.next_cursor?.after_correlation_id
      ? { afterCorrelationId: wire.next_cursor.after_correlation_id }
      : null,
    truncated: wire.truncated ?? false
  };
}

function normalizeRow(wire: CICDRunCorrelationWire): CICDRunCorrelationRow {
  return {
    artifactDigest: nonEmpty(wire.artifact_digest),
    canonicalTarget: nonEmpty(wire.canonical_target),
    canonicalWrites: wire.canonical_writes ?? 0,
    commitSha: nonEmpty(wire.commit_sha),
    correlationId: nonEmpty(wire.correlation_id, "correlation"),
    correlationKind: nonEmpty(wire.correlation_kind),
    environment: nonEmpty(wire.environment),
    evidenceFactIds: wire.evidence_fact_ids ?? [],
    imageRef: nonEmpty(wire.image_ref),
    outcome: nonEmpty(wire.outcome, "unresolved"),
    provider: nonEmpty(wire.provider),
    provenanceOnly: wire.provenance_only ?? false,
    reason: nonEmpty(wire.reason),
    repositoryId: nonEmpty(wire.repository_id),
    runAttempt: nonEmpty(wire.run_attempt),
    runId: nonEmpty(wire.run_id)
  };
}

function normalizeEvidenceSummary(wire: CICDEvidenceSummaryWire | undefined): CICDEvidenceSummary {
  return {
    liveRunCorrelations: normalizeEvidenceBlock(wire?.live_run_correlations),
    missingEvidence: wire?.missing_evidence ?? [],
    reason: nonEmpty(wire?.reason),
    runArtifactEvidence: normalizeRunArtifactEvidence(wire?.run_artifact_evidence),
    staticWorkflowArtifacts: normalizeStaticWorkflowArtifacts(wire?.static_workflow_artifacts)
  };
}

function normalizeEvidenceBlock(wire: CICDEvidenceBlockWire | undefined): CICDEvidenceBlock {
  return {
    count: wire?.count ?? 0,
    reason: nonEmpty(wire?.reason),
    state: nonEmpty(wire?.state, "missing"),
    truncated: wire?.truncated ?? false
  };
}

function normalizeRunArtifactEvidence(wire: CICDRunArtifactEvidenceWire | undefined): CICDRunArtifactEvidence {
  return {
    ...normalizeEvidenceBlock(wire),
    ambiguousCount: wire?.ambiguous_count ?? 0,
    artifactDigestCount: wire?.artifact_digest_count ?? 0,
    imageRefCount: wire?.image_ref_count ?? 0
  };
}

function normalizeStaticWorkflowArtifacts(wire: CICDStaticWorkflowArtifactsWire | undefined): CICDStaticWorkflowArtifacts {
  return {
    ...normalizeEvidenceBlock(wire),
    ambiguousCount: wire?.ambiguous_count ?? 0,
    evidenceClass: nonEmpty(wire?.evidence_class),
    imageRefCount: wire?.image_ref_count ?? 0,
    paths: wire?.paths ?? [],
    unresolvedCount: wire?.unresolved_count ?? 0
  };
}

function hasListAnchor(input: Required<CICDRunCorrelationInput>): boolean {
  return input.scopeId.length > 0 ||
    input.repositoryId.length > 0 ||
    input.commitSha.length > 0 ||
    input.providerRunId.length > 0 ||
    input.artifactDigest.length > 0 ||
    input.imageRef.length > 0 ||
    input.environment.length > 0;
}

function hasProviderRunDisambiguator(input: Required<CICDRunCorrelationInput>): boolean {
  return input.scopeId.length > 0 ||
    input.repositoryId.length > 0 ||
    input.commitSha.length > 0 ||
    input.artifactDigest.length > 0 ||
    input.imageRef.length > 0 ||
    input.environment.length > 0;
}

function clampInt(value: number | undefined, fallback: number, min: number, max: number): number {
  if (value === undefined || !Number.isFinite(value)) {
    return fallback;
  }
  return Math.max(min, Math.min(max, Math.trunc(value)));
}

function nonEmpty(...values: readonly (string | undefined)[]): string {
  return values.find((value) => value !== undefined && value.trim().length > 0)?.trim() ?? "";
}
