import { Maximize2, Minimize2, RotateCcw, ZoomIn, ZoomOut } from "lucide-react";
import { useMemo, useState } from "react";
import type {
  DeploymentGraph,
  DeploymentGraphNode
} from "../api/mockData";
import {
  labelLines,
  labelYOffset,
  layoutGraph,
  type LayoutLink
} from "./deploymentGraphLayout";

interface DeploymentGraphViewProps {
  readonly ariaLabel?: string;
  readonly detailTitle?: string;
  readonly graph: DeploymentGraph;
}

const graphWidth = 1280;
const minGraphHeight = 620;
const semanticRowGap = 110;
const fallbackRowGap = 96;
const minZoom = 0.65;
const maxZoom = 2.4;
const zoomStep = 0.2;

interface ViewportTransform {
  readonly scale: number;
  readonly x: number;
  readonly y: number;
}

interface Point {
  readonly x: number;
  readonly y: number;
}

interface PanState {
  readonly origin: Point;
  readonly pointerId: number;
  readonly start: Point;
}

const defaultViewport: ViewportTransform = { scale: 1, x: 0, y: 0 };

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
  const [expanded, setExpanded] = useState(false);
  const [panState, setPanState] = useState<PanState | undefined>();
  const [viewport, setViewport] = useState<ViewportTransform>(defaultViewport);
  const layout = useMemo(() => layoutGraph(graph), [graph]);
  const selected = selectedItem ?? (
    graph.nodes[0] === undefined ? undefined : { kind: "node" as const, node: graph.nodes[0] }
  );
  const resetView = (): void => {
    setPanState(undefined);
    setViewport(defaultViewport);
  };

  return (
    <div className={`deployment-graph${expanded ? " deployment-graph-expanded" : ""}`}>
      <div className="deployment-graph-toolbar">
        <div className="deployment-graph-counts">
          <span>{`${layout.nodes.length} nodes`}</span>
          <span>{`${layout.links.length} relationships`}</span>
          <span>{`${Math.round(viewport.scale * 100)}%`}</span>
        </div>
        <div className="deployment-graph-controls" aria-label="Evidence graph controls">
          <button
            aria-label="Zoom out evidence graph"
            onClick={() => setViewport((current) => zoomViewport(current, -zoomStep))}
            title="Zoom out"
            type="button"
          >
            <ZoomOut aria-hidden="true" size={16} />
          </button>
          <button
            aria-label="Zoom in evidence graph"
            onClick={() => setViewport((current) => zoomViewport(current, zoomStep))}
            title="Zoom in"
            type="button"
          >
            <ZoomIn aria-hidden="true" size={16} />
          </button>
          <button aria-label="Reset evidence graph view" onClick={resetView} title="Reset view" type="button">
            <RotateCcw aria-hidden="true" size={16} />
          </button>
          <button
            aria-label={expanded ? "Collapse evidence graph widget" : "Expand evidence graph widget"}
            aria-pressed={expanded}
            onClick={() => setExpanded((current) => !current)}
            title={expanded ? "Collapse graph widget" : "Expand graph widget"}
            type="button"
          >
            {expanded ? <Minimize2 aria-hidden="true" size={16} /> : <Maximize2 aria-hidden="true" size={16} />}
          </button>
        </div>
      </div>
      <div className="deployment-graph-canvas">
        <svg
          aria-label={ariaLabel}
          className="deployment-graph-svg"
          onPointerMove={(event) => {
            if (panState === undefined) {
              return;
            }
            const point = svgPoint(event);
            setViewport((current) => ({
              ...current,
              x: panState.origin.x + point.x - panState.start.x,
              y: panState.origin.y + point.y - panState.start.y
            }));
          }}
          onPointerUp={(event) => {
            if (panState?.pointerId === event.pointerId) {
              setPanState(undefined);
              event.currentTarget.releasePointerCapture(event.pointerId);
            }
          }}
          onWheel={(event) => {
            event.preventDefault();
            setViewport((current) => zoomViewport(current, event.deltaY < 0 ? zoomStep : -zoomStep));
          }}
          role="img"
          viewBox={`0 0 ${layout.width} ${layout.height}`}
        >
          <defs>
            <marker
              id="deployment-arrow"
              markerHeight="8"
              markerWidth="8"
              orient="auto"
              refX="8"
              refY="4"
              viewBox="0 0 8 8"
            >
              <path d="M0,0 L8,4 L0,8 Z" />
            </marker>
          </defs>
          <rect
            className="deployment-graph-pan-plane"
            height={layout.height}
            onPointerDown={(event) => {
              const point = svgPoint(event);
              setPanState({
                origin: { x: viewport.x, y: viewport.y },
                pointerId: event.pointerId,
                start: point
              });
              event.currentTarget.ownerSVGElement?.setPointerCapture(event.pointerId);
            }}
            width={layout.width}
            x="0"
            y="0"
          />
          <g data-testid="deployment-graph-viewport" transform={viewportTransform(viewport)}>
            {layout.links.map((link) => (
              <g
                aria-label={`Inspect ${link.label} relationship`}
                className={`deployment-edge deployment-edge-${relationshipClass(link.label)}${
                  selected?.kind === "link" && selected.link.key === link.key ? " deployment-edge-selected" : ""
                }`}
                key={link.key}
                onClick={(event) => {
                  event.stopPropagation();
                  setSelectedItem({ kind: "link", link });
                }}
                onKeyDown={(event) => {
                  if (event.key === "Enter" || event.key === " ") {
                    event.preventDefault();
                    setSelectedItem({ kind: "link", link });
                  }
                }}
                onPointerDown={(event) => event.stopPropagation()}
                onPointerUp={(event) => event.stopPropagation()}
                role="button"
                tabIndex={0}
              >
                <title>{`${link.label}: ${link.source} -> ${link.target}`}</title>
                <path className="deployment-link deployment-link-hitbox" d={link.path} />
                <path className="deployment-link" d={link.path} markerEnd="url(#deployment-arrow)" />
                {shouldShowEdgeLabel(link.label) ? (
                  <>
                    <rect
                      className="deployment-edge-label-bg"
                      height="24"
                      rx="6"
                      width={edgeLabelWidth(link.label)}
                      x={link.labelX - edgeLabelWidth(link.label) / 2}
                      y={link.labelY - 26}
                    />
                    <text className="deployment-edge-label" x={link.labelX} y={link.labelY - 10}>
                      {link.label}
                    </text>
                  </>
                ) : null}
              </g>
            ))}
            {layout.nodes.map((node) => (
              <g
                className={`deployment-node deployment-node-${node.kind}${
                  selected?.kind === "node" && selected.node.id === node.id ? " deployment-node-selected" : ""
                }`}
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
          </g>
        </svg>
      </div>
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

function zoomViewport(viewport: ViewportTransform, delta: number): ViewportTransform {
  return {
    ...viewport,
    scale: Math.min(maxZoom, Math.max(minZoom, Number((viewport.scale + delta).toFixed(2))))
  };
}

function viewportTransform(viewport: ViewportTransform): string {
  return `translate(${Math.round(viewport.x)} ${Math.round(viewport.y)}) scale(${viewport.scale})`;
}

function shouldShowEdgeLabel(label: string): boolean {
  return /^[A-Z0-9_]+$/.test(label);
}

function edgeLabelWidth(label: string): number {
  return Math.min(218, Math.max(94, label.length * 7 + 22));
}

function relationshipClass(label: string): string {
  return label.toLowerCase().replace(/[^a-z0-9]+/g, "_").replace(/^_+|_+$/g, "") || "generic";
}

function svgPoint(event: React.PointerEvent<SVGElement>): Point {
  const svg = event.currentTarget.ownerSVGElement ?? (event.currentTarget as SVGSVGElement);
  const point = svg.createSVGPoint();
  point.x = event.clientX;
  point.y = event.clientY;
  const matrix = svg.getScreenCTM();
  if (matrix === null) {
    return { x: point.x, y: point.y };
  }
  const transformed = point.matrixTransform(matrix.inverse());
  return { x: transformed.x, y: transformed.y };
}
