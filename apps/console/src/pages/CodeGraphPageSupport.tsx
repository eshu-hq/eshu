import { Link } from "react-router-dom";

import type { CodeImportCycleRow } from "../api/codeImports";
import type { FindingRow, GraphModel, GraphNode } from "../console/types";

export interface ImportCycleState {
  readonly status: "idle" | "loading" | "ready" | "error";
  readonly cycles: readonly CodeImportCycleRow[];
  readonly error: string;
  readonly truncated: boolean;
  readonly nextOffset: number | null;
}

export const emptyImportCycleState: ImportCycleState = {
  status: "idle",
  cycles: [],
  error: "",
  truncated: false,
  nextOffset: null,
};

export function codeGraphSelectionKey(repoId: string, entityId: string): string {
  return JSON.stringify([repoId, entityId]);
}

export function ImportCyclesPanel({
  state,
}: {
  readonly state: ImportCycleState;
}): React.JSX.Element {
  const label = state.cycles.length ? `Import cycles · ${state.cycles.length}` : "Import cycles";
  return (
    <>
      <div className="section-label" style={{ marginTop: 16 }}>
        {label}
      </div>
      {state.status === "loading" ? (
        <p className="t-mut" style={{ fontSize: ".8rem", margin: 0 }}>
          Loading source-backed import cycles...
        </p>
      ) : null}
      {state.status === "error" ? (
        <p className="src-err" style={{ margin: 0 }}>
          Import cycle analysis unavailable: {state.error}
        </p>
      ) : null}
      {state.status === "ready" && state.cycles.length === 0 ? (
        <p className="t-mut" style={{ fontSize: ".8rem", margin: 0 }}>
          No source-backed import cycles returned for this repository.
        </p>
      ) : null}
      {state.cycles.length ? (
        <div className="conn-list">
          {state.cycles.map((cycle, index) => {
            const href = cycleSourceHref(cycle);
            return (
              <div className="dead-row" key={`${cycle.sourceFile}:${cycle.targetFile}:${index}`}>
                <span className="mono">{cyclePathLabel(cycle)}</span>
                <span className="t-mut">
                  {cycle.relationshipType} · {cycleRepositoryLabel(cycle)}
                </span>
                {href ? (
                  <Link className="mono" to={href}>
                    {cycleSourceLabel(cycle)}
                  </Link>
                ) : (
                  <span className="mono">{cycleSourceLabel(cycle)}</span>
                )}
              </div>
            );
          })}
        </div>
      ) : null}
      {state.truncated ? (
        <p className="t-mut" style={{ fontSize: ".78rem", margin: "6px 0 0" }}>
          More import cycles are available
          {state.nextOffset !== null ? ` at offset ${state.nextOffset}` : ""}.
        </p>
      ) : null}
    </>
  );
}

export function importCycleRepoScope(finding: FindingRow): string {
  return finding.repoId?.trim() || finding.entity.trim();
}

export function withDeadSiblings(
  graph: GraphModel,
  selected: FindingRow,
  candidates: readonly FindingRow[],
): GraphModel {
  const nodes = [...graph.nodes];
  const existing = new Set(nodes.map((node) => node.id));
  for (const finding of candidates.filter((row) => sameRepositoryScope(row, selected))) {
    const id = `dead:${finding.id}`;
    if (!existing.has(id)) {
      existing.add(id);
      nodes.push({
        id,
        label: symbolFromFinding(finding),
        kind: "vuln",
        sub: finding.filePath ?? finding.detail,
        col: 3,
        truth: "derived",
      });
    }
  }
  return { nodes, edges: graph.edges };
}

export function sameRepositoryScope(left: FindingRow, right: FindingRow): boolean {
  return repositoryScopeKey(left) === repositoryScopeKey(right);
}

export function deadOnlyGraph(
  selected: FindingRow | undefined,
  candidates: readonly FindingRow[],
): GraphModel {
  if (!selected) return { nodes: [], edges: [] };
  return withDeadSiblings(
    {
      nodes: [
        {
          id: selected.entityId ?? selected.id,
          label: symbolFromFinding(selected),
          kind: "client",
          sub: selected.filePath ?? selected.detail,
          col: 1,
          hero: true,
          truth: "derived",
        },
      ],
      edges: [],
    },
    selected,
    candidates,
  );
}

export function hotspotRows(
  graph: GraphModel,
): readonly { readonly id: string; readonly label: string; readonly count: number }[] {
  const inbound = new Map<string, number>();
  for (const edge of graph.edges) inbound.set(edge.t, (inbound.get(edge.t) ?? 0) + 1);
  return [...inbound.entries()]
    .map(([id, count]) => ({
      id,
      count,
      label: graph.nodes.find((node) => node.id === id)?.label ?? id,
    }))
    .sort((a, b) => b.count - a.count || a.label.localeCompare(b.label))
    .slice(0, 5);
}

export function findingForNode(
  node: GraphNode | undefined,
  candidates: readonly FindingRow[],
): FindingRow | undefined {
  if (!node) return undefined;
  const deadId = node.id.startsWith("dead:") ? node.id.slice("dead:".length) : "";
  return candidates.find((finding) => finding.id === deadId || finding.entityId === node.id);
}

export function symbolFromFinding(finding: FindingRow): string {
  return finding.title.replace(/^Unreferenced symbol\s+/i, "").trim() || finding.title;
}

export function candidateIdFromParam(
  candidates: readonly FindingRow[],
  param: string,
): string | undefined {
  const value = param.trim().toLowerCase();
  if (value === "") return undefined;
  return candidates.find(
    (finding) =>
      finding.id.toLowerCase() === value ||
      finding.entityId?.toLowerCase() === value ||
      symbolFromFinding(finding).toLowerCase() === value ||
      finding.filePath?.toLowerCase() === value,
  )?.id;
}

export function locationFromFinding(finding: FindingRow | undefined): string {
  if (!finding) return "source path unavailable";
  const path = finding.filePath ?? finding.detail.split(" · ")[0] ?? "source path unavailable";
  if (finding.startLine !== undefined && finding.endLine !== undefined) {
    return `${path}:${finding.startLine}-${finding.endLine}`;
  }
  if (finding.startLine !== undefined) return `${path}:${finding.startLine}`;
  return path;
}

export function locationFromNode(node: GraphNode | undefined): string {
  const source = node?.source;
  if (!source) return "source path unavailable";
  if (source.startLine !== undefined && source.endLine !== undefined) {
    return `${source.filePath}:${source.startLine}-${source.endLine}`;
  }
  if (source.startLine !== undefined) return `${source.filePath}:${source.startLine}`;
  return source.filePath;
}

export function sourceHref(finding: FindingRow): string | null {
  if (!finding.filePath) return null;
  const repository = finding.repoId ?? finding.entity;
  if (!repository) return null;
  const params = new URLSearchParams({ path: finding.filePath });
  if (finding.startLine !== undefined) params.set("lineStart", String(finding.startLine));
  if (finding.endLine !== undefined) params.set("lineEnd", String(finding.endLine));
  return `/repositories/${encodeURIComponent(repository)}/source?${params.toString()}`;
}

export function sourceHrefFromNode(node: GraphNode | undefined): string | null {
  const source = node?.source;
  if (!source) return null;
  const params = new URLSearchParams({ path: source.filePath });
  if (source.startLine !== undefined) params.set("lineStart", String(source.startLine));
  if (source.endLine !== undefined) params.set("lineEnd", String(source.endLine));
  return `/repositories/${encodeURIComponent(source.repoId)}/source?${params.toString()}`;
}

export function explorerQueryFor(
  node: GraphNode | undefined,
  finding: FindingRow | undefined,
  repositoryLabel: string,
): string {
  const label = repositoryLabel.trim();
  if (label && label !== "unknown" && label !== "unresolved repository") return label;
  if (finding?.repoId) return finding.repoId;
  if (node?.source?.repoId) return node.source.repoId;
  return label === "unknown" && node ? node.label : label;
}

export function sourceMetadataStatus(
  node: GraphNode | undefined,
  finding: FindingRow | undefined,
  href: string | null,
): string {
  if (!node || href) return "";
  if (finding?.type === "Dead code") {
    return "Dead-code scan did not return repository/file metadata.";
  }
  if (finding) return "Structural inventory did not return repository/file metadata.";
  return "Related symbol source metadata unavailable from POST /api/v0/code/relationships/story.";
}

function cyclePathLabel(cycle: CodeImportCycleRow): string {
  if (cycle.cyclePath.length > 0) return cycle.cyclePath.join(" → ");
  return [cycle.sourceFile, cycle.targetFile, cycle.sourceFile]
    .filter((part) => part !== "")
    .join(" → ");
}

function cycleRepositoryLabel(cycle: CodeImportCycleRow): string {
  return cycle.repoName || cycle.repoId || "repository";
}

function cycleSourceLabel(cycle: CodeImportCycleRow): string {
  if (!cycle.sourceFile) return "source path unavailable";
  if (cycle.sourceLineNumber !== undefined) return `${cycle.sourceFile}:${cycle.sourceLineNumber}`;
  return cycle.sourceFile;
}

function cycleSourceHref(cycle: CodeImportCycleRow): string | null {
  if (!cycle.repoId || !cycle.sourceFile) return null;
  const params = new URLSearchParams({ path: cycle.sourceFile });
  if (cycle.sourceLineNumber !== undefined) params.set("lineStart", String(cycle.sourceLineNumber));
  return `/repositories/${encodeURIComponent(cycle.repoId)}/source?${params.toString()}`;
}

function repositoryScopeKey(finding: FindingRow): string {
  return finding.repoId?.trim() || finding.entity.trim() || finding.id;
}
