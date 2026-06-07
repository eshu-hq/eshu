// pages/DashboardPage.tsx
import { useEffect, useMemo, useState } from "react";
import type { EshuApiClient } from "../api/client";
import { loadEntityMapGraph, resolveEntityName } from "../api/eshuGraph";
import type { ConsoleModel, GraphModel, GraphNode, ServiceRow } from "../console/types";
import { fmt, LAYER_COLOR, SEVERITY_COLOR, uiTruth } from "../console/types";
import { StatTile, Panel, TruthChip } from "../components/atoms";
import { AreaChart, Donut, BarRows } from "../components/charts";
import { GraphCanvas } from "../components/GraphCanvas";

type AtlasState =
  | { readonly kind: "idle" }
  | { readonly kind: "loading"; readonly seed: string }
  | { readonly kind: "error"; readonly message: string; readonly seed: string };

export function DashboardPage({ model, client, onOpenService }: {
  readonly model: ConsoleModel;
  readonly client?: EshuApiClient;
  readonly onOpenService?: (name: string) => void;
}): React.JSX.Element {
  const r = model.runtime;
  const atlasSeed = useMemo(() => liveAtlasSeed(model), [model]);
  const seededGraph = useMemo<GraphModel>(
    () => atlasSeed ? { nodes: [atlasSeed], edges: [] } : model.graph,
    [atlasSeed, model.graph]
  );
  const [liveGraph, setLiveGraph] = useState<GraphModel | null>(null);
  const [atlasState, setAtlasState] = useState<AtlasState>({ kind: "idle" });
  const graph = liveGraph ?? seededGraph;
  const [sel, setSel] = useState<GraphNode | undefined>(() => initialSelection(graph));
  const sevTotals = model.vulnerabilities.reduce(
    (a, v) => { const k = v.severity as keyof typeof a; if (k in a) a[k] += 1; return a; },
    { critical: 0, high: 0, medium: 0, low: 0 }
  );
  const relRows = model.relationships.slice().sort((a, b) => b.count - a.count).slice(0, 7)
    .map((x) => ({ label: x.verb, value: x.count, color: LAYER_COLOR[x.layer], detail: x.detail }));
  const serviceNames = new Set(model.services.map((s) => s.name));

  useEffect(() => {
    setSel((current) => graph.nodes.some((n) => n.id === current?.id) ? current : initialSelection(graph));
  }, [graph]);

  useEffect(() => {
    setLiveGraph(null);
    if (!client || !atlasSeed || model.source !== "live") {
      setAtlasState({ kind: "idle" });
      return;
    }

    let cancelled = false;
    setAtlasState({ kind: "loading", seed: atlasSeed.label });
    async function loadSeed(): Promise<void> {
      try {
        const resolved = await resolveEntityName(client, atlasSeed.label);
        const next = await loadEntityMapGraph(client, resolved);
        if (cancelled) return;
        setLiveGraph(next);
        setSel(initialSelection(next));
        setAtlasState({ kind: "idle" });
      } catch (error) {
        if (cancelled) return;
        setAtlasState({ kind: "error", message: errorMessage(error), seed: atlasSeed.label });
      }
    }
    void loadSeed();
    return () => { cancelled = true; };
  }, [atlasSeed, client, model.source]);

  async function expandAtlasNode(node: GraphNode): Promise<void> {
    setSel(node);
    if (!client || model.source !== "live") return;
    setAtlasState({ kind: "loading", seed: node.label });
    try {
      const resolved = await resolveEntityName(client, node.label);
      const next = await loadEntityMapGraph(client, resolved);
      setLiveGraph(next);
      setSel(initialSelection(next));
      setAtlasState({ kind: "idle" });
    } catch (error) {
      setAtlasState({ kind: "error", message: errorMessage(error), seed: node.label });
    }
  }

  return (
    <div className="page">
      <div className="grid g-4">
        <StatTile label="Repositories" value={fmt(r.repositories)} spark={model.series.graphNodes.length ? model.series.graphNodes : undefined} color="var(--teal)" sub={`${r.workloads} workloads · ${r.instances} instances`} />
        <StatTile label="Index status" value={r.indexStatus} color="var(--ember)" sub={`profile ${r.profile}`} />
        <StatTile label="Queue outstanding" value={r.queueOutstanding} spark={model.series.queueDepth.length ? model.series.queueDepth : undefined} color="var(--violet)" sub={`${r.inFlight} in-flight · ${r.deadLetters} dead-letter`} />
        <StatTile label="Succeeded" value={fmt(r.succeeded)} color="var(--blue)" sub="work items (run)" />
      </div>

      <Panel className="mt" title="Code-to-cloud relationship atlas" sub="Select a node to inspect its typed evidence">
        <div className="grid" style={{ gridTemplateColumns: "minmax(0,1fr) 300px", gap: "var(--gap)", alignItems: "start" }}>
          {graph.nodes.length ? (
            <GraphCanvas graph={graph} height={460} onSelect={(node) => { void expandAtlasNode(node); }} selectedId={sel?.id} />
          ) : (
            <div className="gcanvas" style={{ height: 460, display: "grid", placeItems: "center" }}>
              <p className="empty">No graph entities are available from the live model yet.</p>
            </div>
          )}
          <div className="panel" style={{ background: "var(--bg-field)", boxShadow: "none" }}>
            <div className="panel-body">
              {sel ? (
                <div className="inspector">
                  <div className="insp-head"><div><div className="insp-kind">{sel.kind}</div><div className="insp-title">{sel.label}</div></div></div>
                  {sel.sub ? <div className="t-mut mono" style={{ fontSize: ".82rem" }}>{sel.sub}</div> : null}
                  {sel.truth ? <TruthChip level={sel.truth} /> : null}
                  {(sel.kind === "service" || sel.kind === "workload") && onOpenService ? <button className="btn-ghost active" style={{ width: "100%", justifyContent: "center" }} onClick={() => onOpenService(sel.label)}>Open spotlight →</button> : null}
                  {atlasState.kind === "loading" ? <p className="empty">Loading relationships for {atlasState.seed}…</p> : null}
                  {atlasState.kind === "error" ? <p className="src-err">Relationship atlas unavailable for {atlasState.seed}: {atlasState.message}</p> : null}
                  <div className="insp-evi">
                    {graph.edges.filter((e) => e.s === sel.id || e.t === sel.id).map((e, i) => (
                      <div className="insp-evi-row" key={i}>{e.verb} {e.s === sel.id ? `→ ${e.t}` : `← ${e.s}`}</div>
                    ))}
                  </div>
                </div>
              ) : <p className="empty">Select a node.</p>}
            </div>
          </div>
        </div>
      </Panel>

      <div className="grid mt" style={{ gridTemplateColumns: "minmax(0,1.5fr) minmax(0,1fr)", gap: "var(--gap)" }}>
        <Panel title="Ingestion throughput" sub="Facts committed per minute">
          {model.series.ingestRate.length ? <AreaChart data={model.series.ingestRate} color="var(--teal)" h={190} unit=" f/m" /> : <p className="empty" style={{ padding: "48px 12px" }}>Trend history requires the metrics time-series API (#1434). Current queue and runtime numbers are shown above.</p>}
        </Panel>
        <Panel title="Security posture" sub={`${sevTotals.critical} critical · ${sevTotals.high} high`}>
          <div style={{ display: "grid", placeItems: "center", marginBottom: 12 }}>
            <Donut size={138} thickness={17} center={{ value: sevTotals.critical + sevTotals.high, label: "crit + high" }}
              segments={(["critical", "high", "medium", "low"] as const).map((k) => ({ label: k, value: sevTotals[k], color: SEVERITY_COLOR[k] }))} />
          </div>
        </Panel>
      </div>

      <Panel className="mt" title="Relationship coverage" sub="Most-observed typed verbs">
        <BarRows rows={relRows} />
      </Panel>

      <Panel className="mt flush" title="Needs attention" sub="Highest-severity findings with evidence">
        <table className="tbl">
          <thead><tr><th>Finding</th><th>Type</th><th>Entity</th><th>Truth</th></tr></thead>
          <tbody>
            {model.findings.map((f) => {
              // Only services/workloads have a spotlight drawer. Findings keyed by
              // a repo or other entity (e.g. dead code) must not open an empty one.
              const canOpen = onOpenService !== undefined && serviceNames.has(f.entity);
              return (
                <tr key={f.id} onClick={canOpen ? () => onOpenService(f.entity) : undefined} style={canOpen ? { cursor: "pointer" } : undefined}>
                  <td className="cell-stack"><span style={{ color: "var(--bone)", fontWeight: 600 }}>{f.title}</span><small>{f.detail}</small></td>
                  <td className="t-mut">{f.type}</td>
                  <td className="t-name">{f.entity}</td>
                  <td><TruthChip level={uiTruth(f.truth)} /></td>
                </tr>
              );
            })}
          </tbody>
        </table>
      </Panel>
    </div>
  );
}

function initialSelection(graph: GraphModel): GraphNode | undefined {
  return graph.nodes.find((node) => node.hero) ?? graph.nodes[0];
}

function liveAtlasSeed(model: ConsoleModel): GraphNode | undefined {
  if (model.source !== "live" || model.graph.nodes.length > 0) return undefined;
  const row = model.services.find((service) => service.name.trim().length > 0);
  return row ? serviceSeedNode(row) : undefined;
}

function serviceSeedNode(service: ServiceRow): GraphNode {
  const label = service.name.trim();
  const id = service.id.trim() || label;
  const repo = service.repo.trim();
  return {
    col: 1,
    hero: true,
    id,
    kind: serviceKind(service.kind),
    label,
    sub: repo || undefined,
    truth: uiTruth(service.truth)
  };
}

function serviceKind(kind: string): string {
  const lower = kind.toLowerCase();
  if (lower.includes("workload") || lower.includes("deployment")) return "workload";
  if (lower.includes("repo")) return "repo";
  return "service";
}

function errorMessage(error: unknown): string {
  return error instanceof Error ? error.message : "failed";
}
