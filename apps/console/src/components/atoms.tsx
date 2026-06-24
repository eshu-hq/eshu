// components/atoms.tsx
import type { ReactNode } from "react";

import { Sparkline } from "./charts";
import { TRUTH_COLOR, FRESH_COLOR } from "../console/types";
import type { UiTruth, UiFresh } from "../console/types";

export function StatTile({ label, value, sub, spark, color = "var(--teal)", trend }: {
  readonly label: string; readonly value: string | number; readonly sub?: string;
  readonly spark?: readonly number[]; readonly color?: string;
  readonly trend?: { readonly dir: "up" | "down" | "flat"; readonly text: string };
}): React.JSX.Element {
  return (
    <div className="stat-tile">
      <div className="stat-tile-head"><span>{label}</span>{trend ? <em className={`trend ${trend.dir}`}>{trend.text}</em> : null}</div>
      <div className="stat-tile-body">
        <strong>{value}</strong>
        {spark && spark.length > 0 ? <Sparkline data={spark} color={color} /> : null}
      </div>
      {sub ? <div className="stat-tile-sub">{sub}</div> : null}
    </div>
  );
}

export function Badge({ children, tone = "neutral", dot, color }: {
  readonly children: ReactNode; readonly tone?: "neutral" | "teal" | "ember" | "crit" | "warn" | "violet";
  readonly dot?: boolean; readonly color?: string;
}): React.JSX.Element {
  return <span className={`badge badge-${tone}`}>{dot ? <i className="badge-dot" style={color ? { background: color } : undefined} /> : null}{children}</span>;
}

export function TruthChip({ level }: { readonly level: UiTruth }): React.JSX.Element {
  return <span className="truth-chip" style={{ "--tc": TRUTH_COLOR[level] } as React.CSSProperties} title={`Truth: ${level}`}><i />{level}</span>;
}

export function FreshDot({ state }: { readonly state: UiFresh }): React.JSX.Element {
  return <span className="fresh-dot" title={`Freshness: ${state}`}><i style={{ background: FRESH_COLOR[state] }} />{state}</span>;
}

export function Panel({ title, sub, action, children, className, glyph }: {
  readonly title?: string; readonly sub?: string; readonly action?: ReactNode;
  readonly children: ReactNode; readonly className?: string; readonly glyph?: ReactNode;
}): React.JSX.Element {
  return (
    <section className={`panel${className ? ` ${className}` : ""}`}>
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

const COLLECTOR_GLYPH: Record<string, { text: string; color: string }> = {
  git: { text: "Git", color: "#f3ebdd" }, aws: { text: "AWS", color: "#ff9d2e" },
  terraform_state: { text: "TF", color: "#8b5cf6" }, oci_registry: { text: "OCI", color: "#22d3ee" },
  kubernetes: { text: "K8s", color: "#4f8cff" }, vulnerability_intelligence: { text: "CVE", color: "#f0506e" },
  security_alert: { text: "SEC", color: "#fb7185" }, pagerduty: { text: "PD", color: "#22c55e" },
  jira: { text: "JR", color: "#4f8cff" }, package_registry: { text: "NPM", color: "#f0506e" },
  prometheus_mimir: { text: "PR", color: "#ff8a00" }, sbom_attestation: { text: "SB", color: "#2dd4bf" }
};

export function CollectorGlyph({ kind, size = 26 }: { readonly kind: string; readonly size?: number }): React.JSX.Element {
  const g = COLLECTOR_GLYPH[kind] ?? { text: kind.slice(0, 2).toUpperCase(), color: "#9aa4af" };
  return <span className="cglyph" style={{ width: size, height: size, color: g.color, borderColor: g.color }} title={kind}>{g.text}</span>;
}
