import type { EshuApiClient } from "./client";

export interface ChangeSurfaceInvestigation {
  readonly codeSurface: ChangeSurfaceCodeSurface;
  readonly coverage: ChangeSurfaceCoverage;
  readonly directImpact: readonly ChangeSurfaceImpactNode[];
  readonly empty: boolean;
  readonly impact: {
    readonly directCount: number;
    readonly totalCount: number;
    readonly transitiveCount: number;
  };
  readonly nextCalls: readonly ChangeSurfaceNextCall[];
  readonly resolution: ChangeSurfaceResolution;
  readonly scope: ChangeSurfaceScope;
  readonly sourceBackend: string;
  readonly transitiveImpact: readonly ChangeSurfaceImpactNode[];
  readonly truncated: boolean;
}

export interface ChangeSurfaceCodeSurface {
  readonly coverage: ChangeSurfaceCodeCoverage;
  readonly evidenceGroups: readonly ChangeSurfaceEvidenceGroup[];
  readonly files: readonly ChangeSurfaceFile[];
  readonly matchedFileCount: number;
  readonly sourceBackends: readonly string[];
  readonly symbolCount: number;
  readonly symbols: readonly ChangeSurfaceSymbol[];
  readonly topic: string;
  readonly truncated: boolean;
}

export interface ChangeSurfaceCodeCoverage {
  readonly changedPathCount: number;
  readonly limit: number;
  readonly queryShape: string;
  readonly returnedSymbols: number;
  readonly truncated: boolean;
}

export interface ChangeSurfaceCoverage {
  readonly codeSymbolCount: number;
  readonly directCount: number;
  readonly limit: number;
  readonly maxDepth: number;
  readonly queryShape: string;
  readonly transitiveCount: number;
  readonly truncated: boolean;
}

export interface ChangeSurfaceEvidenceGroup {
  readonly entityName: string;
  readonly entityType: string;
  readonly language: string;
  readonly matchedTerms: readonly string[];
  readonly relativePath: string;
  readonly sourceKind: string;
}

export interface ChangeSurfaceFile {
  readonly relativePath: string;
  readonly repoId: string;
}

export interface ChangeSurfaceImpactNode {
  readonly depth: number;
  readonly environment: string;
  readonly id: string;
  readonly labels: readonly string[];
  readonly name: string;
  readonly repoId: string;
}

export interface ChangeSurfaceNextCall {
  readonly args: Record<string, unknown>;
  readonly tool: string;
}

export interface ChangeSurfaceResolution {
  readonly candidates: readonly ChangeSurfaceImpactNode[];
  readonly input: string;
  readonly selected?: ChangeSurfaceImpactNode;
  readonly status: string;
  readonly targetType: string;
  readonly truncated: boolean;
}

export interface ChangeSurfaceScope {
  readonly changedPaths: readonly string[];
  readonly environment: string;
  readonly limit: number;
  readonly maxDepth: number;
  readonly repoId: string;
  readonly target: string;
  readonly targetType: string;
  readonly topic: string;
}

interface ChangeSurfaceResponse {
  readonly code_surface?: ChangeSurfaceCodeSurfaceRecord;
  readonly coverage?: ChangeSurfaceCoverageRecord;
  readonly direct_impact?: readonly ChangeSurfaceImpactRecord[];
  readonly impact_summary?: {
    readonly direct_count?: number;
    readonly total_count?: number;
    readonly transitive_count?: number;
  };
  readonly recommended_next_calls?: readonly ChangeSurfaceNextCallRecord[];
  readonly scope?: ChangeSurfaceScopeRecord;
  readonly source_backend?: string;
  readonly target_resolution?: ChangeSurfaceResolutionRecord;
  readonly transitive_impact?: readonly ChangeSurfaceImpactRecord[];
  readonly truncated?: boolean;
}

interface ChangeSurfaceCodeSurfaceRecord {
  readonly changed_files?: readonly ChangeSurfaceFileRecord[];
  readonly coverage?: ChangeSurfaceCodeCoverageRecord;
  readonly evidence_groups?: readonly ChangeSurfaceEvidenceRecord[];
  readonly matched_file_count?: number;
  readonly source_backends?: readonly string[];
  readonly symbol_count?: number;
  readonly topic?: string;
  readonly touched_symbols?: readonly ChangeSurfaceSymbolRecord[];
  readonly truncated?: boolean;
}

interface ChangeSurfaceCodeCoverageRecord {
  readonly changed_path_count?: number;
  readonly limit?: number;
  readonly query_shape?: string;
  readonly returned_symbols?: number;
  readonly truncated?: boolean;
}

interface ChangeSurfaceCoverageRecord {
  readonly code_symbol_count?: number;
  readonly direct_count?: number;
  readonly limit?: number;
  readonly max_depth?: number;
  readonly query_shape?: string;
  readonly transitive_count?: number;
  readonly truncated?: boolean;
}

interface ChangeSurfaceEvidenceRecord {
  readonly entity_name?: string;
  readonly entity_type?: string;
  readonly language?: string;
  readonly matched_terms?: readonly string[];
  readonly relative_path?: string;
  readonly source_kind?: string;
}

interface ChangeSurfaceFileRecord {
  readonly relative_path?: string;
  readonly repo_id?: string;
}

interface ChangeSurfaceImpactRecord {
  readonly depth?: number;
  readonly environment?: string;
  readonly id?: string;
  readonly labels?: readonly string[];
  readonly name?: string;
  readonly repo_id?: string;
}

interface ChangeSurfaceNextCallRecord {
  readonly args?: Record<string, unknown>;
  readonly tool?: string;
}

interface ChangeSurfaceResolutionRecord {
  readonly candidates?: readonly ChangeSurfaceImpactRecord[];
  readonly input?: string;
  readonly selected?: ChangeSurfaceImpactRecord;
  readonly status?: string;
  readonly target_type?: string;
  readonly truncated?: boolean;
}

interface ChangeSurfaceScopeRecord {
  readonly changed_paths?: readonly string[];
  readonly environment?: string;
  readonly limit?: number;
  readonly max_depth?: number;
  readonly repo_id?: string;
  readonly target?: string;
  readonly target_type?: string;
  readonly topic?: string;
}

export async function loadServiceChangeSurface({
  client,
  repoName,
  serviceName
}: {
  readonly client: EshuApiClient;
  readonly repoName: string;
  readonly serviceName: string;
}): Promise<ChangeSurfaceInvestigation> {
  const topic = `${serviceName} API routes, deployment, dependencies, consumers, and infrastructure changes`;
  const response = await client.postJson<ChangeSurfaceResponse>(
    "/api/v0/impact/change-surface/investigate",
    {
      limit: 16,
      max_depth: 4,
      offset: 0,
      repo_id: repoName,
      service_name: serviceName,
      topic
    }
  );
  return normalizeChangeSurfaceInvestigation(response);
}

export function normalizeChangeSurfaceInvestigation(
  response: ChangeSurfaceResponse
): ChangeSurfaceInvestigation {
  const codeSurface = normalizeCodeSurface(response.code_surface);
  const directImpact = (response.direct_impact ?? []).map(normalizeImpactNode);
  const transitiveImpact = (response.transitive_impact ?? []).map(normalizeImpactNode);
  const coverage = normalizeCoverage(response.coverage);
  const resolution = normalizeResolution(response.target_resolution);
  const impact = {
    directCount: response.impact_summary?.direct_count ?? directImpact.length,
    totalCount: response.impact_summary?.total_count ??
      directImpact.length + transitiveImpact.length,
    transitiveCount: response.impact_summary?.transitive_count ?? transitiveImpact.length
  };
  const hasResolutionGap = resolution.status === "no_match" || resolution.status === "ambiguous";
  const empty = !hasResolutionGap &&
    impact.totalCount === 0 &&
    codeSurface.files.length === 0 &&
    codeSurface.symbols.length === 0 &&
    codeSurface.evidenceGroups.length === 0;
  return {
    codeSurface,
    coverage,
    directImpact,
    empty,
    impact,
    nextCalls: (response.recommended_next_calls ?? []).map((call) => ({
      args: call.args ?? {},
      tool: nonEmpty(call.tool, "tool")
    })),
    resolution,
    scope: normalizeScope(response.scope),
    sourceBackend: nonEmpty(response.source_backend, "unknown"),
    transitiveImpact,
    truncated: response.truncated ?? coverage.truncated ?? codeSurface.truncated
  };
}

function normalizeCodeSurface(
  record: ChangeSurfaceCodeSurfaceRecord | undefined
): ChangeSurfaceCodeSurface {
  return {
    coverage: normalizeCodeCoverage(record?.coverage),
    evidenceGroups: (record?.evidence_groups ?? []).map((group) => ({
      entityName: nonEmpty(group.entity_name),
      entityType: nonEmpty(group.entity_type),
      language: nonEmpty(group.language, "unknown"),
      matchedTerms: group.matched_terms ?? [],
      relativePath: nonEmpty(group.relative_path, "source pending"),
      sourceKind: nonEmpty(group.source_kind, "content")
    })),
    files: (record?.changed_files ?? []).map((file) => ({
      relativePath: nonEmpty(file.relative_path, "source pending"),
      repoId: nonEmpty(file.repo_id)
    })),
    matchedFileCount: record?.matched_file_count ?? record?.changed_files?.length ?? 0,
    sourceBackends: record?.source_backends ?? [],
    symbolCount: record?.symbol_count ?? record?.touched_symbols?.length ?? 0,
    symbols: (record?.touched_symbols ?? []).map((symbol) => ({
      entityId: nonEmpty(symbol.entity_id),
      language: nonEmpty(symbol.language, "unknown"),
      name: nonEmpty(symbol.entity_name, "symbol"),
      relativePath: nonEmpty(symbol.relative_path, "source pending"),
      type: nonEmpty(symbol.entity_type, "entity")
    })),
    topic: nonEmpty(record?.topic),
    truncated: record?.truncated ?? false
  };
}

interface ChangeSurfaceSymbolRecord {
  readonly entity_id?: string;
  readonly entity_name?: string;
  readonly entity_type?: string;
  readonly language?: string;
  readonly relative_path?: string;
}

export interface ChangeSurfaceSymbol {
  readonly entityId: string;
  readonly language: string;
  readonly name: string;
  readonly relativePath: string;
  readonly type: string;
}

function normalizeCodeCoverage(
  record: ChangeSurfaceCodeCoverageRecord | undefined
): ChangeSurfaceCodeCoverage {
  return {
    changedPathCount: record?.changed_path_count ?? 0,
    limit: record?.limit ?? 0,
    queryShape: nonEmpty(record?.query_shape, "content_topic_and_changed_path_surface"),
    returnedSymbols: record?.returned_symbols ?? 0,
    truncated: record?.truncated ?? false
  };
}

function normalizeCoverage(
  record: ChangeSurfaceCoverageRecord | undefined
): ChangeSurfaceCoverage {
  return {
    codeSymbolCount: record?.code_symbol_count ?? 0,
    directCount: record?.direct_count ?? 0,
    limit: record?.limit ?? 0,
    maxDepth: record?.max_depth ?? 0,
    queryShape: nonEmpty(record?.query_shape, "unknown"),
    transitiveCount: record?.transitive_count ?? 0,
    truncated: record?.truncated ?? false
  };
}

function normalizeImpactNode(record: ChangeSurfaceImpactRecord): ChangeSurfaceImpactNode {
  return {
    depth: record.depth ?? 0,
    environment: nonEmpty(record.environment),
    id: nonEmpty(record.id),
    labels: record.labels ?? [],
    name: nonEmpty(record.name, record.id, "unknown"),
    repoId: nonEmpty(record.repo_id)
  };
}

function normalizeResolution(
  record: ChangeSurfaceResolutionRecord | undefined
): ChangeSurfaceResolution {
  const selected = record?.selected === undefined
    ? undefined
    : normalizeImpactNode(record.selected);
  return {
    candidates: (record?.candidates ?? []).map(normalizeImpactNode),
    input: nonEmpty(record?.input),
    selected,
    status: nonEmpty(record?.status, "not_requested"),
    targetType: nonEmpty(record?.target_type),
    truncated: record?.truncated ?? false
  };
}

function normalizeScope(record: ChangeSurfaceScopeRecord | undefined): ChangeSurfaceScope {
  return {
    changedPaths: record?.changed_paths ?? [],
    environment: nonEmpty(record?.environment),
    limit: record?.limit ?? 0,
    maxDepth: record?.max_depth ?? 0,
    repoId: nonEmpty(record?.repo_id),
    target: nonEmpty(record?.target),
    targetType: nonEmpty(record?.target_type),
    topic: nonEmpty(record?.topic)
  };
}

function nonEmpty(...values: readonly (string | undefined)[]): string {
  for (const value of values) {
    if (value !== undefined && value.trim().length > 0) {
      return value;
    }
  }
  return "";
}
