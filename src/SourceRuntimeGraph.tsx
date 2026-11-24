import { ShieldCheck } from "lucide-react";

import { siteContent } from "./siteContent";
import type { HeroTrace } from "./siteContent";

interface SourceRuntimeGraphProps {
  readonly selectedTrace: HeroTrace;
  readonly onSelectTrace: (trace: HeroTrace) => void;
}

interface GraphNodeLayout {
  readonly id: string;
  readonly x: number;
  readonly y: number;
  readonly kind: string;
}

interface GraphEdgeLayout {
  readonly id: string;
  readonly from: string;
  readonly to: string;
  readonly label: string;
}

const nodeLayouts: readonly GraphNodeLayout[] = [
  { id: "code", x: 18, y: 24, kind: "CODE" },
  { id: "iac", x: 224, y: 24, kind: "IAC" },
  { id: "runtime", x: 430, y: 24, kind: "RUNTIME" },
  { id: "supply-chain", x: 18, y: 156, kind: "SUPPLY" },
  { id: "cloud", x: 224, y: 156, kind: "CLOUD" },
  { id: "incidents", x: 430, y: 156, kind: "OPS & DOCS" },
];

const nodeCenters: Record<string, { readonly cx: number; readonly cy: number }> = {
  code: { cx: 104, cy: 56 },
  iac: { cx: 310, cy: 56 },
  runtime: { cx: 516, cy: 56 },
  "supply-chain": { cx: 104, cy: 188 },
  cloud: { cx: 310, cy: 188 },
  incidents: { cx: 516, cy: 188 },
};

const graphEdges: readonly GraphEdgeLayout[] = [
  { id: "code-iac", from: "code", to: "iac", label: "DEPLOYS" },
  { id: "code-supply", from: "code", to: "supply-chain", label: "LOCKFILE" },
  { id: "supply-iac", from: "supply-chain", to: "iac", label: "DIGEST" },
  { id: "iac-runtime", from: "iac", to: "runtime", label: "RUNS" },
  { id: "iac-cloud", from: "iac", to: "cloud", label: "PROVISIONS" },
  { id: "code-cloud", from: "code", to: "cloud", label: "SINK" },
  { id: "cloud-runtime", from: "cloud", to: "runtime", label: "EXPOSES" },
  { id: "runtime-incidents", from: "runtime", to: "incidents", label: "PAGES" },
  { id: "supply-runtime", from: "supply-chain", to: "runtime", label: "SBOM" },
];

/** True 2D interactive evidence-graph topology in the hero. */
export function SourceRuntimeGraph({
  selectedTrace,
  onSelectTrace,
}: SourceRuntimeGraphProps): React.JSX.Element {
  const activeSet = new Set(selectedTrace.activeNodeIds);
  const nodesById = Object.fromEntries(siteContent.demoTrace.nodes.map((node) => [node.id, node]));

  return (
    <aside className="product-visual" aria-label="Source to runtime graph">
      <div className="visual-header">
        <div className="visual-title-group">
          <span className="visual-dot" aria-hidden="true" />
          <span className="visual-eyebrow">{selectedTrace.sourceScope}</span>
        </div>
        <span className="visual-truth-pill">
          <ShieldCheck aria-hidden="true" />
          {selectedTrace.truthLabel}
        </span>
      </div>

      <div className="hero-trace-tabs" role="tablist" aria-label="Trace workflows">
        {siteContent.heroTraces.map((trace) => {
          const isSelected = trace.id === selectedTrace.id;
          return (
            <button
              key={trace.id}
              type="button"
              role="tab"
              aria-selected={isSelected}
              className={isSelected ? "trace-tab is-active" : "trace-tab"}
              onClick={() => onSelectTrace(trace)}
            >
              {trace.label}
            </button>
          );
        })}
      </div>

      <div className="visual-canvas">
        <svg
          className="graph-svg"
          viewBox="0 0 620 244"
          role="img"
          aria-label="Interactive evidence graph showing linked code, supply chain, IaC, cloud, runtime, and operations nodes"
        >
          <defs>
            <marker
              id="arrow-idle"
              viewBox="0 0 10 10"
              refX="8"
              refY="5"
              markerWidth="5"
              markerHeight="5"
              orient="auto-start-reverse"
            >
              <path d="M 0 1.5 L 10 5 L 0 8.5 z" fill="rgba(243,235,221,0.22)" />
            </marker>
            <marker
              id="arrow-active"
              viewBox="0 0 10 10"
              refX="8"
              refY="5"
              markerWidth="6"
              markerHeight="6"
              orient="auto-start-reverse"
            >
              <path d="M 0 1.5 L 10 5 L 0 8.5 z" fill="#14b8a6" />
            </marker>
          </defs>

          {graphEdges.map((edge) => {
            const start = nodeCenters[edge.from];
            const end = nodeCenters[edge.to];
            if (!start || !end) {
              return null;
            }
            const isActive = activeSet.has(edge.from) && activeSet.has(edge.to);
            const midX = (start.cx + end.cx) / 2;
            const midY = (start.cy + end.cy) / 2;
            return (
              <g key={edge.id}>
                <line
                  x1={start.cx}
                  y1={start.cy}
                  x2={end.cx}
                  y2={end.cy}
                  className={isActive ? "edge-line is-active" : "edge-line"}
                  markerEnd={isActive ? "url(#arrow-active)" : "url(#arrow-idle)"}
                />
                {isActive ? (
                  <g transform={`translate(${midX}, ${midY})`}>
                    <rect x="-32" y="-9" width="64" height="18" rx="9" className="edge-pill-bg" />
                    <text x="0" y="3.5" textAnchor="middle" className="edge-pill-text">
                      {edge.label}
                    </text>
                  </g>
                ) : null}
              </g>
            );
          })}

          {nodeLayouts.map((layout) => {
            const node = nodesById[layout.id];
            if (!node) {
              return null;
            }
            const isActive = activeSet.has(layout.id);
            return (
              <g
                key={layout.id}
                transform={`translate(${layout.x}, ${layout.y})`}
                className={
                  isActive ? `svg-node node-${layout.id} is-active` : `svg-node node-${layout.id}`
                }
              >
                <rect width="172" height="64" rx="10" className="svg-node-box" />
                <circle cx="16" cy="18" r="4" className="svg-node-dot" />
                <text x="27" y="21.5" className="svg-node-kind">
                  {layout.kind}
                </text>
                <text x="14" y="40" className="svg-node-title">
                  {node.label}
                </text>
                <text x="14" y="54" className="svg-node-detail">
                  {node.detail}
                </text>
              </g>
            );
          })}
        </svg>
      </div>

      <div className="visual-evidence">
        <div className="visual-evidence-head">
          <code className="visual-command">{selectedTrace.command}</code>
          <span className="visual-freshness">freshness: {selectedTrace.freshness}</span>
        </div>
        <p className="visual-question">&ldquo;{selectedTrace.question}&rdquo;</p>
        <ul className="visual-evidence-list">
          {selectedTrace.evidence.map((line) => (
            <li key={line}>
              <span className="evidence-marker" aria-hidden="true">
                ▸
              </span>
              <code>{line}</code>
            </li>
          ))}
        </ul>
      </div>
    </aside>
  );
}
