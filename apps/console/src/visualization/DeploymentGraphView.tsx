import { curveBumpX, line, scalePoint } from "d3";
import { useMemo, useState } from "react";
import type { DeploymentGraph, DeploymentGraphNode } from "../api/mockData";

interface DeploymentGraphViewProps {
  readonly graph: DeploymentGraph;
}

const graphWidth = 820;
const graphHeight = 430;

export function DeploymentGraphView({
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
        aria-label="Deployment evidence graph"
        className="deployment-graph-svg"
        role="img"
        viewBox={`0 0 ${graphWidth} ${graphHeight}`}
      >
        {layout.links.map((link) => (
          <path className="deployment-link" d={link.path} key={link.key} />
        ))}
        {layout.nodes.map((node) => (
          <g className={`deployment-node deployment-node-${node.kind}`} key={node.id}>
            <circle cx={node.x} cy={node.y} r="18" />
            <text x={node.x} y={node.y + 38}>
              {displayLabel(node.label)}
            </text>
          </g>
        ))}
      </svg>
      <div className="graph-drilldown">
        <h3>Drill down</h3>
        <div className="graph-node-buttons">
          {graph.nodes.map((node) => (
            <button
              key={node.id}
              onClick={() => setSelectedNode(node)}
              type="button"
            >
              <span>{node.label}</span>
              <small>{node.kind}</small>
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
  const columns = [92, 295, 500, 685];
  return columns[column] ?? 685;
}

function displayLabel(label: string): string {
  if (label.length <= 20) {
    return label;
  }
  return `${label.slice(0, 17)}...`;
}
