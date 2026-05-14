import type { EshuApiClient } from "./client";

export interface CodeTopicInvestigation {
  readonly callGraphHandles: readonly CodeTopicNextCall[];
  readonly coverage: {
    readonly empty: boolean;
    readonly limit: number;
    readonly returnedCount: number;
    readonly searchedTerms: readonly string[];
    readonly truncated: boolean;
  };
  readonly evidenceGroups: readonly CodeTopicEvidenceGroup[];
  readonly matchedFiles: readonly CodeTopicFile[];
  readonly matchedSymbols: readonly CodeTopicSymbol[];
  readonly nextCalls: readonly CodeTopicNextCall[];
  readonly topic: string;
}

export interface CodeTopicEvidenceGroup {
  readonly entityName: string;
  readonly entityType: string;
  readonly language: string;
  readonly matchedTerms: readonly string[];
  readonly nextCalls: readonly CodeTopicNextCall[];
  readonly rank: number;
  readonly relativePath: string;
  readonly score: number;
  readonly sourceKind: string;
}

export interface CodeTopicFile {
  readonly language: string;
  readonly relativePath: string;
}

export interface CodeTopicSymbol {
  readonly entityName: string;
  readonly entityType: string;
  readonly language: string;
  readonly rank: number;
  readonly relativePath: string;
}

export interface CodeTopicNextCall {
  readonly args: Record<string, unknown>;
  readonly tool: string;
}

interface CodeTopicResponse {
  readonly call_graph_handles?: readonly NextCallRecord[];
  readonly coverage?: CoverageRecord;
  readonly evidence_groups?: readonly EvidenceGroupRecord[];
  readonly matched_files?: readonly FileRecord[];
  readonly matched_symbols?: readonly SymbolRecord[];
  readonly recommended_next_calls?: readonly NextCallRecord[];
  readonly topic?: string;
}

interface CoverageRecord {
  readonly empty?: boolean;
  readonly limit?: number;
  readonly returned_count?: number;
  readonly searched_terms?: readonly string[];
  readonly truncated?: boolean;
}

interface EvidenceGroupRecord {
  readonly entity_name?: string;
  readonly entity_type?: string;
  readonly language?: string;
  readonly matched_terms?: readonly string[];
  readonly rank?: number;
  readonly recommended_next_calls?: readonly NextCallRecord[];
  readonly relative_path?: string;
  readonly score?: number;
  readonly source_kind?: string;
}

interface FileRecord {
  readonly language?: string;
  readonly relative_path?: string;
}

interface SymbolRecord {
  readonly entity_name?: string;
  readonly entity_type?: string;
  readonly language?: string;
  readonly rank?: number;
  readonly relative_path?: string;
}

interface NextCallRecord {
  readonly args?: Record<string, unknown>;
  readonly tool?: string;
}

export async function loadCodeTopicInvestigation({
  client,
  repoName,
  serviceName
}: {
  readonly client: EshuApiClient;
  readonly repoName: string;
  readonly serviceName: string;
}): Promise<CodeTopicInvestigation> {
  const topic = `${serviceName} route handlers, outbound client calls, and deployment manifests`;
  const response = await client.postJson<CodeTopicResponse>(
    "/api/v0/code/topics/investigate",
    {
      intent: "runtime_surface",
      limit: 8,
      offset: 0,
      repo_id: repoName,
      topic
    }
  );
  return normalizeCodeTopicInvestigation(response, topic);
}

export function normalizeCodeTopicInvestigation(
  response: CodeTopicResponse,
  fallbackTopic: string
): CodeTopicInvestigation {
  const coverage = response.coverage ?? {};
  const evidenceGroups = presentableEvidenceGroups(response.evidence_groups ?? []);
  const matchedFiles = (response.matched_files ?? [])
    .filter((file) => !isNoisySourcePath(file.relative_path ?? ""))
    .map((file) => ({
      language: nonEmpty(file.language, "unknown"),
      relativePath: nonEmpty(file.relative_path, "source pending")
    }));
  return {
    callGraphHandles: nextCallRows(response.call_graph_handles ?? []),
    coverage: {
      empty: (coverage.empty ?? false) || evidenceGroups.length === 0,
      limit: coverage.limit ?? 0,
      returnedCount: evidenceGroups.length,
      searchedTerms: coverage.searched_terms ?? [],
      truncated: coverage.truncated ?? false
    },
    evidenceGroups,
    matchedFiles,
    matchedSymbols: (response.matched_symbols ?? []).map((symbol) => ({
      entityName: nonEmpty(symbol.entity_name, "symbol"),
      entityType: nonEmpty(symbol.entity_type, "entity"),
      language: nonEmpty(symbol.language, "unknown"),
      rank: symbol.rank ?? 0,
      relativePath: nonEmpty(symbol.relative_path, "source pending")
    })),
    nextCalls: presentableNextCalls(response.recommended_next_calls ?? [], evidenceGroups),
    topic: nonEmpty(response.topic, fallbackTopic)
  };
}

function presentableEvidenceGroups(
  groups: readonly EvidenceGroupRecord[]
): readonly CodeTopicEvidenceGroup[] {
  return groups
    .filter((group) => !isNoisySourcePath(group.relative_path ?? ""))
    .map((group) => ({
      entityName: nonEmpty(group.entity_name),
      entityType: nonEmpty(group.entity_type),
      language: nonEmpty(group.language, "unknown"),
      matchedTerms: group.matched_terms ?? [],
      nextCalls: nextCallRows(group.recommended_next_calls ?? []),
      rank: group.rank ?? 0,
      relativePath: nonEmpty(group.relative_path, "source pending"),
      score: group.score ?? 0,
      sourceKind: nonEmpty(group.source_kind, "content")
    }))
    .sort(compareEvidenceGroupsForConsole);
}

function compareEvidenceGroupsForConsole(
  left: CodeTopicEvidenceGroup,
  right: CodeTopicEvidenceGroup
): number {
  const priorityDelta = evidenceDisplayPriority(left) - evidenceDisplayPriority(right);
  if (priorityDelta !== 0) {
    return priorityDelta;
  }
  const rankDelta = left.rank - right.rank;
  if (rankDelta !== 0) {
    return rankDelta;
  }
  return right.score - left.score;
}

function evidenceDisplayPriority(group: CodeTopicEvidenceGroup): number {
  let priority = 50;
  const path = group.relativePath.toLowerCase();
  const kind = group.sourceKind.toLowerCase();
  if (kind === "entity" || kind === "symbol" || group.entityName.length > 0) {
    priority -= 30;
  }
  if (/(^|\/)(server|src|app|routes|handlers|controllers)\//.test(path)) {
    priority -= 20;
  }
  if (/(handler|route|controller|client|api)/.test(path)) {
    priority -= 10;
  }
  if (/(dockerfile|helm|kustomize|terraform|workflow|catalog-spec)/.test(path)) {
    priority -= 6;
  }
  if (/(^|\/)package\.json$/.test(path)) {
    priority += 25;
  }
  return priority;
}

function isNoisySourcePath(path: string): boolean {
  const normalized = path.toLowerCase();
  return /(^|\/)(package-lock\.json|pnpm-lock\.yaml|yarn\.lock|npm-shrinkwrap\.json)$/.test(
    normalized
  );
}

function presentableNextCalls(
  globalCalls: readonly NextCallRecord[],
  evidenceGroups: readonly CodeTopicEvidenceGroup[]
): readonly CodeTopicNextCall[] {
  const calls = [
    ...nextCallRows(globalCalls),
    ...evidenceGroups.flatMap((group) => group.nextCalls)
  ].filter((call) => !isNoisyNextCall(call));
  const seen = new Set<string>();
  return calls.filter((call) => {
    const key = `${call.tool}:${JSON.stringify(call.args)}`;
    if (seen.has(key)) {
      return false;
    }
    seen.add(key);
    return true;
  });
}

function isNoisyNextCall(call: CodeTopicNextCall): boolean {
  const path = call.args.relative_path;
  return typeof path === "string" && isNoisySourcePath(path);
}

function nextCallRows(calls: readonly NextCallRecord[]): readonly CodeTopicNextCall[] {
  return calls.map((call) => ({
    args: call.args ?? {},
    tool: nonEmpty(call.tool, "tool")
  }));
}

function nonEmpty(...values: readonly (string | undefined)[]): string {
  for (const value of values) {
    if (value !== undefined && value.trim().length > 0) {
      return value;
    }
  }
  return "";
}
