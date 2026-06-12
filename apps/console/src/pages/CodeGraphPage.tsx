import { useEffect, useMemo, useState } from "react";
import { Link, useSearchParams } from "react-router-dom";
import type { EshuApiClient } from "../api/client";
import { loadDeadCodePage } from "../api/deadCode";
import { EshuEnvelopeError } from "../api/envelope";
import { codeRelationshipsToGraph } from "../api/eshuGraph";
import type { CodeRelationshipsResponse } from "../api/eshuGraph";
import { loadRepositoryNameMap } from "../api/repoCatalog";
import type { ConsoleModel, FindingRow, GraphModel, GraphNode } from "../console/types";
import { fmt } from "../console/types";
import { GraphCanvas } from "../components/GraphCanvas";
import { Badge, Panel, StatTile } from "../components/atoms";

export function CodeGraphPage({ model, client }: {
  readonly model: ConsoleModel;
  readonly client?: EshuApiClient;
}): React.JSX.Element {
  const snapshotCandidates = useMemo(() => model.findings.filter((finding) => finding.type === "Dead code"), [model.findings]);
  const [liveCandidates, setLiveCandidates] = useState<readonly FindingRow[] | null>(null);
  const candidates = liveCandidates ?? snapshotCandidates;
  const [searchParams] = useSearchParams();
  const candidateParam = searchParams.get("candidate") ?? searchParams.get("q") ?? "";
  const [selectedId, setSelectedId] = useState(candidateIdFromParam(candidates, candidateParam) ?? candidates[0]?.id ?? "");
  const selected = candidates.find((finding) => finding.id === selectedId) ?? candidates[0];
  const [graph, setGraph] = useState<GraphModel>({ nodes: [], edges: [] });
  const [focusedNodeId, setFocusedNodeId] = useState<string | undefined>(selected?.entityId);
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState("");
  const [candidateErr, setCandidateErr] = useState("");

  useEffect(() => {
    let cancelled = false;
    if (!client || model.source !== "live") {
      setLiveCandidates(null);
      setCandidateErr("");
      return () => { cancelled = true; };
    }
    setCandidateErr("");
    void loadCodeGraphCandidates(client)
      .then((rows) => {
        if (!cancelled) setLiveCandidates(rows);
      })
      .catch((error) => {
        if (!cancelled) {
          setLiveCandidates(null);
          setCandidateErr(error instanceof Error ? error.message : "failed to load dead-code candidates");
        }
      });
    return () => { cancelled = true; };
  }, [client, model.source]);

  useEffect(() => {
    const nextId = candidateIdFromParam(candidates, candidateParam);
    if (nextId && nextId !== selectedId) {
      const next = candidates.find((finding) => finding.id === nextId);
      setSelectedId(nextId);
      setFocusedNodeId(next?.entityId ?? nextId);
      return;
    }
    if (!selectedId && candidates[0]) {
      setSelectedId(candidates[0].id);
      setFocusedNodeId(candidates[0].entityId ?? candidates[0].id);
    }
  }, [candidateParam, candidates, selectedId]);

  useEffect(() => {
    let cancelled = false;
    if (!client || !selected?.entityId) {
      setGraph(deadOnlyGraph(selected, candidates));
      setFocusedNodeId(selected?.entityId ?? selected?.id);
      return () => { cancelled = true; };
    }
    setBusy(true);
    setErr("");
    void client.post<CodeRelationshipsResponse>("/api/v0/code/relationships", { entity_id: selected.entityId, max_depth: 1 })
      .then((env) => {
        if (cancelled) return;
        if (env.error) throw new EshuEnvelopeError(env.error);
        const loaded = codeRelationshipsToGraph(env.data ?? {}, {
          id: selected.entityId ?? selected.id,
          name: symbolFromFinding(selected)
        });
        setGraph(withDeadSiblings(loaded, selected, candidates));
        setFocusedNodeId((current) => current ?? selected.entityId ?? selected.id);
      })
      .catch((error) => {
        if (!cancelled) {
          setErr(error instanceof Error ? error.message : "failed to load code graph");
          setGraph(deadOnlyGraph(selected, candidates));
        }
      })
      .finally(() => { if (!cancelled) setBusy(false); });
    return () => { cancelled = true; };
  }, [client, selected, candidates]);

  const deadInRepo = candidates.filter((finding) => selected && finding.entity === selected.entity);
  const importEdges = graph.edges.filter((edge) => edge.verb === "IMPORTS").length;
  const callEdges = graph.edges.filter((edge) => edge.verb === "CALLS").length;
  const hotspots = hotspotRows(graph);
  const focusedNode = graph.nodes.find((node) => node.id === focusedNodeId) ?? graph.nodes.find((node) => node.id === selected?.entityId) ?? graph.nodes[0];
  const focusedFinding = findingForNode(focusedNode, candidates);
  const focusedDegree = focusedNode ? graph.edges.filter((edge) => edge.s === focusedNode.id || edge.t === focusedNode.id).length : 0;
  const focusedSourceHref = focusedFinding ? sourceHref(focusedFinding) : sourceHrefFromNode(focusedNode);
  const focusedRepository = focusedFinding?.entity ?? focusedNode?.source?.repoName ?? focusedNode?.source?.repoId ?? selected?.entity ?? "unknown";
  const focusedLocation = focusedFinding ? locationFromFinding(focusedFinding) : locationFromNode(focusedNode);
  const focusedSourceStatus = sourceMetadataStatus(focusedNode, focusedFinding, focusedSourceHref);

  function selectGraphNode(node: GraphNode): void {
    setFocusedNodeId(node.id);
    const finding = findingForNode(node, candidates);
    if (finding) setSelectedId(finding.id);
  }

  function selectCandidate(id: string): void {
    setSelectedId(id);
    const next = candidates.find((finding) => finding.id === id);
    setFocusedNodeId(next?.entityId ?? id);
  }

  return (
    <div className="page" style={{ maxWidth: "none" }}>
      <div className="page-intro row" style={{ justifyContent: "space-between", alignItems: "flex-end", flexWrap: "wrap", gap: 12 }}>
        <div>
          <h2>Code graph</h2>
          <p>Symbol and module relationships at code grain from <span className="mono">POST /api/v0/code/relationships</span>. Dead-code candidates from the same repository are shown as orphan analyzer nodes.</p>
        </div>
        <select className="code-repo-select mono" value={selected?.id ?? ""} onChange={(event) => selectCandidate(event.target.value)}>
          {candidates.map((finding) => <option key={finding.id} value={finding.id}>{symbolFromFinding(finding)} · {finding.entity}</option>)}
        </select>
      </div>

      <div className="grid g-4">
        <StatTile label="Modules" value={graph.nodes.filter((node) => !node.id.startsWith("dead:")).length} color="var(--teal)" sub={selected?.entity ?? "no repository"} />
        <StatTile label="Import edges" value={importEdges} color="var(--blue)" sub="module graph" />
        <StatTile label="Call edges" value={callEdges} color="var(--ember)" sub="function call graph" />
        <StatTile label="Dead symbols" value={deadInRepo.length} color="var(--crit)" sub={deadInRepo.length ? "orphaned" : "none in repo"} />
      </div>

      <div className="explorer-layout mt">
        <div className="gcanvas-shell">
          {busy ? (
            <div className="conn-state compact"><div className="conn-spinner" aria-hidden /><p>Loading code graph...</p></div>
          ) : (
            <GraphCanvas graph={graph} layout="layered" height={560} selectedId={focusedNode?.id ?? selected?.entityId} onSelect={selectGraphNode} />
          )}
          {err ? <p className="src-err">{err}</p> : null}
          {candidateErr ? <p className="src-err">Failed to load live dead-code candidates: {candidateErr}</p> : null}
          <div className="t-mut" style={{ fontSize: ".74rem", marginTop: 8 }}>
            {selected ? `${symbolFromFinding(selected)} · ${selected.language ?? "code"} · ${selected.filePath ?? "source path unavailable"}` : "No dead-code entity selected."}
          </div>
        </div>
        <Panel title="Analyzer">
          <div className="section-label">Selected symbol</div>
          {focusedNode ? (
            <div className="selected-code-node">
              <div className="row" style={{ justifyContent: "space-between", gap: 8, alignItems: "center" }}>
                <strong className="mono">{focusedNode.label}</strong>
                <Badge tone={focusedFinding ? "crit" : "neutral"}>{focusedFinding?.classification ?? focusedNode.kind}</Badge>
              </div>
              <div className="kv-list" style={{ marginTop: 10 }}>
                <div className="kv"><span>Repository</span><strong>{focusedRepository}</strong></div>
                <div className="kv">
                  <span>Location</span>
                  {focusedSourceHref ? (
                    <Link className="mono" to={focusedSourceHref}>{focusedLocation}</Link>
                  ) : (
                    <strong className="mono">{focusedLocation}</strong>
                  )}
                </div>
                <div className="kv"><span>Graph degree</span><strong>{focusedDegree}</strong></div>
                <div className="kv"><span>Evidence</span><strong>{focusedFinding?.truth ?? focusedNode.truth ?? "derived"}</strong></div>
              </div>
              <div className="row" style={{ gap: 8, flexWrap: "wrap", marginTop: 12 }}>
                {focusedSourceHref ? <Link className="btn-ghost active" to={focusedSourceHref}>Open source</Link> : null}
                <Link className="btn-ghost" to={`/explorer?q=${encodeURIComponent(focusedRepository === "unknown" ? focusedNode.label : focusedRepository)}`}>Explore repo graph</Link>
                {focusedFinding?.filePath ? <Link className="btn-ghost" to={`/dead-code?q=${encodeURIComponent(focusedFinding.filePath)}`}>Filter dead code</Link> : null}
              </div>
              {focusedSourceStatus ? <p className="t-mut" style={{ fontSize: ".78rem", margin: "8px 0 0" }}>{focusedSourceStatus}</p> : null}
            </div>
          ) : <p className="empty" style={{ textAlign: "left" }}>Click a graph node to inspect evidence and next actions.</p>}

          <div className="section-label">Hotspots · most-referenced</div>
          <div className="kv-list">
            {hotspots.map((row) => <div className="kv" key={row.id}><span className="mono" style={{ fontSize: ".76rem" }}>{row.label}</span><strong>{row.count}</strong></div>)}
            {hotspots.length === 0 ? <p className="empty" style={{ textAlign: "left" }}>No relationship hotspots returned.</p> : null}
          </div>
          <div className="section-label" style={{ marginTop: 16 }}>Import cycles</div>
          <p className="t-mut" style={{ fontSize: ".8rem", margin: 0 }}><span style={{ color: "var(--teal)" }}>◆ not reported</span> by the current endpoint.</p>
          <div className="section-label" style={{ marginTop: 16 }}>Dead in this repo · {deadInRepo.length}</div>
          {deadInRepo.length ? (
            <div className="conn-list">
              {deadInRepo.map((finding) => (
                <button type="button" className="dead-row" key={finding.id} onClick={() => selectCandidate(finding.id)}>
                  <span className="mono">{symbolFromFinding(finding)}</span>
                  <span className="t-mut">{finding.classification ?? "candidate"}</span>
                </button>
              ))}
            </div>
          ) : <p className="empty" style={{ padding: "6px 0", textAlign: "left" }}>No dead code in this repository.</p>}
          <div className="section-label" style={{ marginTop: 16 }}>Scan window</div>
          <div className="kv-list">
            <div className="kv"><span>Dead candidates</span><strong>{fmt(candidates.length)}</strong></div>
            <div className="kv"><span>Selected repo</span><strong>{deadInRepo.length}</strong></div>
          </div>
        </Panel>
      </div>
    </div>
  );
}

function withDeadSiblings(graph: GraphModel, selected: FindingRow, candidates: readonly FindingRow[]): GraphModel {
  const nodes = [...graph.nodes];
  const existing = new Set(nodes.map((node) => node.id));
  for (const finding of candidates.filter((row) => row.entity === selected.entity)) {
    const id = `dead:${finding.id}`;
    if (!existing.has(id)) {
      existing.add(id);
      nodes.push({ id, label: symbolFromFinding(finding), kind: "vuln", sub: finding.filePath ?? finding.detail, col: 3, truth: "derived" });
    }
  }
  return { nodes, edges: graph.edges };
}

function deadOnlyGraph(selected: FindingRow | undefined, candidates: readonly FindingRow[]): GraphModel {
  if (!selected) return { nodes: [], edges: [] };
  return withDeadSiblings({
    nodes: [{ id: selected.entityId ?? selected.id, label: symbolFromFinding(selected), kind: "client", sub: selected.filePath ?? selected.detail, col: 1, hero: true, truth: "derived" }],
    edges: []
  }, selected, candidates);
}

function hotspotRows(graph: GraphModel): readonly { readonly id: string; readonly label: string; readonly count: number }[] {
  const inbound = new Map<string, number>();
  for (const edge of graph.edges) inbound.set(edge.t, (inbound.get(edge.t) ?? 0) + 1);
  return [...inbound.entries()]
    .map(([id, count]) => ({ id, count, label: graph.nodes.find((node) => node.id === id)?.label ?? id }))
    .sort((a, b) => b.count - a.count || a.label.localeCompare(b.label))
    .slice(0, 5);
}

function findingForNode(node: GraphNode | undefined, candidates: readonly FindingRow[]): FindingRow | undefined {
  if (!node) return undefined;
  const deadId = node.id.startsWith("dead:") ? node.id.slice("dead:".length) : "";
  return candidates.find((finding) => finding.id === deadId || finding.entityId === node.id);
}

function symbolFromFinding(finding: FindingRow): string {
  return finding.title.replace(/^Unreferenced symbol\s+/i, "").trim() || finding.title;
}

function candidateIdFromParam(candidates: readonly FindingRow[], param: string): string | undefined {
  const value = param.trim().toLowerCase();
  if (value === "") return undefined;
  return candidates.find((finding) =>
    finding.id.toLowerCase() === value ||
    finding.entityId?.toLowerCase() === value ||
    symbolFromFinding(finding).toLowerCase() === value ||
    finding.filePath?.toLowerCase() === value
  )?.id;
}

function locationFromFinding(finding: FindingRow | undefined): string {
  if (!finding) return "source path unavailable";
  const path = finding.filePath ?? finding.detail.split(" · ")[0] ?? "source path unavailable";
  if (finding.startLine !== undefined && finding.endLine !== undefined) return `${path}:${finding.startLine}-${finding.endLine}`;
  if (finding.startLine !== undefined) return `${path}:${finding.startLine}`;
  return path;
}

function locationFromNode(node: GraphNode | undefined): string {
  const source = node?.source;
  if (!source) return "source path unavailable";
  if (source.startLine !== undefined && source.endLine !== undefined) return `${source.filePath}:${source.startLine}-${source.endLine}`;
  if (source.startLine !== undefined) return `${source.filePath}:${source.startLine}`;
  return source.filePath;
}

function sourceHref(finding: FindingRow): string | null {
  if (!finding.filePath) return null;
  const repository = finding.repoId ?? finding.entity;
  if (!repository) return null;
  const params = new URLSearchParams({ path: finding.filePath });
  if (finding.startLine !== undefined) params.set("lineStart", String(finding.startLine));
  if (finding.endLine !== undefined) params.set("lineEnd", String(finding.endLine));
  return `/repositories/${encodeURIComponent(repository)}/source?${params.toString()}`;
}

function sourceHrefFromNode(node: GraphNode | undefined): string | null {
  const source = node?.source;
  if (!source) return null;
  const params = new URLSearchParams({ path: source.filePath });
  if (source.startLine !== undefined) params.set("lineStart", String(source.startLine));
  if (source.endLine !== undefined) params.set("lineEnd", String(source.endLine));
  return `/repositories/${encodeURIComponent(source.repoId)}/source?${params.toString()}`;
}

function sourceMetadataStatus(
  node: GraphNode | undefined,
  finding: FindingRow | undefined,
  href: string | null
): string {
  if (!node || href) return "";
  if (finding) return "Dead-code scan did not return repository/file metadata.";
  return "Related symbol source metadata unavailable from POST /api/v0/code/relationships.";
}

async function loadCodeGraphCandidates(client: EshuApiClient): Promise<readonly FindingRow[]> {
  let repoNames: ReadonlyMap<string, string> = new Map();
  try {
    repoNames = await loadRepositoryNameMap(client);
  } catch {
    repoNames = new Map();
  }
  const page = await loadDeadCodePage(client, { limit: 100 }, repoNames);
  return page.rows;
}
