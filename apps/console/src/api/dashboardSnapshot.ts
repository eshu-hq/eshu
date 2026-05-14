import type { EshuApiClient } from "./client";
import { loadDashboardMetrics } from "./liveData";
import { demoDashboardSnapshot } from "./mockData";
import type {
  DashboardRelationshipSummary,
  DashboardRepository,
  DashboardSnapshot,
  DeploymentGraph,
  DeploymentGraphLink,
  DeploymentGraphNode,
  EvidenceRow
} from "./mockData";
import type {
  ContextResponse,
  DeploymentEvidenceArtifact,
  StoryResponse
} from "./repository";
import type { ConsoleMode } from "../config/environment";

interface LoadDashboardSnapshotOptions {
  readonly client?: EshuApiClient;
  readonly mode: ConsoleMode;
}

interface RepositoryListResponse {
  readonly repositories?: readonly RepositoryRecord[];
}

interface RepositoryRecord extends DashboardRepository {
  readonly id?: string;
  readonly name?: string;
  readonly repo_slug?: string;
}

interface RelationshipContext {
  readonly artifacts: readonly DeploymentEvidenceArtifact[];
}

const canonicalRelationshipVerbs = [
  {
    detail: "Service deploys from source",
    layer: "canonical",
    verb: "DEPLOYS_FROM"
  },
  {
    detail: "Controller discovers configuration",
    layer: "canonical",
    verb: "DISCOVERS_CONFIG_IN"
  },
  {
    detail: "Dependency provisioned for consumer",
    layer: "canonical",
    verb: "PROVISIONS_DEPENDENCY_FOR"
  },
  {
    detail: "Runtime placement",
    layer: "canonical",
    verb: "RUNS_ON"
  },
  {
    detail: "Module use evidence",
    layer: "canonical",
    verb: "USES_MODULE"
  },
  {
    detail: "Config read permission or config source",
    layer: "canonical",
    verb: "READS_CONFIG_FROM"
  },
  {
    detail: "Generic dependency fallback",
    layer: "canonical",
    verb: "DEPENDS_ON"
  }
] satisfies readonly RelationshipContractVerb[];

const topologyRelationshipEdges = [
  {
    detail: "Repository defines workload",
    layer: "topology",
    verb: "DEFINES"
  },
  {
    detail: "Workload instance represents workload",
    layer: "topology",
    verb: "INSTANCE_OF"
  },
  {
    detail: "Infrastructure provisions platform",
    layer: "topology",
    verb: "PROVISIONS_PLATFORM"
  },
  {
    detail: "Deployment source context",
    layer: "topology",
    verb: "DEPLOYMENT_SOURCE"
  }
] satisfies readonly RelationshipContractVerb[];

interface RelationshipContractVerb {
  readonly detail: string;
  readonly layer: "canonical" | "topology";
  readonly verb: string;
}

export async function loadDashboardSnapshot({
  client,
  mode
}: LoadDashboardSnapshotOptions): Promise<DashboardSnapshot> {
  if (mode === "demo") {
    return demoDashboardSnapshot;
  }
  const apiClient = requiredClient(client);
  const [metrics, repositories] = await Promise.all([
    loadDashboardMetrics({ client: apiClient, mode }),
    loadRepositories(apiClient)
  ]);
  const contexts = await loadRelationshipContexts(apiClient, repositories);
  const artifacts = contexts.flatMap((context) => context.artifacts);
  return {
    evidence: evidenceRowsFromArtifacts(artifacts),
    graph: relationshipGraphFromArtifacts(artifacts),
    metrics,
    relationships: relationshipSummaries(artifacts),
    repositories,
    story: relationshipStory(artifacts, repositories.length)
  };
}

async function loadRepositories(client: EshuApiClient): Promise<readonly RepositoryRecord[]> {
  const payload = await client.getJson<RepositoryListResponse>("/api/v0/repositories");
  return payload.repositories ?? [];
}

async function loadRelationshipContexts(
  client: EshuApiClient,
  repositories: readonly RepositoryRecord[]
): Promise<readonly RelationshipContext[]> {
  const selected = selectRelationshipRepositories(repositories);
  return await Promise.all(
    selected.map(async (repository) => loadRepositoryRelationshipContext(client, repository))
  );
}

async function loadRepositoryRelationshipContext(
  client: EshuApiClient,
  repository: RepositoryRecord
): Promise<RelationshipContext> {
  const id = nonEmpty(repository.id, repository.name);
  if (id.length === 0) {
    return { artifacts: [] };
  }
  try {
    const story = await client.getJson<StoryResponse>(
      `/api/v0/repositories/${encodeURIComponent(id)}/story`
    );
    const storyContext = await loadStoryContext(client, story);
    const serviceContext = await loadServiceContext(client, story);
    return {
      artifacts: [
        ...(storyContext?.deployment_evidence?.artifacts ?? []),
        ...(serviceContext?.deployment_evidence?.artifacts ?? [])
      ]
    };
  } catch {
    return { artifacts: [] };
  }
}

async function loadStoryContext(
  client: EshuApiClient,
  story: StoryResponse
): Promise<ContextResponse | undefined> {
  const contextPath = story.drilldowns?.context_path;
  if (contextPath === undefined || contextPath.trim().length === 0) {
    return undefined;
  }
  try {
    return await client.getJson<ContextResponse>(contextPath);
  } catch {
    return undefined;
  }
}

async function loadServiceContext(
  client: EshuApiClient,
  story: StoryResponse
): Promise<ContextResponse | undefined> {
  const workload = story.deployment_overview?.workloads?.[0];
  if (workload === undefined || workload.trim().length === 0) {
    return undefined;
  }
  try {
    return await client.getJson<ContextResponse>(
      `/api/v0/services/${encodeURIComponent(workload)}/context`
    );
  } catch {
    return undefined;
  }
}

function selectRelationshipRepositories(
  repositories: readonly RepositoryRecord[]
): readonly RepositoryRecord[] {
  const scored = repositories.map((repository, index) => ({
    index,
    repository,
    score: relationshipScore(repository)
  }));
  return scored
    .sort((left, right) => right.score - left.score || left.index - right.index)
    .slice(0, 12)
    .map((entry) => entry.repository);
}

function relationshipScore(repository: RepositoryRecord): number {
  const name = nonEmpty(repository.name, repository.repo_slug, repository.id).toLowerCase();
  let score = 0;
  for (const token of ["argocd", "helm", "iac", "chart", "portal", "boats", "pcg"]) {
    if (name.includes(token)) {
      score += 4;
    }
  }
  return score;
}

function relationshipGraphFromArtifacts(
  artifacts: readonly DeploymentEvidenceArtifact[]
): DeploymentGraph {
  const nodes = new Map<string, DeploymentGraphNode>();
  const links: DeploymentGraphLink[] = [];
  for (const artifact of topRelationshipArtifacts(artifacts)) {
    const source = artifactSource(artifact);
    const target = artifactTarget(artifact);
    const verb = artifactVerb(artifact);
    if (source.length === 0 || target.length === 0) {
      continue;
    }
    const lane = `${verb}:${source}:${target}`;
    const sourceID = `repo:${source}`;
    const verbID = `verb:${verb}:${source}:${target}`;
    const targetID = `target:${target}`;
    addGraphNode(nodes, sourceID, "repository", source, "Evidence source repository", lane, 0);
    addGraphNode(nodes, verbID, "relationship", verb, artifactPath(artifact), lane, 1);
    addGraphNode(nodes, targetID, "service", target, artifactPath(artifact), lane, 2);
    links.push({ label: "observed in", source: sourceID, target: verbID });
    links.push({ label: verb, source: verbID, target: targetID });
  }
  return { links: dedupeLinks(links), nodes: Array.from(nodes.values()) };
}

function topRelationshipArtifacts(
  artifacts: readonly DeploymentEvidenceArtifact[]
): readonly DeploymentEvidenceArtifact[] {
  const seen = new Set<string>();
  return artifacts.filter((artifact) => {
    const source = artifactSource(artifact);
    const target = artifactTarget(artifact);
    const key = `${source}:${artifactVerb(artifact)}:${target}`;
    if (source.length === 0 || target.length === 0 || seen.has(key)) {
      return false;
    }
    seen.add(key);
    return true;
  }).slice(0, 5);
}

function relationshipSummaries(
  artifacts: readonly DeploymentEvidenceArtifact[]
): readonly DashboardRelationshipSummary[] {
  const groups = new Map<string, DeploymentEvidenceArtifact[]>();
  for (const artifact of artifacts) {
    const verb = artifactVerb(artifact);
    groups.set(verb, [...(groups.get(verb) ?? []), artifact]);
  }
  const known = canonicalRelationshipVerbs.map((relationship) => ({
    count: groups.get(relationship.verb)?.length ?? 0,
    detail: relationship.detail,
    layer: relationship.layer,
    verb: relationship.verb
  }));
  const topology = topologyRelationshipEdges.map((relationship) => ({
    count: groups.get(relationship.verb)?.length ?? 0,
    detail: relationship.detail,
    layer: relationship.layer,
    verb: relationship.verb
  }));
  const extra = Array.from(groups.entries())
    .filter(([verb]) => !knownRelationshipVerb(verb))
    .map(([verb, group]) => ({
      count: group.length,
      detail: relationshipDetail(verb),
      layer: "canonical" as const,
      verb
    }));
  return [...known, ...topology, ...extra];
}

function knownRelationshipVerb(verb: string): boolean {
  return [...canonicalRelationshipVerbs, ...topologyRelationshipEdges].some(
    (knownVerb) => knownVerb.verb === verb
  );
}

function evidenceRowsFromArtifacts(
  artifacts: readonly DeploymentEvidenceArtifact[]
): readonly EvidenceRow[] {
  return topArtifacts(artifacts).slice(0, 6).map((artifact) => ({
    basis: artifactVerb(artifact),
    category: artifact.artifact_family ?? "deployment",
    detailPath: artifactPath(artifact),
    source: artifactSource(artifact),
    summary: `${artifactSource(artifact)} ${artifactVerb(artifact)} ${artifactTarget(artifact)} via ${artifactPath(artifact)}.`,
    title: relationshipDetail(artifactVerb(artifact))
  }));
}

function relationshipStory(
  artifacts: readonly DeploymentEvidenceArtifact[],
  repositoryCount: number
): string {
  const summaries = relationshipSummaries(artifacts);
  const observed = summaries.filter((summary) => summary.count > 0);
  if (observed.length === 0) {
    return `${repositoryCount} repositories are indexed, but the sampled dashboard graph did not find typed deployment relationship evidence.`;
  }
  const top = observed
    .slice(0, 3)
    .map((summary) => `${summary.count} ${summary.verb}`)
    .join(", ");
  return `${repositoryCount} repositories are indexed. Eshu observed ${observed.length} of ${canonicalRelationshipVerbs.length} canonical relationship verb(s): ${top}. Missing verbs stay visible as not observed in this run.`;
}

function topArtifacts(
  artifacts: readonly DeploymentEvidenceArtifact[]
): readonly DeploymentEvidenceArtifact[] {
  const seen = new Set<string>();
  return artifacts.filter((artifact) => {
    const key = artifactKey(artifact);
    if (seen.has(key) || artifactSource(artifact).length === 0) {
      return false;
    }
    seen.add(key);
    return artifactTarget(artifact).length > 0;
  }).slice(0, 10);
}

function artifactKey(artifact: DeploymentEvidenceArtifact): string {
  return `${artifactSource(artifact)}:${artifactVerb(artifact)}:${artifactTarget(artifact)}:${artifactPath(artifact)}`;
}

function artifactSource(artifact: DeploymentEvidenceArtifact): string {
  return nonEmpty(artifact.source_repo_name, artifact.source_location?.repo_name);
}

function artifactTarget(artifact: DeploymentEvidenceArtifact): string {
  return nonEmpty(artifact.target_repo_name, artifact.name);
}

function artifactVerb(artifact: DeploymentEvidenceArtifact): string {
  return nonEmpty(artifact.relationship_type, artifact.evidence_kind, "deployment_evidence");
}

function artifactPath(artifact: DeploymentEvidenceArtifact): string {
  return nonEmpty(artifact.source_location?.path, artifact.path, artifact.name, "evidence");
}

function relationshipDetail(verb: string): string {
  switch (verb) {
    case "DISCOVERS_CONFIG_IN":
      return "Controller discovers configuration";
    case "DEPLOYS_FROM":
      return "Service deploys from source";
    case "RUNS_ON":
      return "Runtime placement";
    case "PROVISIONS_DEPENDENCY_FOR":
      return "Dependency provisioned for consumer";
    case "USES_MODULE":
      return "Module use evidence";
    case "READS_CONFIG_FROM":
      return "Config read permission or config source";
    case "DEPENDS_ON":
      return "Generic dependency fallback";
    default:
      return "Relationship evidence";
  }
}

function addGraphNode(
  nodes: Map<string, DeploymentGraphNode>,
  id: string,
  kind: DeploymentGraphNode["kind"],
  label: string,
  detail: string,
  lane: string,
  column: number
): void {
  if (!nodes.has(id)) {
    nodes.set(id, { column, detail, id, kind, label, lane });
  }
}

function dedupeLinks(links: readonly DeploymentGraphLink[]): readonly DeploymentGraphLink[] {
  const seen = new Set<string>();
  return links.filter((link) => {
    const key = `${link.source}:${link.target}:${link.label}`;
    if (seen.has(key)) {
      return false;
    }
    seen.add(key);
    return true;
  });
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
