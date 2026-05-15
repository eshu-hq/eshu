import { curveBumpX, line, scalePoint } from "d3";
import { useMemo, useState } from "react";
import type { DeploymentGraph, DeploymentGraphNode } from "../api/mockData";

interface DeploymentGraphViewProps {
  readonly ariaLabel?: string;
  readonly detailTitle?: string;
  readonly graph: DeploymentGraph;
}

const graphWidth = 840;
const graphHeight = 430;

export function DeploymentGraphView({
  ariaLabel = "Deployment evidence graph",
  detailTitle = "Evidence nodes",
  graph
}: DeploymentGraphViewProps): React.JSX.Element {
  const [selectedNode, setSelectedNode] = useState<DeploymentGraphNode | undefined>(
    graph.nodes[0]
  );
  const layout = useMemo(() => layoutGraph(graph), [graph]);
  const selected = selectedNode ?? graph.nodes[0];

  return (
    <div className="deployment-graph">
      <svg
        aria-label={ariaLabel}
        className="deployment-graph-svg"
        role="img"
        viewBox={`0 0 ${graphWidth} ${graphHeight}`}
      >
        {layout.links.map((link) => (
          <path className="deployment-link" d={link.path} key={link.key} />
        ))}
        {layout.nodes.map((node) => (
          <g
            className={`deployment-node deployment-node-${node.kind}`}
            key={node.id}
            onClick={() => setSelectedNode(node)}
            onKeyDown={(event) => {
              if (event.key === "Enter" || event.key === " ") {
                event.preventDefault();
                setSelectedNode(node);
              }
            }}
            role="button"
            tabIndex={0}
          >
            <title>{node.label}</title>
            <rect
              height={node.height}
              rx="8"
              width={node.width}
              x={node.x - node.width / 2}
              y={node.y - node.height / 2}
            />
            <text x={node.x} y={node.y - labelYOffset(node.label)}>
              {labelLines(node.label).map((lineText, index) => (
                <tspan
                  dy={index === 0 ? 0 : 15}
                  key={`${node.id}:${index}:${lineText}`}
                  x={node.x}
                >
                  {lineText}
                </tspan>
              ))}
            </text>
          </g>
        ))}
      </svg>
      <div className="graph-detail">
        <h3>{detailTitle}</h3>
        <div className="graph-node-buttons">
          {graph.nodes.map((node) => (
            <button
              aria-pressed={selected?.id === node.id}
              key={node.id}
              onClick={() => setSelectedNode(node)}
              type="button"
            >
              <span>{node.label}</span>
              <span className="node-kind">{node.kind}</span>
            </button>
          ))}
        </div>
        {selected !== undefined ? (
          <p>
            <strong>{selected.label}</strong>
            {selected.detail !== undefined && selected.detail.trim().length > 0
              ? `: ${selected.detail}`
              : ""}
          </p>
        ) : null}
      </div>
    </div>
  );
}

interface LayoutNode extends DeploymentGraphNode {
  readonly height: number;
  readonly width: number;
  readonly x: number;
  readonly y: number;
}

interface LayoutLink {
  readonly key: string;
  readonly path: string;
}

function layoutGraph(graph: DeploymentGraph): {
  readonly links: readonly LayoutLink[];
  readonly nodes: readonly LayoutNode[];
} {
  const pathLine = line<[number, number]>()
    .curve(curveBumpX)
    .x(([pointX]) => pointX)
    .y(([, pointY]) => pointY);
  if (graph.nodes.every((node) => node.column !== undefined && node.lane !== undefined)) {
    return semanticLayout(graph, pathLine);
  }

  const x = scalePoint<string>()
    .domain(graph.nodes.map((node) => node.id))
    .range([80, graphWidth - 80])
    .padding(0.5);
  const nodes = graph.nodes.map((node, index) => ({
    ...node,
    height: nodeHeight(node.label),
    width: nodeWidth(node.label),
    x: x(node.id) ?? 80,
    y: index % 2 === 0 ? 96 : 176
  }));
  const nodeLookup = new Map(nodes.map((node) => [node.id, node]));
  const links = graph.links.flatMap((link) => {
    const source = nodeLookup.get(link.source);
    const target = nodeLookup.get(link.target);
    if (source === undefined || target === undefined) {
      return [];
    }
    return [
      {
        key: `${link.source}:${link.target}:${link.label}`,
        path:
          pathLine([
            [source.x, source.y],
            [target.x, target.y]
          ]) ?? ""
      }
    ];
  });
  return { links, nodes };
}

function semanticLayout(
  graph: DeploymentGraph,
  pathLine: ReturnType<typeof line<[number, number]>>
): {
  readonly links: readonly LayoutLink[];
  readonly nodes: readonly LayoutNode[];
} {
  const lanes = Array.from(new Set(graph.nodes.map((node) => node.lane ?? "")));
  const laneScale = scalePoint<string>()
    .domain(lanes)
    .range([58, graphHeight - 82])
    .padding(0.4);
  const nodes = graph.nodes.map((node) => ({
    ...node,
    height: nodeHeight(node.label),
    width: nodeWidth(node.label),
    x: columnX(node.column ?? 0),
    y: laneScale(node.lane ?? "") ?? 96
  }));
  const nodeLookup = new Map(nodes.map((node) => [node.id, node]));
  const links = graph.links.flatMap((link) => {
    const source = nodeLookup.get(link.source);
    const target = nodeLookup.get(link.target);
    if (source === undefined || target === undefined) {
      return [];
    }
    return [
      {
        key: `${link.source}:${link.target}:${link.label}`,
        path:
          pathLine([
            [source.x, source.y],
            [target.x, target.y]
          ]) ?? ""
      }
    ];
  });
  return { links, nodes };
}

function columnX(column: number): number {
  const columns = [104, 340, 584, 720];
  return columns[column] ?? 720;
}

function labelLines(label: string): readonly string[] {
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

function labelYOffset(label: string): number {
  return ((labelLines(label).length - 1) * 17) / 2 - 6;
}

function nodeHeight(label: string): number {
  return Math.max(54, 26 + labelLines(label).length * 17);
}

function nodeWidth(label: string): number {
  const longest = Math.max(...labelLines(label).map((lineText) => lineText.length), 10);
  return Math.min(190, Math.max(124, longest * 8 + 32));
}
