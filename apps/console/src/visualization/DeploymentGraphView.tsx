import { curveBumpX, line, scalePoint } from "d3";
import { useMemo, useState } from "react";
import type {
  DeploymentGraph,
  DeploymentGraphLink,
  DeploymentGraphNode
} from "../api/mockData";

interface DeploymentGraphViewProps {
  readonly ariaLabel?: string;
  readonly detailTitle?: string;
  readonly graph: DeploymentGraph;
}

const graphWidth = 680;
const graphHeight = 360;

type SelectedGraphItem =
  | {
    readonly kind: "node";
    readonly node: DeploymentGraphNode;
  }
  | {
    readonly kind: "link";
    readonly link: LayoutLink;
  };

export function DeploymentGraphView({
  ariaLabel = "Deployment evidence graph",
  detailTitle = "Evidence nodes",
  graph
}: DeploymentGraphViewProps): React.JSX.Element {
  const [selectedItem, setSelectedItem] = useState<SelectedGraphItem | undefined>(
    graph.nodes[0] === undefined ? undefined : { kind: "node", node: graph.nodes[0] }
  );
  const layout = useMemo(() => layoutGraph(graph), [graph]);
  const selected = selectedItem ?? (
    graph.nodes[0] === undefined ? undefined : { kind: "node" as const, node: graph.nodes[0] }
  );

  return (
    <div className="deployment-graph">
      <svg
        aria-label={ariaLabel}
        className="deployment-graph-svg"
        role="img"
        viewBox={`0 0 ${graphWidth} ${graphHeight}`}
      >
        {layout.links.map((link) => (
          <g
            aria-label={`Inspect ${link.label} relationship`}
            className="deployment-edge"
            key={link.key}
            onClick={() => setSelectedItem({ kind: "link", link })}
            onKeyDown={(event) => {
              if (event.key === "Enter" || event.key === " ") {
                event.preventDefault();
                setSelectedItem({ kind: "link", link });
              }
            }}
            role="button"
            tabIndex={0}
          >
            <path className="deployment-link deployment-link-hitbox" d={link.path} />
            <path className="deployment-link" d={link.path} />
            <rect
              className="deployment-link-label-bg"
              height="24"
              rx="6"
              width={edgeLabelWidth(link.label)}
              x={link.labelX - edgeLabelWidth(link.label) / 2}
              y={link.labelY - 18}
            />
            <text className="deployment-link-label" x={link.labelX} y={link.labelY - 2}>
              {link.label}
            </text>
          </g>
        ))}
        {layout.nodes.map((node) => (
          <g
            className={`deployment-node deployment-node-${node.kind}`}
            key={node.id}
            onClick={() => setSelectedItem({ kind: "node", node })}
            onKeyDown={(event) => {
              if (event.key === "Enter" || event.key === " ") {
                event.preventDefault();
                setSelectedItem({ kind: "node", node });
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
        {selected !== undefined ? <GraphSelectionDossier selected={selected} /> : null}
        <div className="graph-node-buttons">
          {graph.nodes.map((node) => (
            <button
              aria-pressed={selected?.kind === "node" && selected.node.id === node.id}
              key={node.id}
              onClick={() => setSelectedItem({ kind: "node", node })}
              type="button"
            >
              <span>{node.label}</span>
              <span className="node-kind">{node.kind}</span>
            </button>
          ))}
        </div>
        {layout.links.length > 0 ? (
          <div className="graph-edge-buttons">
            <h4>Relationships</h4>
            {layout.links.map((link) => (
              <button
                aria-pressed={selected?.kind === "link" && selected.link.key === link.key}
                key={link.key}
                onClick={() => setSelectedItem({ kind: "link", link })}
                type="button"
              >
                <span>{link.label}</span>
                <span>{`${link.source} -> ${link.target}`}</span>
              </button>
            ))}
          </div>
        ) : null}
      </div>
    </div>
  );
}

function GraphSelectionDossier({
  selected
}: {
  readonly selected: SelectedGraphItem;
}): React.JSX.Element {
  if (selected.kind === "node") {
    return (
      <p>
        <strong>{selected.node.label}</strong>
        {selected.node.detail !== undefined && selected.node.detail.trim().length > 0
          ? `: ${selected.node.detail}`
          : ""}
      </p>
    );
  }

  return (
    <section className="graph-selected-relationship" aria-label="Selected graph relationship">
      <h4>Selected relationship</h4>
      <dl>
        <div>
          <dt>Verb</dt>
          <dd>{selected.link.label}</dd>
        </div>
        <div>
          <dt>Source</dt>
          <dd>{selected.link.source}</dd>
        </div>
        <div>
          <dt>Target</dt>
          <dd>{selected.link.target}</dd>
        </div>
      </dl>
      {selected.link.detail !== undefined && selected.link.detail.trim().length > 0 ? (
        <p>{selected.link.detail}</p>
      ) : null}
    </section>
  );
}

interface LayoutNode extends DeploymentGraphNode {
  readonly height: number;
  readonly width: number;
  readonly x: number;
  readonly y: number;
}

interface LayoutLink {
  readonly detail?: string;
  readonly key: string;
  readonly label: string;
  readonly labelX: number;
  readonly labelY: number;
  readonly path: string;
  readonly source: string;
  readonly target: string;
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
        detail: link.detail,
        key: `${link.source}:${link.target}:${link.label}`,
        label: link.label,
        labelX: (source.x + target.x) / 2,
        labelY: (source.y + target.y) / 2,
        path:
          pathLine([
            [source.x, source.y],
            [target.x, target.y]
          ]) ?? "",
        source: link.source,
        target: link.target
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
        detail: link.detail,
        key: `${link.source}:${link.target}:${link.label}`,
        label: link.label,
        labelX: (source.x + target.x) / 2,
        labelY: (source.y + target.y) / 2,
        path:
          pathLine([
            [source.x, source.y],
            [target.x, target.y]
          ]) ?? "",
        source: link.source,
        target: link.target
      }
    ];
  });
  return { links, nodes };
}

function columnX(column: number): number {
  const columns = [92, 258, 444, 578];
  return columns[column] ?? 578;
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

function edgeLabelWidth(label: DeploymentGraphLink["label"]): number {
  return Math.max(86, label.length * 7 + 28);
}
