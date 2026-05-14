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

interface CatalogServiceLoadOptions extends LiveLoadOptions {
  readonly onRows?: (rows: readonly CatalogRow[]) => void;
}

interface RepositoryListResponse {
  readonly count?: number;
  readonly repositories?: readonly RepositoryRecord[];
}

interface CatalogResponse {
  readonly repositories?: readonly RepositoryRecord[];
  readonly services?: readonly CatalogWorkloadRecord[];
  readonly workloads?: readonly CatalogWorkloadRecord[];
}

interface CatalogWorkloadRecord {
  readonly environments?: readonly string[];
  readonly id?: string;
  readonly instance_count?: number;
  readonly kind?: string;
  readonly name?: string;
  readonly repo_id?: string;
  readonly repo_name?: string;
}

interface RepositoryRecord {
  readonly id?: string;
  readonly local_path?: string;
  readonly name?: string;
  readonly path?: string;
  readonly repo_slug?: string;
}

interface RepositoryStoryResponse {
  readonly deployment_overview?: {
    readonly workloads?: readonly string[];
  };
  readonly repository?: {
    readonly name?: string;
  };
}

interface IndexStatusResponse {
  readonly queue?: {
    readonly dead_letter?: number;
    readonly in_flight?: number;
    readonly outstanding?: number;
    readonly pending?: number;
    readonly succeeded?: number;
  };
  readonly reasons?: readonly string[];
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
  const repositories = await loadRepositories(requiredClient(client));
  const queue = status.queue ?? {};
  const outstanding = queue.outstanding ?? queue.pending ?? 0;
  const inFlight = queue.in_flight ?? 0;
  const pending = queue.pending ?? 0;
  const deadLetters = queue.dead_letter ?? 0;
  return [
    {
      detail: nonEmpty(status.reasons?.join("; "), "No runtime status reasons reported."),
      label: "Index status",
      value: nonEmpty(status.status, "unknown")
    },
    {
      detail: "Repository count reported by the graph status endpoint.",
      label: "Graph repositories",
      value: String(status.repository_count ?? 0)
    },
    {
      detail: "Repositories available through catalog drilldown.",
      label: "Catalog repositories",
      value: String(repositories.length)
    },
    {
      detail:
        outstanding === 0
          ? "No queued work is waiting on reducers or projectors."
          : `${outstanding} work item(s) still need reducer or projector attention.`,
      label: "Queue outstanding",
      value: String(outstanding)
    },
    {
      detail:
        inFlight === 0
          ? "No reducer or projector work is currently claimed."
          : `${inFlight} work item(s) are actively claimed by a worker.`,
      label: "In flight",
      value: String(inFlight)
    },
    {
      detail:
        pending === 0
          ? "No unclaimed work is waiting in the queue."
          : `${pending} work item(s) are waiting to be claimed.`,
      label: "Pending work",
      value: String(pending)
    },
    {
      detail:
        deadLetters === 0
          ? "No work items are dead-lettered."
          : `${deadLetters} dead-lettered work item(s).`,
      label: "Dead letters",
      value: String(deadLetters)
    },
    {
      detail: "Work items completed by the current local run.",
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
  const apiClient = requiredClient(client);
  const catalog = await loadCatalog(apiClient);
  if (catalog !== undefined) {
    return mapCatalogRows(catalog);
  }
  const repositories = await loadRepositories(apiClient);
  return repositories.map((repository) => ({
    coverage: nonEmpty(repository.local_path, repository.path, repository.repo_slug, "indexed"),
    freshness: "indexed",
    id: nonEmpty(repository.id, repository.name),
    kind: "repositories" as const,
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

export async function loadCatalogServiceRows({
  client,
  mode,
  onRows
}: CatalogServiceLoadOptions): Promise<readonly CatalogRow[]> {
  if (mode === "demo") {
    return demoCatalogRows.filter((row) => row.kind === "services" || row.kind === "workloads");
  }
  const apiClient = requiredClient(client);
  const catalog = await loadCatalog(apiClient);
  if (catalog !== undefined) {
    const rows = mapCatalogWorkloadRows(catalog);
    onRows?.(rows);
    return rows;
  }
  const repositories = await loadRepositories(apiClient);
  return loadServiceCatalogRows(apiClient, repositories, onRows);
}

async function loadCatalog(client: EshuApiClient): Promise<CatalogResponse | undefined> {
  try {
    const catalog = await client.getJson<CatalogResponse>("/api/v0/catalog");
    return hasCatalogCollections(catalog) ? catalog : undefined;
  } catch {
    return undefined;
  }
}

function hasCatalogCollections(catalog: CatalogResponse): boolean {
  return (
    catalog.repositories !== undefined ||
    catalog.services !== undefined ||
    catalog.workloads !== undefined
  );
}

function mapCatalogRows(catalog: CatalogResponse): readonly CatalogRow[] {
  return uniqueCatalogRows([
    ...mapCatalogRepositoryRows(catalog.repositories ?? []),
    ...mapCatalogWorkloadRows(catalog)
  ]);
}

function mapCatalogRepositoryRows(
  repositories: readonly RepositoryRecord[]
): readonly CatalogRow[] {
  return repositories.map((repository) => ({
    coverage: nonEmpty(repository.local_path, repository.path, repository.repo_slug, "indexed"),
    freshness: "indexed",
    id: nonEmpty(repository.id, repository.name),
    kind: "repositories" as const,
    name: nonEmpty(repository.name, repository.id)
  }));
}

function mapCatalogWorkloadRows(catalog: CatalogResponse): readonly CatalogRow[] {
  const serviceIDs = new Set(
    (catalog.services ?? []).map((service) => nonEmpty(service.id, service.name))
  );
  const serviceRows = (catalog.services ?? []).map((service) =>
    catalogRowFromWorkload(service, "services")
  );
  const workloadRows = (catalog.workloads ?? [])
    .filter((workload) => !serviceIDs.has(nonEmpty(workload.id, workload.name)))
    .map((workload) => catalogRowFromWorkload(workload, "workloads"));
  return uniqueCatalogRows([...serviceRows, ...workloadRows]);
}

function catalogRowFromWorkload(
  workload: CatalogWorkloadRecord,
  kind: "services" | "workloads"
): CatalogRow {
  const environmentLabel =
    workload.environments === undefined || workload.environments.length === 0
      ? ""
      : ` across ${workload.environments.join(", ")}`;
  return {
    coverage: nonEmpty(workload.repo_name, workload.repo_id, "graph workload") + environmentLabel,
    freshness: "graph",
    id: nonEmpty(workload.id, workload.name),
    kind,
    name: nonEmpty(workload.name, workload.id)
  };
}

async function loadServiceCatalogRows(
  client: EshuApiClient,
  repositories: readonly RepositoryRecord[],
  onRows: ((rows: readonly CatalogRow[]) => void) | undefined
): Promise<readonly CatalogRow[]> {
  const rows = await mapLimited(prioritizeWorkloadRepositories(repositories), 6, async (repository) => {
    const repositoryID = nonEmpty(repository.id, repository.name);
    if (repositoryID.length === 0) {
      return [];
    }
    try {
      const story = await client.getJson<RepositoryStoryResponse>(
        `/api/v0/repositories/${encodeURIComponent(repositoryID)}/story`
      );
      const serviceRows = (story.deployment_overview?.workloads ?? []).map((workload) => ({
        coverage: `defined by ${nonEmpty(repository.name, story.repository?.name, repositoryID)}`,
        freshness: "story",
        id: workload,
        kind: "services" as const,
        name: workload
      }));
      if (serviceRows.length > 0) {
        onRows?.(serviceRows);
      }
      return serviceRows;
    } catch {
      return [];
    }
  });
  return uniqueCatalogRows(rows.flat());
}

function prioritizeWorkloadRepositories(
  repositories: readonly RepositoryRecord[]
): readonly RepositoryRecord[] {
  return repositories
    .map((repository, index) => ({
      index,
      repository,
      score: workloadRepositoryScore(repository)
    }))
    .sort((left, right) => right.score - left.score || left.index - right.index)
    .map((entry) => entry.repository);
}

function workloadRepositoryScore(repository: RepositoryRecord): number {
  const name = nonEmpty(repository.name, repository.repo_slug, repository.id).toLowerCase();
  const tokens = name.split(/[^a-z0-9]+/).filter((token) => token.length > 0);
  let score = 0;
  for (const token of ["api", "service", "app", "web", "mcp", "portal", "node"]) {
    if (tokens.includes(token)) {
      score += 6;
    }
  }
  for (const token of ["terraform", "helm", "argocd", "iac", "chart"]) {
    if (tokens.includes(token)) {
      score -= 5;
    }
  }
  return score;
}

async function mapLimited<TItem, TResult>(
  items: readonly TItem[],
  limit: number,
  mapper: (item: TItem) => Promise<TResult>
): Promise<readonly TResult[]> {
  const results: TResult[] = [];
  let nextIndex = 0;
  const workerCount = Math.min(limit, items.length);
  await Promise.all(Array.from({ length: workerCount }, async () => {
    while (nextIndex < items.length) {
      const index = nextIndex;
      nextIndex += 1;
      results[index] = await mapper(items[index] as TItem);
    }
  }));
  return results;
}

function uniqueCatalogRows(rows: readonly CatalogRow[]): readonly CatalogRow[] {
  const seen = new Set<string>();
  const unique: CatalogRow[] = [];
  for (const row of rows) {
    const key = `${row.kind}:${row.id}`;
    if (seen.has(key)) {
      continue;
    }
    seen.add(key);
    unique.push(row);
  }
  return unique;
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
