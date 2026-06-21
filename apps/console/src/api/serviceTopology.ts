import type { EshuApiClient } from "./client";
import { envelopePayload } from "./envelopePayload";
import type { ServiceRow } from "./eshuConsoleLive";
import {
  serviceContextFromStoryDossier,
  type ServiceStoryDossierResponse
} from "./serviceStoryDossier";
import {
  type DeploymentArtifactRecord,
  serviceSpotlightFromContext,
  type ServiceContextResponse,
  type ServiceDependency,
  type ServiceDeploymentLane
} from "./serviceSpotlight";
import type { ServiceTrafficPath } from "./serviceTrafficPath";

export type TopologyProvenance = "live" | "unavailable";
export type TopologyKind =
  | "edge"
  | "hostname"
  | "origin"
  | "pending"
  | "repo"
  | "runtime"
  | "service"
  | "workload";

export interface TopologyNode {
  readonly id: string;
  readonly kind: TopologyKind;
  readonly label: string;
  readonly sub: string;
  readonly x: number;
  readonly y: number;
  readonly w: number;
  readonly h: number;
  readonly provenance: TopologyProvenance;
  readonly hero?: boolean;
}

export interface TopologyEdge {
  readonly s: string;
  readonly t: string;
  readonly verb: string;
  readonly layer: "code" | "deploy" | "infra" | "runtime";
  readonly provenance: TopologyProvenance;
}

export interface ServiceTopology {
  readonly nodes: readonly TopologyNode[];
  readonly edges: readonly TopologyEdge[];
  readonly meta: {
    readonly dependencyCount: number;
    readonly environment: string;
    readonly exposure: string;
    readonly provenance: TopologyProvenance;
    readonly serviceName: string;
  };
}

export interface ServiceTopologyInput {
  readonly deploymentArtifacts?: readonly DeploymentArtifactRecord[];
  readonly service: ServiceRow;
  readonly dependencies?: readonly ServiceDependency[];
  readonly lanes?: readonly ServiceDeploymentLane[];
  readonly repoName?: string;
  readonly trafficPaths?: readonly ServiceTrafficPath[];
}

export async function loadServiceTopology(
  client: EshuApiClient,
  service: ServiceRow
): Promise<ServiceTopology> {
  const enc = encodeURIComponent(service.name);
  let storyContext: ServiceContextResponse | undefined;
  let context: ServiceContextResponse | undefined;

  try {
    const response = await client.get<ServiceStoryDossierResponse>(
      `/api/v0/services/${enc}/story`
    );
    const { data } = envelopePayload<ServiceStoryDossierResponse>(response);
    storyContext = serviceContextFromStoryDossier(data, service.name);
  } catch {
    storyContext = undefined;
  }

  try {
    const response = await client.get<ServiceContextResponse>(
      `/api/v0/services/${enc}/context`
    );
    context = envelopePayload<ServiceContextResponse>(response).data;
  } catch {
    context = undefined;
  }

  if (storyContext === undefined && context === undefined) {
    return buildServiceTopology({ service, trafficPaths: [] });
  }

  const mergedContext = mergeServiceContexts(storyContext, context);
  const spotlight = serviceSpotlightFromContext(mergedContext, service.name);
  return buildServiceTopology({
    deploymentArtifacts: mergedContext.deployment_evidence?.artifacts ?? [],
    dependencies: spotlight.dependencies,
    lanes: spotlight.lanes,
    repoName: spotlight.repoName,
    service,
    trafficPaths: spotlight.trafficPaths ?? []
  });
}

export function buildServiceTopology(input: ServiceTopologyInput): ServiceTopology {
  const path = input.trafficPaths?.[0];
  const serviceName = path?.workload || input.service.name;
  const repoName = nonEmpty(path?.sourceRepo, input.repoName, input.service.repo, "source repo pending");
  const runtime = nonEmpty(path?.runtime, firstLaneEnvironment(input.lanes), "runtime pending");
  const environment = nonEmpty(path?.environment, input.service.environments[0], "environment pending");
  const nodes: TopologyNode[] = [];
  const edges: TopologyEdge[] = [];

  const add = (node: Omit<TopologyNode, "h" | "w"> & Partial<Pick<TopologyNode, "h" | "w">>): TopologyNode => {
    const fitted = fitNode({ h: 58, w: 170, ...node });
    nodes.push(fitted);
    return fitted;
  };
  const edge = (
    s: string,
    t: string,
    verb: string,
    layer: TopologyEdge["layer"],
    provenance: TopologyProvenance
  ): void => {
    edges.push({ s, t, verb, layer, provenance });
  };

  const entryProvenance: TopologyProvenance = path === undefined ? "unavailable" : "live";
  if (path === undefined) {
    add({
      id: "entry-pending",
      kind: "pending",
      label: "Entry evidence pending",
      provenance: "unavailable",
      sub: "requires traffic-path evidence",
      x: 148,
      y: 142
    });
  } else {
    add({
      id: "hostname",
      kind: "hostname",
      label: path.hostname,
      provenance: "live",
      sub: `${path.visibility || "visibility pending"} · ${path.evidenceKind}`,
      x: 126,
      y: 142
    });
    const edgeLabel = compactEdgeLabel(path.edge, path.evidenceKind);
    add({
      id: "edge",
      kind: "edge",
      label: edgeLabel.label,
      provenance: "live",
      sub: edgeLabel.sub,
      x: 398,
      y: 142
    });
    add({
      id: "origin",
      kind: "origin",
      label: path.origin,
      provenance: "live",
      sub: path.reason || "origin evidence",
      x: 666,
      y: 142
    });
    edge("hostname", "edge", "ROUTES_TO", "infra", "live");
    edge("edge", "origin", "ORIGINATES_AT", "infra", "live");
  }

  add({
    id: "runtime",
    kind: "runtime",
    label: runtime,
    provenance: entryProvenance,
    sub: environment,
    x: 926,
    y: 142
  });
  add({
    hero: true,
    h: 78,
    id: "workload",
    kind: "workload",
    label: serviceName,
    provenance: entryProvenance,
    sub: `${input.service.kind} · ${input.service.truth}`,
    x: 1178,
    y: 236
  });

  edge(path === undefined ? "entry-pending" : "origin", "runtime", "RUNS_ON", "runtime", entryProvenance);
  edge("runtime", "workload", "HOSTS", "runtime", entryProvenance);
  const hasDeploymentChain = addDeploymentChain(input.deploymentArtifacts ?? [], repoName, serviceName, add, edge);
  if (!hasDeploymentChain) {
    add({
      id: "repo",
      kind: "repo",
      label: repoName,
      provenance: repoName === "source repo pending" ? "unavailable" : "live",
      sub: "source repository",
      x: 398,
      y: 392
    });
    add({
      id: "delivery",
      kind: "service",
      label: "Delivery evidence",
      provenance: input.lanes?.length ? "live" : "unavailable",
      sub: deliverySub(input.lanes),
      x: 724,
      y: 392
    });
    edge("repo", "delivery", "BUILDS", "deploy", repoName === "source repo pending" ? "unavailable" : "live");
    edge("delivery", "workload", "DEPLOYS", "deploy", input.lanes?.length ? "live" : "unavailable");
  }

  return {
    edges,
    meta: {
      dependencyCount: input.dependencies?.length ?? 0,
      environment,
      exposure: path?.visibility ?? "traffic evidence unavailable",
      provenance: path === undefined && !hasDeploymentChain ? "unavailable" : "live",
      serviceName
    },
    nodes
  };
}

interface DeploymentRepoRef {
  readonly id: string;
  readonly name: string;
}

function addDeploymentChain(
  artifacts: readonly DeploymentArtifactRecord[],
  repoName: string,
  serviceName: string,
  add: (node: Omit<TopologyNode, "h" | "w"> & Partial<Pick<TopologyNode, "h" | "w">>) => TopologyNode,
  edge: (
    s: string,
    t: string,
    verb: string,
    layer: TopologyEdge["layer"],
    provenance: TopologyProvenance
  ) => void
): boolean {
  const deployArtifacts = artifacts.filter((artifact) =>
    nonEmpty(artifact.relationship_type).toUpperCase() === "DEPLOYS_FROM"
  );
  if (deployArtifacts.length === 0) return false;

  const sourceRepo = deployArtifacts
    .map((artifact) => repoFromDeploymentArtifact(artifact, "target"))
    .find((repo) => repo?.name === repoName || repo?.name === serviceName) ??
    { id: `repository:${repoName}`, name: repoName };
  const charts = uniqueRepos(deployArtifacts
    .filter(isHelmDeploymentArtifact)
    .map((artifact) => repoFromDeploymentArtifact(artifact, "source"))
    .filter((repo): repo is DeploymentRepoRef => repo !== undefined && repo.id !== sourceRepo.id));
  const chartIds = new Set(charts.map((repo) => repo.id));
  const controllers = uniqueRepos(deployArtifacts
    .filter(isDeploymentControllerArtifact)
    .map((artifact) => repoFromDeploymentArtifact(artifact, "source"))
    .filter((repo): repo is DeploymentRepoRef =>
      repo !== undefined && repo.id !== sourceRepo.id && !chartIds.has(repo.id)
    ));

  add({ id: sourceRepo.id, kind: "repo", label: sourceRepo.name, provenance: "live", sub: "source repository", x: 724, y: 392 });
  charts.forEach((repo, index) => {
    add({ id: repo.id, kind: "repo", label: repo.name, provenance: "live", sub: "Helm chart", x: 398, y: 392 + index * 72 });
    edge(repo.id, sourceRepo.id, "PACKAGES", "deploy", "live");
  });
  controllers.forEach((repo, index) => {
    add({ id: repo.id, kind: "repo", label: repo.name, provenance: "live", sub: "Deployment controller", x: 126, y: 392 + index * 72 });
    if (charts.length === 0) {
      edge(repo.id, sourceRepo.id, "DEPLOYS_FROM", "deploy", "live");
      return;
    }
    charts.forEach((chart) => edge(repo.id, chart.id, "DEPLOYS_HELM", "deploy", "live"));
  });
  edge(sourceRepo.id, "workload", "DEPLOYS_FROM", "deploy", "live");
  return true;
}

function repoFromDeploymentArtifact(
  artifact: DeploymentArtifactRecord,
  side: "source" | "target"
): DeploymentRepoRef | undefined {
  const id = nonEmpty(side === "source" ? artifact.source_repo_id : artifact.target_repo_id);
  const name = nonEmpty(side === "source" ? artifact.source_repo_name : artifact.target_repo_name);
  if (id.length === 0 && name.length === 0) return undefined;
  return { id: id || `repository:${name}`, name: name || id };
}

function uniqueRepos(repos: readonly DeploymentRepoRef[]): readonly DeploymentRepoRef[] {
  const seen = new Set<string>();
  return repos.filter((repo) => {
    if (seen.has(repo.id)) return false;
    seen.add(repo.id);
    return true;
  });
}

function isHelmDeploymentArtifact(artifact: DeploymentArtifactRecord): boolean {
  const family = nonEmpty(artifact.artifact_family).toLowerCase();
  const path = nonEmpty(artifact.path).toLowerCase();
  const sourceRepo = nonEmpty(artifact.source_repo_name).toLowerCase();
  return family === "helm" && (path.endsWith("/chart.yaml") || sourceRepo.includes("helm") || sourceRepo.includes("chart"));
}

function isDeploymentControllerArtifact(artifact: DeploymentArtifactRecord): boolean {
  const family = nonEmpty(artifact.artifact_family).toLowerCase();
  return family === "argocd" || family === "kustomize";
}

function mergeServiceContexts(
  first: ServiceContextResponse | undefined,
  second: ServiceContextResponse | undefined
): ServiceContextResponse {
  return {
    ...(first ?? {}),
    ...(second ?? {}),
    deployment_lanes: second?.deployment_lanes ?? first?.deployment_lanes,
    edge_runtime_evidence: second?.edge_runtime_evidence ?? first?.edge_runtime_evidence,
    hostnames: second?.hostnames ?? first?.hostnames,
    network_paths: second?.network_paths ?? first?.network_paths,
    repo_name: nonEmpty(second?.repo_name, first?.repo_name)
  };
}

function compactEdgeLabel(edge: string, evidenceKind: string): { readonly label: string; readonly sub: string } {
  const cloudfront = edge.match(/^CloudFront distribution\s+(.+)$/i);
  if (cloudfront || evidenceKind === "aws_cloudfront_distribution") {
    return { label: cloudfront?.[1] ?? edge, sub: "CloudFront distribution" };
  }
  const apiGateway = edge.match(/^API Gateway\s+(.+)$/i);
  if (apiGateway) {
    return { label: "API Gateway", sub: apiGateway[1] };
  }
  return { label: edge, sub: "edge runtime evidence" };
}

function deliverySub(lanes: readonly ServiceDeploymentLane[] | undefined): string {
  if (lanes === undefined || lanes.length === 0) {
    return "deployment lane pending";
  }
  return lanes.map((lane) => lane.label).slice(0, 2).join(" + ");
}

function firstLaneEnvironment(lanes: readonly ServiceDeploymentLane[] | undefined): string {
  return nonEmpty(lanes?.[0]?.environments[0], lanes?.[0]?.label);
}

function fitNode(node: TopologyNode): TopologyNode {
  const labelWidth = node.label.length * (node.hero ? 10.5 : 8.4);
  const subWidth = node.sub.length * 5.8;
  const min = node.hero ? 210 : 156;
  const max = node.hero ? 328 : 300;
  return {
    ...node,
    w: Math.round(Math.min(max, Math.max(min, labelWidth + 64, subWidth + 64)))
  };
}

function nonEmpty(...values: readonly (string | undefined)[]): string {
  for (const value of values) {
    if (value !== undefined && value.trim().length > 0) return value.trim();
  }
  return "";
}

// Wire shapes for GET /api/v0/catalog used by loadTopologyServices.
interface TopologyCatalogRecord {
  readonly id?: string;
  readonly name?: string;
  readonly kind?: string;
  readonly repo_name?: string;
  readonly repo_id?: string;
  readonly environments?: readonly string[];
}

interface TopologyCatalogResponse {
  readonly repositories?: readonly TopologyCatalogRecord[];
  readonly services?: readonly TopologyCatalogRecord[];
  readonly workloads?: readonly TopologyCatalogRecord[];
}

// loadTopologyServices fetches a bounded service list from the catalog so the
// Topology page can populate its service picker without depending on the shared
// ConsoleModel snapshot. It reads services and workloads first; when those
// arrays are absent it falls back to repositories so the picker is never empty
// on catalogs that only carry repository entries (955-repo live catalogs hit
// this path). Results are deduplicated by id and sorted so named entries
// precede blank ones. Returns [] on network failure so the caller can show a
// graceful unavailable state.
export async function loadTopologyServices(client: EshuApiClient): Promise<readonly ServiceRow[]> {
  let catalog: TopologyCatalogResponse;
  try {
    catalog = await client.getJson<TopologyCatalogResponse>("/api/v0/catalog?limit=2000&offset=0");
  } catch {
    return [];
  }

  const byId = new Map<string, ServiceRow>();

  const addRecord = (record: TopologyCatalogRecord, defaultKind: string): void => {
    const id = nonEmpty(record.id, record.name);
    if (id === "" || byId.has(id)) return;
    byId.set(id, {
      id,
      name: nonEmpty(record.name, record.id),
      kind: nonEmpty(record.kind, defaultKind),
      repo: nonEmpty(record.repo_name, record.repo_id),
      environments: record.environments ?? [],
      truth: "exact",
      freshness: "fresh"
    });
  };

  const hasServiceEntries =
    (catalog.services?.length ?? 0) > 0 || (catalog.workloads?.length ?? 0) > 0;

  if (hasServiceEntries) {
    for (const s of catalog.services ?? []) addRecord(s, "service");
    for (const w of catalog.workloads ?? []) addRecord(w, "workload");
  } else {
    // Fall back to repositories when the catalog has no promoted service or
    // workload nodes so the Topology picker is still populated.
    for (const r of catalog.repositories ?? []) addRecord(r, "repository");
  }

  const rows = [...byId.values()];
  // Named entries sort first; ties break alphabetically.
  rows.sort((a, b) => {
    if (a.name.length === 0 && b.name.length > 0) return 1;
    if (a.name.length > 0 && b.name.length === 0) return -1;
    return a.name.localeCompare(b.name);
  });
  return rows;
}
