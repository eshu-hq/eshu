import type { EshuApiClient } from "./client";
import { EshuEnvelopeError } from "./envelope";
import { codeRelationshipStoryToGraph } from "./eshuGraph";
import type { CodeRelationshipStoryCoverage, CodeRelationshipStoryResponse } from "./eshuGraph";
import type { FindingRow, GraphModel } from "../console/types";

const CODE_RELATIONSHIP_TYPES = [
  "CALLS",
  "IMPORTS",
  "REFERENCES",
  "INHERITS",
  "OVERRIDES",
  "TAINT_FLOWS_TO",
] as const;

const pendingGraphLoads = new WeakMap<EshuApiClient, Map<string, Promise<LoadedCodeGraph>>>();

export interface CodeGraphTarget {
  readonly entityId: string;
  readonly id: string;
  readonly name: string;
  readonly repoId?: string;
}

export interface LoadedCodeGraph {
  readonly coverage?: CodeRelationshipStoryCoverage;
  readonly graph: GraphModel;
}

export interface CodeGraphRelationshipStory extends CodeRelationshipStoryResponse {
  readonly scope?: { readonly repo_id?: string };
  readonly target_resolution?: {
    readonly status?: string;
    readonly entity_id?: string;
    readonly name?: string;
    readonly repo_id?: string;
  };
}

interface CodeStructureInventoryRecord {
  readonly end_line?: number;
  readonly entity_id?: string;
  readonly entity_name?: string;
  readonly entity_type?: string;
  readonly file_path?: string;
  readonly language?: string;
  readonly name?: string;
  readonly repo_id?: string;
  readonly start_line?: number;
}

interface CodeStructureInventoryResponse {
  readonly next_offset?: number | null;
  readonly results?: readonly CodeStructureInventoryRecord[];
  readonly truncated?: boolean;
}

export interface CodeGraphInventory {
  readonly nextOffset: number | null;
  readonly symbols: readonly FindingRow[];
  readonly truncated: boolean;
}

export function loadCodeGraph(
  client: EshuApiClient,
  target: CodeGraphTarget,
): Promise<LoadedCodeGraph> {
  let requests = pendingGraphLoads.get(client);
  if (!requests) {
    requests = new Map();
    pendingGraphLoads.set(client, requests);
  }
  const key = graphLoadKey(target);
  const pending = requests.get(key);
  if (pending) return pending;

  const request = fetchCodeGraph(client, target);
  requests.set(key, request);
  const removeSettledRequest = (): void => {
    if (requests.get(key) !== request) return;
    requests.delete(key);
    if (requests.size === 0) pendingGraphLoads.delete(client);
  };
  void request.then(removeSettledRequest, removeSettledRequest);
  return request;
}

async function fetchCodeGraph(
  client: EshuApiClient,
  target: CodeGraphTarget,
): Promise<LoadedCodeGraph> {
  const body = {
    direction: "both",
    entity_id: target.entityId,
    limit: 50,
    relationship_types: [...CODE_RELATIONSHIP_TYPES],
    ...(target.repoId ? { repo_id: target.repoId } : {}),
  };
  const storyPromise = client.post<CodeGraphRelationshipStory>(
    "/api/v0/code/relationships/story",
    body,
  );
  const storyEnvelope = await storyPromise;
  if (storyEnvelope.error) throw new EshuEnvelopeError(storyEnvelope.error);
  const story = validatedRelationshipStory(storyEnvelope.data ?? {}, target);
  return codeRelationshipStoryToGraph(story, {
    id: target.entityId || target.id,
    name: target.name,
  });
}

function validatedRelationshipStory(
  story: CodeGraphRelationshipStory,
  target: CodeGraphTarget,
): CodeRelationshipStoryResponse {
  const resolution = story.target_resolution;
  const status = resolution?.status?.trim().toLowerCase();
  if (status && status !== "resolved") {
    throw new Error(`code relationship target ${status} in the selected repository`);
  }
  const resolutionRepoId = clean(resolution?.repo_id);
  const scopeRepoId = clean(story.scope?.repo_id);
  if (
    target.repoId &&
    [resolutionRepoId, scopeRepoId].some((repoId) => repoId !== "" && repoId !== target.repoId)
  ) {
    throw new Error("code relationship target resolved outside the selected repository");
  }
  if (target.repoId && (resolutionRepoId === "" || scopeRepoId === "")) {
    throw new Error("code relationship story did not prove selected repository ownership");
  }
  if (!resolution) return story;
  return {
    ...story,
    entity_id: resolution.entity_id?.trim() || story.entity_id,
    name: resolution.name?.trim() || story.name,
  };
}

export async function loadCodeGraphInventory(
  client: EshuApiClient,
  repoId: string,
  repoName: string,
): Promise<CodeGraphInventory> {
  const envelope = await client.post<CodeStructureInventoryResponse>(
    "/api/v0/code/structure/inventory",
    { inventory_kind: "entity", limit: 100, repo_id: repoId },
  );
  if (envelope.error) throw new EshuEnvelopeError(envelope.error);
  const truthLevel = envelope.truth?.level ?? "derived";
  const seenEntityIds = new Set<string>();
  const symbols = (envelope.data?.results ?? []).flatMap((record) => {
    const rowRepoId = clean(record.repo_id);
    if (rowRepoId && rowRepoId !== repoId) {
      throw new Error("structural inventory returned a cross-repository entity");
    }
    const entityId = clean(record.entity_id);
    const name = clean(record.entity_name) || clean(record.name) || entityId;
    if (!entityId || !name) return [];
    if (seenEntityIds.has(entityId)) {
      throw new Error("structural inventory returned a duplicate entity identity");
    }
    seenEntityIds.add(entityId);
    return [
      {
        classification: clean(record.entity_type) || "symbol",
        detail: clean(record.file_path) || "source path unavailable",
        endLine: finiteNumber(record.end_line),
        entity: repoName || repoId,
        entityId,
        filePath: clean(record.file_path) || undefined,
        id: entityId,
        language: clean(record.language) || undefined,
        repoId: rowRepoId || repoId,
        startLine: finiteNumber(record.start_line),
        title: name,
        truth: truthLevel,
        type: "Code symbol",
      },
    ];
  });
  return {
    nextOffset: typeof envelope.data?.next_offset === "number" ? envelope.data.next_offset : null,
    symbols,
    truncated: envelope.data?.truncated === true,
  };
}

function clean(value: string | undefined): string {
  return value?.trim() ?? "";
}

function finiteNumber(value: number | undefined): number | undefined {
  return typeof value === "number" && Number.isFinite(value) ? value : undefined;
}

function graphLoadKey(target: CodeGraphTarget): string {
  return JSON.stringify({
    direction: "both",
    entity_id: target.entityId,
    fallback_id: target.id,
    limit: 50,
    name: target.name,
    repo_id: target.repoId,
    relationship_types: CODE_RELATIONSHIP_TYPES,
  });
}
