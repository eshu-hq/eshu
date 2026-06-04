/* Eshu Console — interactive graph canvas + lane flow. Exports to window. */
const { useState: useStateG, useRef: useRefG, useMemo: useMemoG, useEffect: useEffectG } = React;

function nodeSize(n) {
  return n.hero ? { w: 196, h: 64 } : { w: 176, h: 56 };
}

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

function GraphCanvas({ graph, layout = "layered", height = 560, onSelect, selectedId, showLabels = true, density }) {
  const [vt, setVt] = useStateG({ x: 0, y: 0, k: 1 });
  const [hover, setHover] = useStateG(null);
  const [pan, setPan] = useStateG(null);
  const svgRef = useRefG(null);
  const pos = useMemoG(() => computeLayout(graph, layout), [graph, layout]);
  const VBW = 1080, VBH = 640;

  const adj = useMemoG(() => {
    const m = {};
    graph.edges.forEach((e) => { (m[e.s] = m[e.s] || new Set()).add(e.t); (m[e.t] = m[e.t] || new Set()).add(e.s); });
    return m;
  }, [graph]);

  const active = hover || selectedId;
  function dim(id) {
    if (!active) return false;
    if (id === active) return false;
    return !(adj[active] && adj[active].has(id));
  }
  function edgeActive(e) {
    if (!active) return false;
    return e.s === active || e.t === active;
  }

  function toSvg(e) {
    const r = svgRef.current.getBoundingClientRect();
    return { x: ((e.clientX - r.left) / r.width) * VBW, y: ((e.clientY - r.top) / r.height) * VBH };
  }
  function onWheel(e) {
    e.preventDefault();
    const dk = e.deltaY < 0 ? 0.12 : -0.12;
    setVt((v) => ({ ...v, k: Math.min(2.6, Math.max(0.55, +(v.k + dk).toFixed(2))) }));
  }
  function onDown(e) { const p = toSvg(e); setPan({ ox: vt.x, oy: vt.y, sx: p.x, sy: p.y }); }
  function onMoveBg(e) {
    if (!pan) return;
    const p = toSvg(e);
    setVt((v) => ({ ...v, x: pan.ox + (p.x - pan.sx), y: pan.oy + (p.y - pan.sy) }));
  }
  function onUp() { setPan(null); }

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
      <svg ref={svgRef} className={cx("gcanvas-svg", pan && "is-panning")} viewBox={`0 0 ${VBW} ${VBH}`}
        onWheel={onWheel} onPointerDown={onDown} onPointerMove={onMoveBg} onPointerUp={onUp} onPointerLeave={onUp}>
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
        <g transform={`translate(${vt.x} ${vt.y}) scale(${vt.k})`}>
          {/* edges */}
          {graph.edges.map((e, i) => {
            const a = pos[e.s], b = pos[e.t];
            if (!a || !b) return null;
            const hot = edgeActive(e);
            const faded = active && !hot;
            const col = ESHU.layerColor[e.layer] || "var(--edge)";
            const mid = { x: (a.x + b.x) / 2, y: (a.y + b.y) / 2 };
            return (
              <g key={i} className={cx("gedge", faded && "is-faded")}>
                <path d={edgePath(a, b)} fill="none" stroke={hot ? col : "var(--edge)"} strokeWidth={hot ? 2.4 : 1.4}
                  markerEnd={hot ? "url(#g-arrow-hot)" : "url(#g-arrow)"} opacity={hot ? 0.95 : 0.5} />
                {hot && showLabels ? (
                  <g>
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
            const isHover = hover === n.id;
            const faded = dim(n.id);
            return (
              <g key={n.id} className={cx("gnode", "gnode-" + n.kind, sel && "is-sel", n.hero && "is-hero", faded && "is-faded")}
                transform={`translate(${p.x - w / 2} ${p.y - h / 2})`}
                onMouseEnter={() => setHover(n.id)} onMouseLeave={() => setHover(null)}
                onClick={(ev) => { ev.stopPropagation(); onSelect && onSelect(n); }}
                style={{ "--nc": ks.color }}>
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
        <rect className="gcanvas-vignette" width={VBW} height={VBH} fill="url(#g-vignette)" pointerEvents="none" />
      </svg>
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

/* lane flow: left-to-right evidence stages */
function LaneFlow({ stages }) {
  return (
    <div className="laneflow">
      {stages.map((st, i) => (
        <React.Fragment key={i}>
          <div className="lane-stage">
            <div className="lane-stage-head"><span className="lane-idx">{String(i + 1).padStart(2, "0")}</span><h4>{st.title}</h4></div>
            <div className="lane-items">
              {st.items.map((it, j) => (
                <div className={cx("lane-item", it.tone && "lane-" + it.tone)} key={j} style={it.color ? { "--lc": it.color } : null}>
                  <strong>{it.label}</strong>
                  {it.sub ? <span>{it.sub}</span> : null}
                  {it.verb ? <em>{it.verb}</em> : null}
                </div>
              ))}
            </div>
          </div>
          {i < stages.length - 1 ? <div className="lane-arrow">→</div> : null}
        </React.Fragment>
      ))}
    </div>
  );
}

Object.assign(window, { GraphCanvas, LaneFlow });
