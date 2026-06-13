import type { EshuTruth } from "./envelope";

export { emptyAnswerGraph } from "./answerVisualization";

const citationLimit = 10;

export type AnswerStatus = "partial" | "supported" | "unavailable" | "unsupported";

export interface AnswerEvidenceHandle {
  readonly endLine?: number;
  readonly entityId?: string;
  readonly evidenceFamily: string;
  readonly kind: string;
  readonly reason: string;
  readonly relativePath?: string;
  readonly repoId?: string;
  readonly startLine?: number;
}

export interface AnswerNextCall {
  readonly args?: Record<string, unknown>;
  readonly params?: Record<string, unknown>;
  readonly reason: string;
  readonly route?: string;
  readonly tool: string;
}

export interface AnswerCompanion {
  readonly citationRef: string;
  readonly evidenceHandles: readonly AnswerEvidenceHandle[];
  readonly limitations: readonly string[];
  readonly missingEvidence: readonly string[];
  readonly partial: boolean;
  readonly partialReasons: readonly string[];
  readonly primaryRoute: string;
  readonly primaryTool: string;
  readonly promptFamily: string;
  readonly question: string;
  readonly recommendedNextCalls: readonly AnswerNextCall[];
  readonly status: AnswerStatus;
  readonly summary: string;
  readonly supported: boolean;
  readonly truth: EshuTruth | null;
  readonly truthClass: string;
  readonly truncated: boolean;
  readonly unsupportedReasons: readonly string[];
}

export interface AnswerMetadataWire {
  readonly coverage?: Record<string, unknown>;
  readonly evidence_handles?: readonly AnswerEvidenceHandleWire[];
  readonly limitations?: readonly string[];
  readonly missing_evidence?: readonly string[];
  readonly partial_reasons?: readonly string[];
  readonly recommended_next_calls?: readonly AnswerNextCallWire[];
  readonly truncated?: boolean;
}

export interface AnswerPacketWire {
  readonly citation_ref?: string;
  readonly evidence_handles?: readonly AnswerEvidenceHandleWire[];
  readonly limitations?: readonly string[];
  readonly missing_evidence?: readonly string[];
  readonly partial?: boolean;
  readonly primary_route?: string;
  readonly primary_tool?: string;
  readonly prompt_family?: string;
  readonly question?: string;
  readonly recommended_next_calls?: readonly AnswerNextCallWire[];
  readonly summary?: string;
  readonly supported?: boolean;
  readonly truth?: EshuTruth | null;
  readonly truth_class?: string;
  readonly truncated?: boolean;
  readonly unsupported_reasons?: readonly string[];
}

export interface AnswerEvidenceHandleWire {
  readonly end_line?: number;
  readonly entity_id?: string;
  readonly evidence_family?: string;
  readonly kind?: string;
  readonly reason?: string;
  readonly relative_path?: string;
  readonly repo_id?: string;
  readonly start_line?: number;
}

export interface AnswerNextCallWire {
  readonly args?: Record<string, unknown>;
  readonly params?: Record<string, unknown>;
  readonly reason?: string;
  readonly route?: string;
  readonly tool?: string;
}

export interface EvidenceCitationRequest {
  readonly handles: readonly AnswerEvidenceHandleWire[];
  readonly limit: number;
  readonly question: string;
  readonly subject: {
    readonly prompt_family: string;
  };
}

export interface EvidenceCitationPacket {
  readonly citations: readonly EvidenceCitation[];
  readonly coverage: EvidenceCitationCoverage;
  readonly missingHandles: readonly AnswerEvidenceHandle[];
  readonly raw: EvidenceCitationResponseWire;
  readonly recommendedNextCalls: readonly AnswerNextCall[];
  readonly truth: EshuTruth | null;
}

export interface EvidenceCitation {
  readonly citationId: string;
  readonly commitSha: string;
  readonly endLine?: number;
  readonly entityId: string;
  readonly entityName: string;
  readonly entityType: string;
  readonly evidenceFamily: string;
  readonly excerpt: string;
  readonly kind: string;
  readonly language: string;
  readonly rank: number;
  readonly reason: string;
  readonly relativePath?: string;
  readonly repoId?: string;
  readonly startLine?: number;
}

export interface EvidenceCitationCoverage {
  readonly inputHandleCount: number;
  readonly limit: number;
  readonly missingCount: number;
  readonly queryShape: string;
  readonly resolvedCount: number;
  readonly sourceBackend: string;
  readonly truncated: boolean;
}

export interface EvidenceCitationResponseWire {
  readonly citations?: readonly EvidenceCitationWire[];
  readonly coverage?: EvidenceCitationCoverageWire;
  readonly missing_handles?: readonly AnswerEvidenceHandleWire[];
  readonly recommended_next_calls?: readonly AnswerNextCallWire[];
}

export interface EvidenceCitationWire {
  readonly citation_id?: string;
  readonly commit_sha?: string;
  readonly end_line?: number;
  readonly entity_id?: string;
  readonly entity_name?: string;
  readonly entity_type?: string;
  readonly evidence_family?: string;
  readonly excerpt?: string;
  readonly kind?: string;
  readonly language?: string;
  readonly rank?: number;
  readonly reason?: string;
  readonly relative_path?: string;
  readonly repo_id?: string;
  readonly start_line?: number;
}

export interface EvidenceCitationCoverageWire {
  readonly input_handle_count?: number;
  readonly limit?: number;
  readonly missing_count?: number;
  readonly query_shape?: string;
  readonly resolved_count?: number;
  readonly source_backend?: string;
  readonly truncated?: boolean;
}

export function normalizeAnswerCompanion({
  answerMetadata,
  answerPacket,
  routeTruth
}: {
  readonly answerMetadata?: AnswerMetadataWire;
  readonly answerPacket?: AnswerPacketWire;
  readonly routeTruth: EshuTruth | null;
}): AnswerCompanion {
  const hasPacket = answerPacket !== undefined;
  const supported = answerPacket?.supported ?? false;
  const truncated = (answerPacket?.truncated ?? false) || (answerMetadata?.truncated ?? false);
  const partial = answerPacket?.partial ?? truncated;
  const missingEvidence = uniqueStrings([
    ...(answerPacket?.missing_evidence ?? []),
    ...(answerMetadata?.missing_evidence ?? []),
    ...(hasPacket ? [] : ["answer packet not returned"])
  ]);
  const unsupportedReasons = supported ? [] : uniqueStrings(answerPacket?.unsupported_reasons ?? []);
  const partialReasons = supported
    ? uniqueStrings([...(answerMetadata?.partial_reasons ?? []), ...(answerPacket?.unsupported_reasons ?? [])])
    : [];
  return {
    citationRef: clean(answerPacket?.citation_ref),
    evidenceHandles: uniqueHandles([
      ...handleRows(answerPacket?.evidence_handles),
      ...handleRows(answerMetadata?.evidence_handles)
    ]),
    limitations: uniqueStrings([
      ...(answerPacket?.limitations ?? []),
      ...(answerMetadata?.limitations ?? [])
    ]),
    missingEvidence,
    partial,
    partialReasons,
    primaryRoute: clean(answerPacket?.primary_route),
    primaryTool: clean(answerPacket?.primary_tool),
    promptFamily: clean(answerPacket?.prompt_family),
    question: clean(answerPacket?.question),
    recommendedNextCalls: nextCalls([
      ...(answerPacket?.recommended_next_calls ?? []),
      ...(answerMetadata?.recommended_next_calls ?? [])
    ]),
    status: answerStatus({ hasPacket, missingEvidence, partial, supported, truncated }),
    summary: supported ? clean(answerPacket?.summary) : "",
    supported,
    truth: answerPacket?.truth ?? routeTruth,
    truthClass: clean(answerPacket?.truth_class) || (supported ? "" : "unsupported"),
    truncated,
    unsupportedReasons
  };
}

export function citationRequest(
  question: string,
  answer: AnswerCompanion
): EvidenceCitationRequest {
  return {
    handles: answer.evidenceHandles.map(wireEvidenceHandle),
    limit: citationLimit,
    question,
    subject: {
      prompt_family: answer.promptFamily
    }
  };
}

export function normalizeCitationPacket(
  response: EvidenceCitationResponseWire,
  truth: EshuTruth | null
): EvidenceCitationPacket {
  const coverage = response.coverage ?? {};
  return {
    citations: (response.citations ?? []).map((citation) => ({
      citationId: clean(citation.citation_id),
      commitSha: clean(citation.commit_sha),
      endLine: citation.end_line,
      entityId: clean(citation.entity_id),
      entityName: clean(citation.entity_name),
      entityType: clean(citation.entity_type),
      evidenceFamily: clean(citation.evidence_family),
      excerpt: clean(citation.excerpt),
      kind: clean(citation.kind),
      language: clean(citation.language),
      rank: citation.rank ?? 0,
      reason: clean(citation.reason),
      relativePath: optionalClean(citation.relative_path),
      repoId: optionalClean(citation.repo_id),
      startLine: citation.start_line
    })),
    coverage: {
      inputHandleCount: coverage.input_handle_count ?? 0,
      limit: coverage.limit ?? 0,
      missingCount: coverage.missing_count ?? 0,
      queryShape: clean(coverage.query_shape),
      resolvedCount: coverage.resolved_count ?? 0,
      sourceBackend: clean(coverage.source_backend),
      truncated: coverage.truncated ?? false
    },
    missingHandles: handleRows(response.missing_handles),
    raw: response,
    recommendedNextCalls: nextCalls(response.recommended_next_calls ?? []),
    truth
  };
}

export function buildSourceCitationHref(handle: {
  readonly relativePath?: string;
  readonly repoId?: string;
  readonly startLine?: number;
}): string {
  const params = new URLSearchParams({
    path: handle.relativePath ?? "",
    lineStart: String(handle.startLine ?? 1)
  });
  return `/repositories/${encodeURIComponent(handle.repoId ?? "")}/source?${params.toString()}`;
}

export function sourceCitationLabel(handle: {
  readonly entityId?: string;
  readonly relativePath?: string;
  readonly startLine?: number;
}): string {
  if (handle.relativePath !== undefined && handle.relativePath.length > 0) {
    return `${handle.relativePath}:${handle.startLine ?? 1}`;
  }
  return handle.entityId ?? "evidence handle";
}

export function wireEvidenceHandle(handle: AnswerEvidenceHandle): AnswerEvidenceHandleWire {
  return {
    ...(handle.endLine === undefined ? {} : { end_line: handle.endLine }),
    ...(handle.entityId === undefined ? {} : { entity_id: handle.entityId }),
    evidence_family: handle.evidenceFamily,
    kind: handle.kind,
    reason: handle.reason,
    ...(handle.relativePath === undefined ? {} : { relative_path: handle.relativePath }),
    ...(handle.repoId === undefined ? {} : { repo_id: handle.repoId }),
    ...(handle.startLine === undefined ? {} : { start_line: handle.startLine })
  };
}

function answerStatus({
  hasPacket,
  missingEvidence,
  partial,
  supported,
  truncated
}: {
  readonly hasPacket: boolean;
  readonly missingEvidence: readonly string[];
  readonly partial: boolean;
  readonly supported: boolean;
  readonly truncated: boolean;
}): AnswerStatus {
  if (!hasPacket) return "unavailable";
  if (!supported) return "unsupported";
  if (partial || truncated || missingEvidence.length > 0) return "partial";
  return "supported";
}

function handleRows(handles: readonly AnswerEvidenceHandleWire[] | undefined): readonly AnswerEvidenceHandle[] {
  return (handles ?? []).map(handleRow).filter((handle): handle is AnswerEvidenceHandle => handle !== null);
}

function handleRow(handle: AnswerEvidenceHandleWire | undefined): AnswerEvidenceHandle | null {
  if (handle === undefined) {
    return null;
  }
  const kind = clean(handle.kind);
  const repoId = optionalClean(handle.repo_id);
  const relativePath = optionalClean(handle.relative_path);
  const entityId = optionalClean(handle.entity_id);
  if (kind.length === 0 || (relativePath === undefined && entityId === undefined)) {
    return null;
  }
  return {
    endLine: handle.end_line,
    entityId,
    evidenceFamily: clean(handle.evidence_family),
    kind,
    reason: clean(handle.reason),
    relativePath,
    repoId,
    startLine: handle.start_line
  };
}

function nextCalls(calls: readonly AnswerNextCallWire[]): readonly AnswerNextCall[] {
  return calls
    .map((call) => ({
      args: call.args,
      params: call.params,
      reason: clean(call.reason),
      route: optionalClean(call.route),
      tool: clean(call.tool)
    }))
    .filter((call) => call.tool.length > 0 || call.route !== undefined);
}

function uniqueHandles(handles: readonly AnswerEvidenceHandle[]): readonly AnswerEvidenceHandle[] {
  const seen = new Set<string>();
  return handles.filter((handle) => {
    const key = JSON.stringify(wireEvidenceHandle(handle));
    if (seen.has(key)) {
      return false;
    }
    seen.add(key);
    return true;
  });
}

function uniqueStrings(values: readonly string[]): readonly string[] {
  const seen = new Set<string>();
  return values
    .map(clean)
    .filter((value) => {
      if (value.length === 0 || seen.has(value)) {
        return false;
      }
      seen.add(value);
      return true;
    });
}

function optionalClean(value: string | undefined): string | undefined {
  const cleaned = clean(value);
  return cleaned.length === 0 ? undefined : cleaned;
}

function clean(value: string | undefined): string {
  return value?.trim() ?? "";
}
