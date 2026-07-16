// pages/DashboardPage.tsx
import { useEffect, useMemo, useRef, useState } from "react";

import {
  dashboardErrorMessage,
  filterGraphByLayer,
  graphNodeMetric,
  hotEntityRows,
  initialSelection,
  LANDING_LAYERS,
  layerCounts,
  liveAtlasSeeds,
  MAX_SEED_PROBES,
  relationshipMetric,
  relationshipRowsFor,
  selectSeedGraph,
} from "./dashboardModel";
import type { EshuApiClient } from "../api/client";
import { loadEntityMapGraph, resolveEntityName } from "../api/eshuGraph";
import type { RepoListItem } from "../api/repoCatalog";
import { StatTile, Panel, TruthChip } from "../components/atoms";
import { AreaChart, Donut, BarRows } from "../components/charts";
import { GraphCanvas } from "../components/GraphCanvas";
import { SuggestedQuestions } from "../components/SuggestedQuestions";
import { fmt, LAYER_COLOR, SEVERITY_COLOR, uiTruth } from "../console/types";
import type { ConsoleModel, GraphLayer, GraphModel, GraphNode } from "../console/types";
import type { RepositoryCatalogState } from "../repositoryCatalogLifecycle";
import { useDashboardSuggestedQuestions } from "./useDashboardSuggestedQuestions";
import "./dashboardLive.css";

type AtlasState =
  | { readonly kind: "idle" }
  | { readonly kind: "loading"; readonly seed: string }
  | { readonly kind: "error"; readonly message: string; readonly seed: string };

interface AtlasLoadOwner {
  readonly cancellation: { cancelled: boolean };
  readonly client: EshuApiClient;
  readonly key: string;
  readonly promise: ReturnType<typeof selectSeedGraph>;
}

export function DashboardPage({
  model,
  client,
  onOpenService,
  repositories,
  repositoryCatalog,
}: {
  readonly model: ConsoleModel;
  readonly client?: EshuApiClient;
  readonly onOpenService?: (name: string) => void;
  readonly repositories?: readonly RepoListItem[];
  readonly repositoryCatalog?: RepositoryCatalogState;
}): React.JSX.Element {
  const r = model.runtime;
  const catalogRepositories = repositoryCatalog?.repositories ?? repositories;
  const atlasSeeds = useMemo(
    () => liveAtlasSeeds(model, catalogRepositories),
    [catalogRepositories, model],
  );
  const atlasSeed = atlasSeeds[0];
  const seededGraph = useMemo<GraphModel>(
    () => (atlasSeed ? { nodes: [atlasSeed], edges: [] } : model.graph),
    [atlasSeed, model.graph],
  );
  const [liveGraph, setLiveGraph] = useState<GraphModel | null>(null);
  const [enabledLayers, setEnabledLayers] = useState<Record<GraphLayer, boolean>>(
    () =>
      Object.fromEntries(LANDING_LAYERS.map((layer) => [layer, true])) as Record<
        GraphLayer,
        boolean
      >,
  );
  const [atlasState, setAtlasState] = useState<AtlasState>({ kind: "idle" });
  const atlasRequestRef = useRef(0);
  const atlasLoadOwner = useRef<AtlasLoadOwner | null>(null);
  const atlasAbortTimer = useRef<ReturnType<typeof setTimeout> | null>(null);
  const atlasLoadKey = useMemo(
    () => JSON.stringify(atlasSeeds.map((seed) => [seed.id, seed.label])),
    [atlasSeeds],
  );
  const baseGraph = liveGraph ?? seededGraph;
  const graph = useMemo(
    () => filterGraphByLayer(baseGraph, enabledLayers),
    [baseGraph, enabledLayers],
  );
  const layerRows = useMemo(() => layerCounts(baseGraph), [baseGraph]);
  const hotEntities = useMemo(() => hotEntityRows(model, atlasSeeds), [atlasSeeds, model]);
  const [sel, setSel] = useState<GraphNode | undefined>(() => initialSelection(graph));
  const atlasLabel = sel?.label ?? atlasSeed?.label ?? "live graph";
  const graphNodeCount = graphNodeMetric(model);
  const relationshipCount = relationshipMetric(model, baseGraph);
  const {
    error: suggestedQuestionsError,
    failures: suggestedQuestionFailures,
    questions: suggestedQuestions,
  } = useDashboardSuggestedQuestions(
    client,
    model.source === "live",
    catalogRepositories,
    repositoryCatalog,
  );
  const selectedSpotlightName =
    sel && (sel.kind === "service" || sel.kind === "workload") ? sel.label : null;
  const nodeLabels = useMemo(
    () => new Map(graph.nodes.map((node) => [node.id, node.label])),
    [graph.nodes],
  );
  const sevTotals = model.vulnerabilities.reduce(
    (a, v) => {
      const k = v.severity as keyof typeof a;
      if (k in a) a[k] += 1;
      return a;
    },
    { critical: 0, high: 0, medium: 0, low: 0 },
  );
  const relRows = useMemo(
    () => relationshipRowsFor(model.relationships, graph),
    [model.relationships, graph],
  );
  const serviceNames = new Set(model.services.map((s) => s.name));

  useEffect(() => {
    setSel((current) =>
      graph.nodes.some((n) => n.id === current?.id) ? current : initialSelection(graph),
    );
  }, [graph]);

  useEffect(() => {
    if (atlasAbortTimer.current !== null) {
      clearTimeout(atlasAbortTimer.current);
      atlasAbortTimer.current = null;
    }
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
    let owner = atlasLoadOwner.current;
    if (!owner || owner.client !== liveClient || owner.key !== atlasLoadKey) {
      if (owner) owner.cancellation.cancelled = true;
      const cancellation = { cancelled: false };
      owner = {
        cancellation,
        client: liveClient,
        key: atlasLoadKey,
        promise: selectSeedGraph(liveClient, atlasSeeds, () => cancellation.cancelled),
      };
      atlasLoadOwner.current = owner;
    }
    const activeOwner = owner;
    async function loadSeed(): Promise<void> {
      try {
        const next = await activeOwner.promise;
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
        setAtlasState({
          kind: "error",
          message: dashboardErrorMessage(error),
          seed: atlasSeeds[0].label,
        });
      }
    }
    void loadSeed();
    return () => {
      cancelled = true;
      atlasAbortTimer.current = setTimeout(() => {
        activeOwner.cancellation.cancelled = true;
        if (atlasLoadOwner.current === activeOwner) atlasLoadOwner.current = null;
        atlasAbortTimer.current = null;
      }, 0);
    };
  }, [atlasLoadKey, atlasSeeds, client, model.source]);

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
      setAtlasState({ kind: "error", message: dashboardErrorMessage(error), seed: node.label });
    }
  }

  return (
    <div className="page">
      <Panel
        className="dashboard-atlas-panel flush"
        title="Code-to-cloud topology"
        sub={`${atlasLabel} neighbourhood — click any node or relationship edge to read its evidence`}
        action={
          selectedSpotlightName && onOpenService ? (
            <button className="btn-ghost" onClick={() => onOpenService(selectedSpotlightName)}>
              Open spotlight →
            </button>
          ) : null
        }
      >
        <div className="dashboard-atlas-controls">
          <div className="dashboard-layer-toggles" aria-label="Topology layers">
            {layerRows.map((layer) => (
              <button
                aria-pressed={enabledLayers[layer.layer]}
                className={`layer-toggle ${enabledLayers[layer.layer] ? "on" : "off"}`}
                key={layer.layer}
                onClick={() =>
                  setEnabledLayers((current) => ({
                    ...current,
                    [layer.layer]: !current[layer.layer],
                  }))
                }
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
              <button
                disabled={!onOpenService}
                key={entity.id}
                onClick={() => onOpenService?.(entity.name)}
              >
                {entity.name}
              </button>
            ))}
            <small>
              Seeded from the live graph neighbourhood (probes capped at {MAX_SEED_PROBES}).
            </small>
          </div>
        </div>
        <div className="dashboard-atlas-layout">
          {graph.nodes.length ? (
            <GraphCanvas
              graph={graph}
              height={520}
              onSelect={(node) => {
                void expandAtlasNode(node);
              }}
              selectedId={sel?.id}
            />
          ) : (
            <div className="gcanvas" style={{ height: 520, display: "grid", placeItems: "center" }}>
              <p className="empty">No graph entities are available from the live model yet.</p>
            </div>
          )}
          <aside
            className="dashboard-atlas-inspector"
            aria-label="Relationship atlas inspector"
            tabIndex={0}
          >
            {sel ? (
              <div className="inspector">
                <div className="insp-head">
                  <div>
                    <div className="insp-kind">{sel.kind}</div>
                    <div className="insp-title">{sel.label}</div>
                  </div>
                </div>
                {sel.sub ? (
                  <div className="t-mut mono" style={{ fontSize: ".82rem" }}>
                    {sel.sub}
                  </div>
                ) : null}
                {sel.truth ? <TruthChip level={sel.truth} /> : null}
                {(sel.kind === "service" || sel.kind === "workload") && onOpenService ? (
                  <button
                    className="btn-ghost active"
                    style={{ width: "100%", justifyContent: "center" }}
                    onClick={() => onOpenService(sel.label)}
                  >
                    Open spotlight →
                  </button>
                ) : null}
                {atlasState.kind === "loading" ? (
                  <p className="empty">Loading relationships for {atlasState.seed}…</p>
                ) : null}
                {atlasState.kind === "error" ? (
                  <p className="src-err">
                    Relationship atlas unavailable for {atlasState.seed}: {atlasState.message}
                  </p>
                ) : null}
                <div className="insp-evi">
                  {graph.edges
                    .filter((e) => e.s === sel.id || e.t === sel.id)
                    .map((e, i) => {
                      const endpointID = e.s === sel.id ? e.t : e.s;
                      const endpointLabel = nodeLabels.get(endpointID) ?? endpointID;
                      return (
                        <div
                          className="insp-evi-row"
                          key={i}
                          title={endpointLabel === endpointID ? undefined : endpointID}
                        >
                          {e.verb} {e.s === sel.id ? "→" : "←"} {endpointLabel}
                        </div>
                      );
                    })}
                </div>
              </div>
            ) : (
              <p className="empty">Select a node.</p>
            )}
          </aside>
        </div>
      </Panel>

      <div className="dashboard-stat-grid grid g-4 mt">
        <StatTile
          label="Graph nodes"
          value={graphNodeCount === null ? "—" : fmt(graphNodeCount)}
          spark={model.series.graphNodes.length ? model.series.graphNodes : undefined}
          color="var(--teal)"
          sub={
            graphNodeCount === null ? "node count metric unavailable" : "NornicDB graph node metric"
          }
        />
        <StatTile
          label="Relationships"
          value={relationshipCount === null ? "—" : fmt(relationshipCount)}
          spark={model.series.graphEdges.length ? model.series.graphEdges : undefined}
          color="var(--ember)"
          sub={
            relationshipCount === null
              ? "relationship count metric unavailable"
              : `${relRows.length} typed verbs observed`
          }
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
          {model.series.ingestRate.length ? (
            <AreaChart data={model.series.ingestRate} color="var(--teal)" h={190} unit=" f/m" />
          ) : (
            <p className="empty" style={{ padding: "48px 12px" }}>
              Trend history appears when a Prometheus/Mimir metrics source has recent samples.
              Current queue and runtime numbers are shown above.
            </p>
          )}
        </Panel>
        <Panel
          title="Security posture"
          sub={`${sevTotals.critical} critical · ${sevTotals.high} high`}
        >
          <div style={{ display: "grid", placeItems: "center", marginBottom: 12 }}>
            <Donut
              size={138}
              thickness={17}
              center={{ value: sevTotals.critical + sevTotals.high, label: "crit + high" }}
              segments={(["critical", "high", "medium", "low"] as const).map((k) => ({
                label: k,
                value: sevTotals[k],
                color: SEVERITY_COLOR[k],
              }))}
            />
          </div>
        </Panel>
      </div>

      <Panel className="mt" title="Relationship coverage" sub="Most-observed typed verbs">
        <BarRows rows={relRows} />
      </Panel>

      <Panel className="mt" title="Suggested questions" sub="Source-backed next reads">
        {suggestedQuestionsError ? (
          <p className="src-err" role="alert">
            Suggested questions are unavailable from this source.
          </p>
        ) : (
          <>
            {suggestedQuestionFailures.length > 0 ? (
              <p className="src-err" role="alert">
                Some suggested questions are unavailable: {suggestedQuestionFailures.join(", ")}.
              </p>
            ) : null}
            <SuggestedQuestions questions={suggestedQuestions} />
          </>
        )}
      </Panel>

      <Panel
        className="dashboard-findings-panel mt flush"
        title="Needs attention"
        sub="Highest-severity findings with evidence"
      >
        <table className="tbl">
          <thead>
            <tr>
              <th>Finding</th>
              <th>Type</th>
              <th>Entity</th>
              <th>Truth</th>
            </tr>
          </thead>
          <tbody>
            {model.findings.map((f) => {
              // Only services/workloads have a spotlight drawer. Findings keyed by
              // a repo or other entity (e.g. dead code) must not open an empty one.
              const canOpen = onOpenService !== undefined && serviceNames.has(f.entity);
              return (
                <tr
                  key={f.id}
                  onClick={canOpen ? () => onOpenService(f.entity) : undefined}
                  style={canOpen ? { cursor: "pointer" } : undefined}
                >
                  <td className="cell-stack">
                    <span style={{ color: "var(--bone)", fontWeight: 600 }}>{f.title}</span>
                    <small>{f.detail}</small>
                  </td>
                  <td className="t-mut">{f.type}</td>
                  <td className="t-name">{f.entity}</td>
                  <td>
                    <TruthChip level={uiTruth(f.truth)} />
                  </td>
                </tr>
              );
            })}
          </tbody>
        </table>
      </Panel>
    </div>
  );
}
