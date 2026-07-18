// pages/ExplorerPage.tsx
import { useEffect, useMemo, useRef, useState } from "react";
import { useSearchParams } from "react-router-dom";

import {
  DeploymentDetailButton,
  EXPLORER_LAYERS,
  ExplorerLayerFilters,
} from "./ExplorerGraphControls";
import {
  currentCenterId,
  hasDeploymentEvidence,
  modeForNode,
  repoIDForNode,
} from "./ExplorerGraphHelpers";
import { ExplorerInspector } from "./ExplorerInspector";
import type { EshuApiClient } from "../api/client";
import { loadEntityGraph, loadEntityStoryGraph, resolveEntityHandle } from "../api/eshuGraph";
import type { DeploymentGraphDetail } from "../api/eshuGraph";
import type { EvidencePanelData } from "../components/EvidencePanel";
import { GraphCanvas } from "../components/GraphCanvas";
import { graphNodeEvidencePanelData } from "../components/graphEvidencePanel";
import { defaultServiceName } from "../console/defaultEntity";
import type { ConsoleModel, GraphLayer, GraphModel, GraphNode } from "../console/types";

export function ExplorerPage({
  model,
  client,
  onOpenService,
  title,
  intro,
  defaultQuery,
}: {
  readonly model: ConsoleModel;
  readonly client?: EshuApiClient;
  readonly onOpenService?: (name: string) => void;
  readonly title?: string;
  readonly intro?: string;
  readonly defaultQuery?: string;
}): React.JSX.Element {
  const live = model.source === "live" && !!client;
  const [layout, setLayout] = useState<"layered" | "radial">("layered");
  const [mode, setMode] = useState<"direct" | "neighborhood">("direct");
  const [deploymentDetail, setDeploymentDetail] = useState<DeploymentGraphDetail>("summary");
  const [on, setOn] = useState<Record<GraphLayer, boolean>>(
    () =>
      Object.fromEntries(EXPLORER_LAYERS.map((layer) => [layer, true])) as Record<
        GraphLayer,
        boolean
      >,
  );
  const [sel, setSel] = useState<GraphNode | undefined>(model.graph.nodes.find((n) => n.hero));
  // evidence holds the inline evidence-panel data for the node or edge the
  // operator is inspecting, or null when no element is open. It is decoupled from
  // sel (the centered node) so opening edge evidence does not recenter the graph.
  const [evidence, setEvidence] = useState<EvidencePanelData | null>(null);
  const [liveGraph, setLiveGraph] = useState<GraphModel | null>(null);
  const [searchParams] = useSearchParams();
  const [query, setQuery] = useState(() => searchParams.get("q") ?? defaultQuery ?? "");
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState("");
  // hint surfaces a non-error explanation (e.g. a code-empty service that should
  // try Neighborhood) without the scary red error banner. See issue #1725.
  const [hint, setHint] = useState("");
  // Deep-link support: when another page links here with ?q=<entity id> (e.g. the
  // Cloud inventory page), seed the search box and expand it once the client is
  // live. seededRef guards against re-expanding on every render or layer toggle.
  const seededRef = useRef(false);
  // modePinnedRef tracks whether the user manually chose a mode. While unpinned,
  // a search auto-selects the mode by the resolved entity kind (code → Direct,
  // service/infra → Neighborhood) so the search lands on the view that has data.
  // A manual toggle pins the choice and disables auto-selection. See issue #1725.
  const modePinnedRef = useRef(false);
  const latestRequestRef = useRef(0);
  const mountedRef = useRef(false);
  const activeClientRef = useRef(client);
  const previousClientRef = useRef(client);
  activeClientRef.current = client;

  useEffect(() => {
    mountedRef.current = true;
    return () => {
      mountedRef.current = false;
    };
  }, []);

  useEffect(() => {
    if (previousClientRef.current === client) return;
    previousClientRef.current = client;
    latestRequestRef.current += 1;
    seededRef.current = false;
    setLiveGraph(null);
    setSel(undefined);
    setEvidence(null);
    setBusy(false);
    setErr("");
    setHint("");
    setDeploymentDetail("summary");
  }, [client]);

  function pickMode(m: "direct" | "neighborhood"): void {
    modePinnedRef.current = true;
    setMode(m);
  }

  function requestIsCurrent(requestID: number, requestClient: EshuApiClient): boolean {
    return (
      mountedRef.current &&
      requestID === latestRequestRef.current &&
      activeClientRef.current === requestClient
    );
  }

  const base = useMemo(
    () => (live ? (liveGraph ?? { nodes: [], edges: [] }) : model.graph),
    [live, liveGraph, model.graph],
  );

  const graph = useMemo(() => {
    const edges = base.edges.filter((e) => on[e.layer]);
    const keep = new Set<string>();
    const incident = new Set<string>();
    base.edges.forEach((e) => {
      incident.add(e.s);
      incident.add(e.t);
    });
    edges.forEach((e) => {
      keep.add(e.s);
      keep.add(e.t);
    });
    base.nodes.forEach((n) => {
      if (n.hero) keep.add(n.id);
    });
    return {
      nodes: base.nodes.filter((n) => keep.has(n.id) || !incident.has(n.id)),
      edges,
    };
  }, [base, on]);
  async function expand(
    name: string,
    forcedMode?: "direct" | "neighborhood",
    forcedDetail?: DeploymentGraphDetail,
  ): Promise<void> {
    if (!client) return;
    const requestClient = client;
    const requestID = latestRequestRef.current + 1;
    latestRequestRef.current = requestID;
    setBusy(true);
    setErr("");
    setHint("");
    try {
      // Resolve the typed name to a canonical entity + kind first
      // (entities/resolve). When the user has not pinned a mode, follow the
      // kind-recommended mode so a service/infra search lands on Neighborhood
      // (which has data) instead of Direct's code-only relationships. A toggle
      // click passes forcedMode so the new view applies before state settles.
      const resolved = await resolveEntityHandle(client, name);
      if (!requestIsCurrent(requestID, requestClient)) return;
      if (resolved.id === "") {
        setLiveGraph({ nodes: [], edges: [] });
        setSel(undefined);
        setEvidence(null);
        setHint(`No indexed entity matched "${name}".`);
        return;
      }
      const effectiveMode = forcedMode ?? (modePinnedRef.current ? mode : resolved.mode);
      const effectiveDetail = forcedDetail ?? deploymentDetail;
      if (effectiveMode !== mode) setMode(effectiveMode);
      if (effectiveDetail !== deploymentDetail) setDeploymentDetail(effectiveDetail);
      const g =
        effectiveMode === "neighborhood"
          ? await loadEntityStoryGraph(client, resolved.name, resolved.repoId, effectiveDetail)
          : await loadEntityGraph(client, resolved.name);
      if (!requestIsCurrent(requestID, requestClient)) return;
      setLiveGraph(g);
      setSel(g.nodes.find((n) => n.hero));
      setEvidence(null);
      // Direct mode with only the center and no edges means the entity has no
      // indexed code relationships (e.g. a service). Nudge toward Neighborhood
      // instead of leaving a blank canvas. See issue #1725.
      if (effectiveMode === "direct" && g.edges.length === 0) {
        setHint("No direct code relationships for this entity — try Neighborhood.");
      }
    } catch (e) {
      if (!requestIsCurrent(requestID, requestClient)) return;
      setLiveGraph(null);
      setSel(undefined);
      setEvidence(null);
      setErr(e instanceof Error ? e.message : "Explorer data is unavailable");
    } finally {
      if (requestIsCurrent(requestID, requestClient)) setBusy(false);
    }
  }

  function onSelect(n: GraphNode): void {
    setSel(n);
    setEvidence(graphNodeEvidencePanelData(n));
    if ((n.kind === "service" || n.kind === "workload") && onOpenService) onOpenService(n.label);
  }

  async function centerOnNode(node: GraphNode): Promise<void> {
    if (!client || node.id === currentCenterId(base)) return;
    const requestClient = client;
    const requestID = latestRequestRef.current + 1;
    latestRequestRef.current = requestID;
    const nextMode = modeForNode(node);
    setBusy(true);
    setErr("");
    setHint("");
    setQuery(node.label);
    if (nextMode !== mode) setMode(nextMode);
    try {
      const handle = node.id.trim() !== "" ? node.id : node.label;
      const g =
        nextMode === "neighborhood"
          ? await loadEntityStoryGraph(client, handle, repoIDForNode(node), deploymentDetail)
          : await loadEntityGraph(client, handle);
      if (!requestIsCurrent(requestID, requestClient)) return;
      setLiveGraph(g);
      setSel(g.nodes.find((n) => n.hero) ?? node);
      setEvidence(null);
      if (nextMode === "direct" && g.edges.length === 0) {
        setHint("No direct code relationships for this entity — try Neighborhood.");
      }
    } catch (e) {
      if (!requestIsCurrent(requestID, requestClient)) return;
      setLiveGraph(null);
      setSel(undefined);
      setEvidence(null);
      setErr(e instanceof Error ? e.message : "failed");
    } finally {
      if (requestIsCurrent(requestID, requestClient)) setBusy(false);
    }
  }

  useEffect(() => {
    // Auto-load a sensible default on open: an explicit deep-link (?q=) or
    // caller-provided defaultQuery wins; otherwise fall back to a real service
    // from the live catalog so the explorer renders a graph immediately instead
    // of an empty canvas. The search box still overrides.
    const seed = searchParams.get("q") ?? defaultQuery ?? defaultServiceName(model);
    if (!seed || !live || seededRef.current) return;
    seededRef.current = true;
    setQuery(seed);
    void expand(seed);
    // expand is stable for a given client/mode; intentionally only re-run when the
    // deep-link param or live state changes.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [client, defaultQuery, searchParams, live, model]);

  return (
    <div className="page" style={{ maxWidth: "none" }}>
      <div
        className="page-intro row"
        style={{
          justifyContent: "space-between",
          alignItems: "flex-end",
          flexWrap: "wrap",
          gap: 12,
        }}
      >
        <div>
          <h2>{title ?? "Graph Explorer"}</h2>
          <p>
            {intro ??
              (live
                ? "Search an entity, select any node, then center its real relationships from the inspector."
                : "Pan, zoom and drill into the relationship graph. Toggle layers, then select a node.")}
          </p>
        </div>
        <div className="seg">
          <button
            className={layout === "layered" ? "active" : ""}
            onClick={() => setLayout("layered")}
          >
            Layered
          </button>
          <button
            className={layout === "radial" ? "active" : ""}
            onClick={() => setLayout("radial")}
          >
            Radial
          </button>
        </div>
      </div>

      {live ? (
        <div className="explorer-filters" style={{ gap: 8 }}>
          <div className="searchbox" style={{ minWidth: 280, height: 36, margin: 0 }}>
            <input
              placeholder="Entity / symbol / service name…"
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === "Enter" && query) void expand(query);
              }}
            />
          </div>
          <button
            className="btn-ghost active"
            onClick={() => query && expand(query)}
            disabled={busy}
          >
            {busy ? "Loading…" : "Load"}
          </button>
          <div className="seg">
            <button
              className={mode === "direct" ? "active" : ""}
              onClick={() => {
                pickMode("direct");
                if (query) void expand(query, "direct");
              }}
            >
              Direct
            </button>
            <button
              className={mode === "neighborhood" ? "active" : ""}
              onClick={() => {
                pickMode("neighborhood");
                if (query) void expand(query, "neighborhood");
              }}
            >
              Neighborhood
            </button>
          </div>
          <DeploymentDetailButton
            busy={busy || query.trim() === ""}
            detail={deploymentDetail}
            onToggle={() => {
              const nextDetail = deploymentDetail === "summary" ? "expanded" : "summary";
              void expand(query, "neighborhood", nextDetail);
            }}
            visible={mode === "neighborhood" && hasDeploymentEvidence(base)}
          />
          {err ? (
            <span className="src-err" role="alert" style={{ marginTop: 0 }}>
              ⚠ {err}
            </span>
          ) : null}
          {!err && hint ? (
            <span className="t-mut" style={{ marginTop: 0, fontSize: ".78rem" }}>
              {hint}
            </span>
          ) : null}
        </div>
      ) : null}

      <ExplorerLayerFilters
        enabled={on}
        onToggle={(layer) => setOn((current) => ({ ...current, [layer]: !current[layer] }))}
      />

      <div className="explorer-layout">
        <div className="gcanvas-shell">
          <GraphCanvas
            graph={graph}
            layout={layout}
            height={640}
            onSelect={onSelect}
            selectedId={sel?.id}
          />
        </div>
        <ExplorerInspector
          base={base}
          busy={busy}
          evidence={evidence}
          live={live}
          onCenter={(node) => void centerOnNode(node)}
          onEvidenceChange={setEvidence}
          relationships={model.relationships}
          selected={sel}
        />
      </div>
    </div>
  );
}
