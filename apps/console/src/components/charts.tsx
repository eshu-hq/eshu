// components/charts.tsx
// SVG data-viz primitives (dark theme, CSS-var driven). Framework-only deps.
import { useId, useRef, useState } from "react";

import { fmt } from "../console/types";

function smoothPath(points: readonly (readonly [number, number])[]): string {
  if (points.length < 2) return "";
  let d = `M${points[0][0].toFixed(1)} ${points[0][1].toFixed(1)}`;
  for (let i = 0; i < points.length - 1; i++) {
    const p0 = points[i], p1 = points[i + 1];
    const mx = (p0[0] + p1[0]) / 2;
    d += ` Q${p0[0].toFixed(1)} ${p0[1].toFixed(1)} ${mx.toFixed(1)} ${((p0[1] + p1[1]) / 2).toFixed(1)}`;
  }
  const last = points[points.length - 1];
  d += ` L${last[0].toFixed(1)} ${last[1].toFixed(1)}`;
  return d;
}

export function Sparkline({ data, color = "#14b8a6", w = 86, h = 30 }: {
  readonly data: readonly number[]; readonly color?: string; readonly w?: number; readonly h?: number;
}): React.JSX.Element {
  const gid = useId().replace(/:/g, "");
  if (data.length === 0) return <svg width={w} height={h} />;
  // A single datapoint can't form a line: duplicate it so the path spans the
  // width as a flat line instead of dividing by (length - 1) === 0 and emitting
  // an invalid SVG path.
  const series = data.length === 1 ? [data[0], data[0]] : data;
  const min = Math.min(...series), max = Math.max(...series), range = max - min || 1;
  const pts = series.map((v, i): [number, number] => [(i / (series.length - 1)) * w, h - 4 - ((v - min) / range) * (h - 8)]);
  return (
    <svg className="spark" viewBox={`0 0 ${w} ${h}`} preserveAspectRatio="none" width={w} height={h}>
      <defs>
        <linearGradient id={`sg${gid}`} x1="0" y1="0" x2="0" y2="1">
          <stop offset="0%" stopColor={color} stopOpacity="0.34" />
          <stop offset="100%" stopColor={color} stopOpacity="0" />
        </linearGradient>
      </defs>
      <path d={`${smoothPath(pts)} L${w} ${h} L0 ${h} Z`} fill={`url(#sg${gid})`} />
      <path d={smoothPath(pts)} fill="none" stroke={color} strokeWidth={1.8} strokeLinecap="round" strokeLinejoin="round" />
    </svg>
  );
}

export function AreaChart({ data, color = "#14b8a6", h = 170, unit = "" }: {
  readonly data: readonly number[]; readonly color?: string; readonly h?: number; readonly unit?: string;
}): React.JSX.Element {
  const [hover, setHover] = useState<number | null>(null);
  const ref = useRef<SVGSVGElement>(null);
  const gid = useId().replace(/:/g, "");
  const W = 600, padB = 22, padT = 10, padL = 4;
  if (data.length === 0) return <div className="empty">No series available from this source.</div>;
  // A single datapoint can't form a line: duplicate it so the path spans the
  // width as a flat line instead of dividing by (length - 1) === 0 and emitting
  // an invalid SVG path.
  const series = data.length === 1 ? [data[0], data[0]] : data;
  const max = Math.max(...series) * 1.12 || 1, innerH = h - padB - padT;
  const x = (i: number): number => padL + (i / (series.length - 1)) * (W - padL - 8);
  const y = (v: number): number => padT + innerH - (v / max) * innerH;
  const pts = series.map((v, i): [number, number] => [x(i), y(v)]);
  const ticks = [0, 1, 2, 3, 4].map((i) => Math.round((max / 4) * i));
  function onMove(e: React.MouseEvent): void {
    if (!ref.current) return;
    const r = ref.current.getBoundingClientRect();
    const idx = Math.round((((e.clientX - r.left) / r.width) * W - padL) / (W - padL - 8) * (series.length - 1));
    setHover(Math.max(0, Math.min(series.length - 1, idx)));
  }
  return (
    <div className="chart-wrap">
      <svg ref={ref} className="areachart" viewBox={`0 0 ${W} ${h}`} preserveAspectRatio="none" onMouseMove={onMove} onMouseLeave={() => setHover(null)}>
        <defs>
          <linearGradient id={`ac${gid}`} x1="0" y1="0" x2="0" y2="1">
            <stop offset="0%" stopColor={color} stopOpacity="0.30" />
            <stop offset="100%" stopColor={color} stopOpacity="0.02" />
          </linearGradient>
        </defs>
        {ticks.map((t, i) => <line key={i} className="chart-grid" x1={padL} x2={W - 8} y1={y(t)} y2={y(t)} />)}
        <path d={`${smoothPath(pts)} L${x(series.length - 1)} ${padT + innerH} L${padL} ${padT + innerH} Z`} fill={`url(#ac${gid})`} />
        <path d={smoothPath(pts)} fill="none" stroke={color} strokeWidth="2.2" strokeLinecap="round" strokeLinejoin="round" />
        {hover !== null ? <g><line className="chart-cursor" x1={x(hover)} x2={x(hover)} y1={padT} y2={padT + innerH} /><circle cx={x(hover)} cy={y(series[hover])} r="3.5" fill={color} stroke="var(--bg-panel)" strokeWidth="2" /></g> : null}
      </svg>
      <div className="chart-yaxis">{ticks.slice().reverse().map((t, i) => <span key={i}>{fmt(t)}</span>)}</div>
      {hover !== null ? <div className="chart-tip" style={{ left: `${(x(hover) / W) * 100}%` }}><strong>{fmt(series[hover])}{unit}</strong><span>t-{series.length - 1 - hover}</span></div> : null}
    </div>
  );
}

export function Donut({ segments, size = 132, thickness = 16, center }: {
  readonly segments: readonly { label: string; value: number; color: string }[];
  readonly size?: number; readonly thickness?: number;
  readonly center?: { readonly value: string | number; readonly label: string };
}): React.JSX.Element {
  const total = segments.reduce((a, s) => a + s.value, 0) || 1;
  const r = (size - thickness) / 2, c = 2 * Math.PI * r;
  let offset = 0;
  return (
    <div className="donut" style={{ width: size, height: size }}>
      <svg viewBox={`0 0 ${size} ${size}`} width={size} height={size}>
        <circle cx={size / 2} cy={size / 2} r={r} fill="none" stroke="var(--line)" strokeWidth={thickness} />
        {segments.map((s, i) => {
          const len = (s.value / total) * c;
          const el = <circle key={i} cx={size / 2} cy={size / 2} r={r} fill="none" stroke={s.color} strokeWidth={thickness} strokeDasharray={`${len} ${c - len}`} strokeDashoffset={-offset} transform={`rotate(-90 ${size / 2} ${size / 2})`}><title>{s.label}: {s.value}</title></circle>;
          offset += len;
          return el;
        })}
      </svg>
      {center ? <div className="donut-center"><strong>{center.value}</strong><span>{center.label}</span></div> : null}
    </div>
  );
}

export function BarRows({ rows }: {
  readonly rows: readonly { label: string; value: number; color?: string; detail?: string }[];
}): React.JSX.Element {
  const m = Math.max(...rows.map((r) => r.value), 1);
  return (
    <div className="barrows">
      {rows.map((r, i) => (
        <div className="barrow" key={i} title={r.detail ?? ""}>
          <span className="barrow-label">{r.label}</span>
          <div className="barrow-track"><div className="barrow-fill" style={{ width: `${(r.value / m) * 100}%`, background: r.color ?? "var(--teal)" }} /></div>
          <span className="barrow-val">{fmt(r.value)}</span>
        </div>
      ))}
    </div>
  );
}

export function SeverityBar({ counts }: {
  readonly counts: Readonly<Record<"critical" | "high" | "medium" | "low", number>>;
}): React.JSX.Element {
  const order: readonly ("critical" | "high" | "medium" | "low")[] = ["critical", "high", "medium", "low"];
  const colors: Record<string, string> = { critical: "#f0506e", high: "#ff8a00", medium: "#f5b73d", low: "#14b8a6" };
  const total = order.reduce((a, k) => a + (counts[k] || 0), 0) || 1;
  return (
    <div className="sevbar">
      {order.map((k) => counts[k] ? <span key={k} style={{ width: `${(counts[k] / total) * 100}%`, background: colors[k] }} /> : null)}
    </div>
  );
}
