import type { EshuApiClient } from "./client";
import { EshuEnvelopeError } from "./envelope";

export interface CodeImportCycleEdge {
  readonly relationshipType: string;
  readonly sourceFile: string;
  readonly targetFile: string;
  readonly sourceModule: string;
  readonly targetModule: string;
  readonly lineNumber?: number;
}

export interface CodeImportCycleRow {
  readonly repoId: string;
  readonly repoName: string;
  readonly sourceFile: string;
  readonly targetFile: string;
  readonly sourceModule: string;
  readonly targetModule: string;
  readonly sourceLineNumber?: number;
  readonly backEdgeLineNumber?: number;
  readonly relationshipType: string;
  readonly cyclePath: readonly string[];
  readonly cycleEdges: readonly CodeImportCycleEdge[];
}

export interface CodeImportCyclesPage {
  readonly cycles: readonly CodeImportCycleRow[];
  readonly count: number;
  readonly truncated: boolean;
  readonly nextOffset: number | null;
}

interface CodeImportCyclesResponse {
  readonly cycles?: readonly CycleRecord[];
  readonly count?: number;
  readonly truncated?: boolean;
  readonly next_offset?: number | null;
}

interface CycleRecord {
  readonly repo_id?: string;
  readonly repo_name?: string;
  readonly source_file?: string;
  readonly target_file?: string;
  readonly source_module?: string;
  readonly target_module?: string;
  readonly source_line_number?: number;
  readonly back_edge_line_number?: number;
  readonly relationship_type?: string;
  readonly cycle_path?: readonly string[];
  readonly cycle_edges?: readonly CycleEdgeRecord[];
}

interface CycleEdgeRecord {
  readonly relationship_type?: string;
  readonly source_file?: string;
  readonly target_file?: string;
  readonly source_module?: string;
  readonly target_module?: string;
  readonly line_number?: number;
}

const pendingImportCycleLoads = new WeakMap<
  EshuApiClient,
  Map<string, Promise<CodeImportCyclesPage>>
>();

export function loadCodeImportCycles(
  client: EshuApiClient,
  repoId: string,
  limit = 6,
): Promise<CodeImportCyclesPage> {
  let requests = pendingImportCycleLoads.get(client);
  if (!requests) {
    requests = new Map();
    pendingImportCycleLoads.set(client, requests);
  }
  const key = JSON.stringify({ limit, repoId });
  const pending = requests.get(key);
  if (pending) return pending;

  const request = fetchCodeImportCycles(client, repoId, limit);
  requests.set(key, request);
  const removeSettledRequest = (): void => {
    if (requests.get(key) !== request) return;
    requests.delete(key);
    if (requests.size === 0) pendingImportCycleLoads.delete(client);
  };
  void request.then(removeSettledRequest, removeSettledRequest);
  return request;
}

async function fetchCodeImportCycles(
  client: EshuApiClient,
  repoId: string,
  limit: number,
): Promise<CodeImportCyclesPage> {
  const env = await client.post<CodeImportCyclesResponse>("/api/v0/code/imports/investigate", {
    query_type: "file_import_cycles",
    repo_id: repoId,
    limit,
  });
  if (env.error) throw new EshuEnvelopeError(env.error);
  const data = env.data ?? {};
  const cycles = (data.cycles ?? []).map(normalizeCycleRecord);
  return {
    cycles,
    count: data.count ?? cycles.length,
    truncated: data.truncated === true,
    nextOffset: typeof data.next_offset === "number" ? data.next_offset : null,
  };
}

function normalizeCycleRecord(record: CycleRecord): CodeImportCycleRow {
  const sourceFile = str(record.source_file);
  const targetFile = str(record.target_file);
  const sourceModule = str(record.source_module);
  const targetModule = str(record.target_module);
  const cyclePath = nonEmptyStrings(record.cycle_path);
  return {
    repoId: str(record.repo_id),
    repoName: str(record.repo_name),
    sourceFile,
    targetFile,
    sourceModule,
    targetModule,
    sourceLineNumber: num(record.source_line_number),
    backEdgeLineNumber: num(record.back_edge_line_number),
    relationshipType: str(record.relationship_type) || "IMPORTS",
    cyclePath: cyclePath.length
      ? cyclePath
      : [sourceFile, targetFile, sourceFile].filter((value) => value !== ""),
    cycleEdges: (record.cycle_edges ?? []).map(normalizeCycleEdge),
  };
}

function normalizeCycleEdge(record: CycleEdgeRecord): CodeImportCycleEdge {
  return {
    relationshipType: str(record.relationship_type) || "IMPORTS",
    sourceFile: str(record.source_file),
    targetFile: str(record.target_file),
    sourceModule: str(record.source_module),
    targetModule: str(record.target_module),
    lineNumber: num(record.line_number),
  };
}

function nonEmptyStrings(values: readonly string[] | undefined): readonly string[] {
  return (values ?? []).map((value) => value.trim()).filter((value) => value !== "");
}

function str(value: string | undefined): string {
  return value?.trim() ?? "";
}

function num(value: number | undefined): number | undefined {
  return typeof value === "number" && Number.isFinite(value) ? value : undefined;
}
