// pages/ExplorerPage.tsx
import { useMemo, useState } from "react";
import type { EshuApiClient } from "../api/client";
import type { ConsoleModel, GraphLayer, GraphModel, GraphNode } from "../console/types";
import { LAYER_COLOR, KIND_COLOR, fmt } from "../console/types";
import { loadEntityGraph, loadEntityMapGraph, resolveEntityName } from "../api/eshuGraph";
import { Panel, TruthChip } from "../components/atoms";
import { GraphCanvas } from "../components/GraphCanvas";

const LAYERS: readonly GraphLayer[] = ["code", "deploy", "infra", "runtime", "security", "ops"];

export function ExplorerPage({ model, client, onOpenService }: {
  readonly model: ConsoleModel;
  readonly client?: EshuApiClient;
  readonly onOpenService?: (name: string) => void;
}): React.JSX.Element {
  const live = model.source === "live" && !!client;
  const [layout, setLayout] = useState<"layered" | "radial">("layered");
  const [mode, setMode] = useState<"direct" | "neighborhood">("direct");
  const [on, setOn] = useState<Record<GraphLayer, boolean>>(
    () => Object.fromEntries(LAYERS.map((l) => [l, true])) as Record<GraphLayer, boolean>
  );
  const [sel, setSel] = useState<GraphNode | undefined>(model.graph.nodes.find((n) => n.hero));
  const [liveGraph, setLiveGraph] = useState<GraphModel | null>(null);
  const [query, setQuery] = useState("");
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState("");

  const base = live ? (liveGraph ?? { nodes: [], edges: [] }) : model.graph;

  const graph = useMemo(() => {
    const edges = base.edges.filter((e) => on[e.layer]);
    const keep = new Set<string>();
    edges.forEach((e) => { keep.add(e.s); keep.add(e.t); });
    base.nodes.forEach((n) => { if (n.hero) keep.add(n.id); });
    return { nodes: base.nodes.filter((n) => keep.has(n.id) || base.edges.length === 0), edges };
  }, [base, on]);

  async function expand(name: string): Promise<void> {
    if (!client) return;
    setBusy(true); setErr("");
    try {
      // Resolve the typed name to a canonical entity first (entities/resolve),
      // then expand via direct relationships or the broader entity-map.
      const resolved = await resolveEntityName(client, name);
      const g = mode === "neighborhood"
        ? await loadEntityMapGraph(client, resolved)
        : await loadEntityGraph(client, resolved);
      setLiveGraph(g);
      setSel(g.nodes.find((n) => n.hero));
    }
    catch (e) { setErr(e instanceof Error ? e.message : "failed"); }
    finally { setBusy(false); }
  }

  function onSelect(n: GraphNode): void {
    setSel(n);
    if ((n.kind === "service" || n.kind === "workload") && onOpenService) onOpenService(n.label);
    else if (live) void expand(n.label);
  }

  return (
    <div className="page" style={{ maxWidth: "none" }}>
      <div className="page-intro row" style={{ justifyContent: "space-between", alignItems: "flex-end", flexWrap: "wrap", gap: 12 }}>
        <div><h2>Graph Explorer</h2><p>{live ? "Search an entity, then click any node to expand its real relationships." : "Pan, zoom and drill into the relationship graph. Toggle layers, then select a node."}</p></div>
        <div className="seg"><button className={layout === "layered" ? "active" : ""} onClick={() => setLayout("layered")}>Layered</button><button className={layout === "radial" ? "active" : ""} onClick={() => setLayout("radial")}>Radial</button></div>
      </div>

      {live ? (
        <div className="explorer-filters" style={{ gap: 8 }}>
          <div className="searchbox" style={{ minWidth: 280, height: 36, margin: 0 }}>
            <input placeholder="Entity / symbol / service name…" value={query}
              onChange={(e) => setQuery(e.target.value)} onKeyDown={(e) => { if (e.key === "Enter" && query) void expand(query); }} />
          </div>
          <button className="btn-ghost active" onClick={() => query && expand(query)} disabled={busy}>{busy ? "Loading…" : "Load"}</button>
          <div className="seg"><button className={mode === "direct" ? "active" : ""} onClick={() => setMode("direct")}>Direct</button><button className={mode === "neighborhood" ? "active" : ""} onClick={() => setMode("neighborhood")}>Neighborhood</button></div>
          {err ? <span className="src-err" style={{ marginTop: 0 }}>⚠ {err}</span> : null}
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
              {live ? <button className="btn-ghost" style={{ width: "100%", justifyContent: "center" }} onClick={() => expand(sel.label)}>Expand relationships →</button> : null}
              <div className="section-label">Edges</div>
              <div className="insp-evi">
                {base.edges.filter((e) => e.s === sel.id || e.t === sel.id).map((e, i) => (
                  <div className="insp-evi-row" key={i}>{e.verb} {e.s === sel.id ? `→ ${e.t}` : `← ${e.s}`}</div>
                ))}
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
        </Panel>
      </div>
    </div>
  );
}
