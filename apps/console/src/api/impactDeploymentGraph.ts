import { boundedCollectionGraphAccounting } from "./impactBoundedCollectionLimits";
import { requiredTopologyRelationshipLimitations } from "./impactDeploymentCompleteness";
import { boundedGraph } from "./impactGraphSelection";
import type {
  DeploymentTraceResult,
  DeploymentTraceTopologyEdge,
  ImpactGraphPresentation,
} from "./impactReviewTypes";
import { runtimeTopologyGraphAccounting } from "./impactRuntimeTopologyLimits";
import type { GraphEdge, GraphLayer, GraphModel, GraphNode, UiTruth } from "../console/types";

export function deploymentTraceGraph(
  trace: DeploymentTraceResult,
  truth: UiTruth,
): {
  readonly graph: GraphModel;
  readonly presentation: ImpactGraphPresentation;
} {
  const rawNodes: GraphNode[] = [];
  const rawEdges: GraphEdge[] = [];
  const limitations = new Set<string>();
  const knownNodeIDs = new Set<string>();
  let identityOmittedNodes = 0;
  let identityOmittedEdges = 0;
  let hasUnverifiedOmission = false;
  const instanceIDs = new Set(trace.instances.map((instance) => instance.id));

  const addNode = (node: GraphNode | null, limitation: string): boolean => {
    if (node === null || node.id.trim().length === 0) {
      identityOmittedNodes += 1;
      hasUnverifiedOmission = true;
      limitations.add(limitation);
      return false;
    }
    rawNodes.push(node);
    knownNodeIDs.add(node.id);
    return true;
  };
  const addEdge = (edge: GraphEdge | null, limitation: string): void => {
    if (edge === null || edge.s.length === 0 || edge.t.length === 0) {
      identityOmittedEdges += 1;
      hasUnverifiedOmission = true;
      limitations.add(limitation);
      return;
    }
    rawEdges.push(edge);
  };
  const addTopologyEdge = (edge: DeploymentTraceTopologyEdge): void => {
    const sourceID = edge.sourceId ?? "";
    const targetID = edge.targetId ?? "";
    const endpointLimitation = `${edge.relationshipType} edge omitted because an endpoint lacks canonical identity`;
    if (sourceID.length === 0 || targetID.length === 0) {
      addEdge(null, endpointLimitation);
      return;
    }
    if (!knownNodeIDs.has(sourceID)) {
      addNode(topologyEndpoint(sourceID, edge.sourceName, truth), endpointLimitation);
    }
    if (!knownNodeIDs.has(targetID)) {
      addNode(topologyEndpoint(targetID, edge.targetName, truth), endpointLimitation);
    }
    addEdge(topologyGraphEdge(edge), endpointLimitation);
  };

  addSubjectNodes(trace, truth, addNode);
  addDeploymentSources(trace, truth, instanceIDs, addNode, addEdge);

  for (const instance of trace.instances) {
    addNode(
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
    for (const platform of instance.platforms) {
      addPlatformNode(platform.id, platform.kind, platform.name, truth, addNode);
      for (const topologyEdge of platform.topologyEdges) {
        if (!hasTopologyEndpoints(topologyEdge)) {
          addTopologyEdge(topologyEdge);
        } else if (
          topologyEdge.relationshipType === "RUNS_ON" &&
          topologyEdge.sourceId === instance.id &&
          topologyEdge.targetId === platform.id
        ) {
          addTopologyEdge(topologyEdge);
        } else {
          addEdge(
            null,
            "RUNS_ON edge omitted because it does not match the containing instance and platform",
          );
        }
      }
    }
  }

  for (const edge of trace.topologyEdges) {
    if (!hasTopologyEndpoints(edge)) {
      addTopologyEdge(edge);
    } else if (edge.relationshipType === "DEFINES") {
      if (edge.sourceId === trace.repoId && edge.targetId === trace.workloadId) {
        addTopologyEdge(edge);
      } else {
        addEdge(
          null,
          "DEFINES edge omitted because it does not match the selected repository and workload",
        );
      }
    } else if (edge.relationshipType === "INSTANCE_OF") {
      if (edge.sourceId !== undefined && instanceIDs.has(edge.sourceId)) {
        if (edge.targetId === trace.workloadId) {
          addTopologyEdge(edge);
          continue;
        }
      }
      addEdge(
        null,
        "INSTANCE_OF edge omitted because it does not match a returned runtime instance",
      );
    } else {
      addEdge(
        null,
        "subject topology edge omitted because it is not a DEFINES or INSTANCE_OF relationship",
      );
    }
  }
  for (const platform of trace.provisionedPlatforms) {
    addPlatformNode(platform.id, platform.kind, platform.name, truth, addNode);
    for (const topologyEdge of platform.topologyEdges) {
      if (!hasTopologyEndpoints(topologyEdge)) {
        addTopologyEdge(topologyEdge);
        continue;
      }
      const exactDependency =
        topologyEdge.relationshipType === "PROVISIONS_DEPENDENCY_FOR" &&
        topologyEdge.sourceId?.startsWith("repository:") === true &&
        topologyEdge.targetId === trace.repoId;
      const exactPlatform =
        topologyEdge.relationshipType === "PROVISIONS_PLATFORM" &&
        topologyEdge.sourceId?.startsWith("repository:") === true &&
        topologyEdge.targetId === platform.id;
      if (exactDependency || exactPlatform) {
        addTopologyEdge(topologyEdge);
      } else {
        addEdge(
          null,
          "provisioning edge omitted because it does not match the selected repository or platform",
        );
      }
    }
  }

  const missingRequiredRelationships = requiredTopologyRelationshipLimitations(trace);
  if (missingRequiredRelationships.length > 0) {
    hasUnverifiedOmission = true;
  }
  for (const limitation of missingRequiredRelationships) limitations.add(limitation);
  if (trace.deploymentSourceLimits === null) {
    limitations.add(
      "deployment-source completeness unverified because coverage metadata is unavailable",
    );
  } else if (trace.deploymentSourceLimits.truncated) {
    const upstreamOmittedEdges = Math.max(
      1,
      trace.deploymentSourceLimits.observedCount - trace.deploymentSourceLimits.returnedCount,
    );
    identityOmittedEdges += upstreamOmittedEdges;
    const quantity = trace.deploymentSourceLimits.observedCountIsLowerBound
      ? `at least ${upstreamOmittedEdges}`
      : String(upstreamOmittedEdges);
    limitations.add(
      `deployment-source input truncated upstream; ${quantity} ${upstreamOmittedEdges === 1 ? "relationship was" : "relationships were"} not returned`,
    );
  }
  const invalidTopologyEdgeCount = trace.invalidTopologyEdgeCount ?? 0;
  if (invalidTopologyEdgeCount > 0) {
    identityOmittedEdges += invalidTopologyEdgeCount;
    hasUnverifiedOmission = true;
    limitations.add(
      `${invalidTopologyEdgeCount} topology rows omitted because their relationship shape is unsupported or malformed`,
    );
  }

  const runtimeAccounting = runtimeTopologyGraphAccounting(trace.runtimeTopologyLimits);
  identityOmittedNodes += runtimeAccounting.omittedNodes;
  identityOmittedEdges += runtimeAccounting.omittedEdges;
  for (const limitation of runtimeAccounting.limitations) limitations.add(limitation);

  const cloudResourceAccounting = boundedCollectionGraphAccounting(trace.cloudResourceLimits, {
    edgeMultiplier: 0,
    family: "cloud-resource",
    familyPlural: "cloud resources",
    item: "resource",
    missingMetadataLimitation:
      "cloud-resource completeness unverified because collection metadata is unavailable",
    nodeMultiplier: 1,
  });
  const k8sResourceAccounting = boundedCollectionGraphAccounting(trace.k8sResourceLimits, {
    edgeMultiplier: 0,
    family: "Kubernetes-resource",
    familyPlural: "Kubernetes resources",
    item: "resource",
    missingMetadataLimitation:
      "Kubernetes-resource completeness unverified because collection metadata is unavailable",
    nodeMultiplier: 1,
  });
  identityOmittedNodes += cloudResourceAccounting.omittedNodes + k8sResourceAccounting.omittedNodes;
  for (const limitation of cloudResourceAccounting.limitations) limitations.add(limitation);
  for (const limitation of k8sResourceAccounting.limitations) limitations.add(limitation);

  addEvidenceNodes(trace, truth, addNode, limitations);
  const bounded = boundedGraph(
    rawNodes,
    rawEdges,
    identityOmittedNodes,
    identityOmittedEdges,
    limitations,
  );
  const completenessVerified =
    trace.runtimeTopologyLimits !== null &&
    trace.deploymentSourceLimits !== null &&
    trace.cloudResourceLimits !== null &&
    trace.k8sResourceLimits !== null &&
    !hasUnverifiedOmission;
  const truncated =
    bounded.presentation.truncated ||
    runtimeAccounting.truncated ||
    cloudResourceAccounting.truncated ||
    k8sResourceAccounting.truncated ||
    trace.deploymentSourceLimits?.truncated === true;
  return {
    graph: bounded.graph,
    presentation: {
      ...bounded.presentation,
      completeness: completenessVerified ? (truncated ? "truncated" : "complete") : "unverified",
      truncated,
    },
  };
}

function addSubjectNodes(
  trace: DeploymentTraceResult,
  truth: UiTruth,
  addNode: (node: GraphNode | null, limitation: string) => boolean,
): void {
  addNode(
    trace.workloadId.length > 0
      ? {
          col: 2,
          hero: true,
          id: trace.workloadId,
          kind: "workload",
          label: trace.serviceName || trace.workloadId,
          sub: "deployment subject",
          truth,
        }
      : null,
    "workload omitted because the trace has no canonical workload_id",
  );
  addNode(
    trace.repoId.length > 0
      ? {
          col: 1,
          id: trace.repoId,
          kind: "repo",
          label: trace.repoName || trace.repoId,
          sub: "source repository",
          truth,
        }
      : null,
    "source repository omitted because the trace has no canonical repo_id",
  );
}

function addDeploymentSources(
  trace: DeploymentTraceResult,
  truth: UiTruth,
  instanceIDs: ReadonlySet<string>,
  addNode: (node: GraphNode | null, limitation: string) => boolean,
  addEdge: (edge: GraphEdge | null, limitation: string) => void,
): void {
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
      const evidence = source.detail ? [source.detail] : [];
      addEdge(
        source.sourceId === configRepoID && source.targetId === trace.repoId
          ? {
              ...(evidence.length > 0 ? { evidence } : {}),
              layer: "deploy",
              s: source.sourceId,
              sourceFamily: "deployment_trace",
              t: source.targetId,
              verb: "DEPLOYS_FROM",
            }
          : null,
        "DEPLOYS_FROM edge omitted because its exact endpoints do not match the trace repositories",
      );
    } else if (source.relationshipType === "DEPLOYMENT_SOURCE") {
      const evidence = source.detail ? [source.detail] : [];
      addEdge(
        source.sourceId !== undefined &&
          instanceIDs.has(source.sourceId) &&
          source.targetId === configRepoID
          ? {
              ...(evidence.length > 0 ? { evidence } : {}),
              layer: "deploy",
              s: source.sourceId,
              sourceFamily: "deployment_trace",
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
}

function addPlatformNode(
  id: string | undefined,
  kind: string | undefined,
  name: string,
  truth: UiTruth,
  addNode: (node: GraphNode | null, limitation: string) => boolean,
): void {
  addNode(
    id
      ? {
          col: 5,
          id,
          kind: "platform",
          label: name,
          sub: kind ?? "runtime platform",
          truth,
        }
      : null,
    "runtime platform omitted because it has no canonical platform_id",
  );
}

function addEvidenceNodes(
  trace: DeploymentTraceResult,
  truth: UiTruth,
  addNode: (node: GraphNode | null, limitation: string) => boolean,
  limitations: Set<string>,
): void {
  for (const resource of trace.cloudResources) {
    addNode(
      resource.id
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
      resource.id
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
}

function topologyGraphEdge(edge: DeploymentTraceTopologyEdge): GraphEdge {
  const evidence = [edge.evidenceSource, edge.reason, edge.sourceTool].filter(isPresent);
  return {
    ...(evidence.length > 0 ? { evidence } : {}),
    layer: topologyLayer(edge),
    ...(edge.sourceTool ? { method: edge.sourceTool } : {}),
    s: edge.sourceId ?? "",
    ...(edge.evidenceSource ? { sourceFamily: edge.evidenceSource } : {}),
    t: edge.targetId ?? "",
    verb: edge.relationshipType,
  };
}

function hasTopologyEndpoints(edge: DeploymentTraceTopologyEdge): boolean {
  return (edge.sourceId?.length ?? 0) > 0 && (edge.targetId?.length ?? 0) > 0;
}

function topologyEndpoint(id: string, name: string | undefined, truth: UiTruth): GraphNode {
  const kind = topologyKind(id);
  return {
    col: topologyColumn(kind),
    id,
    kind,
    label: name ?? id,
    sub: "exact topology endpoint",
    truth,
  };
}

function topologyKind(id: string): string {
  if (id.startsWith("repository:")) return "repo";
  if (id.startsWith("instance:") || id.startsWith("workload-instance:")) return "instance";
  if (id.startsWith("platform:")) return "platform";
  if (id.startsWith("workload:")) return "workload";
  return "service";
}

function topologyColumn(kind: string): number {
  if (kind === "repo") return 1;
  if (kind === "workload") return 2;
  if (kind === "instance") return 3;
  if (kind === "platform") return 5;
  return 4;
}

function topologyLayer(edge: DeploymentTraceTopologyEdge): GraphLayer {
  if (edge.relationshipType === "DEFINES") return "code";
  if (edge.relationshipType === "INSTANCE_OF" || edge.relationshipType === "RUNS_ON") {
    return "runtime";
  }
  return "infra";
}

function isPresent(value: string | undefined): value is string {
  return value !== undefined && value.length > 0;
}
