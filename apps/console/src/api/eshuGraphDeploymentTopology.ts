import type { EshuTruth } from "./envelope";
import type { DeploymentFamilyLimits } from "./eshuGraphDeploymentLimits";
import {
  addOmissionSummary,
  compact,
  graphTruth,
  omissionContract,
  repoNode,
  repoRef,
  summaryNode,
  truthEvidence,
  truthIsCurrent,
} from "./eshuGraphDeploymentPresentation";
import type {
  DeploymentPlatformRecord,
  DeploymentTopologyEdgeRecord,
  DeploymentTraceResponse,
} from "./eshuGraphDeploymentWire";
import { cleanText, layerFor } from "./eshuGraphShared";
import type { GraphEdge, GraphLayer, GraphNode } from "../console/types";

interface TopologyGraphOptions {
  readonly addEdge: (edge: GraphEdge) => void;
  readonly addNode: (node: GraphNode) => boolean;
  readonly hasNode: (id: string) => boolean;
  readonly limits: DeploymentFamilyLimits;
  readonly serviceName: string;
  readonly summaries: GraphNode[];
  readonly trace: DeploymentTraceResponse;
  readonly traceTruth?: EshuTruth | null;
  readonly workloadID: string;
}

export function addCanonicalDeploymentTopology(options: TopologyGraphOptions): void {
  const provisioned = options.trace.provisioned_platforms ?? [];
  provisioned
    .slice(0, options.limits.provisionedPlatforms)
    .forEach((platform) => addProvisionedPlatformNode(platform, options));
  addOmissionSummary(
    options.summaries,
    "provisioned platforms",
    Math.max(0, provisioned.length - options.limits.provisionedPlatforms),
    "provisioned_platforms",
    omissionContract(
      options.limits.provisionedPlatforms,
      provisioned
        .slice(options.limits.provisionedPlatforms)
        .map((platform) => cleanText(platform.platform_kind)),
    ),
  );

  addDeploymentSources(options);
  addTopologyEdges(options);
}

function addDeploymentSources(options: TopologyGraphOptions): void {
  const sources = options.trace.deployment_sources ?? [];
  const missingSourceIdentities: string[] = [];
  let endpointBoundRelationships = 0;
  let staleRelationships = 0;
  sources.slice(0, options.limits.sources).forEach((source) => {
    const repo = repoRef(source.repo_id, source.repo_name);
    if (!repo) {
      missingSourceIdentities.push(cleanText(source.repo_name) || "unnamed source");
      return;
    }
    const sourceID = cleanText(source.source_id);
    const targetID = cleanText(source.target_id);
    const verb = cleanText(source.relationship_type).toUpperCase();
    options.addNode({
      ...repoNode(
        repo,
        compact([
          "Deployment source",
          verb && sourceID && targetID
            ? `${verb} endpoint`
            : "relationship endpoints not exposed by API",
          cleanText(source.reason) ? `reason: ${cleanText(source.reason)}` : "",
          source.confidence !== undefined ? `confidence: ${source.confidence}` : "",
        ]).join(" · "),
      ),
      truth: graphTruth(options.traceTruth),
    });
    if (sourceID === "" || targetID === "" || !deploymentSourceVerbIsSupported(verb)) return;
    if (verb === "DEPLOYMENT_SOURCE" && !options.hasNode(sourceID)) {
      endpointBoundRelationships += 1;
      return;
    }
    addEndpointNode(sourceID, sourceID === repo.id ? repo.name : undefined, options);
    addEndpointNode(targetID, targetID === repo.id ? repo.name : undefined, options);
    if (!truthIsCurrent(options.traceTruth)) {
      staleRelationships += 1;
      return;
    }
    options.addEdge({
      evidence: [
        ...compact([
          cleanText(source.reason) ? `reason: ${cleanText(source.reason)}` : "",
          source.confidence !== undefined ? `confidence: ${source.confidence}` : "",
        ]),
        ...truthEvidence(options.traceTruth),
      ],
      layer: "deploy",
      s: sourceID,
      t: targetID,
      truthState: "derived",
      verb,
    });
  });
  if (missingSourceIdentities.length > 0) {
    options.summaries.push(
      summaryNode(
        "source_identity",
        `${missingSourceIdentities.length} deployment sources missing canonical repository identity`,
        `Relationships not admitted: ${missingSourceIdentities.join(", ")}`,
      ),
    );
  }
  if (endpointBoundRelationships > 0) {
    options.summaries.push(
      summaryNode(
        "source_endpoint_bounds",
        `${endpointBoundRelationships} deployment source relationships outside the selected instance bound`,
      ),
    );
  }
  if (staleRelationships > 0) {
    options.summaries.push(
      summaryNode(
        "stale_source_relationships",
        `${staleRelationships} stale deployment source relationships not admitted`,
      ),
    );
  }
  addOmissionSummary(
    options.summaries,
    "deployment sources",
    Math.max(0, sources.length - options.limits.sources),
    "sources",
    omissionContract(
      options.limits.sources,
      sources
        .slice(options.limits.sources)
        .map((source) => cleanText(source.repo_name) || cleanText(source.repo_id)),
    ),
  );
}

function addTopologyEdges(options: TopologyGraphOptions): void {
  const rows = uniqueTopologyEdges([
    ...(options.trace.topology_edges ?? []),
    ...(options.trace.instances ?? []).flatMap((instance) =>
      (instance.platforms ?? []).flatMap((platform) => platform.topology_edges ?? []),
    ),
    ...(options.trace.provisioned_platforms ?? []).flatMap(
      (platform) => platform.topology_edges ?? [],
    ),
  ]);
  let invalid = 0;
  let endpointBound = 0;
  let stale = 0;
  rows.slice(0, options.limits.topologyEdges).forEach((row) => {
    const sourceID = cleanText(row.source_id);
    const targetID = cleanText(row.target_id);
    const verb = cleanText(row.relationship_type).toUpperCase();
    if (sourceID === "" || targetID === "" || !topologyVerbIsSupported(verb)) {
      invalid += 1;
      return;
    }
    if (!topologyEndpointsAreSelected(verb, sourceID, targetID, options)) {
      endpointBound += 1;
      return;
    }
    addEndpointNode(sourceID, cleanText(row.source_name), options);
    addEndpointNode(targetID, cleanText(row.target_name), options);
    if (!truthIsCurrent(options.traceTruth)) {
      stale += 1;
      return;
    }
    options.addEdge({
      evidence: [
        ...compact([
          cleanText(row.reason) ? `reason: ${cleanText(row.reason)}` : "",
          cleanText(row.evidence_source)
            ? `evidence source: ${cleanText(row.evidence_source)}`
            : "",
          cleanText(row.source_tool) ? `source tool: ${cleanText(row.source_tool)}` : "",
          row.confidence !== undefined ? `confidence: ${row.confidence}` : "",
        ]),
        ...truthEvidence(options.traceTruth),
      ],
      layer: topologyLayer(verb),
      s: sourceID,
      t: targetID,
      truthState: "derived",
      verb,
    });
  });
  if (invalid > 0) {
    options.summaries.push(
      summaryNode("invalid_topology", `${invalid} topology relationships not admitted`),
    );
  }
  if (endpointBound > 0) {
    options.summaries.push(
      summaryNode(
        "topology_endpoint_bounds",
        `${endpointBound} topology relationships outside the selected node-family bounds`,
      ),
    );
  }
  if (stale > 0) {
    options.summaries.push(
      summaryNode("stale_topology", `${stale} stale topology relationships not admitted`),
    );
  }
  addOmissionSummary(
    options.summaries,
    "topology relationships",
    Math.max(0, rows.length - options.limits.topologyEdges),
    "topology_edges",
    omissionContract(
      options.limits.topologyEdges,
      rows.slice(options.limits.topologyEdges).map((row) => cleanText(row.relationship_type)),
    ),
  );
}

function deploymentSourceVerbIsSupported(verb: string): boolean {
  return verb === "DEPLOYMENT_SOURCE" || verb === "DEPLOYS_FROM";
}

function topologyVerbIsSupported(verb: string): boolean {
  return [
    "DEFINES",
    "INSTANCE_OF",
    "PROVISIONS_DEPENDENCY_FOR",
    "PROVISIONS_PLATFORM",
    "RUNS_ON",
  ].includes(verb);
}

function topologyLayer(verb: string): GraphLayer {
  if (verb === "PROVISIONS_DEPENDENCY_FOR" || verb === "PROVISIONS_PLATFORM") return "infra";
  return layerFor(verb);
}

function topologyEndpointsAreSelected(
  verb: string,
  sourceID: string,
  targetID: string,
  options: TopologyGraphOptions,
): boolean {
  if (verb === "INSTANCE_OF" || verb === "RUNS_ON") {
    return options.hasNode(sourceID) && options.hasNode(targetID);
  }
  if (verb === "PROVISIONS_PLATFORM") return options.hasNode(targetID);
  if (verb === "DEFINES") return options.hasNode(targetID);
  return true;
}

function addProvisionedPlatformNode(
  platform: DeploymentPlatformRecord,
  options: TopologyGraphOptions,
): void {
  const id = cleanText(platform.platform_id);
  if (id === "") return;
  const kind = cleanText(platform.platform_kind);
  options.addNode({
    col: 4,
    id,
    kind: kind || "platform",
    label: cleanText(platform.platform_name) || id,
    sub: compact([
      kind || "platform",
      cleanText(platform.topology_basis) || "provisioning topology",
      cleanText(platform.platform_reason),
    ]).join(" · "),
    truth: graphTruth(options.traceTruth),
  });
}

function addEndpointNode(
  id: string,
  name: string | undefined,
  options: TopologyGraphOptions,
): void {
  if (options.hasNode(id)) return;
  const label = cleanText(name) || id;
  if (id.startsWith("repository:")) {
    options.addNode({
      ...repoNode({ id, name: label }, "Canonical deployment topology endpoint"),
      truth: graphTruth(options.traceTruth),
    });
    return;
  }
  options.addNode({
    col: id.startsWith("workload:") ? 2 : id.startsWith("instance:") ? 3 : 4,
    id,
    kind: id.startsWith("workload:")
      ? "workload"
      : id.startsWith("instance:")
        ? "instance"
        : id.startsWith("platform:")
          ? "platform"
          : "service",
    label,
    sub: id === options.workloadID ? options.serviceName : id,
    truth: graphTruth(options.traceTruth),
  });
}

function uniqueTopologyEdges(
  rows: readonly DeploymentTopologyEdgeRecord[],
): DeploymentTopologyEdgeRecord[] {
  const seen = new Set<string>();
  return rows.filter((row) => {
    const key = [row.relationship_type, row.source_id, row.target_id].join("\u0000");
    if (seen.has(key)) return false;
    seen.add(key);
    return true;
  });
}
