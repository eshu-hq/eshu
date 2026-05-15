import { useState } from "react";
import { EshuApiClient } from "../api/client";
import type { EntityResolutionResult, EntityResolutionCandidate } from "../api/entityResolution";
import { resolveEntity } from "../api/entityResolution";
import type { ServiceTrafficPath } from "../api/serviceTrafficPath";
import { loadConsoleEnvironment } from "../config/environment";

const pathNodes = ["hostname", "edge", "origin", "runtime", "workload", "sourceRepo"] as const;
type PathNode = (typeof pathNodes)[number];

type ResolutionState =
  | { readonly state: "idle" }
  | { readonly node: PathNode; readonly state: "loading"; readonly value: string }
  | { readonly message: string; readonly node: PathNode; readonly state: "error"; readonly value: string }
  | {
    readonly node: PathNode;
    readonly result: EntityResolutionResult;
    readonly state: "resolved";
    readonly value: string;
  };

export function ServiceTrafficPathPanel({
  paths,
  serviceName
}: {
  readonly paths: readonly ServiceTrafficPath[] | undefined;
  readonly serviceName: string;
}): React.JSX.Element | null {
  const [selectedIndex, setSelectedIndex] = useState(0);
  const [resolution, setResolution] = useState<ResolutionState>({ state: "idle" });

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
            <g
              aria-label={`Resolve ${nodeLabel(node)} ${nodeValue(selected, node)}`}
              className="service-traffic-node"
              key={node}
              onClick={() => {
                void resolveTrafficNode(node, selected, setResolution);
              }}
              onKeyDown={(event) => {
                if (event.key === "Enter" || event.key === " ") {
                  event.preventDefault();
                  void resolveTrafficNode(node, selected, setResolution);
                }
              }}
              role="button"
              tabIndex={0}
            >
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
          {resolution.state === "idle" ? (
            <TrafficEvidenceDetail
              paths={paths}
              selected={selected}
              selectedIndex={selectedIndex}
              selectPath={(index) => {
                setSelectedIndex(index);
                setResolution({ state: "idle" });
              }}
            />
          ) : (
            <TrafficResolutionDetail resolution={resolution} />
          )}
        </aside>
      </div>
    </section>
  );
}

function TrafficEvidenceDetail({
  paths,
  selectPath,
  selected,
  selectedIndex
}: {
  readonly paths: readonly ServiceTrafficPath[];
  readonly selectPath: (index: number) => void;
  readonly selected: ServiceTrafficPath;
  readonly selectedIndex: number;
}): React.JSX.Element {
  return (
    <>
      <h4>{selected.edge}</h4>
      <p>{trafficSentence(selected)}</p>
      <dl>
        <div>
          <dt>Proof</dt>
          <dd>{humanLabel(selected.evidenceKind)}</dd>
        </div>
        <div>
          <dt>Environment</dt>
          <dd>{selected.environment}</dd>
        </div>
        <div>
          <dt>Visibility</dt>
          <dd>{selected.visibility}</dd>
        </div>
        <div>
          <dt>Source</dt>
          <dd>{selected.sourceRepo}</dd>
        </div>
      </dl>
      <p className="service-traffic-reason">{selected.reason}</p>
      {paths.length > 1 ? (
        <div className="service-traffic-options">
          {paths.map((path, index) => (
            <button
              aria-pressed={selectedIndex === index}
              key={`${path.hostname}:${path.edge}:${index}`}
              onClick={() => selectPath(index)}
              type="button"
            >
              {path.hostname}
            </button>
          ))}
        </div>
      ) : null}
    </>
  );
}

function TrafficResolutionDetail({
  resolution
}: {
  readonly resolution: Exclude<ResolutionState, { readonly state: "idle" }>;
}): React.JSX.Element {
  if (resolution.state === "loading") {
    return (
      <>
        <h4>Resolve selected node</h4>
        <p>Looking up {resolution.value} in Eshu.</p>
      </>
    );
  }
  if (resolution.state === "error") {
    return (
      <>
        <h4>Resolve selected node</h4>
        <p>{resolution.message}</p>
      </>
    );
  }

  return (
    <>
      <h4>Resolve selected node</h4>
      <p>
        {resolution.value} is raw evidence. Pick the canonical match before
        opening a story.
      </p>
      <dl>
        <div>
          <dt>Node</dt>
          <dd>{nodeLabel(resolution.node)}</dd>
        </div>
        <div>
          <dt>Matches</dt>
          <dd>
            Showing {resolution.result.candidates.length} of {resolution.result.limit} candidates
          </dd>
        </div>
      </dl>
      {resolution.result.truncated ? (
        <p className="service-traffic-warning">More matches available</p>
      ) : null}
      <div className="service-traffic-candidates">
        {resolution.result.candidates.map((candidate) => (
          <TrafficCandidateRow candidate={candidate} key={candidate.id} />
        ))}
      </div>
    </>
  );
}

function TrafficCandidateRow({
  candidate
}: {
  readonly candidate: EntityResolutionCandidate;
}): React.JSX.Element {
  const route = workspaceRoute(candidate);
  const actionLabel = route === undefined
    ? `Select ${candidate.id}`
    : `Open ${route.label}`;
  return (
    <div className="service-traffic-candidate">
      <strong>{candidate.name}</strong>
      <span>{candidate.type}</span>
      {candidate.repoName.length > 0 || candidate.repoId.length > 0 ? (
        <span>{candidate.repoName || candidate.repoId}</span>
      ) : null}
      {candidate.filePath.length > 0 ? <span>{candidate.filePath}</span> : null}
      {route === undefined ? (
        <button disabled type="button">{actionLabel}</button>
      ) : (
        <a href={route.href}>{actionLabel}</a>
      )}
    </div>
  );
}

function nodeX(index: number): number {
  return 80 + index * 152;
}

function nodeLabel(node: PathNode): string {
  const labels: Record<PathNode, string> = {
    edge: "Edge",
    hostname: "Hostname",
    origin: "Origin",
    runtime: "Runtime",
    sourceRepo: "Source",
    workload: "Workload"
  };
  return labels[node];
}

function nodeValue(path: ServiceTrafficPath, node: PathNode): string {
  return path[node];
}

async function resolveTrafficNode(
  node: PathNode,
  path: ServiceTrafficPath,
  setResolution: (state: ResolutionState) => void
): Promise<void> {
  const value = nodeValue(path, node);
  setResolution({ node, state: "loading", value });
  const environment = loadConsoleEnvironment();
  if (environment.mode !== "private") {
    setResolution({
      message: "Switch to private API mode to resolve live graph entities.",
      node,
      state: "error",
      value
    });
    return;
  }

  try {
    const result = await resolveEntity({
      client: new EshuApiClient({
        apiKey: environment.apiKey,
        baseUrl: environment.apiBaseUrl
      }),
      limit: 10,
      name: value,
      type: resolverType(node, path)
    });
    setResolution({ node, result, state: "resolved", value });
  } catch (error) {
    setResolution({
      message: error instanceof Error ? error.message : "Entity resolution failed",
      node,
      state: "error",
      value
    });
  }
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

function resolverType(node: PathNode, path: ServiceTrafficPath): string | undefined {
  if (node === "sourceRepo") {
    return "repository";
  }
  if (node === "origin" && path.evidenceKind.includes("cloudfront")) {
    return "terraform_block";
  }
  if (node === "edge" && path.evidenceKind.includes("apigateway")) {
    return "terraform_block";
  }
  return undefined;
}

interface WorkspaceRoute {
  readonly href: string;
  readonly label: string;
}

function workspaceRoute(candidate: EntityResolutionCandidate): WorkspaceRoute | undefined {
  if (candidate.type === "Repository" || candidate.labels.includes("Repository")) {
    return {
      href: `/workspace/repositories/${encodeURIComponent(candidate.id)}`,
      label: candidate.name
    };
  }
  if (candidate.type === "Workload" || candidate.labels.includes("Workload")) {
    return {
      href: `/workspace/services/${encodeURIComponent(candidate.id)}`,
      label: candidate.name
    };
  }
  if (candidate.repoId.length > 0) {
    return {
      href: `/workspace/repositories/${encodeURIComponent(candidate.repoId)}`,
      label: candidate.repoName || candidate.repoId
    };
  }
  return undefined;
}
