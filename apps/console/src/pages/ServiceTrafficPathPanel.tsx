import { useState } from "react";
import type { ServiceTrafficPath } from "../api/serviceTrafficPath";

const pathNodes = ["hostname", "edge", "origin", "runtime", "workload", "sourceRepo"] as const;

export function ServiceTrafficPathPanel({
  paths,
  serviceName
}: {
  readonly paths: readonly ServiceTrafficPath[] | undefined;
  readonly serviceName: string;
}): React.JSX.Element | null {
  const [selectedIndex, setSelectedIndex] = useState(0);

  if (paths === undefined || paths.length === 0) {
    return null;
  }
  const selected = paths[selectedIndex] ?? paths[0];

  return (
    <section aria-label="Traffic path" className="service-panel service-traffic-panel">
      <div className="service-panel-heading">
        <h3>Traffic path</h3>
        <span>{trafficSentence(selected)}</span>
      </div>
      <div className="service-traffic-layout">
        <svg
          aria-label={`${serviceName} traffic path`}
          className="service-traffic-svg"
          role="img"
          viewBox="0 0 920 210"
        >
          {pathNodes.slice(0, -1).map((node, index) => (
            <path
              className="service-traffic-link"
              d={`M ${nodeX(index) + 98} 106 L ${nodeX(index + 1) - 18} 106`}
              key={`${node}:link`}
            />
          ))}
          {pathNodes.map((node, index) => (
            <g className="service-traffic-node" key={node}>
              <title>{nodeValue(selected, node)}</title>
              <rect height="58" rx="8" width="116" x={nodeX(index) - 58} y="77" />
              <text x={nodeX(index)} y="101">
                <tspan x={nodeX(index)}>{nodeLabel(node)}</tspan>
                {splitTrafficLabel(nodeValue(selected, node)).map((line, lineIndex) => (
                  <tspan
                    className="service-traffic-node-value"
                    dy={lineIndex === 0 ? "16" : "11"}
                    key={`${node}:${line}`}
                    x={nodeX(index)}
                  >
                    {line}
                  </tspan>
                ))}
              </text>
            </g>
          ))}
        </svg>
        <aside aria-label="Selected traffic evidence" className="service-traffic-detail">
          <h4>{selected.edge}</h4>
          <p>{trafficSentence(selected)}</p>
          <dl>
            <div>
              <dt>Proof</dt>
              <dd>{humanLabel(selected.evidenceKind)}</dd>
            </div>
            <div>
              <dt>Source</dt>
              <dd>{selected.sourceRepo}</dd>
            </div>
          </dl>
          {paths.length > 1 ? (
            <div className="service-traffic-options">
              {paths.map((path, index) => (
                <button
                  aria-pressed={selectedIndex === index}
                  key={`${path.hostname}:${path.edge}:${index}`}
                  onClick={() => setSelectedIndex(index)}
                  type="button"
                >
                  {path.hostname}
                </button>
              ))}
            </div>
          ) : null}
        </aside>
      </div>
    </section>
  );
}

function nodeX(index: number): number {
  return 80 + index * 152;
}

function nodeLabel(node: (typeof pathNodes)[number]): string {
  const labels: Record<(typeof pathNodes)[number], string> = {
    edge: "Edge",
    hostname: "Hostname",
    origin: "Origin",
    runtime: "Runtime",
    sourceRepo: "Source",
    workload: "Workload"
  };
  return labels[node];
}

function nodeValue(path: ServiceTrafficPath, node: (typeof pathNodes)[number]): string {
  return path[node];
}

function trafficSentence(path: ServiceTrafficPath): string {
  return `${path.hostname} reaches ${path.workload} through ${path.edge}, ${path.origin}, and ${path.runtime}.`;
}

function splitTrafficLabel(value: string): readonly string[] {
  const normalized = value.trim();
  if (normalized.length <= 18) {
    return [normalized];
  }

  const parts = normalized.split(/[-./:_\s]+/).filter(Boolean);
  if (parts.length <= 1) {
    return [normalized.slice(0, 17), normalized.slice(17, 34)];
  }

  const lines: string[] = [];
  let current = "";
  for (const part of parts) {
    const next = current === "" ? part : `${current}-${part}`;
    if (next.length > 18 && current !== "") {
      lines.push(current);
      current = part;
    } else {
      current = next;
    }
  }
  if (current !== "") {
    lines.push(current);
  }

  return lines.slice(0, 2);
}

function humanLabel(value: string): string {
  return value
    .replace(/_/g, " ")
    .replace(/\b\w/g, (letter) => letter.toUpperCase())
    .replace(/\bAws\b/g, "AWS")
    .replace(/\bCdn\b/g, "CDN");
}
