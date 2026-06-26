import { useCallback, useEffect, useRef, useState, type FormEvent } from "react";
import { useNavigate, useParams, useSearchParams } from "react-router-dom";

import type { AnswerNextCall } from "../api/answerPacket";
import type { VisualizationEdge, VisualizationNode } from "../api/answerVisualization";
import type { EshuApiClient } from "../api/client";
import {
  loadServiceEvidenceGraph,
  type ServiceEvidenceGraphResult
} from "../api/serviceEvidenceGraph";
import { Badge, FreshDot, Panel, TruthChip } from "../components/atoms";
import type { EvidenceSelection } from "../components/EvidenceDrawer";
import { EvidencePanel } from "../components/EvidencePanel";
import { GraphCanvas } from "../components/GraphCanvas";
import { visualizationEvidencePanelData } from "../components/visualizationEvidencePanel";
import { defaultServiceName } from "../console/defaultEntity";
import type { ConsoleModel } from "../console/types";
import { uiFresh, uiTruth } from "../console/types";
import "./serviceEvidenceGraph.css";

type Selection = EvidenceSelection;

// ServiceEvidenceGraphPage renders the bounded service-story visualization
// packet as an interactive code-to-cloud graph. Every node, edge, truth label,
// limitation, and truncation note is driven by the derive route; the page never
// invents topology and surfaces empty, unsupported, truncated, and error states
// as first-class UI.
export function ServiceEvidenceGraphPage({
  client,
  model,
  onOpenService
}: {
  readonly client?: EshuApiClient;
  readonly model: ConsoleModel;
  readonly onOpenService?: (name: string) => void;
}): React.JSX.Element {
  const params = useParams();
  const navigate = useNavigate();
  const [searchParams] = useSearchParams();
  const routeName = (params.serviceName ?? searchParams.get("service") ?? "").trim();
  const [input, setInput] = useState(routeName);
  const [result, setResult] = useState<ServiceEvidenceGraphResult | null>(null);
  const [busy, setBusy] = useState(false);
  const [selected, setSelected] = useState<Selection | null>(null);
  const loadedRef = useRef<string | null>(null);
  // Monotonic load token so a slow in-flight request for a previously selected
  // service can never overwrite the result of a newer one. Without it, a late
  // A response could replace B's graph and present a stale, wrong-service graph.
  const loadTokenRef = useRef(0);

  const runLoad = useCallback(
    async (serviceName: string) => {
      const trimmed = serviceName.trim();
      if (!client || trimmed.length === 0) {
        return;
      }
      const token = (loadTokenRef.current += 1);
      setBusy(true);
      setSelected(null);
      try {
        const loaded = await loadServiceEvidenceGraph(client, trimmed);
        if (token === loadTokenRef.current) {
          setResult(loaded);
        }
      } finally {
        if (token === loadTokenRef.current) {
          setBusy(false);
        }
      }
    },
    [client]
  );

  useEffect(() => {
    // Both /service-story and /service-story/:serviceName render the same page
    // instance, so navigating back to the bare route must clear the prior graph
    // rather than leave a stale, no-longer-selected service on screen.
    if (routeName.length === 0) {
      // Auto-load a sensible default on open: when the live catalog has a
      // service, redirect the bare route to it so the page renders evidence
      // immediately instead of an empty form. The form/picker still overrides.
      const fallback = client ? defaultServiceName(model) : "";
      if (fallback.length > 0) {
        navigate(`/service-story/${encodeURIComponent(fallback)}`, { replace: true });
        return;
      }
      loadedRef.current = null;
      loadTokenRef.current += 1;
      setResult(null);
      setSelected(null);
      setInput("");
      return;
    }
    if (routeName === loadedRef.current) {
      return;
    }
    loadedRef.current = routeName;
    setInput(routeName);
    void runLoad(routeName);
  }, [client, model, navigate, routeName, runLoad]);

  function submit(event: FormEvent<HTMLFormElement>): void {
    event.preventDefault();
    const next = input.trim();
    if (next.length === 0) {
      return;
    }
    if (next === routeName) {
      loadedRef.current = null;
      void runLoad(next);
      return;
    }
    navigate(`/service-story/${encodeURIComponent(next)}`);
  }

  const services = model.services.slice(0, 8);

  return (
    <div className="seg-page">
      <Panel
        title="Service evidence graph"
        sub="Render the bounded service-story visualization packet with source-backed truth labels."
      >
        <form className="seg-form" onSubmit={submit}>
          <label className="seg-field">
            <span>Service name</span>
            <input
              autoComplete="off"
              list="seg-service-options"
              name="serviceName"
              onChange={(event) => setInput(event.target.value)}
              placeholder="Service name"
              value={input}
            />
          </label>
          <datalist id="seg-service-options">
            {model.services.map((service) => (
              <option key={service.id} value={service.name} />
            ))}
          </datalist>
          <button className="btn" disabled={busy || !client} type="submit">
            {busy ? "Loading…" : "Show evidence graph"}
          </button>
        </form>
        {services.length > 0 ? (
          <div className="seg-chips" aria-label="Known services">
            {services.map((service) => (
              <button
                key={service.id}
                className="seg-chip"
                onClick={() => navigate(`/service-story/${encodeURIComponent(service.name)}`)}
                type="button"
              >
                {service.name}
              </button>
            ))}
          </div>
        ) : null}
        {!client ? (
          <p className="seg-muted">Connect to an Eshu API to render service evidence graphs.</p>
        ) : null}
      </Panel>

      {result !== null ? <ServiceEvidenceResult
        onOpenService={onOpenService}
        onSelect={setSelected}
        result={result}
        selected={selected}
      /> : null}
    </div>
  );
}

function ServiceEvidenceResult({
  onOpenService,
  onSelect,
  result,
  selected
}: {
  readonly onOpenService?: (name: string) => void;
  readonly onSelect: (selection: Selection | null) => void;
  readonly result: ServiceEvidenceGraphResult;
  readonly selected: Selection | null;
}): React.JSX.Element {
  const { graph, packet, serviceName, storyError, truth } = result;

  if (storyError !== null) {
    return (
      <Panel title={serviceName || "Service"} className="seg-result">
        <div className="seg-state seg-error" role="alert">
          <strong>{storyError.code}: {storyError.message}</strong>
          <p>No service-story evidence is shown because the source route did not return a packet.</p>
        </div>
      </Panel>
    );
  }

  if (packet === null) {
    return (
      <Panel title={serviceName || "Service"} className="seg-result">
        <div className="seg-state" role="status">
          <strong>No visualization packet returned.</strong>
        </div>
      </Panel>
    );
  }

  const title = packet.title || serviceName || "Service story";
  const nodeTypes = uniqueNodeTypes(packet.nodes);

  return (
    <Panel
      className="seg-result"
      title={title}
      action={onOpenService !== undefined && serviceName.length > 0 ? (
        <button className="btn ghost" onClick={() => onOpenService(serviceName)} type="button">
          Open service
        </button>
      ) : undefined}
    >
      <div className="seg-truth">
        {truth === null ? (
          <Badge tone="warn">truth unavailable</Badge>
        ) : (
          <>
            {truth.capability ? <span className="mono">{truth.capability}</span> : null}
            <TruthChip level={uiTruth(truth.level)} />
            <FreshDot state={uiFresh(truth.freshness.state)} />
          </>
        )}
      </div>

      {packet.supported && packet.truncation.truncated ? (
        <div className="seg-trunc" role="status">
          Subgraph truncated to stay within bounds — {packet.truncation.droppedNodeCount} nodes and{" "}
          {packet.truncation.droppedEdgeCount} edges dropped.
        </div>
      ) : null}

      {!packet.supported ? (
        <div className="seg-state" role="status">
          <strong>No renderable subgraph for this service story.</strong>
          <p>The derive route returned an unsupported packet; nothing is invented to fill the gap.</p>
          <StateList title="Limitations" values={packet.limitations} />
          <NextCalls calls={packet.recommendedNextCalls} />
        </div>
      ) : graph.nodes.length === 0 ? (
        <div className="seg-state" role="status">
          <strong>No graph rows returned for this service story.</strong>
          <p>The packet is supported but carried no nodes to render.</p>
          <StateList title="Limitations" values={packet.limitations} />
        </div>
      ) : (
        <>
          <p className="seg-limits">{limitsLine(packet.limits, graph.nodes.length, graph.edges.length)}</p>
          <NodeTypeLegend types={nodeTypes} />
          <GraphCanvas
            graph={graph}
            height={420}
            onSelect={(node) => onSelect({ kind: "node", id: node.id })}
            selectedId={selected?.kind === "node" ? selected.id : undefined}
          />
          <RelationshipList edges={packet.edges} onSelect={onSelect} selected={selected} />
          <SelectedEvidence packet={packet} selected={selected} onClose={() => onSelect(null)} />
          <StateList title="Limitations" values={packet.limitations} />
          <NextCalls calls={packet.recommendedNextCalls} />
        </>
      )}
    </Panel>
  );
}

// SelectedEvidence renders the shared inline evidence panel for the selected
// graph node or evidence-lane relationship pill. It maps the packet selection
// into the packet-agnostic EvidencePanelData contract and renders nothing when
// nothing is selected or the selection is absent from the packet.
function SelectedEvidence({
  onClose,
  packet,
  selected
}: {
  readonly onClose: () => void;
  readonly packet: NonNullable<ServiceEvidenceGraphResult["packet"]>;
  readonly selected: Selection | null;
}): React.JSX.Element | null {
  if (selected === null) {
    return null;
  }
  const panelData = visualizationEvidencePanelData(packet, selected);
  if (panelData === null) {
    return null;
  }
  return <EvidencePanel data={panelData} onClose={onClose} />;
}

function NodeTypeLegend({ types }: { readonly types: readonly string[] }): React.JSX.Element | null {
  if (types.length === 0) {
    return null;
  }
  return (
    <div className="seg-legend" aria-label="Node types present">
      <span className="seg-legend-label">Node types:</span>
      {types.map((type) => (
        <span key={type} className={`seg-kind seg-kind-${type}`}>{type}</span>
      ))}
    </div>
  );
}

function RelationshipList({
  edges,
  onSelect,
  selected
}: {
  readonly edges: readonly VisualizationEdge[];
  readonly onSelect: (selection: Selection | null) => void;
  readonly selected: Selection | null;
}): React.JSX.Element | null {
  if (edges.length === 0) {
    return null;
  }
  return (
    <section className="seg-edges" aria-label="Relationships">
      <h3>Relationships</h3>
      <ul>
        {edges.map((edge) => {
          const active = selected?.kind === "edge" && selected.id === edge.id;
          return (
            <li key={edge.id}>
              <button
                className={`seg-edge${active ? " is-active" : ""}`}
                onClick={() => onSelect({ kind: "edge", id: edge.id })}
                type="button"
              >
                <span className="mono">{edge.source} → {edge.target}</span>
                <span className="seg-edge-verb">{edge.relationship || "RELATED"}</span>
                {edge.truthLabel.length > 0 ? <span className="seg-edge-truth">{edge.truthLabel}</span> : null}
              </button>
            </li>
          );
        })}
      </ul>
    </section>
  );
}

function NextCalls({ calls }: { readonly calls: readonly AnswerNextCall[] }): React.JSX.Element | null {
  if (calls.length === 0) {
    return null;
  }
  return (
    <section className="seg-next" aria-label="Recommended next calls">
      <h3>Recommended next calls</h3>
      <ul>
        {calls.map((call, index) => (
          <li key={`${call.tool}-${call.route ?? ""}-${index}`}>
            <span className="mono">{call.tool || call.route}</span>
            {call.reason.length > 0 ? <span className="seg-next-reason">{call.reason}</span> : null}
          </li>
        ))}
      </ul>
    </section>
  );
}

function StateList({ title, values }: { readonly title: string; readonly values: readonly string[] }): React.JSX.Element | null {
  const unique = [...new Set(values.map((value) => value.trim()).filter((value) => value.length > 0))];
  if (unique.length === 0) {
    return null;
  }
  return (
    <section className="seg-statelist">
      <h3>{title}</h3>
      <ul>
        {unique.map((value) => <li key={value}>{value}</li>)}
      </ul>
    </section>
  );
}

// limitsLine reports how much of the bounded subgraph is shown. It only asserts
// an "up to N" cap when the server actually returned one, so the UI never claims
// a false bound (e.g. "of up to 0 nodes") when the limits block is omitted.
function limitsLine(
  limits: NonNullable<ServiceEvidenceGraphResult["packet"]>["limits"],
  nodesShown: number,
  edgesShown: number
): string {
  const nodeCount = limits.nodeCount || nodesShown;
  const edgeCount = limits.edgeCount || edgesShown;
  const nodeCap = limits.maxNodes > 0 ? `of up to ${limits.maxNodes} ` : "";
  const edgeCap = limits.maxEdges > 0 ? `of up to ${limits.maxEdges} ` : "";
  return `Showing ${nodeCount} ${nodeCap}nodes and ${edgeCount} ${edgeCap}edges.`;
}

function uniqueNodeTypes(nodes: readonly VisualizationNode[]): readonly string[] {
  return [...new Set(nodes.map((node) => node.type).filter((type) => type.length > 0))].sort();
}
