import { addDeploymentArtifactGraph } from "./eshuGraphDeploymentArtifacts";
import { deploymentFamilyLimits, deploymentGraphBounds } from "./eshuGraphDeploymentLimits";
import type { DeploymentGraphBuildOptions } from "./eshuGraphDeploymentLimits";
import {
  addIsolatedRecords,
  addOmissionSummary,
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
import * as deploymentProvenance from "./eshuGraphDeploymentProvenance";
import {
  mergeDeploymentInstances,
  uniqueNamedRecords,
  uniqueNetworkPaths,
} from "./eshuGraphDeploymentWire";
import type {
  DeploymentTraceResponse,
  ServiceDeploymentContextResponse,
} from "./eshuGraphDeploymentWire";
import { cleanText, layerFor } from "./eshuGraphShared";
import type { GraphEdge, GraphModel, GraphNode } from "../console/types";

export function buildDeploymentStoryGraph(
  context: ServiceDeploymentContextResponse,
  fallbackName: string,
  trace: DeploymentTraceResponse,
  options: DeploymentGraphBuildOptions,
): GraphModel {
  const detail = options.detail ?? "summary";
  const limits = deploymentFamilyLimits(detail);
  const { maxEdges, maxNodes } = deploymentGraphBounds(detail);
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
    if (nodes.size >= maxNodes - 12) return false;
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
  addDeploymentArtifactGraph({
    addEdge,
    addNode,
    contextArtifacts: context.deployment_evidence?.artifacts ?? [],
    contextTruth: options.contextTruth,
    limit: limits.artifacts,
    summaries,
    traceArtifacts: trace.deployment_evidence?.artifacts ?? [],
    traceTruth: options.traceTruth,
  });
  const sources = trace.deployment_sources ?? [];
  sources.slice(0, limits.sources).forEach((source) => {
    const repo = repoRef(source.repo_id, source.repo_name);
    if (!repo || nodes.has(repo.id)) return;
    addNode({
      ...repoNode(
        repo,
        compact([
          "Deployment source",
          cleanText(source.reason) ? `reason: ${cleanText(source.reason)}` : "",
          source.confidence !== undefined ? `confidence: ${source.confidence}` : "",
          "relationship endpoints not exposed by API",
        ]).join(" · "),
      ),
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
  const instanceSources = {
    contextRows: context.instances ?? [],
    contextTruth: options.contextTruth,
    traceRows: trace.instances ?? [],
    traceTruth: options.traceTruth,
  };
  let platformPlacements = 0;
  let staleRelationships = 0;
  instances.slice(0, limits.instances).forEach((instance) => {
    const instanceID = cleanText(instance.instance_id);
    if (instanceID === "") return;
    const environment = cleanText(instance.environment);
    const instanceTruth = deploymentProvenance.instanceRecordTruth(instanceID, instanceSources);
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
      staleRelationships += 1;
    }
    (instance.platforms ?? []).forEach((platform) => {
      if (platformPlacements >= limits.platformPlacements) return;
      const platformName = cleanText(platform.platform_name);
      const platformKind = cleanText(platform.platform_kind);
      if (platformName === "" && platformKind === "") return;
      const platformTruth = deploymentProvenance.platformRecordTruth(
        instanceID,
        platform,
        instanceSources,
      );
      const platformID = `platform:${encodeKey(platformKind || "unknown")}:${encodeKey(platformName || "unknown")}`;
      if (
        !addNode({
          col: 4,
          id: platformID,
          kind: platformKind || "platform",
          label: platformName || platformKind,
          sub: `${platformKind || "platform"} · canonical platform identity not exposed by API`,
          truth: graphTruth(platformTruth),
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
              platform.platform_confidence !== undefined
                ? `confidence: ${platform.platform_confidence}`
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
        staleRelationships += 1;
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
        truth: graphTruth(options.traceTruth),
      });
    }
    if (!nodes.has(targetID)) {
      addNode({
        col: 4,
        id: targetID,
        kind: "kubernetes",
        label: cleanText(relationship.target_name) || targetID,
        truth: graphTruth(options.traceTruth),
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
    } else {
      staleRelationships += 1;
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
  const pathSources = {
    contextRows: context.network_paths ?? [],
    contextTruth: options.contextTruth,
    traceRows: trace.network_paths ?? [],
    traceTruth: options.traceTruth,
  };
  const paths = deploymentProvenance.currentRecordsFirst(
    uniqueNetworkPaths([...pathSources.contextRows, ...pathSources.traceRows]),
    (path) => deploymentProvenance.networkPathRecordTruth(path, pathSources),
  );
  paths.slice(0, limits.networkPaths).forEach((path, index) => {
    const pathTruth = deploymentProvenance.networkPathRecordTruth(path, pathSources);
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
      truth: graphTruth(pathTruth),
    });
    addNode({
      col: 5,
      id: toID,
      kind: cleanText(path.to_type) || "network",
      label: to,
      truth: graphTruth(pathTruth),
    });
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
    } else {
      staleRelationships += 1;
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
  if (staleRelationships > 0) {
    summaries.push(
      summaryNode(
        "stale_relationships",
        `${staleRelationships} stale deployment relationships not admitted`,
      ),
    );
  }
  const entrypointSources = {
    contextRows: context.entrypoints ?? [],
    contextTruth: options.contextTruth,
    traceRows: trace.entrypoints ?? [],
    traceTruth: options.traceTruth,
  };
  const entrypoints = deploymentProvenance.currentRecordsFirst(
    uniqueNamedRecords([...entrypointSources.contextRows, ...entrypointSources.traceRows]),
    (entrypoint) => deploymentProvenance.namedRecordTruth(entrypoint, entrypointSources),
  );
  addIsolatedRecords(
    nodes,
    entrypoints,
    limits.entrypoints,
    "entrypoint",
    4,
    maxNodes - 12,
    (entrypoint) =>
      graphTruth(deploymentProvenance.namedRecordTruth(entrypoint, entrypointSources)),
  );
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
    maxNodes - 12,
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
