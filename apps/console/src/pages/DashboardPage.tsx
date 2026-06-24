// pages/DashboardPage.tsx
import { useEffect, useMemo, useRef, useState } from "react";

import type { EshuApiClient } from "../api/client";
import { loadEntityMapGraph, resolveEntityName } from "../api/eshuGraph";
import type { RepoListItem } from "../api/repoCatalog";
import {
  loadSourceBackedSuggestedQuestions,
  type SuggestedQuestion
} from "../api/suggestedQuestions";
import { StatTile, Panel, TruthChip } from "../components/atoms";
import { AreaChart, Donut, BarRows } from "../components/charts";
import { GraphCanvas } from "../components/GraphCanvas";
import { SuggestedQuestions } from "../components/SuggestedQuestions";
import { fmt, LAYER_COLOR, SEVERITY_COLOR, uiTruth } from "../console/types";
import type { ConsoleModel, GraphLayer, GraphModel, GraphNode, RelationshipRow, ServiceRow } from "../console/types";
import "./dashboardLive.css";

const LANDING_LAYERS: readonly GraphLayer[] = ["code", "deploy", "infra", "runtime", "security", "ops"];

type AtlasState =
  | { readonly kind: "idle" }
  | { readonly kind: "loading"; readonly seed: string }
  | { readonly kind: "error"; readonly message: string; readonly seed: string };

type RelationshipCoverageRow = {
  readonly label: string;
  readonly value: number;
  readonly color: string;
  readonly detail: string;
};

export function DashboardPage({ model, client, onOpenService, repositories }: {
  readonly model: ConsoleModel;
  readonly client?: EshuApiClient;
  readonly onOpenService?: (name: string) => void;
  readonly repositories?: readonly RepoListItem[];
}): React.JSX.Element {
  const r = model.runtime;
  const atlasSeeds = useMemo(() => liveAtlasSeeds(model, repositories), [model, repositories]);
  const atlasSeed = atlasSeeds[0];
  const seededGraph = useMemo<GraphModel>(
    () => atlasSeed ? { nodes: [atlasSeed], edges: [] } : model.graph,
    [atlasSeed, model.graph]
  );
  const [liveGraph, setLiveGraph] = useState<GraphModel | null>(null);
  const [enabledLayers, setEnabledLayers] = useState<Record<GraphLayer, boolean>>(
    () => Object.fromEntries(LANDING_LAYERS.map((layer) => [layer, true])) as Record<GraphLayer, boolean>
  );
  const [atlasState, setAtlasState] = useState<AtlasState>({ kind: "idle" });
  const atlasRequestRef = useRef(0);
  const baseGraph = liveGraph ?? seededGraph;
  const graph = useMemo(() => filterGraphByLayer(baseGraph, enabledLayers), [baseGraph, enabledLayers]);
  const layerRows = useMemo(() => layerCounts(baseGraph), [baseGraph]);
  const hotEntities = useMemo(() => hotEntityRows(model, atlasSeeds), [atlasSeeds, model]);
  const [sel, setSel] = useState<GraphNode | undefined>(() => initialSelection(graph));
  const atlasLabel = sel?.label ?? atlasSeed?.label ?? "live graph";
  const graphNodeCount = lastNumber(model.series.graphNodes);
  const relationshipCount = relationshipMetric(model, baseGraph);
  const [suggestedQuestions, setSuggestedQuestions] = useState<readonly SuggestedQuestion[]>([]);
  const selectedSpotlightName = sel && (sel.kind === "service" || sel.kind === "workload") ? sel.label : null;
  const nodeLabels = useMemo(() => new Map(graph.nodes.map((node) => [node.id, node.label])), [graph.nodes]);
  const sevTotals = model.vulnerabilities.reduce(
    (a, v) => { const k = v.severity as keyof typeof a; if (k in a) a[k] += 1; return a; },
    { critical: 0, high: 0, medium: 0, low: 0 }
  );
  const relRows = useMemo(() => relationshipRowsFor(model.relationships, graph), [model.relationships, graph]);
  const serviceNames = new Set(model.services.map((s) => s.name));

  useEffect(() => {
    setSel((current) => graph.nodes.some((n) => n.id === current?.id) ? current : initialSelection(graph));
  }, [graph]);

  useEffect(() => {
    let cancelled = false;
    if (!client || model.source !== "live") {
      setSuggestedQuestions([]);
      return () => { cancelled = true; };
    }
    void loadSourceBackedSuggestedQuestions(client)
      .then((questions) => {
        if (!cancelled) setSuggestedQuestions(questions);
      })
      .catch(() => {
        if (!cancelled) setSuggestedQuestions([]);
      });
    return () => { cancelled = true; };
  }, [client, model.source]);

  useEffect(() => {
    setLiveGraph(null);
    const requestID = atlasRequestRef.current + 1;
    atlasRequestRef.current = requestID;
    if (!client || atlasSeeds.length === 0 || model.source !== "live") {
      setAtlasState({ kind: "idle" });
      return;
    }

    const liveClient = client;
    let cancelled = false;
    setAtlasState({ kind: "loading", seed: atlasSeeds[0].label });
    async function loadSeed(): Promise<void> {
      try {
        const next = await selectSeedGraph(liveClient, atlasSeeds, () => cancelled || requestID !== atlasRequestRef.current);
        if (cancelled || requestID !== atlasRequestRef.current) return;
        if (!next) {
          setAtlasState({ kind: "idle" });
          return;
        }
        setLiveGraph(next.graph);
        setSel(initialSelection(next.graph));
        setAtlasState({ kind: "idle" });
      } catch (error) {
        if (cancelled || requestID !== atlasRequestRef.current) return;
        setAtlasState({ kind: "error", message: errorMessage(error), seed: atlasSeeds[0].label });
      }
    }
    void loadSeed();
    return () => { cancelled = true; };
  }, [atlasSeeds, client, model.source]);

  async function expandAtlasNode(node: GraphNode): Promise<void> {
    const requestID = atlasRequestRef.current + 1;
    atlasRequestRef.current = requestID;
    setSel(node);
    if (!client || model.source !== "live") return;
    setAtlasState({ kind: "loading", seed: node.label });
    try {
      const resolved = await resolveEntityName(client, node.label);
      const next = await loadEntityMapGraph(client, resolved);
      if (requestID !== atlasRequestRef.current) return;
      setLiveGraph(next);
      setSel(initialSelection(next));
      setAtlasState({ kind: "idle" });
    } catch (error) {
      if (requestID !== atlasRequestRef.current) return;
      setAtlasState({ kind: "error", message: errorMessage(error), seed: node.label });
    }
  }

  return (
    <div className="page">
      <Panel
        className="dashboard-atlas-panel flush"
        title="Code-to-cloud topology"
        sub={`${atlasLabel} neighbourhood — click any node or relationship edge to read its evidence`}
        action={selectedSpotlightName && onOpenService ? <button className="btn-ghost" onClick={() => onOpenService(selectedSpotlightName)}>Open spotlight →</button> : null}
      >
        <div className="dashboard-atlas-controls">
          <div className="dashboard-layer-toggles" aria-label="Topology layers">
            {layerRows.map((layer) => (
              <button
                aria-pressed={enabledLayers[layer.layer]}
                className={`layer-toggle ${enabledLayers[layer.layer] ? "on" : "off"}`}
                key={layer.layer}
                onClick={() => setEnabledLayers((current) => ({ ...current, [layer.layer]: !current[layer.layer] }))}
                style={{ "--lc": LAYER_COLOR[layer.layer] } as React.CSSProperties}
              >
                <i style={{ background: LAYER_COLOR[layer.layer] }} />
                <span>{layer.layer}</span>
                <span className="lt-n">{fmt(layer.count)}</span>
              </button>
            ))}
          </div>
          <div className="dashboard-hot-entities">
            <span>Hot entities</span>
            {hotEntities.map((entity) => (
              <button disabled={!onOpenService} key={entity.id} onClick={() => onOpenService?.(entity.name)}>{entity.name}</button>
            ))}
            <small>Seeded from the live graph neighbourhood (probes capped at {MAX_SEED_PROBES}).</small>
          </div>
        </div>
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
                    {graph.edges.filter((e) => e.s === sel.id || e.t === sel.id).map((e, i) => {
                      const endpointID = e.s === sel.id ? e.t : e.s;
                      const endpointLabel = nodeLabels.get(endpointID) ?? endpointID;
                      return (
                        <div className="insp-evi-row" key={i} title={endpointLabel === endpointID ? undefined : endpointID}>
                          {e.verb} {e.s === sel.id ? "→" : "←"} {endpointLabel}
                        </div>
                      );
                    })}
                  </div>
                </div>
              ) : <p className="empty">Select a node.</p>}
          </aside>
        </div>
      </Panel>

      <div className="dashboard-stat-grid grid g-4 mt">
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

      <div className="dashboard-insight-grid grid mt">
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

      <Panel className="mt" title="Suggested questions" sub="Source-backed next reads">
        <SuggestedQuestions questions={suggestedQuestions} />
      </Panel>

      <Panel className="dashboard-findings-panel mt flush" title="Needs attention" sub="Highest-severity findings with evidence">
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

function relationshipRowsFor(
  rows: readonly RelationshipRow[],
  graph: GraphModel
): readonly RelationshipCoverageRow[] {
  if (rows.length > 0) {
    return rows.slice().sort((a, b) => b.count - a.count).slice(0, 7)
      .map((row) => ({ label: row.verb, value: row.count, color: LAYER_COLOR[row.layer], detail: row.detail }));
  }
  const counts = new Map<string, RelationshipRow>();
  for (const edge of graph.edges) {
    const key = `${edge.verb}\u0000${edge.layer}`;
    const current = counts.get(key);
    counts.set(key, {
      verb: edge.verb,
      layer: edge.layer,
      count: (current?.count ?? 0) + 1,
      detail: "Live entity-map relationships"
    });
  }
  return [...counts.values()].sort((a, b) => b.count - a.count).slice(0, 7)
    .map((row) => ({ label: row.verb, value: row.count, color: LAYER_COLOR[row.layer], detail: row.detail }));
}

function filterGraphByLayer(graph: GraphModel, enabledLayers: Readonly<Record<GraphLayer, boolean>>): GraphModel {
  const edges = graph.edges.filter((edge) => enabledLayers[edge.layer]);
  const keep = new Set<string>();
  for (const edge of edges) {
    keep.add(edge.s);
    keep.add(edge.t);
  }
  for (const node of graph.nodes) {
    if (node.hero) keep.add(node.id);
  }
  return {
    edges,
    nodes: graph.nodes.filter((node) => keep.has(node.id) || graph.edges.length === 0)
  };
}

function layerCounts(graph: GraphModel): readonly { readonly count: number; readonly layer: GraphLayer }[] {
  return LANDING_LAYERS.map((layer) => ({
    count: graph.edges.filter((edge) => edge.layer === layer).length,
    layer
  }));
}

function hotEntityRows(
  model: ConsoleModel,
  atlasSeeds: readonly GraphNode[]
): readonly { readonly id: string; readonly name: string }[] {
  const services = atlasSeeds.length > 0
    ? atlasSeeds.map((seed) => ({ id: seed.id, name: seed.label }))
    : model.services.map((service) => ({ id: service.id, name: service.name }));
  return services.filter((row) => row.name.trim().length > 0).slice(0, MAX_SEED_PROBES);
}

// A seed graph is "meaningful" once it has at least this many edges. A single
// edge is the trivial workload->repository self-edge (2 nodes / 1 edge) that
// makes for a weak landing atlas, so we keep probing past it.
const MEANINGFUL_SEED_EDGES = 2;

// Probe at most this many catalog services when hunting for a non-trivial seed.
// Bounds the entity-map calls on first paint regardless of catalog size.
const MAX_SEED_PROBES = 8;

// liveAtlasSeeds returns the ordered candidate seed nodes for the live atlas.
// It prefers named catalog services (in catalog order); when the catalog is
// empty it falls back to indexed repositories, which always exist on a populated
// stack even before any service/workload is admitted to the catalog (issue
// #3398). Both kinds resolve through impact/entity-map, so the loader probes them
// in turn and lands on the first that yields a meaningful neighbourhood, falling
// back to the most-connected candidate it saw. Empty for demo data or once the
// model already carries a graph.
function liveAtlasSeeds(
  model: ConsoleModel,
  repositories: readonly RepoListItem[] | undefined
): readonly GraphNode[] {
  if (model.source !== "live" || model.graph.nodes.length > 0) return [];
  const serviceSeeds = model.services
    .filter((service) => service.name.trim().length > 0)
    .map(serviceSeedNode);
  if (serviceSeeds.length > 0) return serviceSeeds;
  return (repositories ?? [])
    .filter((repository) => repository.name.trim().length > 0)
    .map(repoSeedNode);
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

// repoSeedNode lifts an indexed repository into an atlas seed node. The label is
// the repository handle the entity-map resolver expects; the id keys the node so
// distinct repos never collapse. Used only when the service catalog yields no
// seeds (issue #3398).
function repoSeedNode(repository: RepoListItem): GraphNode {
  const label = repository.name.trim();
  const id = repository.id.trim() || label;
  return {
    col: 1,
    hero: true,
    id,
    kind: "repo",
    label,
    sub: repository.repoSlug.trim() || undefined,
    truth: "exact"
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
