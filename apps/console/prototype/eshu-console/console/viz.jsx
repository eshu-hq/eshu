/* Eshu Console — viz primitives + UI atoms. Exports to window. */
const { useState, useRef, useEffect, useMemo, useId } = React;

/* ---------------------------------------------------------------- helpers */
function cx() { return Array.prototype.filter.call(arguments, Boolean).join(" "); }
function fmt(n) {
  if (n == null) return "—";
  if (Math.abs(n) >= 1e9) return (n / 1e9).toFixed(2) + "B";
  if (Math.abs(n) >= 1e6) return (n / 1e6).toFixed(2) + "M";
  if (Math.abs(n) >= 1e3) return (n / 1e3).toFixed(1) + "k";
  return String(n);
}
function path(points) {
  return points.map((p, i) => (i === 0 ? "M" : "L") + p[0].toFixed(1) + " " + p[1].toFixed(1)).join(" ");
}
function smoothPath(points) {
  if (points.length < 2) return "";
  let d = "M" + points[0][0].toFixed(1) + " " + points[0][1].toFixed(1);
  for (let i = 0; i < points.length - 1; i++) {
    const p0 = points[i], p1 = points[i + 1];
    const mx = (p0[0] + p1[0]) / 2;
    d += " Q" + p0[0].toFixed(1) + " " + p0[1].toFixed(1) + " " + mx.toFixed(1) + " " + ((p0[1] + p1[1]) / 2).toFixed(1);
  }
  const last = points[points.length - 1];
  d += " L" + last[0].toFixed(1) + " " + last[1].toFixed(1);
  return d;
}

/* ------------------------------------------------------------- Sparkline */
function Sparkline({ data, color = "#14b8a6", w = 120, h = 34, fill = true, strokeW = 1.8 }) {
  const min = Math.min.apply(null, data), max = Math.max.apply(null, data);
  const range = max - min || 1;
  const pts = data.map((v, i) => [(i / (data.length - 1)) * w, h - 4 - ((v - min) / range) * (h - 8)]);
  const gid = useId().replace(/:/g, "");
  return (
    <svg className="spark" viewBox={`0 0 ${w} ${h}`} preserveAspectRatio="none" width={w} height={h}>
      <defs>
        <linearGradient id={"sg" + gid} x1="0" y1="0" x2="0" y2="1">
          <stop offset="0%" stopColor={color} stopOpacity="0.34" />
          <stop offset="100%" stopColor={color} stopOpacity="0" />
        </linearGradient>
      </defs>
      {fill ? <path d={smoothPath(pts) + ` L${w} ${h} L0 ${h} Z`} fill={"url(#sg" + gid + ")"} /> : null}
      <path d={smoothPath(pts)} fill="none" stroke={color} strokeWidth={strokeW} strokeLinecap="round" strokeLinejoin="round" />
    </svg>
  );
}

/* -------------------------------------------------- AreaChart (w/ axis + hover) */
function AreaChart({ data, color = "#14b8a6", h = 160, unit = "", labels, format = fmt, yTicks = 4 }) {
  const [hover, setHover] = useState(null);
  const ref = useRef(null);
  const W = 600, PAD_L = 4, PAD_B = 22, PAD_T = 10;
  const min = 0, max = Math.max.apply(null, data) * 1.12 || 1;
  const innerH = h - PAD_B - PAD_T;
  const x = (i) => PAD_L + (i / (data.length - 1)) * (W - PAD_L - 8);
  const y = (v) => PAD_T + innerH - ((v - min) / (max - min)) * innerH;
  const pts = data.map((v, i) => [x(i), y(v)]);
  const gid = useId().replace(/:/g, "");
  const ticks = [];
  for (let i = 0; i <= yTicks; i++) ticks.push(Math.round((max / yTicks) * i));

  function onMove(e) {
    const r = ref.current.getBoundingClientRect();
    const px = ((e.clientX - r.left) / r.width) * W;
    let idx = Math.round(((px - PAD_L) / (W - PAD_L - 8)) * (data.length - 1));
    idx = Math.max(0, Math.min(data.length - 1, idx));
    setHover(idx);
  }
  return (
    <div className="chart-wrap">
      <svg ref={ref} className="areachart" viewBox={`0 0 ${W} ${h}`} preserveAspectRatio="none"
        onMouseMove={onMove} onMouseLeave={() => setHover(null)}>
        <defs>
          <linearGradient id={"ac" + gid} x1="0" y1="0" x2="0" y2="1">
            <stop offset="0%" stopColor={color} stopOpacity="0.30" />
            <stop offset="100%" stopColor={color} stopOpacity="0.02" />
          </linearGradient>
        </defs>
        {ticks.map((t, i) => (
          <line key={i} className="chart-grid" x1={PAD_L} x2={W - 8} y1={y(t)} y2={y(t)} />
        ))}
        <path d={smoothPath(pts) + ` L${x(data.length - 1)} ${PAD_T + innerH} L${PAD_L} ${PAD_T + innerH} Z`} fill={"url(#ac" + gid + ")"} />
        <path d={smoothPath(pts)} fill="none" stroke={color} strokeWidth="2.2" strokeLinecap="round" strokeLinejoin="round" />
        {hover != null ? (
          <g>
            <line className="chart-cursor" x1={x(hover)} x2={x(hover)} y1={PAD_T} y2={PAD_T + innerH} />
            <circle cx={x(hover)} cy={y(data[hover])} r="3.5" fill={color} stroke="var(--bg-panel)" strokeWidth="2" />
          </g>
        ) : null}
      </svg>
      <div className="chart-yaxis">
        {ticks.slice().reverse().map((t, i) => <span key={i}>{format(t)}</span>)}
      </div>
      {hover != null ? (
        <div className="chart-tip" style={{ left: `calc(${(x(hover) / W) * 100}% )` }}>
          <strong>{format(data[hover])}{unit}</strong>
          <span>{labels ? labels[hover] : `t-${data.length - 1 - hover}`}</span>
        </div>
      ) : null}
    </div>
  );
}

/* ----------------------------------------------- MultiLine (latency p50/95/99) */
function MultiLine({ seriesList, h = 160, labels, unit = "ms" }) {
  const [hover, setHover] = useState(null);
  const ref = useRef(null);
  const W = 600, PAD_B = 22, PAD_T = 10, PAD_L = 4;
  const all = [].concat.apply([], seriesList.map((s) => s.data));
  const max = Math.max.apply(null, all) * 1.12 || 1;
  const len = seriesList[0].data.length;
  const innerH = h - PAD_B - PAD_T;
  const x = (i) => PAD_L + (i / (len - 1)) * (W - PAD_L - 8);
  const y = (v) => PAD_T + innerH - (v / max) * innerH;
  function onMove(e) {
    const r = ref.current.getBoundingClientRect();
    const px = ((e.clientX - r.left) / r.width) * W;
    let idx = Math.round(((px - PAD_L) / (W - PAD_L - 8)) * (len - 1));
    setHover(Math.max(0, Math.min(len - 1, idx)));
  }
  return (
    <div className="chart-wrap">
      <svg ref={ref} className="areachart" viewBox={`0 0 ${W} ${h}`} preserveAspectRatio="none"
        onMouseMove={onMove} onMouseLeave={() => setHover(null)}>
        {[0, 0.25, 0.5, 0.75, 1].map((f, i) => <line key={i} className="chart-grid" x1={PAD_L} x2={W - 8} y1={PAD_T + innerH * f} y2={PAD_T + innerH * f} />)}
        {seriesList.map((s, si) => (
          <path key={si} d={smoothPath(s.data.map((v, i) => [x(i), y(v)]))} fill="none" stroke={s.color} strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" opacity="0.95" />
        ))}
        {hover != null ? <line className="chart-cursor" x1={x(hover)} x2={x(hover)} y1={PAD_T} y2={PAD_T + innerH} /> : null}
        {hover != null ? seriesList.map((s, si) => <circle key={si} cx={x(hover)} cy={y(s.data[hover])} r="3" fill={s.color} stroke="var(--bg-panel)" strokeWidth="1.6" />) : null}
      </svg>
      {hover != null ? (
        <div className="chart-tip" style={{ left: `calc(${(x(hover) / W) * 100}%)` }}>
          {seriesList.map((s, si) => <strong key={si} style={{ color: s.color }}>{s.label} {s.data[hover]}{unit}</strong>)}
          <span>{labels ? labels[hover] : `t-${len - 1 - hover}`}</span>
        </div>
      ) : null}
    </div>
  );
}

/* --------------------------------------------------------------- Donut */
function Donut({ segments, size = 132, thickness = 16, center }) {
  const total = segments.reduce((a, s) => a + s.value, 0) || 1;
  const r = (size - thickness) / 2;
  const c = 2 * Math.PI * r;
  let offset = 0;
  return (
    <div className="donut" style={{ width: size, height: size }}>
      <svg viewBox={`0 0 ${size} ${size}`} width={size} height={size}>
        <circle cx={size / 2} cy={size / 2} r={r} fill="none" stroke="var(--line)" strokeWidth={thickness} />
        {segments.map((s, i) => {
          const len = (s.value / total) * c;
          const el = (
            <circle key={i} cx={size / 2} cy={size / 2} r={r} fill="none" stroke={s.color}
              strokeWidth={thickness} strokeDasharray={`${len} ${c - len}`} strokeDashoffset={-offset}
              strokeLinecap="butt" transform={`rotate(-90 ${size / 2} ${size / 2})`}>
              <title>{s.label}: {s.value}</title>
            </circle>
          );
          offset += len;
          return el;
        })}
      </svg>
      {center ? <div className="donut-center"><strong>{center.value}</strong><span>{center.label}</span></div> : null}
    </div>
  );
}

/* ------------------------------------------------------- horizontal bars */
function BarRows({ rows, max, format = fmt }) {
  const m = max || Math.max.apply(null, rows.map((r) => r.value)) || 1;
  return (
    <div className="barrows">
      {rows.map((r, i) => (
        <div className="barrow" key={i} title={r.detail || ""}>
          <span className="barrow-label">{r.label}</span>
          <div className="barrow-track"><div className="barrow-fill" style={{ width: (r.value / m) * 100 + "%", background: r.color || "var(--teal)" }} /></div>
          <span className="barrow-val">{format(r.value)}</span>
        </div>
      ))}
    </div>
  );
}

/* ------------------------------------------------------- stacked severity bar */
function SeverityBar({ counts, sev }) {
  const order = ["critical", "high", "medium", "low"];
  const total = order.reduce((a, k) => a + (counts[k] || 0), 0) || 1;
  return (
    <div className="sevbar" title={order.map((k) => `${k}: ${counts[k] || 0}`).join("  ·  ")}>
      {order.map((k) => counts[k] ? <span key={k} style={{ width: ((counts[k] / total) * 100) + "%", background: sev[k] }} /> : null)}
    </div>
  );
}

/* ------------------------------------------------------------- atoms */
function StatTile({ label, value, sub, spark, color = "var(--teal)", trend, onClick, cta }) {
  const Tag = onClick ? "button" : "div";
  return (
    <Tag className={"stat-tile" + (onClick ? " stat-tile-btn" : "")} {...onClick ? { type: "button", onClick } : {}}>
      <div className="stat-tile-head"><span>{label}</span>{trend ? <em className={"trend " + trend.dir}>{trend.text}</em> : null}</div>
      <div className="stat-tile-body">
        <strong>{value}</strong>
        {spark ? <Sparkline data={spark} color={color} w={86} h={30} /> : null}
      </div>
      {sub ? <div className="stat-tile-sub">{sub}{cta ? <span className="stat-tile-cta">{cta} →</span> : null}</div> : null}
    </Tag>
  );
}

function Badge({ children, tone = "neutral", dot, color }) {
  return <span className={"badge badge-" + tone}>{dot ? <i className="badge-dot" style={color ? { background: color } : null} /> : null}{children}</span>;
}

function TruthChip({ level }) {
  const c = ESHU.truthColor[level] || "#6b7280";
  return <span className="truth-chip" style={{ "--tc": c }} title={`Truth: ${level}`}><i />{level}</span>;
}
function FreshDot({ state }) {
  const c = ESHU.freshColor[state] || "#6b7280";
  return <span className="fresh-dot" title={`Freshness: ${state}`}><i style={{ background: c }} />{state}</span>;
}

function Panel({ title, sub, action, children, className, glyph }) {
  return (
    <section className={cx("panel", className)}>
      {(title || action) ? (
        <header className="panel-head">
          <div className="panel-title">{glyph ? <span className="panel-glyph">{glyph}</span> : null}<div><h3>{title}</h3>{sub ? <p>{sub}</p> : null}</div></div>
          {action ? <div className="panel-action">{action}</div> : null}
        </header>
      ) : null}
      <div className="panel-body">{children}</div>
    </section>
  );
}

/* tiny collector glyph (monogram chip) */
function CollectorGlyph({ kind, size = 26 }) {
  const k = ESHU.collectorKinds[kind] || { color: "#999", label: kind };
  const txt = { git: "Git", aws: "AWS", terraform_state: "TF", oci_registry: "OCI", kubernetes: "K8s", vulnerability_intelligence: "CVE", security_alert: "SEC", pagerduty: "PD", jira: "JR", confluence: "DOC", prometheus_mimir: "PR", sbom_attestation: "SB", cloudwatch: "CW", otel_traces: "OT", grafana_loki: "LK", datadog: "DD", cloudflare: "CF", grafana_synthetic: "SY" }[kind] || "·";
  return <span className="cglyph" style={{ width: size, height: size, color: k.color, borderColor: k.color }} title={k.label}>{txt}</span>;
}

Object.assign(window, {
  cx, fmt, path, smoothPath,
  Sparkline, AreaChart, MultiLine, Donut, BarRows, SeverityBar,
  StatTile, Badge, TruthChip, FreshDot, Panel, CollectorGlyph
});
