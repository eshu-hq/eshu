import type { EshuApiClient } from "./client";
import { EshuEnvelopeError, type EshuTruth } from "./envelope";
import type {
  IncidentAnswerMetadataRecord,
  IncidentContextQueryRecord,
  IncidentContextResponse,
  IncidentEvidenceEdgeRecord,
  IncidentMissingEvidenceRecord,
  IncidentRecordWire,
  IncidentReferenceRecord,
  IncidentRelatedChangeRecord
} from "./incidentContextWireTypes";

export interface IncidentContextInput {
  readonly incidentId: string;
  readonly limit?: number;
  readonly provider?: string;
  readonly scopeId?: string;
  readonly serviceId?: string;
  readonly since?: string;
  readonly until?: string;
}

export type IncidentContextLoadResult =
  | {
      readonly context: IncidentContext;
      readonly status: "ready";
      readonly truth: EshuTruth | null;
    }
  | {
      readonly error: string;
      readonly status: "unavailable";
    };

export interface IncidentContext {
  readonly ambiguousEvidence: readonly IncidentEvidenceEdge[];
  readonly answerMetadata: IncidentAnswerMetadata;
  readonly evidencePath: readonly IncidentEvidenceEdge[];
  readonly incident: IncidentRecord;
  readonly missingEvidence: readonly IncidentMissingEvidence[];
  readonly query: IncidentContextQuery;
  readonly relatedChanges: readonly IncidentRelatedChange[];
  readonly timeline: readonly IncidentTimelineEvent[];
  readonly truncated: boolean;
}

export interface IncidentContextQuery {
  readonly incidentId: string;
  readonly limit: number;
  readonly provider: string;
  readonly scopeId: string;
  readonly serviceId: string;
  readonly since: string;
  readonly until: string;
}

export interface IncidentRecord {
  readonly createdAt: string;
  readonly evidenceFactId: string;
  readonly incidentNumber: number;
  readonly observedAt: string;
  readonly priority: IncidentReference;
  readonly provider: string;
  readonly providerIncidentId: string;
  readonly resolvedAt: string;
  readonly scopeId: string;
  readonly service: IncidentReference;
  readonly sourceConfidence: string;
  readonly sourceUrl: string;
  readonly status: string;
  readonly teams: readonly IncidentReference[];
  readonly title: string;
  readonly updatedAt: string;
  readonly urgency: string;
}

export interface IncidentReference {
  readonly id: string;
  readonly summary: string;
  readonly type: string;
  readonly url: string;
}

export interface IncidentEvidenceEdge {
  readonly candidates: readonly IncidentEvidenceCandidate[];
  readonly evidence: readonly IncidentEvidenceRef[];
  readonly explanation: string;
  readonly slot: string;
  readonly truthLabel: IncidentTruthLabel;
  readonly value: Record<string, string>;
}

export type IncidentTruthLabel =
  | "ambiguous"
  | "derived"
  | "drifted"
  | "exact"
  | "fallback"
  | "missing"
  | "permission_hidden"
  | "rejected"
  | "stale"
  | "unresolved";

export interface IncidentEvidenceCandidate {
  readonly id: string;
  readonly label: string;
  readonly reason: string;
  readonly url: string;
}

export interface IncidentEvidenceRef {
  readonly confidence: string;
  readonly factId: string;
  readonly kind: string;
  readonly observedAt: string;
  readonly recordId: string;
  readonly source: string;
  readonly url: string;
}

export interface IncidentMissingEvidence {
  readonly reason: string;
  readonly slot: string;
}

export interface IncidentRelatedChange {
  readonly changeId: string;
  readonly evidenceFactId: string;
  readonly explanation: string;
  readonly services: readonly IncidentReference[];
  readonly source: string;
  readonly sourceConfidence: string;
  readonly sourceUrl: string;
  readonly summary: string;
  readonly timestamp: string;
  readonly truthLabel: IncidentTruthLabel;
}

export interface IncidentTimelineEvent {
  readonly createdAt: string;
  readonly eventId: string;
  readonly eventType: string;
  readonly summary: string;
}

export interface IncidentAnswerMetadata {
  readonly coverage: {
    readonly limit: number;
    readonly queryShape: string;
  };
  readonly partialReasons: readonly string[];
  readonly recommendedNextCalls: readonly IncidentRecommendedNextCall[];
  readonly truncated: boolean;
}

export interface IncidentRecommendedNextCall {
  readonly args: Record<string, unknown>;
  readonly reason: string;
  readonly route: string;
  readonly tool: string;
}

export async function loadIncidentContext(
  client: EshuApiClient,
  rawInput: IncidentContextInput
): Promise<IncidentContextLoadResult> {
  const input = normalizeInput(rawInput);
  if (input.incidentId.length === 0) {
    return { error: "incident id is required", status: "unavailable" };
  }
  try {
    const envelope = await client.get<IncidentContextResponse>(incidentContextPath(input));
    if (envelope.error !== null) {
      throw new EshuEnvelopeError(envelope.error);
    }
    if (envelope.data === null) {
      throw new Error("Eshu envelope success response is missing data");
    }
    return {
      context: normalizeIncidentContext(envelope.data, input),
      status: "ready",
      truth: envelope.truth
    };
  } catch (error) {
    return {
      error: error instanceof Error ? error.message : "incident context request failed",
      status: "unavailable"
    };
  }
}

function normalizeInput(input: IncidentContextInput): Required<IncidentContextInput> {
  return {
    incidentId: input.incidentId.trim(),
    limit: clampInt(input.limit, 25, 1, 100),
    provider: nonEmpty(input.provider, "pagerduty"),
    scopeId: nonEmpty(input.scopeId),
    serviceId: nonEmpty(input.serviceId),
    since: nonEmpty(input.since),
    until: nonEmpty(input.until)
  };
}

function incidentContextPath(input: Required<IncidentContextInput>): string {
  const params = new URLSearchParams();
  params.set("provider", input.provider);
  if (input.scopeId.length > 0) params.set("scope_id", input.scopeId);
  if (input.serviceId.length > 0) params.set("service_id", input.serviceId);
  if (input.since.length > 0) params.set("since", input.since);
  if (input.until.length > 0) params.set("until", input.until);
  params.set("limit", String(input.limit));
  return `/api/v0/incidents/${encodeURIComponent(input.incidentId)}/context?${params}`;
}

function normalizeIncidentContext(
  response: IncidentContextResponse,
  input: Required<IncidentContextInput>
): IncidentContext {
  return {
    ambiguousEvidence: (response.ambiguous_evidence ?? []).map(normalizeEvidenceEdge),
    answerMetadata: normalizeAnswerMetadata(response.answer_metadata),
    evidencePath: (response.evidence_path ?? []).map(normalizeEvidenceEdge),
    incident: normalizeIncident(response.incident, input),
    missingEvidence: (response.missing_evidence ?? []).map(normalizeMissingEvidence),
    query: normalizeQuery(response.query, input),
    relatedChanges: (response.related_changes ?? []).map(normalizeRelatedChange),
    timeline: (response.timeline ?? []).map((event) => ({
      createdAt: nonEmpty(event.created_at),
      eventId: nonEmpty(event.event_id, "event"),
      eventType: nonEmpty(event.event_type),
      summary: nonEmpty(event.summary, "incident event")
    })),
    truncated: response.truncated ?? response.answer_metadata?.truncated ?? false
  };
}

function normalizeQuery(
  record: IncidentContextQueryRecord | undefined,
  input: Required<IncidentContextInput>
): IncidentContextQuery {
  return {
    incidentId: nonEmpty(record?.provider_incident_id, input.incidentId),
    limit: record?.limit ?? input.limit,
    provider: nonEmpty(record?.provider, input.provider),
    scopeId: nonEmpty(record?.scope_id, input.scopeId),
    serviceId: nonEmpty(record?.service_id, input.serviceId),
    since: nonEmpty(record?.since, input.since),
    until: nonEmpty(record?.until, input.until)
  };
}

function normalizeIncident(
  record: IncidentRecordWire | undefined,
  input: Required<IncidentContextInput>
): IncidentRecord {
  return {
    createdAt: nonEmpty(record?.created_at),
    evidenceFactId: nonEmpty(record?.evidence_fact_id),
    incidentNumber: record?.incident_number ?? 0,
    observedAt: nonEmpty(record?.observed_at),
    priority: normalizeReference(record?.priority),
    provider: nonEmpty(record?.provider, input.provider),
    providerIncidentId: nonEmpty(record?.provider_incident_id, input.incidentId),
    resolvedAt: nonEmpty(record?.resolved_at),
    scopeId: nonEmpty(record?.scope_id, input.scopeId),
    service: normalizeReference(record?.service),
    sourceConfidence: nonEmpty(record?.source_confidence),
    sourceUrl: nonEmpty(record?.source_url),
    status: nonEmpty(record?.status, "unknown"),
    teams: (record?.teams ?? []).map(normalizeReference),
    title: nonEmpty(record?.title, input.incidentId),
    updatedAt: nonEmpty(record?.updated_at),
    urgency: nonEmpty(record?.urgency)
  };
}

function normalizeReference(record: IncidentReferenceRecord | undefined): IncidentReference {
  return {
    id: nonEmpty(record?.id),
    summary: nonEmpty(record?.summary, record?.id),
    type: nonEmpty(record?.type),
    url: nonEmpty(record?.url)
  };
}

function normalizeEvidenceEdge(record: IncidentEvidenceEdgeRecord): IncidentEvidenceEdge {
  return {
    candidates: (record.candidates ?? []).map((candidate) => ({
      id: nonEmpty(candidate.id),
      label: nonEmpty(candidate.label, candidate.id, "candidate"),
      reason: nonEmpty(candidate.reason),
      url: nonEmpty(candidate.url)
    })),
    evidence: (record.evidence ?? []).map((evidence) => ({
      confidence: nonEmpty(evidence.confidence),
      factId: nonEmpty(evidence.fact_id),
      kind: nonEmpty(evidence.kind),
      observedAt: nonEmpty(evidence.observed_at),
      recordId: nonEmpty(evidence.record_id),
      source: nonEmpty(evidence.source),
      url: nonEmpty(evidence.url)
    })),
    explanation: nonEmpty(record.explanation, "No explanation returned."),
    slot: nonEmpty(record.slot, "evidence"),
    truthLabel: normalizeTruthLabel(record.truth_label),
    value: record.value ?? {}
  };
}

function normalizeMissingEvidence(record: IncidentMissingEvidenceRecord): IncidentMissingEvidence {
  return {
    reason: nonEmpty(record.reason, "missing evidence"),
    slot: nonEmpty(record.slot, "evidence")
  };
}

function normalizeRelatedChange(record: IncidentRelatedChangeRecord): IncidentRelatedChange {
  return {
    changeId: nonEmpty(record.change_id, "change"),
    evidenceFactId: nonEmpty(record.evidence_fact_id),
    explanation: nonEmpty(record.explanation),
    services: (record.services ?? []).map(normalizeReference),
    source: nonEmpty(record.source),
    sourceConfidence: nonEmpty(record.source_confidence),
    sourceUrl: nonEmpty(record.source_url),
    summary: nonEmpty(record.summary, record.change_id, "change candidate"),
    timestamp: nonEmpty(record.timestamp),
    truthLabel: normalizeTruthLabel(record.truth_label)
  };
}

function normalizeAnswerMetadata(record: IncidentAnswerMetadataRecord | undefined): IncidentAnswerMetadata {
  return {
    coverage: {
      limit: record?.coverage?.limit ?? 0,
      queryShape: nonEmpty(record?.coverage?.query_shape, "incident_context_evidence_path")
    },
    partialReasons: record?.partial_reasons ?? [],
    recommendedNextCalls: (record?.recommended_next_calls ?? []).map((call) => ({
      args: call.args ?? {},
      reason: nonEmpty(call.reason),
      route: nonEmpty(call.route),
      tool: nonEmpty(call.tool)
    })).filter((call) => call.tool.length > 0 || call.route.length > 0),
    truncated: record?.truncated ?? false
  };
}

function normalizeTruthLabel(value: string | undefined): IncidentTruthLabel {
  switch (value) {
    case "ambiguous":
    case "derived":
    case "drifted":
    case "exact":
    case "fallback":
    case "missing":
    case "permission_hidden":
    case "rejected":
    case "stale":
    case "unresolved":
      return value;
    default:
      return "unresolved";
  }
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
