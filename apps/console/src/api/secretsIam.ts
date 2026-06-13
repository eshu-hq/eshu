import type { EshuApiClient } from "./client";
import { EshuEnvelopeError, type EshuTruth } from "./envelope";

export type SecretsIamState = "exact" | "partial" | "unresolved" | "stale" | "permission_hidden" | "unsupported";

export interface SecretsIamInput {
  readonly limit?: number;
  readonly scopeId?: string;
  readonly state?: string;
}

export interface SecretsIamReview {
  readonly input: Required<SecretsIamInput>;
  readonly postureGaps: SecretsIamSection<SecretsIamPostureGaps> | SecretsIamSkippedSection;
  readonly privilegeObservations: SecretsIamSection<SecretsIamPrivilegeObservations> | SecretsIamSkippedSection;
  readonly secretAccessPaths: SecretsIamSection<SecretsIamSecretAccessPaths> | SecretsIamSkippedSection;
  readonly summary: SecretsIamSection<SecretsIamPostureSummary> | SecretsIamSkippedSection;
  readonly trustChains: SecretsIamSection<SecretsIamTrustChains> | SecretsIamSkippedSection;
}

export type SecretsIamSection<TData> =
  | { readonly data: TData; readonly source: string; readonly status: "ready"; readonly truth: EshuTruth | null }
  | { readonly error: string; readonly source: string; readonly status: "unavailable" };

export interface SecretsIamSkippedSection {
  readonly reason: string;
  readonly source: string;
  readonly status: "skipped";
}

export interface SecretsIamBucketCount {
  readonly bucket: string;
  readonly count: number;
}

export interface SecretsIamPostureSummary {
  readonly identityTrustChainsByState: readonly SecretsIamBucketCount[];
  readonly postureGapsByGapType: readonly SecretsIamBucketCount[];
  readonly privilegeObservationsByRiskType: readonly SecretsIamBucketCount[];
  readonly privilegeObservationsBySeverity: readonly SecretsIamBucketCount[];
  readonly scopeId: string;
  readonly secretAccessPathsByState: readonly SecretsIamBucketCount[];
}

export interface SecretsIamTrustChains {
  readonly chains: readonly SecretsIamTrustChain[];
  readonly count: number;
  readonly limit: number;
  readonly nextCursor: { readonly afterChainId?: string } | null;
  readonly truncated: boolean;
}

export interface SecretsIamTrustChain {
  readonly chainId: string;
  readonly confidence: string;
  readonly evidenceFactIds: readonly string[];
  readonly iamRoleFingerprint: string;
  readonly missingEvidence: readonly string[];
  readonly serviceAccountJoinKey: string;
  readonly sourceGenerations: readonly string[];
  readonly sourceScopes: readonly string[];
  readonly state: string;
  readonly vaultMountJoinKey: string;
  readonly vaultPolicyJoinKeys: readonly string[];
  readonly workloadKind: string;
  readonly workloadObjectId: string;
}

export interface SecretsIamPrivilegeObservations {
  readonly count: number;
  readonly limit: number;
  readonly nextCursor: { readonly afterObservationId?: string } | null;
  readonly observations: readonly SecretsIamPrivilegeObservation[];
  readonly truncated: boolean;
}

export interface SecretsIamPrivilegeObservation {
  readonly confidence: string;
  readonly evidenceFactIds: readonly string[];
  readonly observationId: string;
  readonly reason: string;
  readonly riskType: string;
  readonly severity: string;
  readonly state: string;
  readonly subjectFingerprint: string;
}

export interface SecretsIamSecretAccessPaths {
  readonly count: number;
  readonly limit: number;
  readonly nextCursor: { readonly afterPathId?: string } | null;
  readonly paths: readonly SecretsIamSecretAccessPath[];
  readonly truncated: boolean;
}

export interface SecretsIamSecretAccessPath {
  readonly capabilities: readonly string[];
  readonly chainId: string;
  readonly confidence: string;
  readonly evidenceFactIds: readonly string[];
  readonly kvPathFingerprint: string;
  readonly pathId: string;
  readonly state: string;
  readonly vaultMountJoinKey: string;
  readonly vaultPolicyJoinKey: string;
}

export interface SecretsIamPostureGaps {
  readonly count: number;
  readonly gaps: readonly SecretsIamPostureGap[];
  readonly limit: number;
  readonly nextCursor: { readonly afterGapId?: string } | null;
  readonly truncated: boolean;
}

export interface SecretsIamPostureGap {
  readonly evidenceFactIds: readonly string[];
  readonly gapId: string;
  readonly gapType: string;
  readonly missingEvidence: readonly string[];
  readonly reason: string;
  readonly serviceAccountJoinKey: string;
  readonly state: string;
  readonly unsupportedLayers: readonly string[];
}

interface BucketCountWire {
  readonly bucket?: string;
  readonly count?: number;
}

interface SummaryWire {
  readonly scope_id?: string;
  readonly summary?: {
    readonly identity_trust_chains_by_state?: readonly BucketCountWire[];
    readonly posture_gaps_by_gap_type?: readonly BucketCountWire[];
    readonly privilege_observations_by_risk_type?: readonly BucketCountWire[];
    readonly privilege_observations_by_severity?: readonly BucketCountWire[];
    readonly secret_access_paths_by_state?: readonly BucketCountWire[];
  };
}

interface TrustChainWire {
  readonly chain_id?: string;
  readonly confidence?: string;
  readonly evidence_fact_ids?: readonly string[];
  readonly iam_role_fingerprint?: string;
  readonly missing_evidence?: readonly string[];
  readonly service_account_join_key?: string;
  readonly source_generations?: readonly string[];
  readonly source_scopes?: readonly string[];
  readonly state?: string;
  readonly vault_mount_join_key?: string;
  readonly vault_policy_join_keys?: readonly string[];
  readonly workload_kind?: string;
  readonly workload_object_id?: string;
}

interface TrustChainsWire {
  readonly count?: number;
  readonly identity_trust_chains?: readonly TrustChainWire[];
  readonly limit?: number;
  readonly next_cursor?: { readonly after_chain_id?: string };
  readonly truncated?: boolean;
}

interface PrivilegeObservationWire {
  readonly confidence?: string;
  readonly evidence_fact_ids?: readonly string[];
  readonly observation_id?: string;
  readonly reason?: string;
  readonly risk_type?: string;
  readonly severity?: string;
  readonly state?: string;
  readonly subject_fingerprint?: string;
}

interface PrivilegeObservationsWire {
  readonly count?: number;
  readonly limit?: number;
  readonly next_cursor?: { readonly after_observation_id?: string };
  readonly privilege_posture_observations?: readonly PrivilegeObservationWire[];
  readonly truncated?: boolean;
}

interface SecretAccessPathWire {
  readonly capabilities?: readonly string[];
  readonly chain_id?: string;
  readonly confidence?: string;
  readonly evidence_fact_ids?: readonly string[];
  readonly kv_path_fingerprint?: string;
  readonly path_id?: string;
  readonly state?: string;
  readonly vault_mount_join_key?: string;
  readonly vault_policy_join_key?: string;
}

interface SecretAccessPathsWire {
  readonly count?: number;
  readonly limit?: number;
  readonly next_cursor?: { readonly after_path_id?: string };
  readonly secret_access_paths?: readonly SecretAccessPathWire[];
  readonly truncated?: boolean;
}

interface PostureGapWire {
  readonly evidence_fact_ids?: readonly string[];
  readonly gap_id?: string;
  readonly gap_type?: string;
  readonly missing_evidence?: readonly string[];
  readonly reason?: string;
  readonly service_account_join_key?: string;
  readonly state?: string;
  readonly unsupported_layers?: readonly string[];
}

interface PostureGapsWire {
  readonly count?: number;
  readonly limit?: number;
  readonly next_cursor?: { readonly after_gap_id?: string };
  readonly posture_gaps?: readonly PostureGapWire[];
  readonly truncated?: boolean;
}

const sourcePaths = {
  postureGaps: "/api/v0/secrets-iam/posture-gaps",
  privilegeObservations: "/api/v0/secrets-iam/privilege-posture-observations",
  secretAccessPaths: "/api/v0/secrets-iam/secret-access-paths",
  summary: "/api/v0/secrets-iam/posture-summary",
  trustChains: "/api/v0/secrets-iam/identity-trust-chains"
} as const;

export async function loadSecretsIamPosture(
  client: EshuApiClient,
  rawInput: SecretsIamInput
): Promise<SecretsIamReview> {
  const input = normalizeInput(rawInput);
  if (input.scopeId.length === 0) {
    const reason = "Add scope_id to load reducer-owned secrets/IAM posture.";
    return {
      input,
      postureGaps: skipped(sourcePaths.postureGaps, reason),
      privilegeObservations: skipped(sourcePaths.privilegeObservations, reason),
      secretAccessPaths: skipped(sourcePaths.secretAccessPaths, reason),
      summary: skipped(sourcePaths.summary, reason),
      trustChains: skipped(sourcePaths.trustChains, reason)
    };
  }
  const [summary, trustChains, privilegeObservations, secretAccessPaths, postureGaps] = await Promise.all([
    loadSection(sourcePaths.summary, () => client.get<SummaryWire>(summaryPath(input)), normalizeSummary),
    loadSection(sourcePaths.trustChains, () => client.get<TrustChainsWire>(listPath(sourcePaths.trustChains, input)), normalizeTrustChains),
    loadSection(
      sourcePaths.privilegeObservations,
      () => client.get<PrivilegeObservationsWire>(listPath(sourcePaths.privilegeObservations, input)),
      normalizePrivilegeObservations
    ),
    loadSection(
      sourcePaths.secretAccessPaths,
      () => client.get<SecretAccessPathsWire>(listPath(sourcePaths.secretAccessPaths, input)),
      normalizeSecretAccessPaths
    ),
    loadSection(sourcePaths.postureGaps, () => client.get<PostureGapsWire>(listPath(sourcePaths.postureGaps, input)), normalizePostureGaps)
  ]);
  return { input, postureGaps, privilegeObservations, secretAccessPaths, summary, trustChains };
}

async function loadSection<TWire, TData>(
  source: string,
  load: () => Promise<{
    readonly data: TWire | null;
    readonly error: { readonly code: string; readonly message: string } | null;
    readonly truth: EshuTruth | null;
  }>,
  normalize: (wire: TWire) => TData
): Promise<SecretsIamSection<TData>> {
  try {
    const envelope = await load();
    if (envelope.error !== null) throw new EshuEnvelopeError(envelope.error);
    if (envelope.data === null) throw new Error("Eshu envelope success response is missing data");
    return { data: normalize(envelope.data), source, status: "ready", truth: envelope.truth };
  } catch (error) {
    return { error: error instanceof Error ? error.message : "request failed", source, status: "unavailable" };
  }
}

function skipped(source: string, reason: string): SecretsIamSkippedSection {
  return { reason, source, status: "skipped" };
}

function normalizeInput(input: SecretsIamInput): Required<SecretsIamInput> {
  return {
    limit: clampInt(input.limit, 25, 1, 200),
    scopeId: nonEmpty(input.scopeId),
    state: nonEmpty(input.state)
  };
}

function summaryPath(input: Required<SecretsIamInput>): string {
  const params = new URLSearchParams();
  params.set("scope_id", input.scopeId);
  return `${sourcePaths.summary}?${params}`;
}

function listPath(source: string, input: Required<SecretsIamInput>): string {
  const params = new URLSearchParams();
  params.set("scope_id", input.scopeId);
  params.set("limit", String(input.limit));
  if (input.state.length > 0) params.set("state", input.state);
  return `${source}?${params}`;
}

function normalizeSummary(wire: SummaryWire): SecretsIamPostureSummary {
  const summary = wire.summary ?? {};
  return {
    identityTrustChainsByState: buckets(summary.identity_trust_chains_by_state),
    postureGapsByGapType: buckets(summary.posture_gaps_by_gap_type),
    privilegeObservationsByRiskType: buckets(summary.privilege_observations_by_risk_type),
    privilegeObservationsBySeverity: buckets(summary.privilege_observations_by_severity),
    scopeId: wire.scope_id ?? "",
    secretAccessPathsByState: buckets(summary.secret_access_paths_by_state)
  };
}

function normalizeTrustChains(wire: TrustChainsWire): SecretsIamTrustChains {
  return {
    chains: (wire.identity_trust_chains ?? []).map((row) => ({
      chainId: row.chain_id ?? "",
      confidence: row.confidence ?? "",
      evidenceFactIds: row.evidence_fact_ids ?? [],
      iamRoleFingerprint: row.iam_role_fingerprint ?? "",
      missingEvidence: row.missing_evidence ?? [],
      serviceAccountJoinKey: row.service_account_join_key ?? "",
      sourceGenerations: row.source_generations ?? [],
      sourceScopes: row.source_scopes ?? [],
      state: row.state ?? "",
      vaultMountJoinKey: row.vault_mount_join_key ?? "",
      vaultPolicyJoinKeys: row.vault_policy_join_keys ?? [],
      workloadKind: row.workload_kind ?? "",
      workloadObjectId: row.workload_object_id ?? ""
    })),
    count: wire.count ?? 0,
    limit: wire.limit ?? 0,
    nextCursor: wire.next_cursor ? { afterChainId: wire.next_cursor.after_chain_id } : null,
    truncated: wire.truncated ?? false
  };
}

function normalizePrivilegeObservations(wire: PrivilegeObservationsWire): SecretsIamPrivilegeObservations {
  return {
    count: wire.count ?? 0,
    limit: wire.limit ?? 0,
    nextCursor: wire.next_cursor ? { afterObservationId: wire.next_cursor.after_observation_id } : null,
    observations: (wire.privilege_posture_observations ?? []).map((row) => ({
      confidence: row.confidence ?? "",
      evidenceFactIds: row.evidence_fact_ids ?? [],
      observationId: row.observation_id ?? "",
      reason: row.reason ?? "",
      riskType: row.risk_type ?? "",
      severity: row.severity ?? "",
      state: row.state ?? "",
      subjectFingerprint: row.subject_fingerprint ?? ""
    })),
    truncated: wire.truncated ?? false
  };
}

function normalizeSecretAccessPaths(wire: SecretAccessPathsWire): SecretsIamSecretAccessPaths {
  return {
    count: wire.count ?? 0,
    limit: wire.limit ?? 0,
    nextCursor: wire.next_cursor ? { afterPathId: wire.next_cursor.after_path_id } : null,
    paths: (wire.secret_access_paths ?? []).map((row) => ({
      capabilities: row.capabilities ?? [],
      chainId: row.chain_id ?? "",
      confidence: row.confidence ?? "",
      evidenceFactIds: row.evidence_fact_ids ?? [],
      kvPathFingerprint: row.kv_path_fingerprint ?? "",
      pathId: row.path_id ?? "",
      state: row.state ?? "",
      vaultMountJoinKey: row.vault_mount_join_key ?? "",
      vaultPolicyJoinKey: row.vault_policy_join_key ?? ""
    })),
    truncated: wire.truncated ?? false
  };
}

function normalizePostureGaps(wire: PostureGapsWire): SecretsIamPostureGaps {
  return {
    count: wire.count ?? 0,
    gaps: (wire.posture_gaps ?? []).map((row) => ({
      evidenceFactIds: row.evidence_fact_ids ?? [],
      gapId: row.gap_id ?? "",
      gapType: row.gap_type ?? "",
      missingEvidence: row.missing_evidence ?? [],
      reason: row.reason ?? "",
      serviceAccountJoinKey: row.service_account_join_key ?? "",
      state: row.state ?? "",
      unsupportedLayers: row.unsupported_layers ?? []
    })),
    limit: wire.limit ?? 0,
    nextCursor: wire.next_cursor ? { afterGapId: wire.next_cursor.after_gap_id } : null,
    truncated: wire.truncated ?? false
  };
}

function buckets(rows: readonly BucketCountWire[] | undefined): readonly SecretsIamBucketCount[] {
  return (rows ?? []).map((row) => ({ bucket: row.bucket ?? "unknown", count: row.count ?? 0 }));
}

function nonEmpty(value: string | undefined): string {
  return value?.trim() ?? "";
}

function clampInt(value: number | undefined, fallback: number, min: number, max: number): number {
  if (value === undefined || !Number.isFinite(value)) return fallback;
  return Math.max(min, Math.min(max, Math.trunc(value)));
}
