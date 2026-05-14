import { line, scalePoint } from "d3";
import { useEffect, useMemo, useState } from "react";
import { EshuApiClient } from "../api/client";
import {
  loadServiceChangeSurface,
  type ChangeSurfaceImpactNode,
  type ChangeSurfaceInvestigation,
  type ChangeSurfaceNextCall
} from "../api/changeSurface";
import type { ServiceSpotlight } from "../api/serviceSpotlight";
import { loadConsoleEnvironment } from "../config/environment";

const graphWidth = 780;
const graphHeight = 360;
const graphCenterX = 390;
const graphCenterY = 180;

export function ServiceChangeSurfacePanel({
  spotlight
}: {
  readonly spotlight: ServiceSpotlight;
}): React.JSX.Element | null {
  const [investigation, setInvestigation] = useState<ChangeSurfaceInvestigation | undefined>();

  useEffect(() => {
    const environment = loadConsoleEnvironment();
    if (environment.mode !== "private") {
      return;
    }
    const client = new EshuApiClient({
      apiKey: environment.apiKey,
      baseUrl: environment.apiBaseUrl
    });
    let active = true;
    void loadServiceChangeSurface({
      client,
      repoName: spotlight.repoName,
      serviceName: spotlight.name
    })
      .then((loaded) => {
        if (active) {
          setInvestigation(loaded.empty ? undefined : loaded);
        }
      })
      .catch(() => {
        if (active) {
          setInvestigation(undefined);
        }
      });
    return () => {
      active = false;
    };
  }, [spotlight.name, spotlight.repoName]);

  if (investigation === undefined) {
    return null;
  }

  return (
    <section aria-label="Change surface" className="service-change-surface">
      <div className="service-change-surface-header">
        <div>
          <h2>Impact review</h2>
          <p>{reviewNarrative(spotlight.name, investigation)}</p>
        </div>
        <dl>
          <div>
            <dt>Total</dt>
            <dd>{investigation.impact.totalCount} total</dd>
          </div>
          <div>
            <dt>Direct</dt>
            <dd>{investigation.impact.directCount} direct</dd>
          </div>
          <div>
            <dt>Deeper</dt>
            <dd>{investigation.impact.transitiveCount} deeper</dd>
          </div>
        </dl>
      </div>
      <ResolutionState investigation={investigation} />
      <div className="service-change-surface-body">
        <ChangeSurfaceMap investigation={investigation} serviceName={spotlight.name} />
        <CodeSurfaceRail investigation={investigation} />
      </div>
    </section>
  );
}

function ResolutionState({
  investigation
}: {
  readonly investigation: ChangeSurfaceInvestigation;
}): React.JSX.Element | null {
  const status = investigation.resolution.status;
  if (status !== "no_match" && status !== "ambiguous") {
    return null;
  }
  const explanation = status === "ambiguous"
    ? `${investigation.resolution.candidates.length} possible graph targets matched. Pick a narrower target before trusting blast radius.`
    : "Eshu did not resolve this service to a graph target, so this view is showing the gap and the next safe proof calls instead of inventing an impact graph.";
  return (
    <div className="change-surface-resolution">
      <strong>{status === "ambiguous" ? "Target is ambiguous" : "Target not resolved"}</strong>
      <p>{explanation}</p>
    </div>
  );
}

function ChangeSurfaceMap({
  investigation,
  serviceName
}: {
  readonly investigation: ChangeSurfaceInvestigation;
  readonly serviceName: string;
}): React.JSX.Element {
  const nodes = useMemo(
    () => [...investigation.directImpact, ...investigation.transitiveImpact],
    [investigation.directImpact, investigation.transitiveImpact]
  );
  const [selectedID, setSelectedID] = useState(nodes[0]?.id);
  const selected = nodes.find((node) => node.id === selectedID) ?? nodes[0];
  const layout = useMemo(() => layoutImpact(nodes), [nodes]);

  return (
    <div className="change-surface-map">
      <svg
        aria-label={`${serviceName} change surface`}
        className="change-surface-map-svg"
        role="img"
        viewBox={`0 0 ${graphWidth} ${graphHeight}`}
      >
        <circle className="change-surface-ring change-surface-ring-direct" cx={graphCenterX} cy={graphCenterY} r="118" />
        <circle className="change-surface-ring change-surface-ring-transitive" cx={graphCenterX} cy={graphCenterY} r="164" />
        {layout.links.map((link) => (
          <path className="change-surface-link" d={link.path} key={link.key} />
        ))}
        <g className="change-surface-root">
          <rect height="58" rx="8" width="200" x={graphCenterX - 100} y={graphCenterY - 29} />
          <text x={graphCenterX} y={graphCenterY - labelYOffset(serviceName, 20)}>
            {labelLines(serviceName, 20).map((lineText, index) => (
              <tspan dy={index === 0 ? 0 : 15} key={`${lineText}:${index}`} x={graphCenterX}>
                {lineText}
              </tspan>
            ))}
          </text>
        </g>
        {layout.nodes.map((node) => (
          <g
            aria-label={`${node.name} ${node.kindLabel}`}
            className={`change-surface-node${selected?.id === node.id ? " change-surface-node-selected" : ""}`}
            key={node.id}
            onClick={() => setSelectedID(node.id)}
            onKeyDown={(event) => {
              if (event.key === "Enter" || event.key === " ") {
                event.preventDefault();
                setSelectedID(node.id);
              }
            }}
            role="button"
            tabIndex={0}
          >
            <title>{node.name}</title>
            <rect height={node.height} rx="8" width={node.width} x={node.x - node.width / 2} y={node.y - node.height / 2} />
            <text x={node.x} y={node.y - labelYOffset(node.name, 18)}>
              {labelLines(node.name, 18).map((lineText, index) => (
                <tspan dy={index === 0 ? 0 : 15} key={`${node.id}:${lineText}:${index}`} x={node.x}>
                  {lineText}
                </tspan>
              ))}
            </text>
          </g>
        ))}
      </svg>
      {selected !== undefined ? <ImpactNodeDetail node={selected} /> : null}
    </div>
  );
}

function CodeSurfaceRail({
  investigation
}: {
  readonly investigation: ChangeSurfaceInvestigation;
}): React.JSX.Element {
  return (
    <aside className="change-surface-rail">
      <section>
        <h3>Code surface</h3>
        <p>{codeSurfaceSentence(investigation)}</p>
        <div className="service-chip-row">
          {investigation.codeSurface.sourceBackends.slice(0, 3).map((backend) => (
            <span key={backend}>{humanLabel(backend)}</span>
          ))}
          <span>{investigation.coverage.queryShape.replace(/_/g, " ")}</span>
        </div>
      </section>
      <section>
        <h3>Files and symbols</h3>
        <div className="change-surface-list">
          {investigation.codeSurface.symbols.slice(0, 5).map((symbol) => (
            <article key={`${symbol.entityId}:${symbol.relativePath}`}>
              <strong>{symbol.name}</strong>
              <p>{symbol.relativePath}</p>
              <small>{humanLabel(symbol.type)}; {symbol.language}</small>
            </article>
          ))}
          {investigation.codeSurface.symbols.length === 0
            ? investigation.codeSurface.files.slice(0, 5).map((file) => (
              <article key={file.relativePath}>
                <strong>{file.relativePath}</strong>
                <p>{file.repoId || "Repository scoped"}</p>
              </article>
            ))
            : null}
        </div>
      </section>
      <NextCallList calls={investigation.nextCalls} />
    </aside>
  );
}

function ImpactNodeDetail({
  node
}: {
  readonly node: ChangeSurfaceImpactNode;
}): React.JSX.Element {
  return (
    <aside aria-label="Selected impact node" className="change-surface-detail">
      <div>
        <h3>{node.name}</h3>
        <p>{node.repoId || node.environment || node.id}</p>
      </div>
      <dl>
        <div>
          <dt>Depth</dt>
          <dd>Depth {node.depth}</dd>
        </div>
        <div>
          <dt>Kind</dt>
          <dd>{humanList(node.labels)}</dd>
        </div>
      </dl>
    </aside>
  );
}

function NextCallList({
  calls
}: {
  readonly calls: readonly ChangeSurfaceNextCall[];
}): React.JSX.Element | null {
  if (calls.length === 0) {
    return null;
  }
  return (
    <section>
      <h3>Next proof calls</h3>
      <div className="change-surface-list">
        {calls.slice(0, 3).map((call, index) => (
          <article key={`${call.tool}:${index}`}>
            <strong>{humanToolLabel(call.tool)}</strong>
            <p>{argumentSummary(call.args)}</p>
          </article>
        ))}
      </div>
    </section>
  );
}

interface LayoutImpactNode extends ChangeSurfaceImpactNode {
  readonly height: number;
  readonly kindLabel: string;
  readonly width: number;
  readonly x: number;
  readonly y: number;
}

function layoutImpact(nodes: readonly ChangeSurfaceImpactNode[]): {
  readonly links: readonly { readonly key: string; readonly path: string }[];
  readonly nodes: readonly LayoutImpactNode[];
} {
  const angle = scalePoint<string>()
    .domain(nodes.map((node) => node.id))
    .range([-Math.PI * 0.82, Math.PI * 0.82])
    .padding(0.35);
  const pathLine = line<[number, number]>()
    .x(([pointX]) => pointX)
    .y(([, pointY]) => pointY);
  const layoutNodes = nodes.map((node) => {
    const radius = node.depth <= 1 ? 118 : 164;
    const pointAngle = angle(node.id) ?? 0;
    return {
      ...node,
      height: nodeHeight(node.name),
      kindLabel: humanList(node.labels),
      width: nodeWidth(node.name),
      x: graphCenterX + Math.cos(pointAngle - Math.PI / 2) * radius,
      y: graphCenterY + Math.sin(pointAngle - Math.PI / 2) * radius
    };
  });
  const links = layoutNodes.map((node) => {
    return {
      key: node.id,
      path: pathLine([[graphCenterX, graphCenterY], [node.x, node.y]]) ?? ""
    };
  });
  return { links, nodes: layoutNodes };
}

function reviewNarrative(
  serviceName: string,
  investigation: ChangeSurfaceInvestigation
): string {
  const status = investigation.resolution.status.replace(/_/g, " ");
  return `${serviceName} resolved as ${status}; Eshu found ${investigation.impact.directCount} direct and ${investigation.impact.transitiveCount} deeper impact item(s) from graph and content evidence.`;
}

function codeSurfaceSentence(investigation: ChangeSurfaceInvestigation): string {
  return `${investigation.codeSurface.matchedFileCount} file(s), ${investigation.codeSurface.symbolCount} symbol(s), limit ${investigation.coverage.limit || 16}${investigation.truncated ? ", truncated" : ""}.`;
}

function argumentSummary(argumentsValue: Record<string, unknown>): string {
  return Object.entries(argumentsValue)
    .slice(0, 3)
    .map(([key, value]) => `${key}: ${String(value)}`)
    .join(", ");
}

function humanList(values: readonly string[]): string {
  return values.map(humanLabel).filter((value) => value.length > 0).join(", ");
}

function humanToolLabel(tool: string): string {
  const labels: Record<string, string> = {
    find_change_surface: "Change surface",
    get_code_relationship_story: "Code relationship story",
    investigate_code_topic: "Code topic investigation"
  };
  return labels[tool] ?? humanLabel(tool);
}

function humanLabel(value: string): string {
  return value
    .replace(/_/g, " ")
    .replace(/\b\w/g, (letter) => letter.toUpperCase())
    .replace(/\bApi\b/g, "API")
    .replace(/\bEcs\b/g, "ECS")
    .replace(/\bEks\b/g, "EKS");
}

function labelLines(label: string, maxLength: number): readonly string[] {
  const words = label.split(/\s+/).flatMap((word) => splitLongWord(word));
  const lines: string[] = [];
  let current = "";
  for (const word of words) {
    const joiner = current.endsWith("-") || current.endsWith("_") ? "" : " ";
    const candidate = current.length === 0 ? word : `${current}${joiner}${word}`;
    if (candidate.length <= maxLength) {
      current = candidate;
      continue;
    }
    if (current.length > 0) {
      lines.push(current);
    }
    current = word;
  }
  if (current.length > 0) {
    lines.push(current);
  }
  return lines.length > 0 ? lines : [label];
}

function splitLongWord(word: string): readonly string[] {
  if (word.length <= 18) {
    return [word];
  }
  return word
    .replaceAll("-", "- ")
    .replaceAll("_", "_ ")
    .split(/\s+/)
    .filter((token) => token.length > 0);
}

function labelYOffset(label: string, maxLength: number): number {
  return ((labelLines(label, maxLength).length - 1) * 15) / 2 - 5;
}

function nodeHeight(label: string): number {
  return Math.max(48, 24 + labelLines(label, 18).length * 15);
}

function nodeWidth(label: string): number {
  const longest = Math.max(...labelLines(label, 18).map((lineText) => lineText.length), 10);
  return Math.min(180, Math.max(124, longest * 7.3 + 30));
}
