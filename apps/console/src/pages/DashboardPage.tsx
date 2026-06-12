// pages/DashboardPage.tsx
import { useEffect, useMemo, useState } from "react";
import type { EshuApiClient } from "../api/client";
import { loadEntityMapGraph, resolveEntityName } from "../api/eshuGraph";
import type { ConsoleModel, GraphModel, GraphNode, ServiceRow } from "../console/types";
import { fmt, LAYER_COLOR, SEVERITY_COLOR, uiTruth } from "../console/types";
import { StatTile, Panel, TruthChip } from "../components/atoms";
import { AreaChart, Donut, BarRows } from "../components/charts";
import { GraphCanvas } from "../components/GraphCanvas";
import "./dashboardLive.css";

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
  const atlasSeeds = useMemo(() => liveAtlasSeeds(model), [model]);
  const atlasSeed = atlasSeeds[0];
  const seededGraph = useMemo<GraphModel>(
    () => atlasSeed ? { nodes: [atlasSeed], edges: [] } : model.graph,
    [atlasSeed, model.graph]
  );
  const [liveGraph, setLiveGraph] = useState<GraphModel | null>(null);
  const [atlasState, setAtlasState] = useState<AtlasState>({ kind: "idle" });
  const graph = liveGraph ?? seededGraph;
  const [sel, setSel] = useState<GraphNode | undefined>(() => initialSelection(graph));
  const atlasLabel = sel?.label ?? atlasSeed?.label ?? "live graph";
  const graphNodeCount = lastNumber(model.series.graphNodes);
  const relationshipCount = relationshipMetric(model, graph);
  const selectedSpotlightName = sel && (sel.kind === "service" || sel.kind === "workload") ? sel.label : null;
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
    if (!client || atlasSeeds.length === 0 || model.source !== "live") {
      setAtlasState({ kind: "idle" });
      return;
	}

	const liveClient = client;
	let cancelled = false;
	setAtlasState({ kind: "loading", seed: atlasSeeds[0].label });
	async function loadSeed(): Promise<void> {
		try {
			const next = await selectSeedGraph(liveClient, atlasSeeds, () => cancelled);
			if (cancelled) return;
			if (!next) {
				setAtlasState({ kind: "idle" });
          return;
        }
        setLiveGraph(next.graph);
        setSel(initialSelection(next.graph));
			setAtlasState({ kind: "idle" });
		} catch (error) {
			if (cancelled) return;
			setAtlasState({ kind: "error", message: errorMessage(error), seed: atlasSeeds[0].label });
		}
	}
    void loadSeed();
    return () => { cancelled = true; };
  }, [atlasSeeds, client, model.source]);

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
      <div className="dashboard-stat-grid grid g-4">
        <StatTile
          label="Graph nodes"
          value={graphNodeCount === null ? "—" : fmt(graphNodeCount)}
          spark={model.series.graphNodes.length ? model.series.graphNodes : undefined}
          color="var(--teal)"
          sub={graphNodeCount === null ? "node count metric unavailable" : "NornicDB graph node metric"}
        />
        <StatTile
          label="Relationships"
          value={relationshipCount === null ? "—" : fmt(relationshipCount)}
          spark={model.series.graphEdges.length ? model.series.graphEdges : undefined}
          color="var(--ember)"
          sub={relationshipCount === null ? "relationship count metric unavailable" : `${relRows.length} typed verbs observed`}
        />
        <StatTile
          label="Indexed repos"
          value={fmt(r.repositories)}
          color="var(--blue)"
          sub={`${r.workloads} services · ${r.instances} workloads`}
        />
        <StatTile
          label="Queue outstanding"
          value={r.queueOutstanding}
          spark={model.series.queueDepth.length ? model.series.queueDepth : undefined}
          color="var(--violet)"
          sub={`${r.inFlight} in-flight · ${r.deadLetters} dead-letter`}
        />
      </div>

      <Panel
        className="dashboard-atlas-panel mt flush"
        title="Code-to-cloud relationship atlas"
        sub={`${atlasLabel} neighbourhood — click any node or relationship edge to read its evidence`}
        action={selectedSpotlightName && onOpenService ? <button className="btn-ghost" onClick={() => onOpenService(selectedSpotlightName)}>Open spotlight →</button> : null}
      >
        <div className="dashboard-atlas-layout">
          {graph.nodes.length ? (
            <GraphCanvas graph={graph} height={520} onSelect={(node) => { void expandAtlasNode(node); }} selectedId={sel?.id} />
          ) : (
            <div className="gcanvas" style={{ height: 520, display: "grid", placeItems: "center" }}>
              <p className="empty">No graph entities are available from the live model yet.</p>
            </div>
          )}
          <aside className="dashboard-atlas-inspector" aria-label="Relationship atlas inspector">
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
          </aside>
        </div>
      </Panel>

      <div className="grid mt" style={{ gridTemplateColumns: "minmax(0,1.5fr) minmax(0,1fr)", gap: "var(--gap)" }}>
        <Panel title="Ingestion throughput" sub="Facts committed per minute">
          {model.series.ingestRate.length ? <AreaChart data={model.series.ingestRate} color="var(--teal)" h={190} unit=" f/m" /> : <p className="empty" style={{ padding: "48px 12px" }}>Trend history appears when a Prometheus/Mimir metrics source has recent samples. Current queue and runtime numbers are shown above.</p>}
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

function lastNumber(values: readonly number[]): number | null {
  const value = values.at(-1);
  return typeof value === "number" && Number.isFinite(value) ? value : null;
}

function relationshipMetric(model: ConsoleModel, graph: GraphModel): number | null {
  const graphEdgeCount = lastNumber(model.series.graphEdges);
  if (graphEdgeCount !== null) return graphEdgeCount;
  const relationshipTotal = model.relationships.reduce((total, row) => total + row.count, 0);
  if (relationshipTotal > 0) return relationshipTotal;
  return graph.edges.length > 0 ? graph.edges.length : null;
}

// A seed graph is "meaningful" once it has at least this many edges. A single
// edge is the trivial workload->repository self-edge (2 nodes / 1 edge) that
// makes for a weak landing atlas, so we keep probing past it.
const MEANINGFUL_SEED_EDGES = 2;

// Probe at most this many catalog services when hunting for a non-trivial seed.
// Bounds the entity-map calls on first paint regardless of catalog size.
const MAX_SEED_PROBES = 8;

// liveAtlasSeeds returns the ordered candidate seed nodes for the live atlas:
// every named catalog service, in catalog order. The loader probes them in turn
// and lands on the first that yields a meaningful neighbourhood, falling back to
// the most-connected candidate it saw. Empty for demo data or once the model
// already carries a graph.
function liveAtlasSeeds(model: ConsoleModel): readonly GraphNode[] {
  if (model.source !== "live" || model.graph.nodes.length > 0) return [];
  return model.services
    .filter((service) => service.name.trim().length > 0)
    .map(serviceSeedNode);
}

// selectSeedGraph probes candidate seeds in order and returns the first whose
// live neighbourhood clears MEANINGFUL_SEED_EDGES. If none do, it returns the
// most-connected graph seen so it still opens on real (never fabricated) edges.
// Probing is bounded by MAX_SEED_PROBES so first paint stays cheap on large
// catalogs, and stops early when isCancelled() reports the effect was torn down
// (e.g. the model changed mid-scan) to avoid wasting reads.
async function selectSeedGraph(
  client: EshuApiClient,
  seeds: readonly GraphNode[],
  isCancelled: () => boolean
): Promise<{ readonly seed: GraphNode; readonly graph: GraphModel } | undefined> {
  let best: { readonly seed: GraphNode; readonly graph: GraphModel } | undefined;
  for (const seed of seeds.slice(0, MAX_SEED_PROBES)) {
    if (isCancelled()) return best;
    const resolved = await resolveEntityName(client, seed.label);
    const graph = await loadEntityMapGraph(client, resolved);
    if (graph.edges.length >= MEANINGFUL_SEED_EDGES) return { seed, graph };
    if (!best || graph.edges.length > best.graph.edges.length) best = { seed, graph };
  }
  return best;
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
