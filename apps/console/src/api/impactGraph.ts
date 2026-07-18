import type { ChangeSurfaceImpactNode, ChangeSurfaceInvestigation } from "./changeSurface";
import type {
  BlastRadiusResult,
  DeploymentTraceResult,
  ImpactGraphPresentation,
  ImpactSection,
  ImpactTargetKind,
} from "./impactReviewTypes";
import type { GraphEdge, GraphLayer, GraphModel, GraphNode } from "../console/types";

const nodeLimit = 60;
const edgeLimit = 120;

export function selectImpactGraph(
  target: string,
  targetKind: ImpactTargetKind,
  blast: ImpactSection<BlastRadiusResult>,
  changeSurface: ImpactSection<ChangeSurfaceInvestigation>,
  deploymentTrace: ImpactSection<DeploymentTraceResult>,
): { readonly graph: GraphModel; readonly presentation: ImpactGraphPresentation } {
  if (blast.status === "ready" && blast.data.graph.nodes.length > 1) {
    return existingGraph(blast.data.graph, "blast_radius", [blast.source], "Blast radius");
  }
  if (changeSurface.status === "ready" && changeSurface.data.impact.totalCount > 0) {
    return existingGraph(
      changeSurfaceGraph(target, changeSurface.data),
      "change_surface",
      [changeSurface.source],
      "Change surface",
    );
  }
  if (
    (targetKind === "service" || targetKind === "workload") &&
    deploymentTrace.status === "ready"
  ) {
    const deployment = deploymentTraceGraph(deploymentTrace.data);
    if (deployment.graph.edges.length > 0) {
      return deployment;
    }
  }
  if (changeSurface.status === "ready") {
    return existingGraph(
      changeSurfaceGraph(target, changeSurface.data),
      "change_surface",
      [changeSurface.source],
      "Change surface",
    );
  }
  if (blast.status === "ready") {
    return existingGraph(blast.data.graph, "blast_radius", [blast.source], "Blast radius");
  }
  return existingGraph({ edges: [], nodes: [] }, "empty", [], "Impact graph");
}

function deploymentTraceGraph(trace: DeploymentTraceResult): {
  readonly graph: GraphModel;
  readonly presentation: ImpactGraphPresentation;
} {
  const rawNodes: GraphNode[] = [];
  const rawEdges: GraphEdge[] = [];
  const limitations = new Set<string>();
  let identityOmittedNodes = 0;
  let identityOmittedEdges = 0;

  const addNode = (node: GraphNode | null, limitation: string): boolean => {
    if (node === null || node.id.trim().length === 0) {
      identityOmittedNodes += 1;
      limitations.add(limitation);
      return false;
    }
    rawNodes.push(node);
    return true;
  };
  const addEdge = (edge: GraphEdge | null, limitation: string): void => {
    if (edge === null || edge.s.length === 0 || edge.t.length === 0) {
      identityOmittedEdges += 1;
      limitations.add(limitation);
      return;
    }
    rawEdges.push(edge);
  };

  const workloadID = trace.workloadId;
  const sourceRepoID = trace.repoId;
  addNode(
    workloadID.length > 0
      ? {
          col: 2,
          hero: true,
          id: workloadID,
          kind: "workload",
          label: trace.serviceName || workloadID,
          sub: "deployment subject",
          truth: "exact",
        }
      : null,
    "workload omitted because the trace has no canonical workload_id",
  );
  addNode(
    sourceRepoID.length > 0
      ? {
          col: 1,
          id: sourceRepoID,
          kind: "repo",
          label: trace.repoName || sourceRepoID,
          sub: "source repository",
          truth: "exact",
        }
      : null,
    "source repository omitted because the trace has no canonical repo_id",
  );
  if (sourceRepoID.length > 0 && workloadID.length > 0) {
    addEdge({ layer: "code", s: sourceRepoID, t: workloadID, verb: "DEFINES" }, "");
  }

  for (const source of trace.deploymentSources) {
    const configRepoID = source.id ?? "";
    addNode(
      configRepoID.length > 0
        ? {
            col: 0,
            id: configRepoID,
            kind: "repo",
            label: source.name,
            sub: source.detail ?? "deployment source",
            truth: "exact",
          }
        : null,
      "deployment source omitted because it has no canonical repo_id",
    );
    addEdge(
      configRepoID.length > 0 && sourceRepoID.length > 0
        ? { layer: "deploy", s: configRepoID, t: sourceRepoID, verb: "DEPLOYS_FROM" }
        : null,
      "deployment-source edge omitted because an endpoint lacks canonical identity",
    );
  }

  const environmentKeys = new Set<string>();
  for (const instance of trace.instances) {
    const instanceAdded = addNode(
      instance.id.length > 0
        ? {
            col: 3,
            id: instance.id,
            kind: "service",
            label: instance.environment ?? instance.id,
            sub: "runtime instance",
            truth: "exact",
          }
        : null,
      "runtime instance omitted because it has no canonical instance_id",
    );
    addEdge(
      instanceAdded && workloadID.length > 0
        ? { layer: "runtime", s: instance.id, t: workloadID, verb: "INSTANCE_OF" }
        : null,
      "instance edge omitted because an endpoint lacks canonical identity",
    );

    if (instance.environment !== undefined && instance.environment.length > 0) {
      const environmentID = `environment:${instance.environment}`;
      addNode(
        {
          col: 4,
          id: environmentID,
          kind: "env",
          label: instance.environment,
          sub: "materialized environment",
          truth: "exact",
        },
        "",
      );
      if (!environmentKeys.has(environmentID)) {
        addEdge(
          workloadID.length > 0
            ? {
                layer: "runtime",
                s: workloadID,
                t: environmentID,
                verb: "MATERIALIZED_IN_ENVIRONMENT",
              }
            : null,
          "environment edge omitted because the workload lacks canonical identity",
        );
        environmentKeys.add(environmentID);
      }
    }

    for (const platform of instance.platforms) {
      const platformID = platform.id ?? "";
      addNode(
        platformID.length > 0
          ? {
              col: 4,
              id: platformID,
              kind: "service",
              label: platform.name,
              sub: platform.kind ?? "runtime platform",
              truth: "exact",
            }
          : null,
        "runtime platform omitted because it has no canonical platform_id",
      );
      addEdge(
        instanceAdded && platformID.length > 0
          ? { layer: "runtime", s: instance.id, t: platformID, verb: "RUNS_ON" }
          : null,
        "RUNS_ON edge omitted because an endpoint lacks canonical identity",
      );
    }
  }

  if (trace.cloudResources.length > 0 || trace.k8sResources.length > 0) {
    limitations.add(
      "cloud and Kubernetes evidence stays in the evidence groups unless the trace supplies exact topology endpoints",
    );
  }

  return boundedGraph(rawNodes, rawEdges, identityOmittedNodes, identityOmittedEdges, limitations);
}

function boundedGraph(
  rawNodes: readonly GraphNode[],
  rawEdges: readonly GraphEdge[],
  identityOmittedNodes: number,
  identityOmittedEdges: number,
  limitations: ReadonlySet<string>,
): { readonly graph: GraphModel; readonly presentation: ImpactGraphPresentation } {
  const nodesByID = new Map<string, GraphNode>();
  for (const node of rawNodes) {
    if (!nodesByID.has(node.id)) nodesByID.set(node.id, node);
  }
  const duplicateNodes = rawNodes.length - nodesByID.size;
  const uniqueNodes = [...nodesByID.values()].sort(
    (left, right) =>
      left.col - right.col ||
      left.kind.localeCompare(right.kind) ||
      left.id.localeCompare(right.id),
  );
  const nodes = uniqueNodes.slice(0, nodeLimit);
  const renderedIDs = new Set(nodes.map((node) => node.id));
  const edgeByKey = new Map<string, GraphEdge>();
  for (const edge of rawEdges) {
    const key = `${edge.s}\u0000${edge.verb}\u0000${edge.t}`;
    if (!edgeByKey.has(key)) edgeByKey.set(key, edge);
  }
  const duplicateEdges = rawEdges.length - edgeByKey.size;
  const uniqueEdges = [...edgeByKey.values()];
  const referenceOmittedEdges = uniqueEdges.filter(
    (edge) => !renderedIDs.has(edge.s) || !renderedIDs.has(edge.t),
  ).length;
  const eligibleEdges = uniqueEdges.filter(
    (edge) => renderedIDs.has(edge.s) && renderedIDs.has(edge.t),
  );
  const edges = eligibleEdges.slice(0, edgeLimit);
  const omittedNodes = identityOmittedNodes + Math.max(0, uniqueNodes.length - nodes.length);
  const omittedEdges =
    identityOmittedEdges + referenceOmittedEdges + Math.max(0, eligibleEdges.length - edges.length);
  const truncated = uniqueNodes.length > nodeLimit || eligibleEdges.length > edgeLimit;
  return {
    graph: { edges, nodes },
    presentation: {
      duplicateEdges,
      duplicateNodes,
      edgeLimit,
      inputEdges: rawEdges.length + identityOmittedEdges,
      inputNodes: rawNodes.length + identityOmittedNodes,
      limitations: [...limitations],
      mode: "deployment_trace",
      nodeLimit,
      omittedEdges,
      omittedNodes,
      renderedEdges: edges.length,
      renderedNodes: nodes.length,
      sourceApis: ["/api/v0/impact/trace-deployment-chain"],
      title: "Deployment topology",
      truncated,
    },
  };
}

function existingGraph(
  graph: GraphModel,
  mode: ImpactGraphPresentation["mode"],
  sourceApis: readonly string[],
  title: string,
): { readonly graph: GraphModel; readonly presentation: ImpactGraphPresentation } {
  return {
    graph,
    presentation: {
      duplicateEdges: 0,
      duplicateNodes: 0,
      edgeLimit,
      inputEdges: graph.edges.length,
      inputNodes: graph.nodes.length,
      limitations: [],
      mode,
      nodeLimit,
      omittedEdges: 0,
      omittedNodes: 0,
      renderedEdges: graph.edges.length,
      renderedNodes: graph.nodes.length,
      sourceApis,
      title,
      truncated: false,
    },
  };
}

function changeSurfaceGraph(
  fallbackTarget: string,
  investigation: ChangeSurfaceInvestigation,
): GraphModel {
  const selected = investigation.resolution.selected;
  const centerId = selected?.id ?? investigation.scope.target ?? fallbackTarget;
  const centerName = selected?.name ?? investigation.scope.target ?? fallbackTarget;
  const nodes = new Map<string, GraphNode>();
  nodes.set(centerId, {
    col: 0,
    hero: true,
    id: centerId,
    kind: kindForLabels(selected?.labels ?? [investigation.resolution.targetType]),
    label: centerName,
    sub: "impact origin",
    truth: "derived",
  });
  const edges: GraphEdge[] = [];
  for (const node of [...investigation.directImpact, ...investigation.transitiveImpact]) {
    const id = node.id || node.name;
    if (id === centerId || id.length === 0) continue;
    nodes.set(id, graphNodeForImpact(node));
    edges.push({
      layer: graphLayerForImpact(node),
      s: id,
      t: centerId,
      verb: node.depth <= 1 ? "DIRECT_IMPACT" : "TRANSITIVE_IMPACT",
    });
  }
  return { edges, nodes: [...nodes.values()] };
}

function graphNodeForImpact(node: ChangeSurfaceImpactNode): GraphNode {
  return {
    col: Math.max(1, node.depth),
    id: node.id || node.name,
    kind: kindForLabels(node.labels),
    label: node.name,
    sub: node.repoId || node.environment || `depth ${node.depth}`,
    truth: "derived",
  };
}

function graphLayerForImpact(node: ChangeSurfaceImpactNode): GraphLayer {
  const kind = kindForLabels(node.labels);
  if (kind === "repo" || kind === "client" || kind === "library") return "code";
  if (kind === "aws") return "infra";
  return "runtime";
}

function kindForLabels(labels: readonly string[]): string {
  const normalized = labels.join(" ").toLowerCase();
  if (normalized.includes("repository")) return "repo";
  if (normalized.includes("cloud") || normalized.includes("resource")) return "aws";
  if (normalized.includes("module") || normalized.includes("package")) return "library";
  if (
    normalized.includes("function") ||
    normalized.includes("class") ||
    normalized.includes("symbol")
  )
    return "client";
  if (normalized.includes("workload")) return "workload";
  return "service";
}
