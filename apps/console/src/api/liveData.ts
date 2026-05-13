import type { EshuApiClient } from "./client";
import { unwrapEnvelope } from "./envelope";
import {
  demoCatalogRows,
  demoDashboardMetrics,
  demoFindingRows,
  demoSearchCandidates
} from "./mockData";
import type {
  CatalogRow,
  DashboardMetric,
  FindingRow,
  SearchCandidate
} from "./mockData";
import type { ConsoleMode } from "../config/environment";

interface LiveLoadOptions {
  readonly client?: EshuApiClient;
  readonly mode: ConsoleMode;
}

interface RepositoryListResponse {
  readonly count?: number;
  readonly repositories?: readonly RepositoryRecord[];
}

interface RepositoryRecord {
  readonly id?: string;
  readonly local_path?: string;
  readonly name?: string;
  readonly path?: string;
  readonly repo_slug?: string;
}

interface IndexStatusResponse {
  readonly queue?: {
    readonly outstanding?: number;
    readonly pending?: number;
    readonly succeeded?: number;
  };
  readonly repository_count?: number;
  readonly status?: string;
}

interface DeadCodeResponse {
  readonly results?: readonly DeadCodeResult[];
}

interface DeadCodeResult {
  readonly classification?: string;
  readonly file_path?: string;
  readonly name?: string;
  readonly repo_id?: string;
  readonly repo_name?: string;
}

export async function loadSearchCandidates({
  client,
  mode
}: LiveLoadOptions): Promise<readonly SearchCandidate[]> {
  if (mode === "demo") {
    return demoSearchCandidates;
  }
  const repositories = await loadRepositories(requiredClient(client));
  return repositories.map((repository) => ({
    description: repositoryDescription(repository),
    id: nonEmpty(repository.id, repository.name),
    kind: "repositories",
    label: nonEmpty(repository.name, repository.id)
  }));
}

export async function loadDashboardMetrics({
  client,
  mode
}: LiveLoadOptions): Promise<readonly DashboardMetric[]> {
  if (mode === "demo") {
    return demoDashboardMetrics;
  }
  const status = await requiredClient(client).getJson<IndexStatusResponse>(
    "/api/v0/index-status"
  );
  const queue = status.queue ?? {};
  return [
    { label: "Index status", value: nonEmpty(status.status, "unknown") },
    {
      label: "Repositories",
      value: String(status.repository_count ?? 0)
    },
    {
      label: "Queue outstanding",
      value: String(queue.outstanding ?? queue.pending ?? 0)
    },
    {
      label: "Succeeded work",
      value: String(queue.succeeded ?? 0)
    }
  ];
}

export async function loadCatalogRows({
  client,
  mode
}: LiveLoadOptions): Promise<readonly CatalogRow[]> {
  if (mode === "demo") {
    return demoCatalogRows;
  }
  const repositories = await loadRepositories(requiredClient(client));
  return repositories.map((repository) => ({
    coverage: nonEmpty(repository.local_path, repository.path, repository.repo_slug, "indexed"),
    freshness: "indexed",
    id: nonEmpty(repository.id, repository.name),
    kind: "repositories",
    name: nonEmpty(repository.name, repository.id)
  }));
}

export async function loadFindingRows({
  client,
  mode
}: LiveLoadOptions): Promise<readonly FindingRow[]> {
  if (mode === "demo") {
    return demoFindingRows;
  }
  const apiClient = requiredClient(client);
  const [envelope, repositories] = await Promise.all([
    apiClient.post<DeadCodeResponse>("/api/v0/code/dead-code", { limit: 25 }),
    loadRepositories(apiClient)
  ]);
  const { data: payload, truth } = unwrapEnvelope(envelope);
  const repositoryNames = new Map(
    repositories
      .filter((repository) => repository.id !== undefined && repository.name !== undefined)
      .map((repository) => [repository.id as string, repository.name as string])
  );
  return (payload.results ?? []).map((result) => ({
    entity: nonEmpty(
      result.repo_name,
      result.repo_id === undefined ? undefined : repositoryNames.get(result.repo_id),
      result.repo_id,
      "repository"
    ),
    findingType: "Dead code",
    location: nonEmpty(result.file_path, "unknown"),
    name: nonEmpty(result.name, "unnamed candidate"),
    truthLevel: truth.level
  }));
}

async function loadRepositories(client: EshuApiClient): Promise<readonly RepositoryRecord[]> {
  const payload = await client.getJson<RepositoryListResponse>("/api/v0/repositories");
  return payload.repositories ?? [];
}

function repositoryDescription(repository: RepositoryRecord): string {
  const location = nonEmpty(repository.local_path, repository.path, repository.repo_slug, "");
  if (location.length === 0) {
    return "Indexed repository from the live Eshu API";
  }
  return location;
}

function requiredClient(client: EshuApiClient | undefined): EshuApiClient {
  if (client === undefined) {
    throw new Error("Eshu API client is required outside demo mode");
  }
  return client;
}

function nonEmpty(...values: readonly (string | undefined)[]): string {
  for (const value of values) {
    if (value !== undefined && value.trim().length > 0) {
      return value;
    }
  }
  return "";
}
