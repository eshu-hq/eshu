import type { EshuTruth } from "./envelope";
import {
  addIsolatedRecords,
  addOmissionSummary,
  artifactAdmissionStatus,
  artifactEvidence,
  compact,
  encodeKey,
  graphTruth,
  materializationEvidence,
  omissionContract,
  repoNode,
  repoRef,
  summaryNode,
  truthEvidence,
  truthIsCurrent,
} from "./eshuGraphDeploymentPresentation";
import {
  mergeDeploymentInstances,
  uniqueDeploymentArtifacts,
  uniqueNamedRecords,
  uniqueNetworkPaths,
} from "./eshuGraphDeploymentWire";
import type {
  DeploymentGraphDetail,
  DeploymentTraceResponse,
  ServiceDeploymentContextResponse,
} from "./eshuGraphDeploymentWire";
import { cleanText, layerFor } from "./eshuGraphShared";
import type { GraphEdge, GraphModel, GraphNode } from "../console/types";

interface BuildOptions {
  readonly contextTruth?: EshuTruth | null;
  readonly detail?: DeploymentGraphDetail;
  readonly traceTruth?: EshuTruth | null;
}

interface FamilyLimits {
  readonly artifacts: number;
  readonly cloud: number;
  readonly entrypoints: number;
  readonly instances: number;
  readonly k8sRelationships: number;
  readonly k8sResources: number;
  readonly networkPaths: number;
  readonly platformPlacements: number;
  readonly sources: number;
}

const SUMMARY_LIMITS: FamilyLimits = {
  artifacts: 3,
  cloud: 1,
  entrypoints: 1,
  instances: 6,
  k8sRelationships: 2,
  k8sResources: 4,
  networkPaths: 1,
  platformPlacements: 6,
  sources: 2,
};
const EXPANDED_LIMITS: FamilyLimits = {
  artifacts: 4,
  cloud: 1,
  entrypoints: 1,
  instances: 14,
  k8sRelationships: 2,
  k8sResources: 4,
  networkPaths: 1,
  platformPlacements: 14,
  sources: 3,
};

export function buildDeploymentStoryGraph(
  context: ServiceDeploymentContextResponse,
  fallbackName: string,
  trace: DeploymentTraceResponse,
  options: BuildOptions,
): GraphModel {
  const detail = options.detail ?? "summary";
  const limits = detail === "expanded" ? EXPANDED_LIMITS : SUMMARY_LIMITS;
  const maxNodes = detail === "expanded" ? 60 : 42;
  const maxEdges = detail === "expanded" ? 90 : 48;
  const nodes = new Map<string, GraphNode>();
  const edges: GraphEdge[] = [];
  const edgeKeys = new Set<string>();
  const summaries: GraphNode[] = [];
  const serviceName = cleanText(trace.service_name) || cleanText(context.name) || fallbackName;
  const workloadID =
    cleanText(trace.workload_id) || cleanText(context.id) || `workload:${serviceName}`;
  const truth = graphTruth(options.traceTruth ?? options.contextTruth);
  const addNode = (node: GraphNode): boolean => {
    if (nodes.has(node.id)) return true;
    if (nodes.size >= maxNodes - 8) return false;
    nodes.set(node.id, node);
    return true;
  };
  const addEdge = (edge: GraphEdge): void => {
    if (edges.length >= maxEdges || !nodes.has(edge.s) || !nodes.has(edge.t)) return;
    const key = [
      edge.s,
      edge.t,
      edge.verb,
      edge.sourceFamily ?? "",
      edge.method ?? "",
      ...(edge.evidence ?? []),
    ].join("\u0000");
    if (edgeKeys.has(key)) return;
    edgeKeys.add(key);
    edges.push(edge);
  };

  addNode({
    col: 2,
    hero: true,
    id: workloadID,
    kind: "workload",
    label: serviceName,
    sub: workloadID,
    truth,
  });

  const contextArtifacts = context.deployment_evidence?.artifacts ?? [];
  const traceArtifacts = trace.deployment_evidence?.artifacts ?? [];
  const artifacts = uniqueDeploymentArtifacts(
    contextArtifacts.length > 0 ? contextArtifacts : traceArtifacts,
  );
  const artifactTruth = contextArtifacts.length > 0 ? options.contextTruth : options.traceTruth;
  let notAdmitted = 0;
  artifacts.slice(0, limits.artifacts).forEach((artifact) => {
    const status = truthIsCurrent(artifactTruth)
      ? artifactAdmissionStatus(artifact)
      : `Deployment evidence · ${artifactTruth?.freshness.state ?? "stale"} · relationship not admitted`;
    const source = repoRef(artifact.source_repo_id, artifact.source_repo_name);
    const target = repoRef(artifact.target_repo_id, artifact.target_repo_name);
    if (source)
      addNode(
        repoNode(
          source,
          status === "admitted" ? "Deployment evidence" : status,
          cleanText(artifact.path) || undefined,
        ),
      );
    if (target) addNode(repoNode(target, "Deployment target"));
    if (status !== "admitted") {
      notAdmitted += 1;
      return;
    }
    const verb = cleanText(artifact.relationship_type).toUpperCase();
    if (!source || !target || verb === "") {
      notAdmitted += 1;
      return;
    }
    addEdge({
      evidence: [...artifactEvidence(artifact), ...truthEvidence(artifactTruth)],
      layer: layerFor(verb),
      method: cleanText(artifact.resolution_source) || undefined,
      s: source.id,
      sourceFamily: cleanText(artifact.artifact_family) || undefined,
      t: target.id,
      truthState: "derived",
      verb,
    });
  });
  const omittedArtifacts = Math.max(0, artifacts.length - limits.artifacts);
  addOmissionSummary(
    summaries,
    "deployment artifacts",
    omittedArtifacts,
    "artifacts",
    omissionContract(
      limits.artifacts,
      artifacts
        .slice(limits.artifacts)
        .map(
          (artifact) => cleanText(artifact.artifact_family) || cleanText(artifact.evidence_kind),
        ),
    ),
  );
  if (notAdmitted > 0) {
    summaries.push(
      summaryNode("not_admitted", `${notAdmitted} deployment relationships not admitted`),
    );
  }

  const sources = trace.deployment_sources ?? [];
  sources.slice(0, limits.sources).forEach((source) => {
    const repo = repoRef(source.repo_id, source.repo_name);
    if (!repo || nodes.has(repo.id)) return;
    addNode({
      ...repoNode(repo, "Deployment source · relationship endpoints not exposed by API"),
      truth: graphTruth(options.traceTruth),
    });
  });
  addOmissionSummary(
    summaries,
    "deployment sources",
    Math.max(0, sources.length - limits.sources),
    "sources",
    omissionContract(
      limits.sources,
      sources
        .slice(limits.sources)
        .map((source) => cleanText(source.repo_name) || cleanText(source.repo_id)),
    ),
  );

  const instances = mergeDeploymentInstances(context.instances ?? [], trace.instances ?? []);
  const contextInstanceIDs = new Set(
    (context.instances ?? []).map((instance) => cleanText(instance.instance_id)),
  );
  const traceInstanceIDs = new Set(
    (trace.instances ?? []).map((instance) => cleanText(instance.instance_id)),
  );
  let platformPlacements = 0;
  let staleInstanceRelationships = 0;
  instances.slice(0, limits.instances).forEach((instance) => {
    const instanceID = cleanText(instance.instance_id);
    if (instanceID === "") return;
    const environment = cleanText(instance.environment);
    const instanceTruth = contextInstanceIDs.has(instanceID)
      ? options.contextTruth
      : options.traceTruth;
    const platformTruth = traceInstanceIDs.has(instanceID)
      ? options.traceTruth
      : options.contextTruth;
    const instanceAdded = addNode({
      col: 3,
      id: instanceID,
      kind: "instance",
      label: environment || instanceID,
      sub: environment === "" ? `${instanceID} · environment not provided` : instanceID,
      truth: graphTruth(instanceTruth),
    });
    if (!instanceAdded) return;
    if (truthIsCurrent(instanceTruth)) {
      addEdge({
        evidence: [...materializationEvidence(instance), ...truthEvidence(instanceTruth)],
        layer: "runtime",
        s: instanceID,
        t: workloadID,
        truthState: "derived",
        verb: "INSTANCE_OF",
      });
    } else {
      staleInstanceRelationships += 1;
    }
    (instance.platforms ?? []).forEach((platform) => {
      if (platformPlacements >= limits.platformPlacements) return;
      const platformName = cleanText(platform.platform_name);
      const platformKind = cleanText(platform.platform_kind);
      if (platformName === "" && platformKind === "") return;
      const platformID = `platform:${encodeKey(platformKind || "unknown")}:${encodeKey(platformName || "unknown")}`;
      if (
        !addNode({
          col: 4,
          id: platformID,
          kind: platformKind || "platform",
          label: platformName || platformKind,
          sub: `${platformKind || "platform"} · canonical platform identity not exposed by API`,
          truth: graphTruth(options.traceTruth),
        })
      )
        return;
      platformPlacements += 1;
      if (truthIsCurrent(platformTruth)) {
        addEdge({
          evidence: [
            ...compact([
              platformKind ? `platform kind: ${platformKind}` : "",
              cleanText(platform.platform_reason)
                ? `reason: ${cleanText(platform.platform_reason)}`
                : "",
            ]),
            ...truthEvidence(platformTruth),
          ],
          layer: "runtime",
          s: instanceID,
          t: platformID,
          truthState: "derived",
          verb: "RUNS_ON",
        });
      } else {
        staleInstanceRelationships += 1;
      }
    });
  });
  addOmissionSummary(
    summaries,
    "instances",
    Math.max(0, instances.length - limits.instances),
    "instances",
    omissionContract(
      limits.instances,
      instances
        .slice(limits.instances)
        .map((instance) => cleanText(instance.environment) || cleanText(instance.instance_id)),
    ),
  );
  const totalPlatformPlacements = instances.reduce(
    (total, instance) => total + (instance.platforms?.length ?? 0),
    0,
  );
  addOmissionSummary(
    summaries,
    "platform placements",
    Math.max(0, totalPlatformPlacements - platformPlacements),
    "platforms",
    omissionContract(
      limits.platformPlacements,
      instances.flatMap((instance) =>
        (instance.platforms ?? []).map((platform) => cleanText(platform.platform_kind)),
      ),
    ),
  );
  if (staleInstanceRelationships > 0) {
    summaries.push(
      summaryNode(
        "stale_instances",
        `${staleInstanceRelationships} stale instance relationships not admitted`,
      ),
    );
  }

  const k8sResources = trace.k8s_resources ?? [];
  k8sResources.slice(0, limits.k8sResources).forEach((resource) => {
    const id = cleanText(resource.entity_id);
    if (id === "") return;
    addNode({
      col: 4,
      id,
      kind: cleanText(resource.kind) || "kubernetes",
      label: cleanText(resource.entity_name) || id,
      sub:
        cleanText(resource.qualified_name) ||
        cleanText(resource.relative_path) ||
        "Kubernetes resource",
      truth: graphTruth(options.traceTruth),
    });
  });
  addOmissionSummary(
    summaries,
    "Kubernetes resources",
    Math.max(0, k8sResources.length - limits.k8sResources),
    "k8s_resources",
    omissionContract(
      limits.k8sResources,
      k8sResources.slice(limits.k8sResources).map((resource) => cleanText(resource.kind)),
    ),
  );
  const k8sRelationships = trace.k8s_relationships ?? [];
  k8sRelationships.slice(0, limits.k8sRelationships).forEach((relationship) => {
    const sourceID = cleanText(relationship.source_id);
    const targetID = cleanText(relationship.target_id);
    const verb = cleanText(relationship.type).toUpperCase();
    if (sourceID === "" || targetID === "" || verb === "") return;
    if (!nodes.has(sourceID)) {
      addNode({
        col: 4,
        id: sourceID,
        kind: "kubernetes",
        label: cleanText(relationship.source_name) || sourceID,
        truth,
      });
    }
    if (!nodes.has(targetID)) {
      addNode({
        col: 4,
        id: targetID,
        kind: "kubernetes",
        label: cleanText(relationship.target_name) || targetID,
        truth,
      });
    }
    if (truthIsCurrent(options.traceTruth)) {
      addEdge({
        evidence: [
          ...compact([
            cleanText(relationship.reason) ? `reason: ${cleanText(relationship.reason)}` : "",
          ]),
          ...truthEvidence(options.traceTruth),
        ],
        layer: layerFor(verb),
        s: sourceID,
        t: targetID,
        truthState: "derived",
        verb,
      });
    }
  });
  addOmissionSummary(
    summaries,
    "Kubernetes relationships",
    Math.max(0, k8sRelationships.length - limits.k8sRelationships),
    "k8s_relationships",
    omissionContract(
      limits.k8sRelationships,
      k8sRelationships
        .slice(limits.k8sRelationships)
        .map((relationship) => cleanText(relationship.type)),
    ),
  );

  const paths = uniqueNetworkPaths([
    ...(context.network_paths ?? []),
    ...(trace.network_paths ?? []),
  ]);
  const pathTruth =
    (context.network_paths?.length ?? 0) > 0 ? options.contextTruth : options.traceTruth;
  paths.slice(0, limits.networkPaths).forEach((path, index) => {
    const from = cleanText(path.from);
    const to = cleanText(path.to);
    const verb = cleanText(path.path_type).toUpperCase();
    if (from === "" || to === "" || verb === "") return;
    const fromID = `network:${index}:from:${encodeKey(from)}`;
    const toID = `network:${index}:to:${encodeKey(to)}`;
    addNode({
      col: 4,
      id: fromID,
      kind: cleanText(path.from_type) || "network",
      label: from,
      truth,
    });
    addNode({ col: 5, id: toID, kind: cleanText(path.to_type) || "network", label: to, truth });
    if (truthIsCurrent(pathTruth)) {
      addEdge({
        evidence: [
          ...compact([
            cleanText(path.environment) ? `environment: ${cleanText(path.environment)}` : "",
            cleanText(path.reason) ? `reason: ${cleanText(path.reason)}` : "",
            cleanText(path.visibility) ? `visibility: ${cleanText(path.visibility)}` : "",
          ]),
          ...truthEvidence(pathTruth),
        ],
        layer: "runtime",
        s: fromID,
        t: toID,
        truthState: "derived",
        verb,
      });
    }
  });
  addOmissionSummary(
    summaries,
    "network paths",
    Math.max(0, paths.length - limits.networkPaths),
    "network_paths",
    omissionContract(
      limits.networkPaths,
      paths.slice(limits.networkPaths).map((path) => cleanText(path.path_type)),
    ),
  );

  const entrypoints = uniqueNamedRecords([
    ...(context.entrypoints ?? []),
    ...(trace.entrypoints ?? []),
  ]);
  addIsolatedRecords(nodes, entrypoints, limits.entrypoints, "entrypoint", 4, maxNodes - 8, truth);
  addOmissionSummary(
    summaries,
    "entrypoints",
    Math.max(0, entrypoints.length - limits.entrypoints),
    "entrypoints",
    omissionContract(
      limits.entrypoints,
      entrypoints.slice(limits.entrypoints).map((entrypoint) => cleanText(entrypoint.type)),
    ),
  );
  addIsolatedRecords(
    nodes,
    trace.cloud_resources ?? [],
    limits.cloud,
    "cloud",
    5,
    maxNodes - 8,
    truth,
  );
  addOmissionSummary(
    summaries,
    "cloud resources",
    Math.max(0, (trace.cloud_resources?.length ?? 0) - limits.cloud),
    "cloud",
    omissionContract(
      limits.cloud,
      (trace.cloud_resources ?? []).slice(limits.cloud).map((resource) => cleanText(resource.kind)),
    ),
  );

  const endpointCount =
    context.api_surface?.endpoint_count ?? context.api_surface?.endpoints?.length ?? 0;
  if (endpointCount > 0)
    summaries.unshift(summaryNode("api_endpoints", `${endpointCount} API endpoints aggregated`));
  summaries.slice(0, maxNodes - nodes.size).forEach((node) => nodes.set(node.id, node));
  return { edges, nodes: [...nodes.values()] };
}
