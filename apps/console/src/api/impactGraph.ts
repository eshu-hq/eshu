import type { ChangeSurfaceImpactNode, ChangeSurfaceInvestigation } from "./changeSurface";
import { deploymentTraceGraph } from "./impactDeploymentGraph";
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
      [],
      boundedSourceCompleteness(blast.data.truncated),
    );
  }
  const traceSelectionLimitation = deploymentTraceSelectionLimitation(
    targetKind,
    changeSurface,
    deploymentTrace,
  );
  if (traceSelectionLimitation !== undefined) {
    if (changeSurface.status === "ready") {
      return existingGraph(
        changeSurfaceGraph(target, changeSurface.data),
        "change_surface",
        [changeSurface.source, deploymentTrace.source],
        "Change surface",
        changeSurface.truth,
        [traceSelectionLimitation],
        boundedSourceCompleteness(changeSurface.data.truncated),
      );
    }
    return existingGraph(
      { edges: [], nodes: [] },
      "empty",
      [changeSurface.source, deploymentTrace.source],
      "Impact graph",
      null,
      [traceSelectionLimitation],
      "unverified",
    );
  }
  if (changeSurface.status === "ready" && changeSurface.data.impact.totalCount > 0) {
    return existingGraph(
      changeSurfaceGraph(target, changeSurface.data),
      "change_surface",
      [changeSurface.source],
      "Change surface",
      changeSurface.truth,
      [],
      boundedSourceCompleteness(changeSurface.data.truncated),
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
    if (deployment.presentation.limitations.length > 0) {
      if (changeSurface.status === "ready") {
        return existingGraph(
          changeSurfaceGraph(target, changeSurface.data),
          "change_surface",
          [changeSurface.source, deploymentTrace.source],
          "Change surface",
          changeSurface.truth,
          deployment.presentation.limitations,
          boundedSourceCompleteness(
            changeSurface.data.truncated,
            deployment.presentation.completeness,
          ),
          changeSurface.data.truncated || deployment.presentation.truncated,
        );
      }
      return existingGraph(
        { edges: [], nodes: [] },
        "empty",
        [deploymentTrace.source],
        "Impact graph",
        deploymentTrace.truth,
        deployment.presentation.limitations,
        deployment.presentation.completeness,
        deployment.presentation.truncated,
      );
    }
  }
  if (changeSurface.status === "ready") {
    return existingGraph(
      changeSurfaceGraph(target, changeSurface.data),
      "change_surface",
      [changeSurface.source],
      "Change surface",
      changeSurface.truth,
      [],
      boundedSourceCompleteness(changeSurface.data.truncated),
    );
  }
  if (blast.status === "ready") {
    return existingGraph(
      blast.data.graph,
      "blast_radius",
      [blast.source],
      "Blast radius",
      blast.truth,
      [],
      boundedSourceCompleteness(blast.data.truncated),
    );
  }
  return existingGraph(
    { edges: [], nodes: [] },
    "empty",
    [],
    "Impact graph",
    null,
    [],
    "unverified",
  );
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
  completeness: ImpactGraphPresentation["completeness"] = "complete",
  truncated = completeness === "truncated",
): { readonly graph: GraphModel; readonly presentation: ImpactGraphPresentation } {
  const activeLimitations = new Set(limitations);
  if (truncated) activeLimitations.add(`${title.toLowerCase()} input truncated upstream`);
  return {
    graph,
    presentation: {
      completeness,
      compositionDurationMs: 0,
      duplicateEdges: 0,
      duplicateNodes: 0,
      edgeLimit,
      freshness: truth?.freshness.state,
      inputEdges: graph.edges.length,
      inputNodes: graph.nodes.length,
      limitations: [...activeLimitations],
      mode,
      nodeLimit,
      omittedEdges: 0,
      omittedNodes: 0,
      renderedEdges: graph.edges.length,
      renderedNodes: graph.nodes.length,
      sourceApis,
      title,
      truncated,
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
    deploymentTrace.status !== "ready"
  ) {
    return undefined;
  }
  if (changeSurface.status !== "ready") {
    return "deployment topology not selected because exact service identity verification is unavailable";
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
  const selectedRepoID =
    changeSurface.data.resolution.selected?.repoId.trim() || changeSurface.data.scope.repoId.trim();
  if (selectedRepoID.length > 0 && selectedRepoID !== deploymentTrace.data.repoId) {
    return "deployment topology not selected because trace and change-surface repository identities disagree";
  }
  return undefined;
}

function boundedSourceCompleteness(
  truncated: boolean,
  inherited: ImpactGraphPresentation["completeness"] = "complete",
): ImpactGraphPresentation["completeness"] {
  if (inherited === "unverified") return "unverified";
  if (truncated || inherited === "truncated") return "truncated";
  return "complete";
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
