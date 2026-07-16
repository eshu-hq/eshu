import type { EshuApiClient } from "./client";
import { EshuEnvelopeError, type TruthLevel } from "./envelope";
import type { FindingRow } from "./eshuConsoleLive";

export interface DeadCodeQuery {
  readonly candidateKind?: string;
  readonly language?: string;
  readonly limit: number;
  readonly repoId?: string;
}

export interface DeadCodeResponse {
  readonly analysis?: Readonly<Record<string, unknown>>;
  readonly candidate_scan_truncated?: boolean;
  readonly display_truncated?: boolean;
  readonly limit?: number;
  readonly results?: readonly DeadCodeRecord[];
  readonly truncated?: boolean;
}

export interface DeadCodeRecord {
  readonly classification?: string;
  readonly end_line?: number;
  readonly entity_id?: string;
  readonly file_path?: string;
  readonly labels?: readonly string[];
  readonly language?: string;
  readonly name?: string;
  readonly repo_id?: string;
  readonly repo_name?: string;
  readonly start_line?: number;
}

export interface DeadCodeTruth {
  readonly freshness: string;
  readonly level: string;
  readonly profile: string;
}

export interface DeadCodePage {
  readonly analysis: Readonly<Record<string, unknown>>;
  readonly candidateScanTruncated: boolean;
  readonly displayTruncated: boolean;
  readonly limit: number;
  readonly rows: readonly FindingRow[];
  readonly truncated: boolean;
  readonly truth: DeadCodeTruth;
}

export async function loadDeadCodePage(
  client: EshuApiClient,
  query: DeadCodeQuery,
  repoNames?: ReadonlyMap<string, string>,
): Promise<DeadCodePage> {
  const env = await client.post<DeadCodeResponse>(
    "/api/v0/code/dead-code",
    deadCodePostBody(query),
  );
  if (env.error) throw new EshuEnvelopeError(env.error);
  const payload = env.data ?? {};
  const truthLevel = env.truth?.level ?? "derived";
  return {
    analysis: payload.analysis ?? {},
    candidateScanTruncated: payload.candidate_scan_truncated === true,
    displayTruncated: payload.display_truncated === true,
    limit: payload.limit ?? query.limit,
    rows: deadCodeRowsFromResponse(payload, truthLevel, repoNames),
    truncated: payload.truncated === true,
    truth: {
      freshness: env.truth?.freshness?.state ?? "unknown",
      level: truthLevel,
      profile: env.truth?.profile ?? "unknown",
    },
  };
}

export function deadCodeRowsFromResponse(
  response: DeadCodeResponse | null | undefined,
  truthLevel: TruthLevel,
  repoNames?: ReadonlyMap<string, string>,
): readonly FindingRow[] {
  return (response?.results ?? []).map((row, index) => {
    const filePath = nonEmpty(row.file_path, "unknown");
    const classification = row.classification?.trim();
    return {
      classification,
      detail: `${filePath}${classification ? ` · ${classification}` : ""}`,
      endLine: row.end_line,
      entity: deadCodeRepositoryLabel(row, repoNames),
      entityId: row.entity_id,
      filePath: row.file_path,
      id: row.entity_id ?? `dead-code-${index}`,
      labels: row.labels,
      language: row.language,
      repoId: row.repo_id,
      startLine: row.start_line,
      title: `Unreferenced symbol ${nonEmpty(row.name, "candidate")}`,
      truth: truthLevel,
      type: "Dead code",
    };
  });
}

function deadCodePostBody(query: DeadCodeQuery): Record<string, string | number> {
  const body: Record<string, string | number> = { limit: query.limit };
  const candidateKind = query.candidateKind?.trim();
  if (candidateKind) body.candidate_kind = candidateKind;
  const repoId = query.repoId?.trim();
  if (repoId) body.repo_id = repoId;
  const language = query.language?.trim();
  if (language) body.language = language;
  return body;
}

function deadCodeRepositoryLabel(
  row: DeadCodeRecord,
  repoNames?: ReadonlyMap<string, string>,
): string {
  const explicitName = row.repo_name?.trim();
  if (explicitName) return explicitName;
  const repoId = row.repo_id?.trim();
  if (repoId && repoNames?.has(repoId)) return repoNames.get(repoId) ?? repoId;
  return repositoryFallbackLabel(repoId);
}

function repositoryFallbackLabel(repoId: string | undefined): string {
  const id = repoId?.trim();
  if (!id) return "repository";
  const prefixed = id.match(/^repository[:_](.+)$/i);
  if (!prefixed) return id;
  const suffix = prefixed[1] ?? "";
  if (/^r_[0-9a-f]+$/i.test(suffix) || /^r[0-9a-f]+$/i.test(suffix)) return "unresolved repository";
  return suffix || "repository";
}

function nonEmpty(...values: readonly (string | undefined)[]): string {
  return values.find((value) => value !== undefined && value.trim().length > 0)?.trim() ?? "";
}
