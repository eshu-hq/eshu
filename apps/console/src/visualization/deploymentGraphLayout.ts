import { curveBumpX, line, scalePoint } from "d3";
import type { DeploymentGraph, DeploymentGraphNode } from "../api/mockData";

const graphWidth = 1280;
const minGraphHeight = 620;
const semanticRowGap = 110;
const fallbackRowGap = 96;

export interface LayoutNode extends DeploymentGraphNode {
  readonly height: number;
  readonly width: number;
  readonly x: number;
  readonly y: number;
}

export interface LayoutLink {
  readonly detail?: string;
  readonly key: string;
  readonly label: string;
  readonly labelX: number;
  readonly labelY: number;
  readonly path: string;
  readonly source: string;
  readonly target: string;
}

export interface DeploymentGraphLayout {
  readonly height: number;
  readonly links: readonly LayoutLink[];
  readonly nodes: readonly LayoutNode[];
  readonly width: number;
}

export function layoutGraph(graph: DeploymentGraph): DeploymentGraphLayout {
  const pathLine = line<[number, number]>()
    .curve(curveBumpX)
    .x(([pointX]) => pointX)
    .y(([, pointY]) => pointY);
  if (graph.nodes.every((node) => node.column !== undefined && node.lane !== undefined)) {
    return semanticLayout(graph, pathLine);
  }

  const x = scalePoint<string>()
    .domain(graph.nodes.map((node) => node.id))
    .range([150, graphWidth - 150])
    .padding(0.5);
  const height = Math.max(minGraphHeight, 180 + Math.ceil(graph.nodes.length / 2) * fallbackRowGap);
  const nodes = graph.nodes.map((node, index) => ({
    ...node,
    height: nodeHeight(node.label),
    width: nodeWidth(node.label),
    x: x(node.id) ?? 80,
    y: 104 + (index % 2) * fallbackRowGap
  }));
  return layoutLinks(graph, nodes, height, pathLine);
}

function semanticLayout(
  graph: DeploymentGraph,
  pathLine: ReturnType<typeof line<[number, number]>>
): DeploymentGraphLayout {
  const lanes = Array.from(new Set(graph.nodes.map((node) => node.lane ?? "")));
  const height = Math.max(minGraphHeight, 180 + lanes.length * semanticRowGap);
  const laneY = new Map(lanes.map((lane, index) => [lane, 96 + index * semanticRowGap]));
  const duplicateOffsets = duplicateLaneColumnOffsets(graph.nodes);
  const nodes = graph.nodes.map((node) => {
    const lane = node.lane ?? "";
    const offset = duplicateOffsets.get(`${lane}:${node.column ?? 0}:${node.id}`) ?? 0;
    return {
      ...node,
      height: nodeHeight(node.label),
      width: nodeWidth(node.label),
      x: columnX(node.column ?? 0),
      y: (laneY.get(lane) ?? height / 2) + offset
    };
  });
  return layoutLinks(graph, nodes, height, pathLine);
}

function layoutLinks(
  graph: DeploymentGraph,
  nodes: readonly LayoutNode[],
  height: number,
  pathLine: ReturnType<typeof line<[number, number]>>
): DeploymentGraphLayout {
  const nodeLookup = new Map(nodes.map((node) => [node.id, node]));
  const links = graph.links.flatMap((link) => {
    const source = nodeLookup.get(link.source);
    const target = nodeLookup.get(link.target);
    if (source === undefined || target === undefined) {
      return [];
    }
    return [
      {
        detail: link.detail,
        key: `${link.source}:${link.target}:${link.label}`,
        label: link.label,
        labelX: (source.x + target.x) / 2,
        labelY: (source.y + target.y) / 2,
        path: linkPath(source, target, pathLine),
        source: link.source,
        target: link.target
      }
    ];
  });
  return { height, links, nodes, width: graphWidth };
}

function columnX(column: number): number {
  const columns = [170, 530, 850, 1140];
  return columns[column] ?? Math.min(graphWidth - 170, 170 + column * 300);
}

function duplicateLaneColumnOffsets(
  nodes: readonly DeploymentGraphNode[]
): ReadonlyMap<string, number> {
  const groups = new Map<string, readonly DeploymentGraphNode[]>();
  for (const node of nodes) {
    const key = `${node.lane ?? ""}:${node.column ?? 0}`;
    groups.set(key, [...(groups.get(key) ?? []), node]);
  }

  const offsets = new Map<string, number>();
  for (const [key, group] of groups) {
    if (group.length === 1) {
      offsets.set(`${key}:${group[0]?.id ?? ""}`, 0);
      continue;
    }
    const rowHeight = Math.max(...group.map((node) => nodeHeight(node.label))) + 16;
    group.forEach((node, index) => {
      offsets.set(`${key}:${node.id}`, (index - (group.length - 1) / 2) * rowHeight);
    });
  }
  return offsets;
}

function linkPath(
  source: LayoutNode,
  target: LayoutNode,
  pathLine: ReturnType<typeof line<[number, number]>>
): string {
  const sourceAnchorX =
    target.x >= source.x ? source.x + source.width / 2 + 8 : source.x - source.width / 2 - 8;
  const targetAnchorX =
    target.x >= source.x ? target.x - target.width / 2 - 8 : target.x + target.width / 2 + 8;
  const midX = (sourceAnchorX + targetAnchorX) / 2;
  return (
    pathLine([
      [sourceAnchorX, source.y],
      [midX, source.y],
      [midX, target.y],
      [targetAnchorX, target.y]
    ]) ?? ""
  );
}

export function labelLines(label: string): readonly string[] {
  const tokens = label.split(/\s+/).flatMap((word) => splitLongWord(word));
  const lines: string[] = [];
  let current = "";
  for (const token of tokens) {
    const joiner = current.endsWith("-") || token === "/" ? "" : " ";
    const candidate = current.length === 0 ? token : `${current}${joiner}${token}`;
    if (candidate.length <= 16) {
      current = candidate;
      continue;
    }
    if (current.length > 0) {
      lines.push(current);
    }
    current = token;
  }
  if (current.length > 0) {
    lines.push(current);
  }
  return lines.length === 0 ? [label] : lines;
}

function splitLongWord(word: string): readonly string[] {
  if (word.length <= 18) {
    return [word];
  }
  return word
    .replaceAll("/", "/ ")
    .replaceAll("-", "- ")
    .replaceAll("_", "_ ")
    .split(/\s+/)
    .filter((token) => token.length > 0);
}

export function labelYOffset(label: string): number {
  return ((labelLines(label).length - 1) * 17) / 2 - 6;
}

function nodeHeight(label: string): number {
  return Math.max(54, 26 + labelLines(label).length * 17);
}

function nodeWidth(label: string): number {
  const longest = Math.max(...labelLines(label).map((lineText) => lineText.length), 10);
  return Math.min(190, Math.max(124, longest * 8 + 32));
}
