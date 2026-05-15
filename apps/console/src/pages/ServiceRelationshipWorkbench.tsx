import { Maximize2, Minimize2 } from "lucide-react";
import { useMemo, useState } from "react";
import type { ServiceSpotlight } from "../api/serviceSpotlight";
import {
  RelationshipInspector,
  RelationshipSelectionSummary,
  type SelectedGraphItem
} from "./ServiceRelationshipInspector";
import {
  applyDraggedPositions,
  buildGraphModel,
  edgePoint,
  edgeToModel,
  graphHeight,
  graphWidth,
  layoutGraph,
  nodeId,
  nodeLabel,
  technologyLabel,
  type GraphMode,
  type LayoutEdge,
  type LayoutNode,
  type Point
} from "./serviceRelationshipGraphModel";

interface ViewportTransform {
  readonly scale: number;
  readonly x: number;
  readonly y: number;
}

interface PanState {
  readonly origin: Point;
  readonly pointerId: number;
  readonly start: Point;
}

const defaultViewport: ViewportTransform = { scale: 0.8, x: 0, y: 0 };
const maxZoom = 2.2;
const minZoom = 0.55;
const zoomStep = 0.25;

export function ServiceRelationshipWorkbench({
  spotlight
}: {
  readonly spotlight: ServiceSpotlight;
}): React.JSX.Element {
  const [mode, setMode] = useState<GraphMode>("deployment");
  const [expanded, setExpanded] = useState(false);
  const [selected, setSelected] = useState<SelectedGraphItem | undefined>();
  const [panState, setPanState] = useState<PanState | undefined>();
  const [viewport, setViewport] = useState<ViewportTransform>(defaultViewport);
  const [draggedPositions, setDraggedPositions] = useState<ReadonlyMap<string, Point>>(
    () => new Map()
  );
  const model = useMemo(() => buildGraphModel(spotlight, mode), [mode, spotlight]);
  const layout = useMemo(
    () => applyDraggedPositions(layoutGraph(model), draggedPositions),
    [draggedPositions, model]
  );
  const selectedNode = selected?.node ?? (selected?.edge === undefined ? layout.nodes[0] : undefined);
  const resetView = (): void => {
    setDraggedPositions(new Map());
    setPanState(undefined);
    setSelected(undefined);
    setViewport(defaultViewport);
  };

  return (
    <section
      className={`relationship-workbench${expanded ? " relationship-workbench-expanded" : ""}`}
      aria-label="Interactive relationship story"
    >
      <div className="relationship-workbench-toolbar">
        <div className="relationship-workbench-title">
          <h3>Relationship map</h3>
          <p>Drag nodes, switch graph modes, and click nodes or edges for evidence.</p>
        </div>
        <div className="relationship-mode-tabs" aria-label="Relationship map mode">
          {modeButtons.map((button) => (
            <button
              aria-pressed={mode === button.mode}
              key={button.mode}
              onClick={() => {
                setMode(button.mode);
                setSelected(undefined);
              }}
              type="button"
            >
              {button.label}
            </button>
          ))}
        </div>
      </div>
      <div className="relationship-workbench-grid">
        <div className="relationship-map-shell">
          <div className="relationship-map-controls">
            <span>{`${layout.nodes.length} nodes`}</span>
            <span>{`${layout.edges.length} edges`}</span>
            <div className="relationship-map-zoom-controls" aria-label="Relationship map zoom">
              <button
                aria-label="Zoom out"
                onClick={() => setViewport((current) => zoomViewport(current, -zoomStep))}
                type="button"
              >
                -
              </button>
              <span>{`${Math.round(viewport.scale * 100)}%`}</span>
              <button
                aria-label="Zoom in"
                onClick={() => setViewport((current) => zoomViewport(current, zoomStep))}
                type="button"
              >
                +
              </button>
            </div>
            <button onClick={resetView} type="button">
              Reset view
            </button>
            <button
              aria-label={expanded ? "Collapse graph widget" : "Expand graph widget"}
              aria-pressed={expanded}
              className="relationship-map-widget-toggle"
              onClick={() => setExpanded((current) => !current)}
              title={expanded ? "Collapse graph widget" : "Expand graph widget"}
              type="button"
            >
              {expanded ? <Minimize2 aria-hidden="true" size={16} /> : <Maximize2 aria-hidden="true" size={16} />}
              <span>{expanded ? "Collapse" : "Expand"}</span>
            </button>
          </div>
          {selected?.edge !== undefined ? (
            <RelationshipSelectionSummary edge={selected.edge} />
          ) : null}
          <div className="relationship-map-stage" data-testid="relationship-map-stage">
            <svg
              aria-label={`${spotlight.name} relationship map`}
              className="relationship-map-svg"
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
                setViewport((current) =>
                  zoomViewport(current, event.deltaY < 0 ? zoomStep : -zoomStep)
                );
              }}
              role="img"
              viewBox={`0 0 ${graphWidth} ${graphHeight}`}
            >
              <defs>
                <marker
                  id="relationship-arrow"
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
                className="relationship-map-pan-plane"
                height={graphHeight}
                onPointerDown={(event) => {
                  const point = svgPoint(event);
                  setPanState({
                    origin: { x: viewport.x, y: viewport.y },
                    pointerId: event.pointerId,
                    start: point
                  });
                  event.currentTarget.ownerSVGElement?.setPointerCapture(event.pointerId);
                }}
                width={graphWidth}
                x="0"
                y="0"
              />
              <g
                data-testid="relationship-map-viewport"
                transform={viewportTransform(viewport)}
              >
                {layout.edges.map((edge) => (
                  <RelationshipEdgeView
                    edge={edge}
                    key={`${nodeId(edge.source)}:${nodeId(edge.target)}:${edge.label}`}
                    onSelect={() => setSelected({ edge: edgeToModel(edge) })}
                    selected={selected?.edge?.source === nodeId(edge.source) &&
                      selected.edge.target === nodeId(edge.target) &&
                      selected.edge.label === edge.label}
                  />
                ))}
                {layout.nodes.map((node) => (
                  <RelationshipNodeView
                    key={node.id}
                    node={node}
                    onDrag={(point) => {
                      setDraggedPositions((current) => {
                        const next = new Map(current);
                        next.set(node.id, point);
                        return next;
                      });
                    }}
                    onSelect={() => setSelected({ node })}
                    selected={selected?.node?.id === node.id ||
                      (selected === undefined && selectedNode?.id === node.id)}
                    viewport={viewport}
                  />
                ))}
              </g>
            </svg>
            <RelationshipInspector selected={selected ?? { node: selectedNode }} spotlight={spotlight} />
          </div>
        </div>
      </div>
    </section>
  );
}

function RelationshipEdgeView({
  edge,
  onSelect,
  selected
}: {
  readonly edge: LayoutEdge;
  readonly onSelect: () => void;
  readonly selected: boolean;
}): React.JSX.Element {
  const source = edgePoint(edge.source);
  const target = edgePoint(edge.target);
  const path = `M${source.x},${source.y} C${(source.x + target.x) / 2},${source.y} ${(source.x + target.x) / 2},${target.y} ${target.x},${target.y}`;
  const labelX = (source.x + target.x) / 2;
  const labelY = (source.y + target.y) / 2;

  return (
    <g
      aria-label={`Inspect ${edge.label} relationship from ${nodeLabel(edge.source)} to ${nodeLabel(edge.target)}`}
      className={`relationship-edge relationship-edge-${edge.family}${selected ? " relationship-edge-selected" : ""}`}
      onClick={(event) => {
        event.stopPropagation();
        onSelect();
      }}
      onKeyDown={(event) => {
        if (event.key === "Enter" || event.key === " ") {
          event.preventDefault();
          onSelect();
        }
      }}
      onPointerDown={(event) => event.stopPropagation()}
      onPointerUp={(event) => event.stopPropagation()}
      role="button"
      tabIndex={0}
    >
      <path className="relationship-edge-hitbox" d={path} />
      <path className="relationship-edge-path" d={path} markerEnd="url(#relationship-arrow)" />
      <rect
        className="relationship-edge-label-bg"
        height="24"
        rx="6"
        width={Math.max(96, edge.label.length * 7 + 24)}
        x={labelX - Math.max(96, edge.label.length * 7 + 24) / 2}
        y={labelY - 18}
      />
      <text className="relationship-edge-label" x={labelX} y={labelY - 2}>
        {edge.label}
      </text>
    </g>
  );
}

function RelationshipNodeView({
  node,
  onDrag,
  onSelect,
  selected,
  viewport
}: {
  readonly node: LayoutNode;
  readonly onDrag: (point: Point) => void;
  readonly onSelect: () => void;
  readonly selected: boolean;
  readonly viewport: ViewportTransform;
}): React.JSX.Element {
  const [dragging, setDragging] = useState(false);
  const width = node.kind === "service" ? 164 : Math.max(142, Math.min(230, node.label.length * 7 + 42));
  const height = node.kind === "service" ? 58 : 48;

  return (
    <g
      aria-label={`Inspect ${node.label} ${nodeDescriptor(node)}`}
      className={`relationship-node relationship-node-kind-${node.kind} relationship-node-${node.technology}${selected ? " relationship-node-selected" : ""}`}
      data-draggable="true"
      onClick={onSelect}
      onKeyDown={(event) => {
        if (event.key === "Enter" || event.key === " ") {
          event.preventDefault();
          onSelect();
        }
      }}
      onPointerDown={(event) => {
        event.stopPropagation();
        setDragging(true);
        event.currentTarget.setPointerCapture(event.pointerId);
      }}
      onPointerMove={(event) => {
        if (!dragging) {
          return;
        }
        const point = graphPoint(event, viewport);
        onDrag({
          x: clamp(point.x, 56, graphWidth - 56),
          y: clamp(point.y, 46, graphHeight - 46)
        });
      }}
      onPointerUp={(event) => {
        event.stopPropagation();
        setDragging(false);
        event.currentTarget.releasePointerCapture(event.pointerId);
      }}
      role="button"
      tabIndex={0}
      transform={`translate(${node.x}, ${node.y})`}
    >
      <rect height={height} rx="8" width={width} x={-width / 2} y={-height / 2} />
      <circle className="relationship-node-tech" cx={-width / 2 + 20} cy={0} r="8" />
      <text x={node.kind === "service" ? 0 : -width / 2 + 38} y="5">
        {node.label}
      </text>
    </g>
  );
}

function graphPoint(event: React.PointerEvent<SVGElement>, viewport: ViewportTransform): Point {
  const point = svgPoint(event);
  return {
    x: (point.x - viewport.x) / viewport.scale,
    y: (point.y - viewport.y) / viewport.scale
  };
}

function svgPoint(event: React.PointerEvent<SVGElement>): Point {
  const svg = event.currentTarget instanceof SVGSVGElement ?
    event.currentTarget :
    event.currentTarget.ownerSVGElement;
  if (svg === null) {
    return { x: event.clientX, y: event.clientY };
  }
  const point = svg.createSVGPoint();
  point.x = event.clientX;
  point.y = event.clientY;
  const matrix = svg.getScreenCTM();
  if (matrix === null) {
    return { x: event.clientX, y: event.clientY };
  }
  const transformed = point.matrixTransform(matrix.inverse());
  return { x: transformed.x, y: transformed.y };
}

function zoomViewport(
  viewport: ViewportTransform,
  delta: number
): ViewportTransform {
  return {
    ...viewport,
    scale: clamp(roundToHundredth(viewport.scale + delta), minZoom, maxZoom)
  };
}

function viewportTransform(viewport: ViewportTransform): string {
  return `translate(${formatGraphNumber(viewport.x)} ${formatGraphNumber(viewport.y)}) scale(${formatGraphNumber(viewport.scale)})`;
}

function roundToHundredth(value: number): number {
  return Math.round(value * 100) / 100;
}

function formatGraphNumber(value: number): string {
  return Number.isInteger(value) ? String(value) : value.toFixed(2).replace(/0+$/, "").replace(/\.$/, "");
}

function nodeDescriptor(node: LayoutNode): string {
  if (node.kind === "service") {
    return "Service workload";
  }
  if (node.kind === "runtime") {
    return "Runtime target";
  }
  return technologyLabel(node.technology);
}

function clamp(value: number, min: number, max: number): number {
  return Math.max(min, Math.min(max, value));
}

const modeButtons: readonly { readonly label: string; readonly mode: GraphMode }[] = [
  { label: "Deployment flow", mode: "deployment" },
  { label: "Config dependencies", mode: "config" },
  { label: "All relationships", mode: "all" }
];
