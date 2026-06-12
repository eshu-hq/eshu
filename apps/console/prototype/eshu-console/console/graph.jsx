/* Eshu Console — interactive graph canvas + lane flow. Exports to window. */
const { useState: useStateG, useRef: useRefG, useMemo: useMemoG, useEffect: useEffectG } = React;

function nodeSize(n) {
  return n.hero ? { w: 196, h: 64 } : { w: 176, h: 56 };
}

/* stable key for an edge so it can be selected/compared */
function edgeKey(e) { return e ? e.s + "→" + e.t + "·" + (e.verb || "") : null; }

/* layered (left-to-right) or radial layout */
function computeLayout(graph, mode) {
  const W = 1080, H = 640;
  const pos = {};
  if (mode === "radial") {
    const hero = graph.nodes.find((n) => n.hero) || graph.nodes[0];
    const others = graph.nodes.filter((n) => n !== hero);
    pos[hero.id] = { x: W / 2, y: H / 2 };
    const R1 = 230, R2 = 410;
    others.forEach((n, i) => {
      const ring = i % 2 === 0 ? R1 : R2;
      const a = (i / others.length) * Math.PI * 2 - Math.PI / 2;
      pos[n.id] = { x: W / 2 + Math.cos(a) * ring * 1.32, y: H / 2 + Math.sin(a) * ring };
    });
  } else {
    const cols = {};
    graph.nodes.forEach((n) => { (cols[n.col] = cols[n.col] || []).push(n); });
    const colKeys = Object.keys(cols).map(Number).sort((a, b) => a - b);
    const colW = W / (colKeys.length);
    colKeys.forEach((ck, ci) => {
      const list = cols[ck];
      const gap = H / (list.length + 1);
      list.forEach((n, i) => {
        pos[n.id] = { x: colW * ci + colW / 2, y: gap * (i + 1) };
      });
    });
  }
  return pos;
}

function edgePath(a, b) {
  const dx = b.x - a.x;
  const c1x = a.x + dx * 0.5, c2x = b.x - dx * 0.5;
  return `M${a.x} ${a.y} C${c1x} ${a.y} ${c2x} ${b.y} ${b.x} ${b.y}`;
}

function GraphCanvas({ graph, data, layout = "layered", height = 560, onSelect, onSelectEdge, onExpand, onClear, selectedId, selectedEdge, expandedIds, showLabels = true, density, tracePath = null }) {
  const [vt, setVt] = useStateG({ x: 0, y: 0, k: 1 });
  const [hover, setHover] = useStateG(null);
  const [hoverEdge, setHoverEdge] = useStateG(null);
  const [pan, setPan] = useStateG(null);
  const [card, setCard] = useStateG(null); // in-canvas edge card (used when no external onSelectEdge)
  const svgRef = useRefG(null);
  const movedRef = useRefG(false);
  const pos = useMemoG(() => computeLayout(graph, layout), [graph, layout]);
  const VBW = 1080, VBH = 640;
  const selEdgeKey = selectedEdge ? edgeKey(selectedEdge) : null;

  const adj = useMemoG(() => {
    const m = {};
    graph.edges.forEach((e) => { (m[e.s] = m[e.s] || new Set()).add(e.t); (m[e.t] = m[e.t] || new Set()).add(e.s); });
    return m;
  }, [graph]);

  // any node ids that should stay lit
  const tracedNodes = tracePath ? tracePath.nodes : (selectedEdge ? new Set([selectedEdge.s, selectedEdge.t]) : null);
  const activeNode = hover || selectedId;

  function dim(id) {
    if (tracedNodes) return !tracedNodes.has(id);
    if (hoverEdge) return id !== hoverEdge.s && id !== hoverEdge.t;
    if (!activeNode) return false;
    if (id === activeNode) return false;
    return !(adj[activeNode] && adj[activeNode].has(id));
  }
  function edgeIsHot(e) {
    if (tracePath) return tracePath.edges.has(edgeKey(e));
    if (selEdgeKey && edgeKey(e) === selEdgeKey) return true;
    if (hoverEdge && edgeKey(e) === edgeKey(hoverEdge)) return true;
    if (activeNode && (e.s === activeNode || e.t === activeNode)) return true;
    return false;
  }
  const anyActive = !!(activeNode || hoverEdge || selectedEdge || tracePath);

  function toSvg(e) {
    const r = svgRef.current.getBoundingClientRect();
    return { x: ((e.clientX - r.left) / r.width) * VBW, y: ((e.clientY - r.top) / r.height) * VBH };
  }
  function onWheel(e) {
    e.preventDefault();
    const dk = e.deltaY < 0 ? 0.12 : -0.12;
    setVt((v) => ({ ...v, k: Math.min(2.6, Math.max(0.55, +(v.k + dk).toFixed(2))) }));
  }
  function onDown(e) { movedRef.current = false; const p = toSvg(e); setPan({ ox: vt.x, oy: vt.y, sx: p.x, sy: p.y }); }
  function onMoveBg(e) {
    if (!pan) return;
    const p = toSvg(e);
    if (Math.abs(p.x - pan.sx) > 3 || Math.abs(p.y - pan.sy) > 3) movedRef.current = true;
    setVt((v) => ({ ...v, x: pan.ox + (p.x - pan.sx), y: pan.oy + (p.y - pan.sy) }));
  }
  function onUp() { setPan(null); }
  function onBgClick(e) {
    // genuine background click (not a drag, not on a node/edge) clears selection
    if (movedRef.current) return;
    const t = e.target;
    if (t === svgRef.current || (t.getAttribute && t.getAttribute("data-bg") === "1")) {
      setCard(null);
      onClear && onClear();
    }
  }

  function selectEdge(e, p) {
    if (onSelectEdge) { onSelectEdge(e); setCard(null); }
    else {
      // self-contained card for graphs without an external inspector
      const mid = { x: (pos[e.s].x + pos[e.t].x) / 2, y: (pos[e.s].y + pos[e.t].y) / 2 };
      setCard({ edge: e, x: mid.x, y: mid.y });
    }
  }

  // close internal card if selection changes externally
  useEffectG(() => { if (onSelectEdge) setCard(null); }, [selEdgeKey]);

  return (
    <div className="gcanvas" style={{ height }}>
      <div className="gcanvas-toolbar">
        <span className="gcanvas-count">{graph.nodes.length} nodes · {graph.edges.length} edges{anyActive ? " · drilling" : ""}</span>
        <div className="gcanvas-zoom">
          <button onClick={() => setVt((v) => ({ ...v, k: Math.min(2.6, +(v.k + 0.2).toFixed(2)) }))} title="Zoom in">+</button>
          <span>{Math.round(vt.k * 100)}%</span>
          <button onClick={() => setVt((v) => ({ ...v, k: Math.max(0.55, +(v.k - 0.2).toFixed(2)) }))} title="Zoom out">−</button>
          <button onClick={() => setVt({ x: 0, y: 0, k: 1 })} title="Reset view">⟲</button>
        </div>
      </div>
      <svg ref={svgRef} className={cx("gcanvas-svg", pan && "is-panning")} viewBox={`0 0 ${VBW} ${VBH}`}
        onWheel={onWheel} onPointerDown={onDown} onPointerMove={onMoveBg} onPointerUp={onUp} onPointerLeave={onUp} onClick={onBgClick}>
        <defs>
          <marker id="g-arrow" markerWidth="9" markerHeight="9" refX="7.5" refY="4" orient="auto">
            <path d="M0 0 L8 4 L0 8 Z" fill="var(--edge)" />
          </marker>
          <marker id="g-arrow-hot" markerWidth="9" markerHeight="9" refX="7.5" refY="4" orient="auto">
            <path d="M0 0 L8 4 L0 8 Z" fill="var(--teal)" />
          </marker>
          <radialGradient id="g-vignette" cx="50%" cy="46%" r="70%">
            <stop offset="60%" stopColor="transparent" />
            <stop offset="100%" stopColor="rgba(0,0,0,0.35)" />
          </radialGradient>
        </defs>
        <rect data-bg="1" x="-2000" y="-2000" width="6000" height="6000" fill="transparent" />
        <g transform={`translate(${vt.x} ${vt.y}) scale(${vt.k})`}>
          {/* edges */}
          {graph.edges.map((e, i) => {
            const a = pos[e.s], b = pos[e.t];
            if (!a || !b) return null;
            const hot = edgeIsHot(e);
            const onPath = tracePath && tracePath.edges.has(edgeKey(e));
            const sel = (selEdgeKey && edgeKey(e) === selEdgeKey) || onPath;
            const faded = anyActive && !hot;
            const col = ESHU.layerColor[e.layer] || "var(--edge)";
            const mid = { x: (a.x + b.x) / 2, y: (a.y + b.y) / 2 };
            const d = edgePath(a, b);
            const showLbl = (hot || sel) && showLabels;
            return (
              <g key={i} className={cx("gedge", faded && "is-faded", sel && "is-sel")}>
                <path d={d} fill="none" stroke={hot ? col : "var(--edge)"} strokeWidth={sel ? 3 : hot ? 2.4 : 1.4}
                  markerEnd={hot ? "url(#g-arrow-hot)" : "url(#g-arrow)"} opacity={hot ? 0.95 : 0.5}
                  className={cx("gedge-line", (sel || onPath) && "is-flow")} style={sel ? { stroke: col } : null} />
                {/* wide invisible hit target — makes the whole edge clickable */}
                <path className="gedge-hit" d={d} fill="none" stroke="transparent" strokeWidth="20"
                  onMouseEnter={() => setHoverEdge(e)} onMouseLeave={() => setHoverEdge(null)}
                  onClick={(ev) => { ev.stopPropagation(); selectEdge(e); }} />
                {showLbl ? (
                  <g pointerEvents="none">
                    <rect className="gedge-lbl-bg" x={mid.x - (e.verb.length * 3.4 + 10)} y={mid.y - 19} rx="5"
                      width={e.verb.length * 6.8 + 20} height="16" />
                    <text className="gedge-lbl" x={mid.x} y={mid.y - 7} style={{ fill: col }}>{e.verb}</text>
                  </g>
                ) : null}
              </g>
            );
          })}
          {/* nodes */}
          {graph.nodes.map((n) => {
            const p = pos[n.id]; if (!p) return null;
            const { w, h } = nodeSize(n);
            const ks = ESHU.kindStyle[n.kind] || { color: "#999", label: n.kind };
            const sel = selectedId === n.id;
            const traced = tracedNodes && tracedNodes.has(n.id);
            const faded = dim(n.id);
            const canExpand = onExpand && expandedIds && !expandedIds.has(n.id);
            return (
              <g key={n.id} className={cx("gnode", "gnode-" + n.kind, sel && "is-sel", traced && "is-traced", n.hero && "is-hero", faded && "is-faded")}
                transform={`translate(${p.x - w / 2} ${p.y - h / 2})`}
                onMouseEnter={() => setHover(n.id)} onMouseLeave={() => setHover(null)}
                onClick={(ev) => { ev.stopPropagation(); onSelect && onSelect(n); }}
                onDoubleClick={(ev) => { ev.stopPropagation(); onExpand && onExpand(n); }}
                style={{ "--nc": ks.color }}>
                <rect className="gnode-box" width={w} height={h} rx="12" />
                <rect className="gnode-accent" width="4" height={h} rx="2" />
                <circle className="gnode-dot" cx="22" cy={h / 2} r="6" />
                <text className="gnode-label" x="38" y={n.sub ? h / 2 - 4 : h / 2 + 5}>{n.label}</text>
                {n.sub ? <text className="gnode-sub" x="38" y={h / 2 + 14}>{n.sub}</text> : null}
                {n.kind === "vuln" ? <text className="gnode-flag" x={w - 15} y={h / 2 + 5}>!</text> : null}
                {canExpand ? (
                  <g className="gnode-expand" onClick={(ev) => { ev.stopPropagation(); onExpand(n); }}>
                    <circle cx={w - 13} cy="13" r="9" />
                    <text x={w - 13} y="17.5">＋</text>
                  </g>
                ) : null}
              </g>
            );
          })}
        </g>
        <rect className="gcanvas-vignette" width={VBW} height={VBH} fill="url(#g-vignette)" pointerEvents="none" />
      </svg>
      {card ? <EdgeCard edge={card.edge} graph={graph} data={data} onClose={() => setCard(null)} onOpenNode={(id) => { setCard(null); const nd = graph.nodes.find((n) => n.id === id); if (nd && onSelect) onSelect(nd); }} /> : null}
      <GraphLegend />
    </div>
  );
}

function GraphLegend() {
  const layers = [
    ["code", "Code"], ["deploy", "Deploy"], ["infra", "Infra"], ["runtime", "Runtime"], ["security", "Security"], ["ops", "Ops"]
  ];
  return (
    <div className="gcanvas-legend">
      {layers.map(([k, l]) => <span key={k}><i style={{ background: ESHU.layerColor[k] }} />{l}</span>)}
    </div>
  );
}

/* floating in-canvas evidence card for a clicked edge (graphs without a side inspector) */
function EdgeCard({ edge, graph, data, onClose, onOpenNode }) {
  const info = edgeEvidence(edge, graph, data);
  const col = ESHU.layerColor[edge.layer] || "var(--teal)";
  return (
    <div className="edge-card" style={{ "--ec": col }} role="dialog" aria-label="Relationship evidence">
      <button className="edge-card-close" onClick={onClose} aria-label="Close">×</button>
      <div className="edge-card-verb" style={{ color: col }}>{edge.verb}</div>
      <div className="edge-card-ends">
        <button className="edge-end" onClick={() => onOpenNode(edge.s)}>{info.sLabel}</button>
        <span className="edge-end-arrow" style={{ color: col }}>→</span>
        <button className="edge-end" onClick={() => onOpenNode(edge.t)}>{info.tLabel}</button>
      </div>
      <p className="edge-card-desc">{info.desc}</p>
      <div className="edge-card-facts">{info.facts.map((f, i) => <div key={i} className="edge-fact">{f}</div>)}</div>
      <div className="edge-card-foot"><span className="edge-layer-chip" style={{ "--ec": col }}><i />{edge.layer} layer</span>{info.count != null ? <span className="t-mut mono" style={{ fontSize: ".68rem" }}>{fmt(info.count)} in graph</span> : null}</div>
    </div>
  );
}

/* lane flow: left-to-right evidence stages; items are clickable to drill into evidence */
function LaneFlow({ stages, onDrill }) {
  const [sel, setSel] = React.useState(null);
  const active = sel ? stages[sel.i] && stages[sel.i].items[sel.j] : null;
  return (
    <div>
      <div className="laneflow">
        {stages.map((st, i) => (
          <React.Fragment key={i}>
            <div className="lane-stage">
              <div className="lane-stage-head"><span className="lane-idx">{String(i + 1).padStart(2, "0")}</span><h4>{st.title}</h4></div>
              <div className="lane-items">
                {st.items.map((it, j) => {
                  const isSel = sel && sel.i === i && sel.j === j;
                  return (
                    <button type="button" className={cx("lane-item", "lane-item-btn", it.tone && "lane-" + it.tone, isSel && "is-sel")} key={j} style={it.color ? { "--lc": it.color } : null}
                      onClick={() => {
                        if (it.action) { it.action(); return; }
                        if (it.drillTo && onDrill) { onDrill(it.drillTo); return; }
                        setSel(isSel ? null : { i, j });
                      }}>
                      <strong>{it.label}{(it.action || it.drillTo) ? <span className="lane-caret">→</span> : (it.evidence && it.evidence.length) ? <span className="lane-caret">{isSel ? "▾" : "▸"}</span> : null}</strong>
                      {it.sub ? <span>{it.sub}</span> : null}
                      {it.verb ? <em>{it.verb}</em> : null}
                    </button>
                  );
                })}
              </div>
            </div>
            {i < stages.length - 1 ? <div className="lane-arrow">→</div> : null}
          </React.Fragment>
        ))}
      </div>
      {active && active.evidence && active.evidence.length ?
        <div className="lane-detail">
          <div className="lane-detail-head"><span className="lane-detail-verb">{active.verb || "EVIDENCE"}</span><strong>{active.label}</strong>{active.sub ? <span className="t-mut">{active.sub}</span> : null}</div>
          <div className="insp-evi">{active.evidence.map((e, k) => <div className="insp-evi-row" key={k}>{e}</div>)}</div>
        </div> :
        null}
    </div>
  );
}

Object.assign(window, { GraphCanvas, LaneFlow, EdgeCard, edgeKey });
