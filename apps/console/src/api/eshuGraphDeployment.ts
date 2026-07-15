import type { EshuApiClient } from "./client";
import { EshuApiHttpError } from "./client";
import { EshuEnvelopeError } from "./envelope";
import { loadEntityMapGraph } from "./eshuGraphNeighborhood";
import { cleanText, layerFor } from "./eshuGraphShared";
import type { GraphEdge, GraphModel, GraphNode } from "../console/types";

interface DeploymentArtifactRecord {
  readonly source_repo_id?: string;
  readonly source_repo_name?: string;
  readonly target_repo_id?: string;
  readonly target_repo_name?: string;
  readonly relationship_type?: string;
  readonly artifact_family?: string;
  readonly evidence_kind?: string;
  readonly environment?: string;
  readonly path?: string;
}

export interface ServiceDeploymentContextResponse {
  readonly name?: string;
  readonly repo_name?: string;
  readonly deployment_evidence?: {
    readonly artifacts?: readonly DeploymentArtifactRecord[];
  };
}

interface RepositoryDeploymentContextResponse {
  readonly repository?: {
    readonly id?: string;
    readonly name?: string;
  };
  readonly deployment_evidence?: ServiceDeploymentContextResponse["deployment_evidence"];
}

interface RepoRef {
  readonly id: string;
  readonly name: string;
}

// Builds controller repo -> chart repo -> source repo -> workload from evidence.
export function deploymentStoryToGraph(
  data: ServiceDeploymentContextResponse,
  fallbackName: string,
): GraphModel {
  const serviceName = cleanText(data.name) || fallbackName;
  const sourceRepoName = cleanText(data.repo_name) || serviceName;
  const deployArtifacts = (data.deployment_evidence?.artifacts ?? []).filter(
    (artifact) => cleanText(artifact.relationship_type).toUpperCase() === "DEPLOYS_FROM",
  );
  const artifactTargetRepo = deployArtifacts
    .map((artifact) => repoFromArtifact(artifact, "target"))
    .find((repo) => repo?.name === sourceRepoName);
  const sourceRepo = artifactTargetRepo ?? {
    id: `repository:${sourceRepoName}`,
    name: sourceRepoName,
  };
  const serviceID = `workload:${serviceName}`;
  const nodes = new Map<string, GraphNode>();
  const edges: GraphEdge[] = [];
  const edgeKeys = new Set<string>();
  const chartRepoIDs = new Set<string>();

  addStoryNode(nodes, {
    id: serviceID,
    label: serviceName,
    kind: "workload",
    sub: "Workload",
    col: 3,
    hero: true,
    truth: "derived",
  });
  addStoryNode(nodes, {
    id: sourceRepo.id,
    label: sourceRepo.name,
    kind: "repo",
    sub: "Source repository",
    col: 2,
    truth: "derived",
  });

  const chartRepos = uniqueRepos(
    deployArtifacts
      .filter(isHelmChartArtifact)
      .map((artifact) => repoFromArtifact(artifact, "source"))
      .filter((repo): repo is RepoRef => !!repo && repo.id !== sourceRepo.id),
  );
  chartRepos.forEach((repo) => {
    chartRepoIDs.add(repo.id);
    addStoryNode(nodes, {
      id: repo.id,
      label: repo.name,
      kind: "repo",
      sub: "Helm chart",
      col: 1,
      truth: "derived",
    });
  });
  deployArtifacts.filter(isHelmChartArtifact).forEach((artifact) => {
    const repo = repoFromArtifact(artifact, "source");
    if (repo && repo.id !== sourceRepo.id) {
      addStoryEdge(
        edges,
        edgeKeys,
        repo.id,
        sourceRepo.id,
        "PACKAGES",
        artifactEdgeEvidence(artifact),
      );
    }
  });

  const controllerArtifacts = deployArtifacts.filter(isDeploymentControllerArtifact);
  const controllerRepos = uniqueRepos(
    controllerArtifacts
      .map((artifact) => repoFromArtifact(artifact, "source"))
      .filter(
        (repo): repo is RepoRef =>
          !!repo && repo.id !== sourceRepo.id && !chartRepoIDs.has(repo.id),
      ),
  );
  controllerRepos.forEach((repo) => {
    addStoryNode(nodes, {
      id: repo.id,
      label: repo.name,
      kind: "repo",
      sub: "Deployment controller",
      col: 0,
      truth: "derived",
    });
  });
  controllerArtifacts.forEach((artifact) => {
    const repo = repoFromArtifact(artifact, "source");
    if (!repo || repo.id === sourceRepo.id || chartRepoIDs.has(repo.id)) return;
    if (chartRepos.length === 0) {
      addStoryEdge(
        edges,
        edgeKeys,
        repo.id,
        sourceRepo.id,
        "DEPLOYS_FROM",
        artifactEdgeEvidence(artifact),
      );
      return;
    }
    chartRepos.forEach((chartRepo) =>
      addStoryEdge(
        edges,
        edgeKeys,
        repo.id,
        chartRepo.id,
        "DEPLOYS_HELM",
        artifactEdgeEvidence(artifact),
      ),
    );
  });

  if (deployArtifacts.length > 0) {
    addStoryEdge(
      edges,
      edgeKeys,
      sourceRepo.id,
      serviceID,
      "DEPLOYS_FROM",
      artifactEdgeEvidence(deployArtifacts[0]),
    );
  }
  return { nodes: [...nodes.values()], edges };
}

// Prefers deployment context and falls back to impact/entity-map.
export async function loadEntityStoryGraph(
  client: EshuApiClient,
  name: string,
  repoID?: string,
): Promise<GraphModel> {
  const storyClient = client as EshuApiClient & { readonly get?: EshuApiClient["get"] };
  if (typeof storyClient.get === "function") {
    try {
      const env = await storyClient.get<ServiceDeploymentContextResponse>(
        `/api/v0/services/${encodeURIComponent(name)}/context`,
      );
      if (env.error) throw new EshuEnvelopeError(env.error);
      const graph = deploymentStoryToGraph(env.data ?? {}, name);
      if (graph.edges.length > 0) return graph;
    } catch (error) {
      if (!shouldFallbackFromServiceContext(error)) throw error;
    }
    const repositoryGraph = await loadRepositoryDeploymentStoryGraph(storyClient, name, repoID);
    if (repositoryGraph !== null) return repositoryGraph;
  }
  return loadEntityMapGraph(client, name);
}

async function loadRepositoryDeploymentStoryGraph(
  client: EshuApiClient & { readonly get: EshuApiClient["get"] },
  name: string,
  repoID: string | undefined,
): Promise<GraphModel | null> {
  const id = cleanText(repoID);
  if (id === "") return null;
  try {
    const env = await client.get<RepositoryDeploymentContextResponse>(
      `/api/v0/repositories/${encodeURIComponent(id)}/context`,
    );
    if (env.error) throw new EshuEnvelopeError(env.error);
    const data = env.data ?? {};
    const repoName = cleanText(data.repository?.name) || name;
    const graph = deploymentStoryToGraph(
      {
        name,
        repo_name: repoName,
        deployment_evidence: data.deployment_evidence,
      },
      name,
    );
    return graph.edges.length > 0 ? graph : null;
  } catch (error) {
    if (shouldFallbackFromServiceContext(error)) return null;
    throw error;
  }
}

function repoFromArtifact(
  artifact: DeploymentArtifactRecord,
  side: "source" | "target",
): RepoRef | undefined {
  const id = cleanText(side === "source" ? artifact.source_repo_id : artifact.target_repo_id);
  const name = cleanText(side === "source" ? artifact.source_repo_name : artifact.target_repo_name);
  if (id === "" && name === "") return undefined;
  return { id: id || `repository:${name}`, name: name || id };
}

function uniqueRepos(repos: readonly RepoRef[]): RepoRef[] {
  const seen = new Set<string>();
  const unique: RepoRef[] = [];
  repos.forEach((repo) => {
    if (seen.has(repo.id)) return;
    seen.add(repo.id);
    unique.push(repo);
  });
  return unique;
}

function isHelmChartArtifact(artifact: DeploymentArtifactRecord): boolean {
  const family = cleanText(artifact.artifact_family).toLowerCase();
  const path = cleanText(artifact.path).toLowerCase();
  const sourceRepo = cleanText(artifact.source_repo_name).toLowerCase();
  if (family !== "helm") return false;
  return (
    path.endsWith("/chart.yaml") || sourceRepo.includes("helm") || sourceRepo.includes("chart")
  );
}

function isDeploymentControllerArtifact(artifact: DeploymentArtifactRecord): boolean {
  const family = cleanText(artifact.artifact_family).toLowerCase();
  return family === "argocd" || family === "kustomize";
}

function addStoryNode(nodes: Map<string, GraphNode>, node: GraphNode): void {
  if (!nodes.has(node.id)) nodes.set(node.id, node);
}

function addStoryEdge(
  edges: GraphEdge[],
  seen: Set<string>,
  source: string,
  target: string,
  verb: string,
  evidence?: readonly string[],
): void {
  const key = `${source}\u0000${target}\u0000${verb}`;
  if (seen.has(key)) return;
  seen.add(key);
  edges.push({ s: source, t: target, verb, layer: layerFor(verb), evidence });
}

function artifactEdgeEvidence(artifact: DeploymentArtifactRecord): readonly string[] {
  return [
    cleanText(artifact.artifact_family)
      ? `artifact family: ${cleanText(artifact.artifact_family)}`
      : "",
    cleanText(artifact.evidence_kind) ? `evidence kind: ${cleanText(artifact.evidence_kind)}` : "",
    cleanText(artifact.path) ? `path: ${cleanText(artifact.path)}` : "",
    cleanText(artifact.environment) ? `environment: ${cleanText(artifact.environment)}` : "",
  ].filter((value): value is string => value !== "");
}

function shouldFallbackFromServiceContext(error: unknown): boolean {
  if (error instanceof EshuApiHttpError) return error.status === 404;
  if (!(error instanceof EshuEnvelopeError)) return false;
  const code = error.error.code.toLowerCase();
  return code === "not_found" || code === "service_not_found" || code === "unknown_service";
}
