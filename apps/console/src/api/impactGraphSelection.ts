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
  const edgeByKey = new Map<string, GraphEdge>();
  for (const edge of rawEdges) {
    const key = `${edge.s}\u0000${edge.verb}\u0000${edge.t}`;
    if (!edgeByKey.has(key)) edgeByKey.set(key, edge);
  }
  const duplicateEdges = rawEdges.length - edgeByKey.size;
  const uniqueEdges = [...edgeByKey.values()];
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
