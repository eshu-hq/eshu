import {
  forceCenter,
  forceCollide,
  forceLink,
  forceManyBody,
  forceSimulation,
  forceX,
  forceY,
  type SimulationLinkDatum,
  type SimulationNodeDatum
} from "d3";
import type {
  ServiceRelationshipCluster,
  ServiceRelationshipRepository,
  ServiceSpotlight,
  ServiceTechnologyKind
} from "../api/serviceSpotlight";

export type GraphMode = "deployment" | "config" | "all";

export interface Point {
  readonly x: number;
  readonly y: number;
}

export interface RelationshipNode {
  readonly detail: string;
  readonly id: string;
  readonly kind: "service" | "repository" | "runtime";
  readonly label: string;
  readonly technology: ServiceTechnologyKind;
  readonly x: number;
  readonly y: number;
}

export interface RelationshipEdge {
  readonly detail: string;
  readonly family: string;
  readonly label: string;
  readonly source: string;
  readonly target: string;
}

export interface LayoutNode extends RelationshipNode, SimulationNodeDatum {}

export interface LayoutEdge extends SimulationLinkDatum<LayoutNode> {
  readonly detail: string;
  readonly family: string;
  readonly label: string;
  source: string | LayoutNode;
  target: string | LayoutNode;
}

export interface GraphModel {
  readonly edges: readonly RelationshipEdge[];
  readonly nodes: readonly RelationshipNode[];
}

export const graphWidth = 920;
export const graphHeight = 520;

export function buildGraphModel(spotlight: ServiceSpotlight, mode: GraphMode): GraphModel {
  const nodeMap = new Map<string, RelationshipNode>();
  const edgeMap = new Map<string, RelationshipEdge>();
  addNode(nodeMap, serviceNode(spotlight.name));

  if (mode === "deployment" || mode === "all") {
    for (const lane of spotlight.lanes) {
      const runtime = runtimeNode(lane.label, lane.environments);
      addNode(nodeMap, runtime);
      addEdge(edgeMap, {
        detail: `${spotlight.name} runs through ${lane.label}.`,
        family: "runtime",
        label: "RUNS_ON",
        source: `service:${spotlight.name}`,
        target: runtime.id
      });
      for (const repo of lane.sourceRepos) {
        const technology = technologyForRepository(repo, spotlight.relationshipClusters, lane.label);
        addNode(
          nodeMap,
          repositoryNode(repo, spotlight.name, technology, `${repo} participates in ${lane.label}.`)
        );
        addEdge(edgeMap, {
          detail: `${repo} provides ${lane.relationshipTypes.join(", ") || "deployment"} evidence for ${spotlight.name}.`,
          family: "deployment",
          label: lane.relationshipTypes[0] ?? "DEPLOYS_FROM",
          source: `repo:${repo}`,
          target: `service:${spotlight.name}`
        });
      }
    }
  }

  for (const cluster of spotlight.relationshipClusters) {
    if (!clusterEnabled(cluster, mode)) {
      continue;
    }
    for (const repository of cluster.repositories) {
      addRepositoryRelationship(nodeMap, edgeMap, spotlight.name, cluster, repository);
    }
  }

  return { edges: [...edgeMap.values()], nodes: [...nodeMap.values()] };
}

export function layoutGraph(model: GraphModel): {
  readonly edges: readonly LayoutEdge[];
  readonly nodes: readonly LayoutNode[];
} {
  const nodes = model.nodes.map((node): LayoutNode => ({ ...node }));
  const edges = model.edges.map((edge): LayoutEdge => ({ ...edge }));
  const simulation = forceSimulation<LayoutNode>(nodes)
    .force("link", forceLink<LayoutNode, LayoutEdge>(edges).id((node) => node.id).distance(170))
    .force("charge", forceManyBody().strength(-520))
    .force("collide", forceCollide<LayoutNode>().radius((node) => node.kind === "service" ? 76 : 58))
    .force("x", forceX<LayoutNode>((node) => desiredX(node)).strength(0.34))
    .force("y", forceY<LayoutNode>((node) => desiredY(node)).strength(0.2))
    .force("center", forceCenter(graphWidth / 2, graphHeight / 2))
    .stop();
  for (let index = 0; index < 140; index += 1) {
    simulation.tick();
  }
  return { edges, nodes };
}

export function applyDraggedPositions(
  layout: { readonly edges: readonly LayoutEdge[]; readonly nodes: readonly LayoutNode[] },
  positions: ReadonlyMap<string, Point>
): { readonly edges: readonly LayoutEdge[]; readonly nodes: readonly LayoutNode[] } {
  const nodes = layout.nodes.map((node) => {
    const position = positions.get(node.id);
    return position === undefined ? node : { ...node, ...position };
  });
  const nodeMap = new Map(nodes.map((node) => [node.id, node]));
  const edges = layout.edges.map((edge) => ({
    ...edge,
    source: nodeMap.get(nodeId(edge.source)) ?? edge.source,
    target: nodeMap.get(nodeId(edge.target)) ?? edge.target
  }));
  return { edges, nodes };
}

export function nodeId(node: string | LayoutNode): string {
  return typeof node === "string" ? node : node.id;
}

export function nodeLabel(node: string | LayoutNode): string {
  return typeof node === "string" ? node : node.label;
}

export function edgePoint(node: string | LayoutNode): Point {
  return typeof node === "string"
    ? { x: graphWidth / 2, y: graphHeight / 2 }
    : { x: node.x ?? graphWidth / 2, y: node.y ?? graphHeight / 2 };
}

export function edgeToModel(edge: LayoutEdge): RelationshipEdge {
  return {
    detail: edge.detail,
    family: edge.family,
    label: edge.label,
    source: nodeId(edge.source),
    target: nodeId(edge.target)
  };
}

export function technologyLabel(technology: ServiceTechnologyKind): string {
  switch (technology) {
    case "argocd":
      return "ArgoCD";
    case "config":
      return "Config";
    case "github_actions":
      return "GitHub Actions";
    case "helm":
      return "Helm chart";
    case "kubernetes":
      return "Kubernetes";
    case "terraform":
      return "Terraform resource";
    default:
      return "Repository";
  }
}

function addRepositoryRelationship(
  nodeMap: Map<string, RelationshipNode>,
  edgeMap: Map<string, RelationshipEdge>,
  serviceName: string,
  cluster: ServiceRelationshipCluster,
  repository: ServiceRelationshipRepository
): void {
  addNode(
    nodeMap,
    repositoryNode(
      repository.repository,
      serviceName,
      repository.technology,
      repository.paths[0] ?? repository.evidenceKinds[0] ?? cluster.description
    )
  );
  const label = repository.relationshipTypes[0] ?? cluster.relationshipTypes[0] ?? cluster.label;
  addEdge(edgeMap, {
    detail: repository.paths[0] ?? cluster.description,
    family: cluster.kind,
    label,
    source: `repo:${repository.repository}`,
    target: `service:${serviceName}`
  });
}

function clusterEnabled(cluster: ServiceRelationshipCluster, mode: GraphMode): boolean {
  if (mode === "all") {
    return true;
  }
  if (mode === "deployment") {
    return cluster.kind === "deployment" || cluster.kind === "runtime_provisioning";
  }
  return cluster.kind === "configuration_access" || cluster.kind === "configuration_discovery";
}

function serviceNode(name: string): RelationshipNode {
  return {
    detail: "Selected workload or service.",
    id: `service:${name}`,
    kind: "service",
    label: name,
    technology: "repository",
    x: graphWidth / 2,
    y: graphHeight / 2
  };
}

function runtimeNode(label: string, environments: readonly string[]): RelationshipNode {
  return {
    detail: environments.join(", ") || "Runtime evidence observed.",
    id: `runtime:${label}`,
    kind: "runtime",
    label,
    technology: technologyFromLabel(label),
    x: graphWidth - 150,
    y: graphHeight / 2
  };
}

function repositoryNode(
  repository: string,
  serviceName: string,
  technology: ServiceTechnologyKind,
  detail: string
): RelationshipNode {
  return {
    detail,
    id: `repo:${repository}`,
    kind: "repository",
    label: repository === serviceName ? `${repository} repo` : repository,
    technology,
    x: 150,
    y: graphHeight / 2
  };
}

function desiredX(node: RelationshipNode): number {
  if (node.kind === "service") {
    return graphWidth / 2;
  }
  if (node.kind === "runtime") {
    return graphWidth - 150;
  }
  return 165;
}

function desiredY(node: RelationshipNode): number {
  if (node.technology === "terraform") {
    return graphHeight * 0.32;
  }
  if (node.technology === "argocd" || node.technology === "helm") {
    return graphHeight * 0.58;
  }
  return graphHeight / 2;
}

function addNode(nodeMap: Map<string, RelationshipNode>, node: RelationshipNode): void {
  if (!nodeMap.has(node.id)) {
    nodeMap.set(node.id, node);
  }
}

function addEdge(edgeMap: Map<string, RelationshipEdge>, edge: RelationshipEdge): void {
  edgeMap.set(`${edge.source}:${edge.target}:${edge.label}`, edge);
}

function technologyForRepository(
  repository: string,
  clusters: readonly ServiceRelationshipCluster[],
  fallbackLabel: string
): ServiceTechnologyKind {
  for (const cluster of clusters) {
    const match = cluster.repositories.find((candidate) => candidate.repository === repository);
    if (match !== undefined) {
      return match.technology;
    }
  }
  return technologyFromLabel(fallbackLabel);
}

function technologyFromLabel(label: string): ServiceTechnologyKind {
  const value = label.toLowerCase();
  if (value.includes("terraform") || value.includes("ecs")) {
    return "terraform";
  }
  if (value.includes("helm")) {
    return "helm";
  }
  if (value.includes("argocd") || value.includes("gitops")) {
    return "argocd";
  }
  if (value.includes("kubernetes") || value.includes("eks")) {
    return "kubernetes";
  }
  return "repository";
}
