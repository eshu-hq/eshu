import { useEffect, useMemo, useState } from "react";

import type { EshuApiClient } from "../api/client";
import {
  buildServiceTopology,
  loadServiceTopology,
  loadTopologyServices,
  type ServiceTopology,
  type TopologyEdge,
  type TopologyKind,
  type TopologyNode,
} from "../api/serviceTopology";
import { Badge, Panel, StatTile } from "../components/atoms";
import type { ConsoleModel, ServiceRow } from "../console/types";
import { LAYER_COLOR } from "../console/types";
import "./topology.css";

const KIND_COLOR: Record<TopologyKind, string> = {
  edge: "#ff9d2e",
  hostname: "#14b8a6",
  origin: "#8b5cf6",
  pending: "#9aa4af",
  repo: "#f3ebdd",
  runtime: "#4f8cff",
  service: "#ff8a00",
  workload: "#14b8a6",
};

export function TopologyPage({
  client,
  model,
  onOpenService,
}: {
  readonly client?: EshuApiClient;
  readonly model: ConsoleModel;
  readonly onOpenService: (name: string) => void;
}): React.JSX.Element {
  // Seed the service list from the shared snapshot; fetch from the catalog when
  // the snapshot has not yet populated (or when the catalog returns only
  // repositories rather than promoted service/workload nodes).
  const snapshotServices = useMemo(
    () => model.services.filter((row) => row.name.length > 0),
    [model],
  );
  const [catalogServices, setCatalogServices] = useState<readonly ServiceRow[]>([]);
  const [pickerLoading, setPickerLoading] = useState(false);

  useEffect(() => {
    if (!client) return;
    // Only fetch catalog services when the snapshot is empty so we don't issue
    // a redundant round-trip on pages that already have services.
    if (snapshotServices.length > 0) return;
    let active = true;
    setPickerLoading(true);
    void loadTopologyServices(client).then((rows) => {
      if (!active) return;
      setCatalogServices(rows);
      setPickerLoading(false);
    });
    return () => {
      active = false;
    };
  }, [client, snapshotServices.length]);

  const services = snapshotServices.length > 0 ? snapshotServices : catalogServices;
  const [selectedName, setSelectedName] = useState("");
  const selected = services.find((row) => row.name === selectedName) ?? services[0];
  const [graph, setGraph] = useState<ServiceTopology | null>(null);
  const [selectedNode, setSelectedNode] = useState<TopologyNode | null>(null);

  // Default to the first available service once the list populates.
  useEffect(() => {
    if (selectedName.length === 0 && services[0]) {
      setSelectedName(services[0].name);
    }
  }, [selectedName, services]);

  const isPickerReady = !pickerLoading && (services.length > 0 || !client);
  const [loadState, setLoadState] = useState<"loading" | "ready" | "unavailable">("loading");

  useEffect(() => {
    let active = true;
    setSelectedNode(null);
    if (!selected) {
      setGraph(null);
      setLoadState(isPickerReady ? "unavailable" : "loading");
      return () => {
        active = false;
      };
    }
    if (!client) {
      setGraph(buildServiceTopology({ service: selected, trafficPaths: [] }));
      setLoadState("unavailable");
      return () => {
        active = false;
      };
    }
    setLoadState("loading");
    void loadServiceTopology(client, selected)
      .then((topology) => {
        if (!active) return;
        setGraph(topology);
        setLoadState(topology.meta.provenance === "live" ? "ready" : "unavailable");
      })
      .catch(() => {
        if (!active) return;
        setGraph(buildServiceTopology({ service: selected, trafficPaths: [] }));
        setLoadState("unavailable");
      });
    return () => {
      active = false;
    };
  }, [client, isPickerReady, selected]);

  const topology =
    graph ?? (selected ? buildServiceTopology({ service: selected, trafficPaths: [] }) : null);

  return (
    <div className="page topology-page" style={{ maxWidth: "none" }}>
      <div className="page-intro">
        <h2>Topology</h2>
        <p>
          Service-level code-to-cloud topology from{" "}
          <span className="mono">GET /api/v0/services/{"{name}"}/story</span> and{" "}
          <span className="mono">GET /api/v0/services/{"{name}"}/context</span>. Missing
          cloud-resource segments stay explicit instead of being invented.
        </p>
      </div>

      <div className="topology-controls">
        <label className="topology-select">
          <span>Service</span>
          <select
            aria-label="Service"
            disabled={services.length === 0}
            onChange={(event) => setSelectedName(event.target.value)}
            value={selected?.name ?? ""}
          >
            {services.map((service) => (
              <option key={service.id || service.name} value={service.name}>
                {service.name}
              </option>
            ))}
          </select>
        </label>
        <Badge tone={loadState === "ready" ? "teal" : "warn"} dot>
          {loadState === "loading" ? "loading" : loadState}
        </Badge>
      </div>

      {selected && topology ? (
        <>
          <div className="grid g-4">
            <StatTile
              color={topology.meta.provenance === "live" ? "var(--teal)" : "var(--ember)"}
              label="Exposure"
              sub={topology.meta.environment}
              value={topology.meta.exposure}
            />
            <StatTile
              color="var(--blue)"
              label="Nodes"
              sub={`${topology.edges.length} typed edges`}
              value={topology.nodes.length}
            />
            <StatTile
              color="var(--violet)"
              label="Dependencies"
              sub="upstream relationship evidence"
              value={topology.meta.dependencyCount}
            />
            <StatTile
              color="var(--ember)"
              label="Service"
              sub={selected.repo || "repo pending"}
              value={topology.meta.serviceName}
            />
          </div>

          <Panel
            className="flush mt"
            title={`${selected.name} topology`}
            sub="Click a node for evidence. Open service jumps to the Service Atlas."
            action={
              <button className="btn-ghost active" onClick={() => onOpenService(selected.name)}>
                Open service
              </button>
            }
          >
            <div className="topology-stage">
              <TopologyCanvas
                graph={topology}
                onSelect={setSelectedNode}
                selectedId={selectedNode?.id}
              />
              {selectedNode ? (
                <TopologyInspector
                  graph={topology}
                  node={selectedNode}
                  onClose={() => setSelectedNode(null)}
                />
              ) : null}
            </div>
          </Panel>
        </>
      ) : (
        <p className="empty">No services are available from this source.</p>
      )}
    </div>
  );
}

function TopologyCanvas({
  graph,
  onSelect,
  selectedId,
}: {
  readonly graph: ServiceTopology;
  readonly onSelect: (node: TopologyNode) => void;
  readonly selectedId?: string;
}): React.JSX.Element {
  const byId = new Map(graph.nodes.map((node) => [node.id, node]));
  const activeEdges =
    selectedId === undefined
      ? new Set<string>()
      : new Set(
          graph.edges
            .filter((edge) => edge.s === selectedId || edge.t === selectedId)
            .flatMap((edge) => [edge.s, edge.t]),
        );

  return (
    <div className="topology-canvas" tabIndex={0}>
      <svg aria-label={`${graph.meta.serviceName} topology`} viewBox="0 0 1360 560">
        <defs>
          <marker
            id="topology-arrow"
            markerHeight="9"
            markerWidth="9"
            orient="auto"
            refX="7.5"
            refY="4"
          >
            <path d="M0 0 L8 4 L0 8 Z" fill="var(--edge)" />
          </marker>
        </defs>
        <text className="topology-lane" x="40" y="46">
          TRAFFIC PATH
        </text>
        <line className="topology-lane-line" x1="40" x2="1240" y1="60" y2="60" />
        <text className="topology-lane" x="40" y="332">
          SUPPLY CHAIN
        </text>
        <line className="topology-lane-line" x1="40" x2="1240" y1="346" y2="346" />
        {graph.edges.map((edge) => {
          const source = byId.get(edge.s);
          const target = byId.get(edge.t);
          if (source === undefined || target === undefined) return null;
          return (
            <TopologyEdgePath
              edge={edge}
              key={`${edge.s}:${edge.t}:${edge.verb}`}
              source={source}
              target={target}
            />
          );
        })}
        {graph.nodes.map((node) => (
          <TopologyNodeView
            active={selectedId === undefined || selectedId === node.id || activeEdges.has(node.id)}
            key={node.id}
            node={node}
            onSelect={onSelect}
            selected={selectedId === node.id}
          />
        ))}
      </svg>
      <div className="topology-legend">
        {(["infra", "runtime", "deploy", "code"] as const).map((layer) => (
          <span key={layer}>
            <i style={{ background: LAYER_COLOR[layer] }} />
            {layer}
          </span>
        ))}
      </div>
    </div>
  );
}

function TopologyEdgePath({
  edge,
  source,
  target,
}: {
  readonly edge: TopologyEdge;
  readonly source: TopologyNode;
  readonly target: TopologyNode;
}): React.JSX.Element {
  const startX = source.x + (target.x >= source.x ? source.w / 2 : -source.w / 2);
  const endX = target.x + (target.x >= source.x ? -target.w / 2 : target.w / 2);
  const midX = startX + (endX - startX) * 0.5;
  const path = `M${startX} ${source.y} C${midX} ${source.y} ${midX} ${target.y} ${endX} ${target.y}`;
  const color = LAYER_COLOR[edge.layer];
  return (
    <g className="topology-edge">
      <path
        d={path}
        markerEnd="url(#topology-arrow)"
        style={{ "--edge-color": color } as React.CSSProperties}
      />
      <text
        className="topology-edge-label"
        style={{ fill: color }}
        x={(source.x + target.x) / 2}
        y={(source.y + target.y) / 2 - 10}
      >
        {edge.verb}
      </text>
    </g>
  );
}

function TopologyNodeView({
  active,
  node,
  onSelect,
  selected,
}: {
  readonly active: boolean;
  readonly node: TopologyNode;
  readonly onSelect: (node: TopologyNode) => void;
  readonly selected: boolean;
}): React.JSX.Element {
  const color = KIND_COLOR[node.kind];
  const clipId = `topology-clip-${node.id.replace(/[^a-zA-Z0-9_-]/g, "-")}`;
  return (
    <g
      className={`topology-node${selected ? " is-selected" : ""}${node.hero ? " is-hero" : ""}${active ? "" : " is-faded"}`}
      onClick={() => onSelect(node)}
      role="button"
      style={{ "--node-color": color } as React.CSSProperties}
      tabIndex={0}
      transform={`translate(${node.x - node.w / 2} ${node.y - node.h / 2})`}
    >
      <rect className="topology-node-box" height={node.h} rx="12" width={node.w} />
      <rect className="topology-node-accent" height={node.h} rx="2" width="4" />
      <clipPath id={clipId}>
        <rect height={node.h} width={node.w - 18} x="0" y="0" />
      </clipPath>
      <circle
        className="topology-node-dot"
        cx="22"
        cy={node.h / 2 - (node.hero ? 10 : 0)}
        r="5.5"
      />
      <g clipPath={`url(#${clipId})`}>
        <text
          className="topology-node-label"
          x="40"
          y={node.h / 2 - (node.sub ? 4 : -4) - (node.hero ? 10 : 0)}
        >
          {node.label}
        </text>
        <text className="topology-node-sub" x="40" y={node.h / 2 + 14 - (node.hero ? 10 : 0)}>
          {node.sub}
        </text>
      </g>
      <text className="topology-node-provenance" x={node.w - 14} y={node.h - 12}>
        {node.provenance}
      </text>
    </g>
  );
}

function TopologyInspector({
  graph,
  node,
  onClose,
}: {
  readonly graph: ServiceTopology;
  readonly node: TopologyNode;
  readonly onClose: () => void;
}): React.JSX.Element {
  const inbound = graph.edges.filter((edge) => edge.t === node.id);
  const outbound = graph.edges.filter((edge) => edge.s === node.id);
  const byId = new Map(graph.nodes.map((item) => [item.id, item]));
  return (
    <aside
      className="topology-inspector"
      style={{ "--node-color": KIND_COLOR[node.kind] } as React.CSSProperties}
    >
      <button
        aria-label="Close topology inspector"
        className="topology-inspector-close"
        onClick={onClose}
      >
        ×
      </button>
      <span className="topology-inspector-kind">{node.kind}</span>
      <h3>{node.label}</h3>
      <p>{node.sub}</p>
      <dl>
        <div>
          <dt>Provenance</dt>
          <dd>{node.provenance}</dd>
        </div>
        <div>
          <dt>Inbound</dt>
          <dd>{inbound.length}</dd>
        </div>
        <div>
          <dt>Outbound</dt>
          <dd>{outbound.length}</dd>
        </div>
      </dl>
      {[...inbound, ...outbound].length > 0 ? (
        <div className="topology-inspector-edges">
          {[...inbound, ...outbound].map((edge) => {
            const other = byId.get(edge.s === node.id ? edge.t : edge.s);
            return (
              <div key={`${edge.s}:${edge.t}:${edge.verb}`}>
                <strong>{edge.verb}</strong>
                <span>{other?.label ?? "unknown"}</span>
              </div>
            );
          })}
        </div>
      ) : null}
    </aside>
  );
}
