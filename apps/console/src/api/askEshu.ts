import type { EshuApiClient } from "./client";
import type { EshuEnvelope, EshuError, EshuTruth } from "./envelope";
import type { GraphModel } from "../console/types";
import {
  citationRequest,
  normalizeAnswerCompanion,
  normalizeCitationPacket,
  type AnswerCompanion,
  type AnswerMetadataWire,
  type AnswerPacketWire,
  type EvidenceCitationPacket,
  type EvidenceCitationResponseWire
} from "./answerPacket";
import {
  emptyAnswerGraph,
  graphFromVisualizationPacket,
  normalizeVisualizationPacket,
  visualizationRequest,
  type VisualizationDeriveResponseWire,
  type VisualizationPacket
} from "./answerVisualization";

const semanticLimit = 5;
const semanticTimeoutMs = 5000;
const topicLimit = 5;

export type AskEshuStatus = "answered" | "needs_scope" | "partial" | "unanswered";

export type AskEshuSource = "citation" | "code_topic" | "question" | "scope" | "semantic" | "visualization";

/** Free-text console question scoped to one repository corpus. */
export interface AskEshuRequest {
  readonly question: string;
  readonly repoId: string;
}

/** Operator-visible source error from one bounded query route. */
export interface AskEshuSourceError {
  readonly message: string;
  readonly source: AskEshuSource;
}

/** Source-file handle that can route to the repository source viewer. */
export interface AskSourceHandle {
  readonly endLine?: number;
  readonly relativePath: string;
  readonly repoId: string;
  readonly startLine?: number;
}

/** Normalized semantic-search result for the Ask page. */
export interface AskSemanticResult {
  readonly contextText: string;
  readonly freshnessState: string;
  readonly path: string;
  readonly rank: number;
  readonly score: number;
  readonly searchMethod: string;
  readonly sourceKind: string;
  readonly title: string;
  readonly truthBasis: string;
  readonly truthLevel: string;
}

/** Semantic-search response with envelope truth preserved. */
export interface AskSemanticSection {
  readonly indexedDocumentCount: number;
  readonly results: readonly AskSemanticResult[];
  readonly route: "/api/v0/search/semantic";
  readonly truncated: boolean;
  readonly truth: EshuTruth | null;
}

/** Normalized code-topic evidence group. */
export interface AskCodeTopicEvidenceGroup {
  readonly entityName: string;
  readonly entityType: string;
  readonly language: string;
  readonly rank: number;
  readonly score: number;
  readonly sourceHandle: AskSourceHandle | null;
  readonly sourceKind: string;
}

/** Code-topic investigation response with answer-packet metadata preserved. */
export interface AskCodeTopicSection {
  readonly answerPacket: AnswerCompanion;
  readonly evidenceGroups: readonly AskCodeTopicEvidenceGroup[];
  readonly recommendedNextCalls: readonly unknown[];
  readonly route: "/api/v0/code/topics/investigate";
  readonly searchedTerms: readonly string[];
  readonly truncated: boolean;
  readonly truth: EshuTruth | null;
}

/** Complete Ask Eshu answer assembled from bounded read routes. */
export interface AskEshuAnswer {
  readonly answerGraph: GraphModel;
  readonly answerPacket: AnswerCompanion;
  readonly citationPacket: EvidenceCitationPacket | null;
  readonly codeTopic: AskCodeTopicSection;
  readonly errors: readonly AskEshuSourceError[];
  readonly question: string;
  readonly repoId: string;
  readonly semantic: AskSemanticSection;
  readonly status: AskEshuStatus;
  readonly visualizationPacket: VisualizationPacket | null;
}

interface SemanticSearchResponse {
  readonly indexed_document_count?: number;
  readonly results?: readonly SemanticSearchResult[];
  readonly truncated?: boolean;
}

interface SemanticSearchResult {
  readonly document?: SemanticSearchDocument;
  readonly freshness?: { readonly state?: string };
  readonly rank?: number;
  readonly score?: number;
  readonly search_method?: string;
  readonly truth_scope?: { readonly basis?: string; readonly level?: string };
}

interface SemanticSearchDocument {
  readonly context_text?: string;
  readonly path?: string;
  readonly source_kind?: string;
  readonly title?: string;
}

interface CodeTopicResponse {
  readonly answer_metadata?: AnswerMetadataWire;
  readonly answer_packet?: WireAnswerPacket;
  readonly evidence_groups?: readonly WireEvidenceGroup[];
  readonly recommended_next_calls?: readonly unknown[];
  readonly searched_terms?: readonly string[];
  readonly truncated?: boolean;
}

type WireAnswerPacket = AnswerPacketWire;

interface WireEvidenceGroup {
  readonly entity_name?: string;
  readonly entity_type?: string;
  readonly language?: string;
  readonly rank?: number;
  readonly score?: number;
  readonly source_handle?: WireSourceHandle;
  readonly source_kind?: string;
}

interface WireSourceHandle {
  readonly end_line?: number;
  readonly relative_path?: string;
  readonly repo_id?: string;
  readonly start_line?: number;
}

interface SourceResult<TData> {
  readonly data: TData | null;
  readonly error: AskEshuSourceError | null;
  readonly truth: EshuTruth | null;
}

/** Runs a scoped console question through bounded semantic and code-topic reads. */
export async function askEshuQuestion(
  client: EshuApiClient,
  request: AskEshuRequest
): Promise<AskEshuAnswer> {
  const question = request.question.trim();
  const repoId = request.repoId.trim();
  if (question.length === 0) {
    return emptyAnswer(question, repoId, "unanswered", {
      message: "question is required",
      source: "question"
    });
  }
  if (repoId.length === 0) {
    return emptyAnswer(question, repoId, "needs_scope", {
      message: "repository scope is required",
      source: "scope"
    });
  }

  const [semanticResult, codeTopicResult] = await Promise.all([
    runSource("semantic", () => client.post<SemanticSearchResponse>("/api/v0/search/semantic", {
      limit: semanticLimit,
      mode: "hybrid",
      query: question,
      repo_id: repoId,
      timeout_ms: semanticTimeoutMs
    })),
    runSource("code_topic", () => client.post<CodeTopicResponse>("/api/v0/code/topics/investigate", {
      limit: topicLimit,
      query: question,
      repo_id: repoId
    }))
  ]);

  const semantic = semanticSection(semanticResult);
  const codeTopic = codeTopicSection(codeTopicResult);
  const citationResult = codeTopic.answerPacket.evidenceHandles.length === 0
    ? emptySource<EvidenceCitationResponseWire>()
    : await runSource("citation", () => client.post<EvidenceCitationResponseWire>(
      "/api/v0/evidence/citations",
      citationRequest(question, codeTopic.answerPacket)
    ));
  const citationPacket = citationResult.data === null
    ? null
    : normalizeCitationPacket(citationResult.data, citationResult.truth);
  const visualizationResult = citationPacket === null
    ? emptySource<VisualizationDeriveResponseWire>()
    : await runSource("visualization", () => client.post<VisualizationDeriveResponseWire>(
      "/api/v0/visualizations/derive",
      visualizationRequest(citationPacket, citationResult.truth)
    ));
  const visualizationPacket = visualizationResult.data === null
    ? null
    : normalizeVisualizationPacket(visualizationResult.data, visualizationResult.truth);
  const errors = [
    semanticResult.error,
    codeTopicResult.error,
    citationResult.error,
    visualizationResult.error
  ].filter(
    (error): error is AskEshuSourceError => error !== null
  );
  const answerPacket = codeTopic.answerPacket;
  const hasEvidence = semantic.results.length > 0 || codeTopic.evidenceGroups.length > 0 ||
    answerPacket.supported || (citationPacket?.citations.length ?? 0) > 0;
  return {
    answerGraph: graphFromVisualizationPacket(visualizationPacket),
    answerPacket,
    citationPacket,
    codeTopic,
    errors,
    question,
    repoId,
    semantic,
    status: answerStatus(hasEvidence, errors),
    visualizationPacket
  };
}

function answerStatus(
  hasEvidence: boolean,
  errors: readonly AskEshuSourceError[]
): AskEshuStatus {
  if (!hasEvidence) return "unanswered";
  if (errors.length > 0) return "partial";
  return "answered";
}

async function runSource<TData>(
  source: "citation" | "code_topic" | "semantic" | "visualization",
  load: () => Promise<EshuEnvelope<TData>>
): Promise<SourceResult<TData>> {
  try {
    const env = await load();
    if (env.error !== null) {
      return { data: null, error: errorFromEnvelope(source, env.error), truth: null };
    }
    return { data: env.data, error: null, truth: env.truth };
  } catch (error) {
    return {
      data: null,
      error: { message: error instanceof Error ? error.message : "request failed", source },
      truth: null
    };
  }
}

function emptyAnswer(
  question: string,
  repoId: string,
  status: AskEshuStatus,
  error: AskEshuSourceError
): AskEshuAnswer {
  const semantic = semanticSection({ data: null, error: null, truth: null });
  const codeTopic = codeTopicSection({ data: null, error: null, truth: null });
  return {
    answerGraph: emptyAnswerGraph(),
    answerPacket: codeTopic.answerPacket,
    citationPacket: null,
    codeTopic,
    errors: [error],
    question,
    repoId,
    semantic,
    status,
    visualizationPacket: null
  };
}

function semanticSection(result: SourceResult<SemanticSearchResponse>): AskSemanticSection {
  return {
    indexedDocumentCount: result.data?.indexed_document_count ?? 0,
    results: (result.data?.results ?? []).map(semanticResult),
    route: "/api/v0/search/semantic",
    truncated: result.data?.truncated ?? false,
    truth: result.truth
  };
}

function semanticResult(result: SemanticSearchResult): AskSemanticResult {
  const document = result.document ?? {};
  const truthScope = result.truth_scope ?? {};
  return {
    contextText: clean(document.context_text),
    freshnessState: clean(result.freshness?.state),
    path: clean(document.path),
    rank: result.rank ?? 0,
    score: result.score ?? 0,
    searchMethod: clean(result.search_method),
    sourceKind: clean(document.source_kind),
    title: clean(document.title) || clean(document.path) || "Untitled result",
    truthBasis: clean(truthScope.basis),
    truthLevel: clean(truthScope.level)
  };
}

function codeTopicSection(result: SourceResult<CodeTopicResponse>): AskCodeTopicSection {
  const data = result.data;
  return {
    answerPacket: normalizeAnswerCompanion({
      answerMetadata: data?.answer_metadata,
      answerPacket: data?.answer_packet,
      routeTruth: result.truth
    }),
    evidenceGroups: (data?.evidence_groups ?? []).map(evidenceGroup),
    recommendedNextCalls: data?.recommended_next_calls ?? [],
    route: "/api/v0/code/topics/investigate",
    searchedTerms: data?.searched_terms ?? [],
    truncated: data?.truncated ?? false,
    truth: result.truth
  };
}

function evidenceGroup(group: WireEvidenceGroup): AskCodeTopicEvidenceGroup {
  return {
    entityName: clean(group.entity_name),
    entityType: clean(group.entity_type),
    language: clean(group.language),
    rank: group.rank ?? 0,
    score: group.score ?? 0,
    sourceHandle: sourceHandle(group.source_handle),
    sourceKind: clean(group.source_kind)
  };
}

function sourceHandle(handle: WireSourceHandle | undefined): AskSourceHandle | null {
  const repoId = clean(handle?.repo_id);
  const relativePath = clean(handle?.relative_path);
  if (repoId.length === 0 || relativePath.length === 0) return null;
  return {
    endLine: handle?.end_line,
    relativePath,
    repoId,
    startLine: handle?.start_line
  };
}

function errorFromEnvelope(source: AskEshuSource, error: EshuError): AskEshuSourceError {
  return {
    message: `${error.code}: ${error.message}`,
    source
  };
}

function emptySource<TData>(): SourceResult<TData> {
  return { data: null, error: null, truth: null };
}

function clean(value: string | undefined): string {
  return value?.trim() ?? "";
}
