// components/GraphCanvas.tsx
import { useMemo, useRef, useState } from "react";

import type { GraphModel, GraphNode } from "../console/types";
import { LAYER_COLOR, KIND_COLOR } from "../console/types";

type Pos = { x: number; y: number };
const VBW = 1080, VBH = 640;

function nodeSize(n: GraphNode): { w: number; h: number } {
  return n.hero ? { w: 196, h: 64 } : { w: 176, h: 56 };
}

function computeLayout(graph: GraphModel, mode: "layered" | "radial"): Record<string, Pos> {
  const pos: Record<string, Pos> = {};
  if (mode === "radial") {
    const hero = graph.nodes.find((n) => n.hero) ?? graph.nodes[0];
    const others = graph.nodes.filter((n) => n !== hero);
    if (hero) pos[hero.id] = { x: VBW / 2, y: VBH / 2 };
    others.forEach((n, i) => {
      const ring = i % 2 === 0 ? 230 : 410;
      const a = (i / Math.max(1, others.length)) * Math.PI * 2 - Math.PI / 2;
      pos[n.id] = { x: VBW / 2 + Math.cos(a) * ring * 1.32, y: VBH / 2 + Math.sin(a) * ring };
    });
  } else {
    const cols = new Map<number, GraphNode[]>();
    graph.nodes.forEach((n) => { const list = cols.get(n.col) ?? []; list.push(n); cols.set(n.col, list); });
    const keys = [...cols.keys()].sort((a, b) => a - b);
    const colW = VBW / keys.length;
    keys.forEach((ck, ci) => {
      const list = cols.get(ck) ?? [];
      const gap = VBH / (list.length + 1);
      list.forEach((n, i) => { pos[n.id] = { x: colW * ci + colW / 2, y: gap * (i + 1) }; });
    });
  }
  return pos;
}

function edgePath(a: Pos, b: Pos): string {
  const dx = b.x - a.x;
  return `M${a.x} ${a.y} C${a.x + dx * 0.5} ${a.y} ${b.x - dx * 0.5} ${b.y} ${b.x} ${b.y}`;
}

export function GraphCanvas({ graph, layout = "layered", height = 560, onSelect, selectedId }: {
  readonly graph: GraphModel; readonly layout?: "layered" | "radial"; readonly height?: number;
  readonly onSelect?: (n: GraphNode) => void; readonly selectedId?: string;
}): React.JSX.Element {
  const [vt, setVt] = useState({ x: 0, y: 0, k: 1 });
  const [hover, setHover] = useState<string | null>(null);
  const [pan, setPan] = useState<{ ox: number; oy: number; sx: number; sy: number } | null>(null);
  const svgRef = useRef<SVGSVGElement>(null);
  const pos = useMemo(() => computeLayout(graph, layout), [graph, layout]);
  const adj = useMemo(() => {
    const m = new Map<string, Set<string>>();
    graph.edges.forEach((e) => {
      (m.get(e.s) ?? m.set(e.s, new Set()).get(e.s))!.add(e.t);
      (m.get(e.t) ?? m.set(e.t, new Set()).get(e.t))!.add(e.s);
    });
    return m;
  }, [graph]);

  const active = hover ?? selectedId;
  const dim = (id: string): boolean => !!active && id !== active && !(adj.get(active)?.has(id));
  const edgeHot = (s: string, t: string): boolean => !!active && (s === active || t === active);

  function toSvg(e: React.PointerEvent): Pos {
    const r = svgRef.current!.getBoundingClientRect();
    return { x: ((e.clientX - r.left) / r.width) * VBW, y: ((e.clientY - r.top) / r.height) * VBH };
  }

  if (graph.nodes.length === 0) {
    return <div className="gcanvas" style={{ height, display: "grid", placeItems: "center" }}><p className="empty">No graph rows returned from this source yet.</p></div>;
  }

  return (
    <div className="gcanvas" style={{ height }}>
      <div className="gcanvas-toolbar">
        <span className="gcanvas-count">{graph.nodes.length} nodes · {graph.edges.length} edges</span>
        <div className="gcanvas-zoom">
          <button onClick={() => setVt((v) => ({ ...v, k: Math.min(2.6, +(v.k + 0.2).toFixed(2)) }))} title="Zoom in">+</button>
          <span>{Math.round(vt.k * 100)}%</span>
          <button onClick={() => setVt((v) => ({ ...v, k: Math.max(0.55, +(v.k - 0.2).toFixed(2)) }))} title="Zoom out">−</button>
          <button onClick={() => setVt({ x: 0, y: 0, k: 1 })} title="Reset">⟲</button>
        </div>
      </div>
      <svg ref={svgRef} className={`gcanvas-svg${pan ? " is-panning" : ""}`} viewBox={`0 0 ${VBW} ${VBH}`}
        onWheel={(e) => { e.preventDefault(); setVt((v) => ({ ...v, k: Math.min(2.6, Math.max(0.55, +(v.k + (e.deltaY < 0 ? 0.12 : -0.12)).toFixed(2))) })); }}
        onPointerDown={(e) => { const p = toSvg(e); setPan({ ox: vt.x, oy: vt.y, sx: p.x, sy: p.y }); }}
        onPointerMove={(e) => { if (!pan) return; const p = toSvg(e); setVt((v) => ({ ...v, x: pan.ox + (p.x - pan.sx), y: pan.oy + (p.y - pan.sy) })); }}
        onPointerUp={() => setPan(null)} onPointerLeave={() => setPan(null)}>
        <defs>
          <marker id="g-arrow" markerWidth="9" markerHeight="9" refX="7.5" refY="4" orient="auto"><path d="M0 0 L8 4 L0 8 Z" fill="var(--edge)" /></marker>
          <marker id="g-arrow-hot" markerWidth="9" markerHeight="9" refX="7.5" refY="4" orient="auto"><path d="M0 0 L8 4 L0 8 Z" fill="var(--teal)" /></marker>
        </defs>
        <g transform={`translate(${vt.x} ${vt.y}) scale(${vt.k})`}>
          {graph.edges.map((e, i) => {
            const a = pos[e.s], b = pos[e.t]; if (!a || !b) return null;
            const hot = edgeHot(e.s, e.t), faded = active && !hot;
            const col = LAYER_COLOR[e.layer];
            const mid = { x: (a.x + b.x) / 2, y: (a.y + b.y) / 2 };
            return (
              <g key={i} className={`gedge${faded ? " is-faded" : ""}`}>
                <path d={edgePath(a, b)} fill="none" stroke={hot ? col : "var(--edge)"} strokeWidth={hot ? 2.4 : 1.4} markerEnd={hot ? "url(#g-arrow-hot)" : "url(#g-arrow)"} opacity={hot ? 0.95 : 0.5} />
                {hot ? <g><rect className="gedge-lbl-bg" x={mid.x - (e.verb.length * 3.4 + 10)} y={mid.y - 19} rx="5" width={e.verb.length * 6.8 + 20} height="16" /><text className="gedge-lbl" x={mid.x} y={mid.y - 7} style={{ fill: col }}>{e.verb}</text></g> : null}
              </g>
            );
          })}
          {graph.nodes.map((n) => {
            const p = pos[n.id]; if (!p) return null;
            const { w, h } = nodeSize(n);
            const nc = KIND_COLOR[n.kind] ?? "#9aa4af";
            const sel = selectedId === n.id;
            return (
              <g key={n.id} className={`gnode gnode-${n.kind}${sel ? " is-sel" : ""}${n.hero ? " is-hero" : ""}${dim(n.id) ? " is-faded" : ""}`}
                transform={`translate(${p.x - w / 2} ${p.y - h / 2})`} style={{ "--nc": nc } as React.CSSProperties}
                onMouseEnter={() => setHover(n.id)} onMouseLeave={() => setHover(null)}
                onClick={(ev) => { ev.stopPropagation(); onSelect?.(n); }}>
                <rect className="gnode-box" width={w} height={h} rx="12" />
                <rect className="gnode-accent" width="4" height={h} rx="2" />
                <circle className="gnode-dot" cx="22" cy={h / 2} r="6" />
                <text className="gnode-label" x="38" y={n.sub ? h / 2 - 4 : h / 2 + 5}>{n.label}</text>
                {n.sub ? <text className="gnode-sub" x="38" y={h / 2 + 14}>{n.sub}</text> : null}
                {n.kind === "vuln" ? <text className="gnode-flag" x={w - 15} y={h / 2 + 5}>!</text> : null}
              </g>
            );
          })}
        </g>
      </svg>
      <div className="gcanvas-legend">
        {(["code", "deploy", "infra", "runtime", "security", "ops"] as const).map((k) => <span key={k}><i style={{ background: LAYER_COLOR[k] }} />{k}</span>)}
      </div>
    </div>
  );
}
