// api/semanticSearch.ts
// Typed loader for the semantic-search surface: POST /api/v0/search/semantic.
// Bounded, repository-scoped retrieval over the persisted curated
// search-document index (keyword/semantic/hybrid modes), with a language
// facet computed over the post-filter result set. Mirrors the
// relationshipsCatalog.ts client/envelope pattern: client.post +
// EshuEnvelopeError, snake_case wire records normalized to camelCase view
// types. See go/internal/query/semantic_search.go for the wire contract
// (semanticSearchRequest / semanticSearchResponse).
import type { EshuApiClient } from "./client";
import { EshuEnvelopeError } from "./envelope";

export type SemanticSearchMode = "keyword" | "semantic" | "hybrid";

// defaultSemanticSearchLimit / defaultSemanticSearchTimeoutMs are the console's
// bounded defaults for a request that does not specify them. The backend
// requires both fields (1-100 for limit, >0 for timeout_ms); these keep every
// caller of searchSemantic within the contract without repeating literals.
export const defaultSemanticSearchLimit = 20;
export const defaultSemanticSearchTimeoutMs = 8000;
export const defaultSemanticSearchMode: SemanticSearchMode = "hybrid";

export interface SemanticSearchRequest {
  readonly repoId: string;
  readonly query: string;
  readonly mode?: SemanticSearchMode;
  readonly limit?: number;
  readonly timeoutMs?: number;
  readonly serviceId?: string;
  readonly workloadId?: string;
  readonly environment?: string;
  readonly sourceKinds?: readonly string[];
  // languages filters the bounded corpus to documents carrying one of these
  // language labels. Empty/absent means no filter. An unmatched language
  // returns an empty result set rather than an error (server contract).
  readonly languages?: readonly string[];
  readonly rerank?: boolean;
}

export interface SemanticSearchDocument {
  readonly id: string;
  readonly repoId: string;
  readonly sourceKind: string;
  readonly title: string;
  readonly path: string;
  readonly contextText: string;
  readonly labels: readonly string[];
  readonly updatedAt: string;
}

export interface SemanticSearchTruthScope {
  readonly level: string;
  readonly basis: string;
}

export interface SemanticSearchFreshness {
  readonly state: string;
}

export interface SemanticSearchResult {
  readonly rank: number;
  readonly score: number;
  readonly searchMethod: string;
  readonly document: SemanticSearchDocument;
  readonly truthScope: SemanticSearchTruthScope;
  readonly freshness: SemanticSearchFreshness;
  readonly failures: readonly string[];
}

// SemanticSearchFacets carries per-facet counts computed over the
// post-filter result set. languages is always present (never undefined) on a
// successful response, matching the server's "always present" contract.
export interface SemanticSearchFacets {
  readonly languages: Readonly<Record<string, number>>;
}

export interface SemanticSearchResponse {
  readonly query: string;
  readonly repoId: string;
  readonly mode: string;
  readonly searchMode: string;
  readonly limit: number;
  readonly timeoutMs: number;
  readonly results: readonly SemanticSearchResult[];
  readonly truncated: boolean;
  readonly indexedDocumentCount: number;
  readonly corpusLimit: number;
  readonly corpusMayBeTruncated: boolean;
  readonly retrievalState: string;
  readonly facets: SemanticSearchFacets;
}

// ---------------------------------------------------------------------------
// Wire records (snake_case, as returned by the API) — kept private to this
// module; callers only see the normalized camelCase view types above.
// ---------------------------------------------------------------------------

interface ResponseRecord {
  readonly query?: string;
  readonly repo_id?: string;
  readonly mode?: string;
  readonly search_mode?: string;
  readonly limit?: number;
  readonly timeout_ms?: number;
  readonly results?: readonly ResultRecord[];
  readonly truncated?: boolean;
  readonly indexed_document_count?: number;
  readonly corpus_limit?: number;
  readonly corpus_may_be_truncated?: boolean;
  readonly retrieval_state?: string;
  readonly facets?: { readonly languages?: Record<string, number> };
}

interface ResultRecord {
  readonly rank?: number;
  readonly score?: number;
  readonly search_method?: string;
  readonly document?: DocumentRecord;
  readonly truth_scope?: { readonly level?: string; readonly basis?: string };
  readonly freshness?: { readonly state?: string };
  readonly failures?: readonly string[];
}

interface DocumentRecord {
  readonly id?: string;
  readonly repo_id?: string;
  readonly source_kind?: string;
  readonly title?: string;
  readonly path?: string;
  readonly context_text?: string;
  readonly labels?: readonly string[];
  readonly updated_at?: string;
}

// searchSemantic calls POST /api/v0/search/semantic and returns the
// normalized response. Throws EshuEnvelopeError on a server-reported error.
export async function searchSemantic(
  client: EshuApiClient,
  req: SemanticSearchRequest,
): Promise<SemanticSearchResponse> {
  const body: Record<string, unknown> = {
    repo_id: req.repoId,
    query: req.query,
    mode: req.mode ?? defaultSemanticSearchMode,
    limit: req.limit ?? defaultSemanticSearchLimit,
    timeout_ms: req.timeoutMs ?? defaultSemanticSearchTimeoutMs,
  };
  if (req.serviceId) body.service_id = req.serviceId;
  if (req.workloadId) body.workload_id = req.workloadId;
  if (req.environment) body.environment = req.environment;
  if (req.sourceKinds && req.sourceKinds.length > 0) body.source_kinds = req.sourceKinds;
  if (req.languages && req.languages.length > 0) body.languages = req.languages;
  if (req.rerank) body.rerank = true;

  const env = await client.post<ResponseRecord>("/api/v0/search/semantic", body);
  if (env.error) throw new EshuEnvelopeError(env.error);
  return normalizeResponse(env.data ?? {});
}

function normalizeResponse(data: ResponseRecord): SemanticSearchResponse {
  return {
    query: str(data.query),
    repoId: str(data.repo_id),
    mode: str(data.mode),
    searchMode: str(data.search_mode),
    limit: num(data.limit) ?? 0,
    timeoutMs: num(data.timeout_ms) ?? 0,
    results: (data.results ?? []).map(normalizeResult),
    truncated: data.truncated === true,
    indexedDocumentCount: num(data.indexed_document_count) ?? 0,
    corpusLimit: num(data.corpus_limit) ?? 0,
    corpusMayBeTruncated: data.corpus_may_be_truncated === true,
    retrievalState: str(data.retrieval_state),
    facets: { languages: normalizeLanguageFacet(data.facets?.languages) },
  };
}

function normalizeResult(record: ResultRecord): SemanticSearchResult {
  return {
    rank: num(record.rank) ?? 0,
    score: num(record.score) ?? 0,
    searchMethod: str(record.search_method),
    document: normalizeDocument(record.document ?? {}),
    truthScope: {
      level: str(record.truth_scope?.level),
      basis: str(record.truth_scope?.basis),
    },
    freshness: { state: str(record.freshness?.state) },
    failures: record.failures ?? [],
  };
}

function normalizeDocument(record: DocumentRecord): SemanticSearchDocument {
  return {
    id: str(record.id),
    repoId: str(record.repo_id),
    sourceKind: str(record.source_kind),
    title: str(record.title),
    path: str(record.path),
    contextText: str(record.context_text),
    labels: record.labels ?? [],
    updatedAt: str(record.updated_at),
  };
}

function normalizeLanguageFacet(
  raw: Record<string, number> | undefined,
): Readonly<Record<string, number>> {
  if (!raw || typeof raw !== "object") return {};
  const result: Record<string, number> = {};
  for (const [key, value] of Object.entries(raw)) {
    const count = num(value);
    if (key && count !== undefined) result[key] = count;
  }
  return result;
}

function str(value: string | undefined): string {
  return value?.trim() ?? "";
}

function num(value: number | undefined): number | undefined {
  return typeof value === "number" && Number.isFinite(value) ? value : undefined;
}
