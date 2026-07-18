import type { GraphModel, GraphNode } from "../console/types";

export function currentCenterId(graph: GraphModel): string | undefined {
  return graph.nodes.find((node) => node.hero)?.id;
}

export function modeForNode(node: GraphNode): "direct" | "neighborhood" {
  if (node.kind === "client" || node.kind === "library") return "direct";
  return "neighborhood";
}

export function repoIDForNode(node: GraphNode): string | undefined {
  if (node.kind !== "repo") return undefined;
  return node.id.trim() === "" ? undefined : node.id;
}

export function sourceHref(node: GraphNode): string | null {
  const source = node.source;
  if (!source) return null;
  const params = new URLSearchParams({ path: source.filePath });
  if (source.startLine !== undefined) params.set("lineStart", String(source.startLine));
  if (source.endLine !== undefined) params.set("lineEnd", String(source.endLine));
  return `/repositories/${encodeURIComponent(source.repoId)}/source?${params.toString()}`;
}

export function sourceLabel(node: GraphNode): string {
  const source = node.source;
  if (!source) return "source path unavailable";
  if (source.startLine !== undefined && source.endLine !== undefined)
    return `${source.filePath}:${source.startLine}-${source.endLine}`;
  if (source.startLine !== undefined) return `${source.filePath}:${source.startLine}`;
  return source.filePath;
}
