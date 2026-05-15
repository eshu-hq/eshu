import { useMemo, useState } from "react";
import type { ServiceSpotlight } from "../api/serviceSpotlight";
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
  type Point,
  type RelationshipEdge,
  type RelationshipNode
} from "./serviceRelationshipGraphModel";

interface SelectedGraphItem {
  readonly edge?: RelationshipEdge;
  readonly node?: RelationshipNode;
}

export function ServiceRelationshipWorkbench({
  spotlight
}: {
  readonly spotlight: ServiceSpotlight;
}): React.JSX.Element {
  const [mode, setMode] = useState<GraphMode>("deployment");
  const [selected, setSelected] = useState<SelectedGraphItem | undefined>();
  const [draggedPositions, setDraggedPositions] = useState<ReadonlyMap<string, Point>>(
    () => new Map()
  );
  const model = useMemo(() => buildGraphModel(spotlight, mode), [mode, spotlight]);
  const layout = useMemo(
    () => applyDraggedPositions(layoutGraph(model), draggedPositions),
    [draggedPositions, model]
  );
  const selectedNode = selected?.node ?? layout.nodes[0];

  return (
    <section className="relationship-workbench" aria-label="Interactive relationship story">
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
            <button
              onClick={() => {
                setDraggedPositions(new Map());
                setSelected(undefined);
              }}
              type="button"
            >
              Reset view
            </button>
          </div>
          <svg
            aria-label={`${spotlight.name} relationship map`}
            className="relationship-map-svg"
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
                selected={selected?.node?.id === node.id || selectedNode?.id === node.id}
              />
            ))}
          </svg>
        </div>
        <RelationshipInspector selected={selected ?? { node: selectedNode }} />
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
      onClick={onSelect}
      onKeyDown={(event) => {
        if (event.key === "Enter" || event.key === " ") {
          event.preventDefault();
          onSelect();
        }
      }}
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
  selected
}: {
  readonly node: LayoutNode;
  readonly onDrag: (point: Point) => void;
  readonly onSelect: () => void;
  readonly selected: boolean;
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
        setDragging(true);
        event.currentTarget.setPointerCapture(event.pointerId);
      }}
      onPointerMove={(event) => {
        if (!dragging) {
          return;
        }
        const point = svgPoint(event);
        onDrag({
          x: clamp(point.x, 56, graphWidth - 56),
          y: clamp(point.y, 46, graphHeight - 46)
        });
      }}
      onPointerUp={(event) => {
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

function RelationshipInspector({
  selected
}: {
  readonly selected: SelectedGraphItem;
}): React.JSX.Element {
  if (selected.edge !== undefined) {
    return (
      <aside className="relationship-inspector" aria-label="Relationship inspector">
        <h4>Selected relationship</h4>
        <dl>
          <div>
            <dt>Verb</dt>
            <dd>{selected.edge.label}</dd>
          </div>
          <div>
            <dt>Source</dt>
            <dd>{selected.edge.source}</dd>
          </div>
          <div>
            <dt>Target</dt>
            <dd>{selected.edge.target}</dd>
          </div>
        </dl>
        <p>{selected.edge.detail}</p>
      </aside>
    );
  }
  return (
    <aside className="relationship-inspector" aria-label="Relationship inspector">
      <h4>{selected.node?.label ?? "Select a node"}</h4>
      {selected.node !== undefined ? (
        <>
          <span>{nodeDescriptor(selected.node)}</span>
          <p>{selected.node.detail}</p>
        </>
      ) : (
        <p>Select a node or relationship to inspect the supporting evidence.</p>
      )}
    </aside>
  );
}

function svgPoint(event: React.PointerEvent<SVGGElement>): Point {
  const svg = event.currentTarget.ownerSVGElement;
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

function nodeDescriptor(node: RelationshipNode): string {
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
