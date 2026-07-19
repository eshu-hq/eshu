import type { ImpactGraphPresentation } from "./impactReviewTypes";
import type { GraphEdge, GraphModel, GraphNode } from "../console/types";

const nodeLimit = 60;
const edgeLimit = 120;

export function boundedGraph(
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
  const edgeByKey = new Map<
    string,
    {
      readonly base: GraphEdge;
      readonly evidence: Set<string>;
      readonly methods: Set<string>;
      readonly sourceFamilies: Set<string>;
    }
  >();
  const orderedRawEdges = [...rawEdges].sort((left, right) =>
    edgeObservationKey(left).localeCompare(edgeObservationKey(right)),
  );
  for (const edge of orderedRawEdges) {
    const key = `${edge.s}\u0000${edge.verb}\u0000${edge.t}`;
    const existing = edgeByKey.get(key);
    if (existing === undefined) {
      edgeByKey.set(key, {
        base: edge,
        evidence: new Set(edge.evidence ?? []),
        methods: new Set(edge.method ? [edge.method] : []),
        sourceFamilies: new Set(edge.sourceFamily ? [edge.sourceFamily] : []),
      });
      continue;
    }
    for (const evidence of edge.evidence ?? []) existing.evidence.add(evidence);
    if (edge.method) existing.methods.add(edge.method);
    if (edge.sourceFamily) existing.sourceFamilies.add(edge.sourceFamily);
  }
  const duplicateEdges = rawEdges.length - edgeByKey.size;
  const uniqueEdges = [...edgeByKey.values()]
    .map(mergedGraphEdge)
    .sort(
      (left, right) =>
        left.s.localeCompare(right.s) ||
        left.verb.localeCompare(right.verb) ||
        left.t.localeCompare(right.t) ||
        left.layer.localeCompare(right.layer),
    );
  const nodes = selectBoundedNodes(uniqueNodes, uniqueEdges, nodeLimit);
  const renderedIDs = new Set(nodes.map((node) => node.id));
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
      compositionDurationMs: 0,
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

function edgeObservationKey(edge: GraphEdge): string {
  return JSON.stringify([
    edge.s,
    edge.verb,
    edge.t,
    edge.layer,
    edge.sourceFamily ?? "",
    edge.method ?? "",
    [...(edge.evidence ?? [])].sort(),
    edge.confidenceTier ?? "",
    edge.truthState ?? "",
  ]);
}

function mergedGraphEdge(aggregate: {
  readonly base: GraphEdge;
  readonly evidence: Set<string>;
  readonly methods: Set<string>;
  readonly sourceFamilies: Set<string>;
}): GraphEdge {
  const evidence = [...aggregate.evidence].sort();
  const methods = [...aggregate.methods].sort();
  const sourceFamilies = [...aggregate.sourceFamilies].sort();
  return {
    ...aggregate.base,
    ...(evidence.length > 0 ? { evidence } : {}),
    ...(methods.length > 0 ? { method: methods.join(" + ") } : {}),
    ...(sourceFamilies.length > 0 ? { sourceFamily: sourceFamilies.join(" + ") } : {}),
  };
}

function selectBoundedNodes(
  nodes: readonly GraphNode[],
  edges: readonly GraphEdge[],
  limit: number,
): readonly GraphNode[] {
  if (nodes.length <= limit) return nodes;
  const required = new Set<string>();
  const add = (node: GraphNode | undefined): void => {
    if (node !== undefined && required.size < limit) required.add(node.id);
  };
  add(nodes.find((node) => node.hero));
  for (const kind of ["repo", "env", "aws", "k8s"] as const) {
    add(nodes.find((node) => node.kind === kind));
  }
  const platformKinds = new Set<string>();
  for (const node of nodes) {
    const platformKind = node.sub ?? "unknown";
    if (node.kind !== "platform" || platformKinds.has(platformKind)) continue;
    platformKinds.add(platformKind);
    add(node);
    const runsOn = edges.find((edge) => edge.verb === "RUNS_ON" && edge.t === node.id);
    add(nodes.find((candidate) => candidate.id === runsOn?.s));
  }
  for (const verb of ["DEFINES", "DEPLOYS_FROM", "DEPLOYMENT_SOURCE"] as const) {
    const edge = edges.find((candidate) => candidate.verb === verb);
    add(nodes.find((node) => node.id === edge?.s));
    add(nodes.find((node) => node.id === edge?.t));
  }
  const selected = nodes.filter((node) => required.has(node.id));
  for (const node of nodes) {
    if (selected.length >= limit) break;
    if (!required.has(node.id)) selected.push(node);
  }
  return selected;
}
