import type { EshuApiClient } from "./client";
import { EshuEnvelopeError } from "./envelope";

export interface FileSearchCursor {
  readonly repoId: string;
  readonly relativePath: string;
}

export interface FileSearchRequest {
  readonly query: string;
  readonly repoId?: string;
  readonly limit?: number;
  readonly offset?: number;
  readonly cursor?: FileSearchCursor;
}

export interface FileSearchResult {
  readonly repoId: string;
  readonly relativePath: string;
  readonly content: string;
  readonly language: string;
  readonly lineCount: number;
}

export interface FileSearchPartial {
  readonly reason: string;
  readonly rowsScannedInOrder: number;
  readonly rowsMatched: number;
  readonly cursor: FileSearchCursor;
  readonly progressed: boolean;
  readonly hint: string;
}

export interface FileSearchResponse {
  readonly results: readonly FileSearchResult[];
  readonly count: number;
  readonly limit: number;
  readonly offset: number;
  readonly truncated: boolean;
  readonly documentsNotSearched: number;
  readonly partial?: FileSearchPartial;
  readonly incomplete: boolean;
  readonly resume?: { readonly cursor: FileSearchCursor; readonly offset: number };
}

interface WireCursor {
  readonly repo_id: string;
  readonly relative_path: string;
}
interface WireResult {
  readonly repo_id: string;
  readonly relative_path: string;
  readonly content: string;
  readonly language?: string;
  readonly line_count: number;
}
interface WirePartial {
  readonly reason: string;
  readonly rows_scanned_in_order: number;
  readonly rows_matched: number;
  readonly cursor: WireCursor;
  readonly progressed: boolean;
  readonly hint: string;
}
interface WireResponse {
  readonly results: readonly WireResult[];
  readonly count: number;
  readonly limit: number;
  readonly offset: number;
  readonly truncated: boolean;
  readonly documents_not_searched: number;
  readonly partial?: WirePartial;
}

export async function searchFileContent(
  client: EshuApiClient,
  request: FileSearchRequest,
): Promise<FileSearchResponse> {
  if (request.cursor && request.repoId)
    throw new Error("A scoped search cannot resume a corpus cursor");
  const body: Record<string, unknown> = {
    query: request.query,
    limit: request.limit ?? 50,
    offset: request.offset ?? 0,
  };
  if (request.repoId) body.repo_id = request.repoId;
  if (request.cursor) {
    body.cursor = { repo_id: request.cursor.repoId, relative_path: request.cursor.relativePath };
  }
  const envelope = await client.post<WireResponse>("/api/v0/content/files/search", body);
  if (envelope.error) throw new EshuEnvelopeError(envelope.error);
  if (!envelope.data || !envelope.truth)
    throw new Error("File search response has no data or truth");
  const data = envelope.data;
  if (request.repoId && data.results.some((row) => row.repo_id !== request.repoId)) {
    throw new Error("File search returned a cross-repository result");
  }
  const partial = data.partial && {
    reason: data.partial.reason,
    rowsScannedInOrder: data.partial.rows_scanned_in_order,
    rowsMatched: data.partial.rows_matched,
    cursor: {
      repoId: data.partial.cursor.repo_id,
      relativePath: data.partial.cursor.relative_path,
    },
    progressed: data.partial.progressed,
    hint: data.partial.hint,
  };
  const incomplete =
    envelope.truth.level === "partial" ||
    data.documents_not_searched !== 0 ||
    partial !== undefined;
  const resume =
    partial?.progressed && partial.cursor.repoId && !request.repoId
      ? { cursor: partial.cursor, offset: Math.max(0, (request.offset ?? 0) - partial.rowsMatched) }
      : undefined;
  return {
    results: data.results.map((row) => ({
      repoId: row.repo_id,
      relativePath: row.relative_path,
      content: row.content,
      language: row.language ?? "",
      lineCount: row.line_count,
    })),
    count: data.count,
    limit: data.limit,
    offset: data.offset,
    truncated: data.truncated,
    documentsNotSearched: data.documents_not_searched,
    partial,
    incomplete,
    resume,
  };
}
