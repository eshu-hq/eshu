import type { EshuApiClient } from "./client";
import { loadDeadCodePage } from "./deadCode";
import { EshuEnvelopeError } from "./envelope";
import { codeRelationshipStoryToGraph } from "./eshuGraph";
import type { CodeRelationshipStoryCoverage, CodeRelationshipStoryResponse } from "./eshuGraph";
import { loadRepositoryNameMap } from "./repoCatalog";
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
}

export interface LoadedCodeGraph {
  readonly coverage?: CodeRelationshipStoryCoverage;
  readonly graph: GraphModel;
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
  const storyPromise = client.post<CodeRelationshipStoryResponse>(
    "/api/v0/code/relationships/story",
    {
      direction: "both",
      entity_id: target.entityId,
      limit: 50,
      relationship_types: [...CODE_RELATIONSHIP_TYPES],
    },
  );
  const storyEnvelope = await storyPromise;
  if (storyEnvelope.error) throw new EshuEnvelopeError(storyEnvelope.error);
  return codeRelationshipStoryToGraph(storyEnvelope.data ?? {}, {
    id: target.entityId || target.id,
    name: target.name,
  });
}

export async function loadCodeGraphCandidates(
  client: EshuApiClient,
): Promise<readonly FindingRow[]> {
  let repoNames: ReadonlyMap<string, string> = new Map();
  try {
    repoNames = await loadRepositoryNameMap(client);
  } catch {
    repoNames = new Map();
  }
  const page = await loadDeadCodePage(client, { limit: 100 }, repoNames);
  return page.rows;
}

function graphLoadKey(target: CodeGraphTarget): string {
  return JSON.stringify({
    direction: "both",
    entity_id: target.entityId,
    fallback_id: target.id,
    limit: 50,
    name: target.name,
    relationship_types: CODE_RELATIONSHIP_TYPES,
  });
}
