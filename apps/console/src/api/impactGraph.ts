import type { ChangeSurfaceImpactNode, ChangeSurfaceInvestigation } from "./changeSurface";
import { boundedGraph } from "./impactGraphSelection";
import type {
  BlastRadiusResult,
  DeploymentTraceResult,
  ImpactGraphPresentation,
  ImpactSection,
  ImpactTargetKind,
} from "./impactReviewTypes";
import {
  uiTruth,
  type GraphEdge,
  type GraphLayer,
  type GraphModel,
  type GraphNode,
  type UiTruth,
} from "../console/types";

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
    return existingGraph(
      blast.data.graph,
      "blast_radius",
      [blast.source],
      "Blast radius",
      blast.truth,
    );
  }
  const traceSelectionLimitation = deploymentTraceSelectionLimitation(
    targetKind,
    changeSurface,
    deploymentTrace,
  );
  if (traceSelectionLimitation !== undefined && changeSurface.status === "ready") {
    return existingGraph(
      changeSurfaceGraph(target, changeSurface.data),
      "change_surface",
      [changeSurface.source, deploymentTrace.source],
      "Change surface",
      changeSurface.truth,
      [traceSelectionLimitation],
    );
  }
  if (changeSurface.status === "ready" && changeSurface.data.impact.totalCount > 0) {
    return existingGraph(
      changeSurfaceGraph(target, changeSurface.data),
      "change_surface",
      [changeSurface.source],
      "Change surface",
      changeSurface.truth,
    );
  }
  if (
    (targetKind === "service" || targetKind === "workload") &&
    deploymentTrace.status === "ready"
  ) {
    const traceTruth = deploymentTrace.truth;
    const deployment = deploymentTraceGraph(
      deploymentTrace.data,
      traceTruth === null ? "inferred" : uiTruth(traceTruth.level),
    );
    if (deployment.graph.edges.length > 0) {
      return {
        graph: deployment.graph,
        presentation: {
          ...deployment.presentation,
          freshness: deploymentTrace.truth?.freshness.state,
          truthLevel: deploymentTrace.truth?.level,
          truthBasis: deploymentTrace.truth?.basis,
        },
      };
    }
  }
  if (changeSurface.status === "ready") {
    return existingGraph(
      changeSurfaceGraph(target, changeSurface.data),
      "change_surface",
      [changeSurface.source],
      "Change surface",
      changeSurface.truth,
    );
  }
  if (blast.status === "ready") {
    return existingGraph(
      blast.data.graph,
      "blast_radius",
      [blast.source],
      "Blast radius",
      blast.truth,
    );
  }
  return existingGraph({ edges: [], nodes: [] }, "empty", [], "Impact graph");
}

function deploymentTraceGraph(
  trace: DeploymentTraceResult,
  truth: UiTruth,
): {
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
          truth,
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
          truth,
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
            truth,
          }
        : null,
      "deployment source omitted because it has no canonical repo_id",
    );
    if (source.relationshipType === "DEPLOYS_FROM") {
      addEdge(
        source.sourceId === configRepoID && source.targetId === sourceRepoID
          ? {
              layer: "deploy",
              s: source.sourceId,
              t: source.targetId,
              verb: "DEPLOYS_FROM",
            }
          : null,
        "DEPLOYS_FROM edge omitted because its exact endpoints do not match the trace repositories",
      );
    } else if (source.relationshipType === "DEPLOYMENT_SOURCE") {
      addEdge(
        source.sourceId !== undefined && source.targetId === configRepoID
          ? {
              layer: "deploy",
              s: source.sourceId,
              t: source.targetId,
              verb: "DEPLOYMENT_SOURCE",
            }
          : null,
        "DEPLOYMENT_SOURCE edge omitted because its exact instance or repository endpoint is unavailable",
      );
    } else {
      addEdge(
        null,
        "deployment-source edge omitted because the trace did not identify its relationship family and endpoints",
      );
    }
  }

  const environmentKeys = new Set<string>();
  for (const instance of trace.instances) {
    const instanceAdded = addNode(
      instance.id.length > 0
        ? {
            col: 3,
            id: instance.id,
            kind: "instance",
            label: instance.environment ?? instance.id,
            sub: "runtime instance",
            truth,
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
          truth,
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
              col: 5,
              id: platformID,
              kind: "platform",
              label: platform.name,
              sub: platform.kind ?? "runtime platform",
              truth,
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

  for (const resource of trace.cloudResources) {
    addNode(
      resource.id !== undefined
        ? {
            col: 6,
            id: resource.id,
            kind: "aws",
            label: resource.name,
            sub: "evidence only · no exact topology edge",
            truth,
          }
        : null,
      "cloud-resource evidence omitted because it has no canonical identity",
    );
  }
  for (const resource of trace.k8sResources) {
    addNode(
      resource.id !== undefined
        ? {
            col: 6,
            id: resource.id,
            kind: "k8s",
            label: resource.name,
            sub: "evidence only · no exact topology edge",
            truth,
          }
        : null,
      "Kubernetes evidence omitted because it has no canonical identity",
    );
  }
  if (trace.cloudResources.length > 0 || trace.k8sResources.length > 0) {
    limitations.add(
      "cloud and Kubernetes evidence nodes remain disconnected unless the trace supplies exact topology endpoints",
    );
  }

  return boundedGraph(rawNodes, rawEdges, identityOmittedNodes, identityOmittedEdges, limitations);
}

function existingGraph(
  graph: GraphModel,
  mode: ImpactGraphPresentation["mode"],
  sourceApis: readonly string[],
  title: string,
  truth?: {
    readonly basis?: string;
    readonly freshness: { readonly state: string };
    readonly level: string;
  } | null,
  limitations: readonly string[] = [],
): { readonly graph: GraphModel; readonly presentation: ImpactGraphPresentation } {
  return {
    graph,
    presentation: {
      compositionDurationMs: 0,
      duplicateEdges: 0,
      duplicateNodes: 0,
      edgeLimit,
      freshness: truth?.freshness.state,
      inputEdges: graph.edges.length,
      inputNodes: graph.nodes.length,
      limitations,
      mode,
      nodeLimit,
      omittedEdges: 0,
      omittedNodes: 0,
      renderedEdges: graph.edges.length,
      renderedNodes: graph.nodes.length,
      sourceApis,
      title,
      truncated: false,
      truthBasis: truth?.basis,
      truthLevel: truth?.level,
    },
  };
}

function deploymentTraceSelectionLimitation(
  targetKind: ImpactTargetKind,
  changeSurface: ImpactSection<ChangeSurfaceInvestigation>,
  deploymentTrace: ImpactSection<DeploymentTraceResult>,
): string | undefined {
  if (
    (targetKind !== "service" && targetKind !== "workload") ||
    changeSurface.status !== "ready" ||
    deploymentTrace.status !== "ready"
  ) {
    return undefined;
  }
  if (changeSurface.data.resolution.status === "ambiguous") {
    return "deployment topology not selected because the service target is ambiguous";
  }
  if (changeSurface.data.resolution.status !== "resolved") {
    return `deployment topology not selected because the service target is ${changeSurface.data.resolution.status}`;
  }
  if (changeSurface.data.resolution.selected?.id !== deploymentTrace.data.workloadId) {
    return "deployment topology not selected because trace and change-surface workload identities disagree";
  }
  return undefined;
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
