// pages/ExplorerPage.tsx
import { useEffect, useMemo, useRef, useState } from "react";
import { Link, useSearchParams } from "react-router-dom";
import type { EshuApiClient } from "../api/client";
import type { ConsoleModel, GraphEdge, GraphLayer, GraphModel, GraphNode } from "../console/types";
import { LAYER_COLOR, KIND_COLOR, fmt } from "../console/types";
import { loadEntityGraph, loadEntityStoryGraph, resolveEntityHandle } from "../api/eshuGraph";
import { defaultServiceName } from "../console/defaultEntity";
import { Panel, TruthChip } from "../components/atoms";
import { GraphCanvas } from "../components/GraphCanvas";
import { EvidencePanel, type EvidencePanelData } from "../components/EvidencePanel";
import { graphEdgeEvidencePanelData, graphNodeEvidencePanelData } from "../components/graphEvidencePanel";

const LAYERS: readonly GraphLayer[] = ["code", "deploy", "infra", "runtime", "security", "ops"];

export function ExplorerPage({ model, client, onOpenService, title, intro, defaultQuery }: {
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
  const [on, setOn] = useState<Record<GraphLayer, boolean>>(
    () => Object.fromEntries(LAYERS.map((l) => [l, true])) as Record<GraphLayer, boolean>
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

  function pickMode(m: "direct" | "neighborhood"): void {
    modePinnedRef.current = true;
    setMode(m);
  }

  const base = live ? (liveGraph ?? { nodes: [], edges: [] }) : model.graph;

  const graph = useMemo(() => {
    const edges = base.edges.filter((e) => on[e.layer]);
    const keep = new Set<string>();
    edges.forEach((e) => { keep.add(e.s); keep.add(e.t); });
    base.nodes.forEach((n) => { if (n.hero) keep.add(n.id); });
    return { nodes: base.nodes.filter((n) => keep.has(n.id) || base.edges.length === 0), edges };
  }, [base, on]);
  const nodeLabels = useMemo(() => new Map(base.nodes.map((node) => [node.id, node.label])), [base.nodes]);

  async function expand(name: string, forcedMode?: "direct" | "neighborhood"): Promise<void> {
    if (!client) return;
    setBusy(true); setErr(""); setHint("");
    try {
      // Resolve the typed name to a canonical entity + kind first
      // (entities/resolve). When the user has not pinned a mode, follow the
      // kind-recommended mode so a service/infra search lands on Neighborhood
      // (which has data) instead of Direct's code-only relationships. A toggle
      // click passes forcedMode so the new view applies before state settles.
      const resolved = await resolveEntityHandle(client, name);
      const effectiveMode = forcedMode ?? (modePinnedRef.current ? mode : resolved.mode);
      if (effectiveMode !== mode) setMode(effectiveMode);
      const g = effectiveMode === "neighborhood"
        ? await loadEntityStoryGraph(client, resolved.name, resolved.repoId)
        : await loadEntityGraph(client, resolved.name);
      setLiveGraph(g);
      setSel(g.nodes.find((n) => n.hero));
      setEvidence(null);
      // Direct mode with only the center and no edges means the entity has no
      // indexed code relationships (e.g. a service). Nudge toward Neighborhood
      // instead of leaving a blank canvas. See issue #1725.
      if (effectiveMode === "direct" && g.edges.length === 0) {
        setHint("No direct code relationships for this entity — try Neighborhood.");
      }
    }
    catch (e) { setErr(e instanceof Error ? e.message : "failed"); }
    finally { setBusy(false); }
  }

  function onSelect(n: GraphNode): void {
    setSel(n);
    setEvidence(graphNodeEvidencePanelData(n));
    if ((n.kind === "service" || n.kind === "workload") && onOpenService) onOpenService(n.label);
  }

  function openEdgeEvidence(edge: GraphEdge): void {
    const fromLabel = nodeLabels.get(edge.s) ?? edge.s;
    const toLabel = nodeLabels.get(edge.t) ?? edge.t;
    setEvidence(graphEdgeEvidencePanelData(edge, fromLabel, toLabel));
  }

  async function centerOnNode(node: GraphNode): Promise<void> {
    if (!client || node.id === currentCenterId(base)) return;
    const nextMode = modeForNode(node);
    setBusy(true); setErr(""); setHint(""); setQuery(node.label);
    if (nextMode !== mode) setMode(nextMode);
    try {
      const handle = node.id.trim() !== "" ? node.id : node.label;
      const g = nextMode === "neighborhood"
        ? await loadEntityStoryGraph(client, handle, repoIDForNode(node))
        : await loadEntityGraph(client, handle);
      setLiveGraph(g);
      setSel(g.nodes.find((n) => n.hero) ?? node);
      setEvidence(null);
      if (nextMode === "direct" && g.edges.length === 0) {
        setHint("No direct code relationships for this entity — try Neighborhood.");
      }
    } catch (e) {
      setErr(e instanceof Error ? e.message : "failed");
    } finally {
      setBusy(false);
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
  }, [defaultQuery, searchParams, live, model]);

  return (
    <div className="page" style={{ maxWidth: "none" }}>
      <div className="page-intro row" style={{ justifyContent: "space-between", alignItems: "flex-end", flexWrap: "wrap", gap: 12 }}>
        <div>
          <h2>{title ?? "Graph Explorer"}</h2>
          <p>{intro ?? (live ? "Search an entity, select any node, then center its real relationships from the inspector." : "Pan, zoom and drill into the relationship graph. Toggle layers, then select a node.")}</p>
        </div>
        <div className="seg"><button className={layout === "layered" ? "active" : ""} onClick={() => setLayout("layered")}>Layered</button><button className={layout === "radial" ? "active" : ""} onClick={() => setLayout("radial")}>Radial</button></div>
      </div>

      {live ? (
        <div className="explorer-filters" style={{ gap: 8 }}>
          <div className="searchbox" style={{ minWidth: 280, height: 36, margin: 0 }}>
            <input placeholder="Entity / symbol / service name…" value={query}
              onChange={(e) => setQuery(e.target.value)} onKeyDown={(e) => { if (e.key === "Enter" && query) void expand(query); }} />
          </div>
          <button className="btn-ghost active" onClick={() => query && expand(query)} disabled={busy}>{busy ? "Loading…" : "Load"}</button>
          <div className="seg"><button className={mode === "direct" ? "active" : ""} onClick={() => { pickMode("direct"); if (query) void expand(query, "direct"); }}>Direct</button><button className={mode === "neighborhood" ? "active" : ""} onClick={() => { pickMode("neighborhood"); if (query) void expand(query, "neighborhood"); }}>Neighborhood</button></div>
          {err ? <span className="src-err" style={{ marginTop: 0 }}>⚠ {err}</span> : null}
          {!err && hint ? <span className="t-mut" style={{ marginTop: 0, fontSize: ".78rem" }}>{hint}</span> : null}
        </div>
      ) : null}

      <div className="explorer-filters">
        {LAYERS.map((k) => (
          <button key={k} className={`layer-toggle ${on[k] ? "on" : "off"}`} style={{ "--lc": LAYER_COLOR[k] } as React.CSSProperties} onClick={() => setOn((s) => ({ ...s, [k]: !s[k] }))}>
            <i style={{ background: LAYER_COLOR[k] }} /><span style={{ textTransform: "capitalize" }}>{k}</span>
          </button>
        ))}
      </div>

      <div className="explorer-layout">
        <div className="gcanvas-shell"><GraphCanvas graph={graph} layout={layout} height={640} onSelect={onSelect} selectedId={sel?.id} /></div>
        <Panel title="Inspector">
          {sel ? (
            <div className="inspector">
              <div className="insp-head"><span className="cglyph" style={{ width: 30, height: 30, color: KIND_COLOR[sel.kind] ?? "#9aa4af", borderColor: KIND_COLOR[sel.kind] ?? "#9aa4af" }}>{sel.kind.slice(0, 1).toUpperCase()}</span><div><div className="insp-kind">{sel.kind}</div><div className="insp-title">{sel.label}</div></div></div>
              {sel.sub ? <div className="t-mut mono" style={{ fontSize: ".82rem" }}>{sel.sub}</div> : null}
              {sel.truth ? <TruthChip level={sel.truth} /> : null}
              {sourceHref(sel) ? (
                <div className="kv-list">
                  <div className="kv">
                    <span>Source</span>
                    <Link className="mono" to={sourceHref(sel) ?? ""}>{sourceLabel(sel)}</Link>
                  </div>
                </div>
              ) : null}
              {live ? (
                <button
                  className="btn-ghost"
                  disabled={busy || sel.id === currentCenterId(base)}
                  style={{ width: "100%", justifyContent: "center" }}
                  onClick={() => { void centerOnNode(sel); }}
                >
                  {sel.id === currentCenterId(base) ? "Current center" : busy ? "Loading…" : "Center graph here →"}
                </button>
              ) : null}
              {sourceHref(sel) ? <Link className="btn-ghost active" to={sourceHref(sel) ?? ""}>Open source</Link> : null}
              <div className="section-label">Edges</div>
              <div className="insp-evi">
                {base.edges.filter((e) => e.s === sel.id || e.t === sel.id).map((e, i) => {
                  const endpointID = e.s === sel.id ? e.t : e.s;
                  const endpointLabel = nodeLabels.get(endpointID) ?? endpointID;
                  return (
                    <button
                      className="insp-evi-row insp-evi-btn"
                      key={i}
                      onClick={() => openEdgeEvidence(e)}
                      title={`Inspect ${e.verb} evidence`}
                      type="button"
                    >
                      {e.verb} {e.s === sel.id ? "→" : "←"} {endpointLabel}
                    </button>
                  );
                })}
              </div>
            </div>
          ) : <p className="empty">{live ? "Search for an entity to begin." : "Select a node."}</p>}
          {model.relationships.length ? (
            <>
              <div className="section-label" style={{ marginTop: 16 }}>Relationship verbs</div>
              <div className="kv-list">
                {model.relationships.slice(0, 6).map((rel) => (
                  <div className="kv" key={rel.verb}><span className="mono" style={{ fontSize: ".78rem" }}><i style={{ display: "inline-block", width: 8, height: 8, borderRadius: 2, background: LAYER_COLOR[rel.layer], marginRight: 7 }} />{rel.verb}</span><strong>{fmt(rel.count)}</strong></div>
                ))}
              </div>
            </>
          ) : null}
          {evidence !== null ? (
            <div className="insp-evidence-panel">
              <EvidencePanel data={evidence} onClose={() => setEvidence(null)} />
            </div>
          ) : null}
        </Panel>
      </div>
    </div>
  );
}

function currentCenterId(graph: GraphModel): string | undefined {
  return graph.nodes.find((node) => node.hero)?.id;
}

function modeForNode(node: GraphNode): "direct" | "neighborhood" {
  if (node.kind === "client" || node.kind === "library") return "direct";
  return "neighborhood";
}

function repoIDForNode(node: GraphNode): string | undefined {
  if (node.kind !== "repo") return undefined;
  return node.id.trim() === "" ? undefined : node.id;
}

function sourceHref(node: GraphNode): string | null {
  const source = node.source;
  if (!source) return null;
  const params = new URLSearchParams({ path: source.filePath });
  if (source.startLine !== undefined) params.set("lineStart", String(source.startLine));
  if (source.endLine !== undefined) params.set("lineEnd", String(source.endLine));
  return `/repositories/${encodeURIComponent(source.repoId)}/source?${params.toString()}`;
}

function sourceLabel(node: GraphNode): string {
  const source = node.source;
  if (!source) return "source path unavailable";
  if (source.startLine !== undefined && source.endLine !== undefined) return `${source.filePath}:${source.startLine}-${source.endLine}`;
  if (source.startLine !== undefined) return `${source.filePath}:${source.startLine}`;
  return source.filePath;
}
