/* Eshu Console — Code intelligence: Dead code (analyzer findings) + Code graph
   (symbol/module CALLS + IMPORTS relationships). Grounded in the real
   /api/v0/code/dead-code output and the code-layer call/import graph.
   Exports to window. Loaded after drill.jsx. */
const { useState: useStateCd, useMemo: useMemoCd, useEffect: useEffectCd } = React;

const DEADKIND = {
  function: { label: "function", color: "#14b8a6" },
  class: { label: "class", color: "#8b5cf6" },
  const: { label: "const", color: "#f5b73d" },
  export: { label: "export", color: "#22d3ee" },
  route: { label: "route", color: "#4f8cff" },
  file: { label: "file", color: "#c4b59a" }
};

function deadCodeSourceHash(d) {
  const repo = deadCodeSourceRepo(d);
  if (!d || !repo || !d.file) return window.ESHU_ROUTES.hashFor("deadcode");
  const params = new URLSearchParams({ path: d.file });
  if (d.line) params.set("lineStart", String(d.line));
  if (d.endLine && d.endLine !== d.line) params.set("lineEnd", String(d.endLine));
  return window.ESHU_ROUTES.hashFor("reposource", "/" + encodeURIComponent(repo) + "/source?" + params.toString());
}

function sourceHref(d) {
  return sourceAvailable(d) ? deadCodeSourceHash(d) : "";
}

function sourceAvailable(d) {
  return !!(d && deadCodeSourceRepo(d) && d.file);
}

function codeGraphHash(d) {
  if (!d || !d.id) return window.ESHU_ROUTES.hashFor("codegraph");
  return window.ESHU_ROUTES.hashFor("codegraph", "?candidate=" + encodeURIComponent(d.id));
}

function deadCodeSourceRepo(d) {
  return (d && (d.repoId || d.repo || d.repoName)) || "";
}

function deadCodeRepoLabel(d) {
  return (d && (d.repoName || d.repo || d.repoId)) || "repository";
}

function deadCodeRepoKey(d) {
  return (d && (d.repoId || d.repo || d.repoName)) || "";
}

function sameDeadCodeRepo(a, b) {
  return a && b && deadCodeRepoKey(a) === deadCodeRepoKey(b);
}

function findingForCodeNode(node, candidates) {
  if (!node) return null;
  const deadId = String(node.id || "").startsWith("dead:") ? node.id.slice("dead:".length) : "";
  return candidates.find((d) => d.id === deadId || d.entityId === node.id) || null;
}

function locationLabel(d) {
  if (!d) return "source path unavailable";
  const file = d.file || "source path unavailable";
  if (d.line && d.endLine && d.endLine !== d.line) return file + ":" + d.line + "-" + d.endLine;
  if (d.line) return file + ":" + d.line;
  return file;
}

function sourceHrefFromNode(node) {
  const source = node && node.source;
  if (!source || !source.repoId || !source.filePath) return "";
  const params = new URLSearchParams({ path: source.filePath });
  if (source.startLine) params.set("lineStart", String(source.startLine));
  if (source.endLine) params.set("lineEnd", String(source.endLine));
  return window.ESHU_ROUTES.hashFor("reposource", "/" + encodeURIComponent(source.repoId) + "/source?" + params.toString());
}

function sourceMetadataStatus(node, candidate, href) {
  if (!node || href) return "";
  if (candidate) return "Dead-code scan did not return repository/file metadata.";
  return "Related symbol source metadata unavailable from POST /api/v0/code/relationships.";
}

function locationLabelFromNode(node) {
  const source = node && node.source;
  if (!source || !source.filePath) return "source path unavailable";
  if (source.startLine && source.endLine && source.endLine !== source.startLine) return source.filePath + ":" + source.startLine + "-" + source.endLine;
  if (source.startLine) return source.filePath + ":" + source.startLine;
  return source.filePath;
}

function sourceLocationFromCodeEdge(edge, side) {
  const prefix = side === "source" ? "source_" : "target_";
  const repoId = textField(edge[prefix + "repo_id"]) || textField(edge.repo_id);
  const filePath = textField(edge[prefix + "file_path"]) || textField(edge.file_path);
  if (!repoId || !filePath) return null;
  return {
    repoId,
    repoName: textField(edge[prefix + "repo_name"]) || textField(edge.repo_name),
    filePath,
    startLine: edge[prefix + "start_line"] || edge.start_line,
    endLine: edge[prefix + "end_line"] || edge.end_line
  };
}

function textField(value) {
  return typeof value === "string" ? value.trim() : "";
}

function codeGraphCandidateParam() {
  const parts = String(location.hash || "").split("?");
  if (parts.length < 2) return "";
  const params = new URLSearchParams(parts.slice(1).join("?"));
  return params.get("candidate") || params.get("q") || "";
}

function candidateForParam(candidates, param) {
  const value = String(param || "").trim().toLowerCase();
  if (!value) return null;
  return candidates.find((d) =>
    String(d.id || "").toLowerCase() === value ||
    String(d.entityId || "").toLowerCase() === value ||
    String(d.symbol || "").toLowerCase() === value ||
    String(d.file || "").toLowerCase() === value
  ) || null;
}

function codeRelationshipsGraph(payload, selected, candidates) {
  const data = payload || {};
  const centerId = data.entity_id || selected.entityId || selected.id;
  const nodes = [{ id: centerId, label: data.name || selected.symbol, sub: selected.file, col: 1, kind: selected.kind || "function", hero: true }];
  const edges = [];
  const seen = new Set([centerId]);
  (data.incoming || []).forEach((e) => {
    const id = e.source_id || e.source_name;
    if (!id) return;
    const verb = (e.type || "RELATED").toUpperCase();
    if (!seen.has(id)) { seen.add(id); nodes.push({ id, label: e.source_name || id, sub: e.source_type || relationshipNodeSub(verb, "incoming"), col: 0, kind: e.source_type ? relationshipTypeKind(e.source_type) : relationshipNodeKind(verb), source: sourceLocationFromCodeEdge(e, "source") }); }
    edges.push({ s: id, t: centerId, verb, layer: "code" });
  });
  (data.outgoing || []).forEach((e) => {
    const id = e.target_id || e.target_name;
    if (!id) return;
    const verb = (e.type || "RELATED").toUpperCase();
    if (!seen.has(id)) { seen.add(id); nodes.push({ id, label: e.target_name || id, sub: e.target_type || relationshipNodeSub(verb, "outgoing"), col: 2, kind: e.target_type ? relationshipTypeKind(e.target_type) : relationshipNodeKind(verb), source: sourceLocationFromCodeEdge(e, "target") }); }
    edges.push({ s: centerId, t: id, verb, layer: "code" });
  });
  candidates.filter((d) => sameDeadCodeRepo(d, selected)).forEach((d) => {
    const id = "dead:" + d.id;
    if (!seen.has(id)) {
      seen.add(id);
      nodes.push({ id, label: d.symbol, sub: d.file, col: 5, kind: "vuln", dead: true });
    }
  });
  return { nodes, edges, dead: candidates.filter((d) => sameDeadCodeRepo(d, selected)) };
}

function relationshipNodeKind(verb) {
  const normalized = String(verb || "").toUpperCase();
  if (normalized === "IMPORTS" || normalized === "REFERENCES") return "library";
  if (normalized === "CALLS") return "client";
  if (normalized === "INHERITS" || normalized === "OVERRIDES") return "client";
  return "client";
}

function relationshipTypeKind(type) {
  const normalized = String(type || "").toLowerCase();
  if (normalized.indexOf("module") >= 0 || normalized.indexOf("package") >= 0 || normalized.indexOf("library") >= 0) return "library";
  if (normalized.indexOf("function") >= 0 || normalized.indexOf("class") >= 0 || normalized.indexOf("symbol") >= 0) return "client";
  return relationshipNodeKind(type);
}

function relationshipNodeSub(verb, direction) {
  return direction + " " + String(verb || "RELATED").toUpperCase();
}

function envelopeErrorMessage(error) {
  if (!error) return "";
  if (typeof error === "string") return error;
  if (typeof error !== "object") return "api error";
  if (error.code && error.message) return String(error.code) + ": " + String(error.message);
  return String(error.message || error.code || "api error");
}

function apiData(env) {
  const message = envelopeErrorMessage(env && env.error);
  if (message) throw new Error(message);
  return env && env.data ? env.data : {};
}

function deadOnlyLiveGraph(selected, candidates) {
  if (!selected) return { nodes: [], edges: [], dead: [] };
  return codeRelationshipsGraph({ entity_id: selected.entityId || selected.id, name: selected.symbol, incoming: [], outgoing: [] }, selected, candidates);
}

/* ================================================================ DEAD CODE */
function DeadCode({ data, onOpenService }) {
  const D = data || ESHU;
  const all = D.deadCode || [];
  const [kind, setKind] = useStateCd("all");
  const [conf, setConf] = useStateCd("all");
  const [q, setQ] = useStateCd("");
  const kinds = Array.from(new Set(all.map((d) => d.kind)));
  const rows = all.filter((d) =>
    (kind === "all" || d.kind === kind) && (conf === "all" || d.confidence === conf) &&
    (q === "" || (d.symbol + d.file + d.repo).toLowerCase().includes(q.toLowerCase())));
  const repos = Array.from(new Set(all.map((d) => deadCodeRepoKey(d))));
  const totalLoc = all.reduce((a, d) => a + (d.loc || 0), 0);
  const byKind = {};all.forEach((d) => byKind[d.kind] = (byKind[d.kind] || 0) + 1);
  const grouped = {};
  const groupedLabels = {};
  rows.forEach((d) => {
    const key = deadCodeRepoKey(d);
    grouped[key] = grouped[key] || [];
    groupedLabels[key] = deadCodeRepoLabel(d);
    grouped[key].push(d);
  });

  return (
    <div className="page">
      <div className="page-intro"><h2>Dead code</h2><p>Unreferenced symbols the analyzer found with <strong>zero inbound</strong> <span className="mono">CALLS</span> / <span className="mono">IMPORTS</span> edges — safe-to-delete candidates from <span className="mono">/api/v0/code/dead-code</span>. Each carries its confidence and the reason it reads as dead. Select a location to open the source file.</p></div>

      <div className="grid g-4">
        <StatTile label="Dead symbols" value={all.length} color="var(--ember)" sub="0 references" />
        <StatTile label="Repos affected" value={repos.length} color="var(--blue)" sub={"of " + D.services.filter((s) => s.repo).length + " indexed"} />
        <StatTile label="Est. dead LOC" value={fmt(totalLoc)} color="var(--violet)" sub="reclaimable" />
        <StatTile label="High confidence" value={all.filter((d) => d.confidence === "exact").length} color="var(--teal)" sub="exact — no call sites" />
      </div>

      <div className="repo-toolbar mt">
        <div className="searchbox" style={{ minWidth: 240, height: 38, margin: 0, flex: 1 }}><Icon.search size={16} /><input placeholder="Find a symbol, file or repo…" value={q} onChange={(e) => setQ(e.target.value)} /></div>
        <div className="seg">{["all"].concat(kinds).map((k) => <button key={k} className={kind === k ? "active" : ""} onClick={() => setKind(k)}>{k === "all" ? "All kinds" : (DEADKIND[k] || {}).label || k}{k !== "all" ? " · " + byKind[k] : ""}</button>)}</div>
        <div className="seg">{["all", "exact", "derived", "inferred"].map((c) => <button key={c} className={conf === c ? "active" : ""} onClick={() => setConf(c)}>{c === "all" ? "Any" : c}</button>)}</div>
      </div>

      <Panel className="flush mt" title={rows.length + " candidates"} sub="Grouped by repository · locations open source" glyph={<Icon.findings />}>
        <table className="tbl">
          <thead><tr><th>Symbol</th><th>Kind</th><th>Location</th><th>Refs</th><th>LOC</th><th>Confidence</th><th>Why dead</th><th></th></tr></thead>
          <tbody>
            {Object.keys(grouped).map((repo) => {
              const svc = D.servicesById[repo];
              return (
                <React.Fragment key={repo}>
                  <tr className="group-row"><td colSpan={8}><span className="group-label" style={{ color: "var(--ember)" }}>{svc ? svc.name : groupedLabels[repo]}</span><span className="group-meta">{grouped[repo].length} dead · {fmt(grouped[repo].reduce((a, d) => a + (d.loc || 0), 0))} LOC</span></td></tr>
                  {grouped[repo].map((d) => {
                    const dk = DEADKIND[d.kind] || { label: d.kind, color: "var(--muted)" };
                    return (
                      <tr key={d.id} className="cloud-row" onClick={() => svc && onOpenService(repo)} style={{ cursor: "pointer" }}>
                        <td className="cell-stack"><span className="mono" style={{ color: "var(--bone)", fontWeight: 600 }}>{d.symbol}</span></td>
                        <td><span className="dead-kind" style={{ "--dk": dk.color }}>{dk.label}</span></td>
                        <td className="t-mut mono" style={{ fontSize: ".74rem" }}>
                          {sourceAvailable(d) ? <a className="mono" href={deadCodeSourceHash(d)} title="Open source" onClick={(e) => e.stopPropagation()}>{locationLabel(d)}</a> : <span>{locationLabel(d)}</span>}
                        </td>
                        <td><span className="mono" style={{ color: "var(--crit)", fontWeight: 700 }}>0</span></td>
                        <td className="t-mut mono" style={{ fontSize: ".78rem" }}>{d.loc}</td>
                        <td><TruthChip level={d.confidence} /></td>
                        <td className="t-mut" style={{ fontSize: ".78rem", maxWidth: 320 }}>{d.reason}</td>
                        <td><a className="btn-ghost" href={codeGraphHash(d)} onClick={(e) => e.stopPropagation()}>Open graph</a></td>
                      </tr>
                    );
                  })}
                </React.Fragment>
              );
            })}
            {rows.length === 0 ? <tr><td colSpan={8}><p className="empty">No dead-code candidates match.</p></td></tr> : null}
          </tbody>
        </table>
      </Panel>
    </div>
  );
}

/* =============================================================== CODE GRAPH */
/* symbol/module-level relationships for one repo: IMPORTS (module) + CALLS (function) */
function buildCodeGraph(D, svc) {
  if (!svc) return { nodes: [], edges: [], dead: [] };
  const dead0 = (D.deadCode || []).filter((d) => d.repo === svc.id);
  // live: module dependency graph from /api/v0/code/imports/investigate
  const live = D.codeImports && D.codeImports[svc.id];
  if (live && live.modEdges && live.modEdges.length) {
    const ids = new Set();
    live.modEdges.forEach((e) => { ids.add(e.s); ids.add(e.t); });
    const short = (m) => String(m).split("/").pop();
    const nodes = Array.from(ids).map((m, i) => ({ id: m, label: short(m), sub: m, kind: i === 0 ? "repo" : "library", col: Math.min(4, i % 5) }));
    const edges = live.modEdges.map((e) => ({ s: e.s, t: e.t, verb: "IMPORTS", layer: "code" }));
    dead0.forEach((d) => nodes.push({ id: "dead:" + d.id, label: d.symbol, sub: d.file, col: 5, kind: "vuln", dead: true }));
    return { nodes, edges, dead: dead0, hubs: live.hubs, cycles: live.cycles };
  }
  const base = svc.name.replace(/^svc-|^job-|^web-/, "");
  const ext = D.lang[svc.lang] && svc.lang === "go" ? "go" : svc.lang === "py" ? "py" : "ts";
  const N = (id, label, col, kind) => ({ id, label, sub: id, col, kind: kind || "library" });
  const nodes = [
    N("index", "index." + ext, 0, "repo"),
    N("app", "app." + ext, 1, "library"),
    N("routes", "routes/" + base, 2, "client"),
    N("service", "services/" + base + ".service", 3, "service"),
    N("model", "models/" + base, 4, "library"),
    N("db", "lib/db", 4, "library"),
    N("logger", "lib/logger", 4, "library")
  ];
  const edges = [
    { s: "index", t: "app", verb: "IMPORTS", layer: "code" },
    { s: "app", t: "routes", verb: "IMPORTS", layer: "code" },
    { s: "routes", t: "service", verb: "IMPORTS", layer: "code" },
    { s: "service", t: "model", verb: "IMPORTS", layer: "code" },
    { s: "service", t: "db", verb: "IMPORTS", layer: "code" },
    { s: "service", t: "logger", verb: "IMPORTS", layer: "code" },
    { s: "routes", t: "service", verb: "CALLS", layer: "code" },
    { s: "service", t: "db", verb: "CALLS", layer: "code" }
  ];
  // cross-repo: consumers import the catalog client
  if ((svc.deps || []).includes("svc-catalog") && svc.id !== "svc-catalog") {
    nodes.push(N("client:catalog", "@acme/svc-catalog-client", 4, "client"));
    edges.push({ s: "service", t: "client:catalog", verb: "IMPORTS", layer: "code" });
    edges.push({ s: "service", t: "client:catalog", verb: "CALLS", layer: "code" });
  }
  // dead-code symbols for this repo become orphan nodes (no inbound edges)
  const dead = (D.deadCode || []).filter((d) => d.repo === svc.id);
  dead.forEach((d, i) => nodes.push({ id: "dead:" + d.id, label: d.symbol, sub: d.file, col: 5, kind: "vuln", dead: true }));
  return { nodes, edges, dead };
}

function CodeGraph({ data, client, onOpenService }) {
  const D = data || ESHU;
  const liveMode = !!(client && D.prov && D.prov.deadCode === "live");
  const liveCandidates = liveMode ? (D.deadCode || []).filter((d) => d.entityId) : [];
  const [candidateId, setCandidateId] = useStateCd((candidateForParam(liveCandidates, codeGraphCandidateParam()) || liveCandidates[0] || {}).id || "");
  const selectedCandidate = liveCandidates.find((d) => d.id === candidateId) || liveCandidates[0];
  const [liveState, setLiveState] = useStateCd({ status: "idle", graph: null, error: "" });
  const [focusedNodeId, setFocusedNodeId] = useStateCd((selectedCandidate || {}).entityId || "");
  const repos = D.services.filter((s) => s.repo);
  const [repoId, setRepoId] = useStateCd((repos.find((s) => s.id === "svc-catalog") || repos[0] || {}).id);
  const svc = D.servicesById[repoId];
  const demoGraph = useMemoCd(() => buildCodeGraph(D, svc), [D, svc]);
  useEffectCd(() => {
    const requested = candidateForParam(liveCandidates, codeGraphCandidateParam());
    if (requested && requested.id !== candidateId) {
      setCandidateId(requested.id);
      setFocusedNodeId(requested.entityId || requested.id);
      return;
    }
    if (!candidateId && liveCandidates[0]) setCandidateId(liveCandidates[0].id);
  }, [candidateId, liveCandidates.length]);
  useEffectCd(() => {
    let cancelled = false;
    if (!liveMode || !selectedCandidate) {
      setLiveState({ status: "idle", graph: null, error: "" });
      return () => { cancelled = true; };
    }
    setFocusedNodeId(selectedCandidate.entityId || selectedCandidate.id);
    setLiveState({ status: "loading", graph: deadOnlyLiveGraph(selectedCandidate, liveCandidates), error: "" });
    client.post("/api/v0/code/relationships", { entity_id: selectedCandidate.entityId, max_depth: 1 })
      .then((env) => {
        const data = apiData(env);
        if (!cancelled) setLiveState({ status: "ready", graph: codeRelationshipsGraph(data, selectedCandidate, liveCandidates), error: "" });
      })
      .catch((e) => {
        if (!cancelled) setLiveState({ status: "error", graph: deadOnlyLiveGraph(selectedCandidate, liveCandidates), error: (e && e.message) || "failed to load code graph" });
      });
    return () => { cancelled = true; };
  }, [liveMode, client, selectedCandidate && selectedCandidate.id, selectedCandidate && selectedCandidate.entityId, liveCandidates.length]);
  const g = liveMode ? (liveState.graph || deadOnlyLiveGraph(selectedCandidate, liveCandidates)) : demoGraph;
  // hotspots = most inbound edges (import + call)
  const inbound = {};g.edges.forEach((e) => inbound[e.t] = (inbound[e.t] || 0) + 1);
  const hotspots = (g.hubs && g.hubs.length) ? g.hubs.map((h) => ({ n: { id: h.name, label: h.name }, c: h.c })).slice(0, 5) : g.nodes.filter((n) => !n.dead).map((n) => ({ n, c: inbound[n.id] || 0 })).sort((a, b) => b.c - a.c).slice(0, 5);
  const importEdges = g.edges.filter((e) => e.verb === "IMPORTS").length;
  const callEdges = g.edges.filter((e) => e.verb === "CALLS").length;
  const deadRows = liveMode ? liveCandidates.filter((d) => sameDeadCodeRepo(d, selectedCandidate)) : g.dead;
  const focusedNode = g.nodes.find((n) => n.id === focusedNodeId) || g.nodes.find((n) => n.hero) || g.nodes[0];
  const focusedCandidate = findingForCodeNode(focusedNode, liveCandidates);
  const focusedNodeSourceHref = sourceHrefFromNode(focusedNode);
  const focusedRepositoryLabel = focusedCandidate ? deadCodeRepoLabel(focusedCandidate) : (focusedNode && focusedNode.source && (focusedNode.source.repoName || focusedNode.source.repoId)) || deadCodeRepoLabel(selectedCandidate);
  const focusedLocationLabel = focusedCandidate ? locationLabel(focusedCandidate) : locationLabelFromNode(focusedNode);
  const focusedSourceHref = focusedCandidate ? sourceHref(focusedCandidate) : focusedNodeSourceHref;
  const focusedSourceStatus = sourceMetadataStatus(focusedNode, focusedCandidate, focusedSourceHref);
  const explorerQuery = focusedRepositoryLabel !== "repository" ? focusedRepositoryLabel : ((focusedNode && focusedNode.label) || "");
  const focusedDegree = focusedNode ? g.edges.filter((e) => e.s === focusedNode.id || e.t === focusedNode.id).length : 0;
  function selectCandidate(id) {
    const next = liveCandidates.find((d) => d.id === id);
    setCandidateId(id);
    setFocusedNodeId((next && (next.entityId || next.id)) || id);
  }
  function selectGraphNode(n) {
    setFocusedNodeId(n.id);
    if (liveMode && n.id && n.id.indexOf("dead:") === 0) {
      setCandidateId(n.id.slice("dead:".length));
      return;
    }
    if (!liveMode && (n.id === "service" || n.id === "index")) onOpenService(repoId);
  }

  return (
    <div className="page" style={{ maxWidth: "none" }}>
      <div className="page-intro row" style={{ justifyContent: "space-between", alignItems: "flex-end", flexWrap: "wrap", gap: 12 }}>
        <div><h2>Code graph</h2><p>Symbol and module relationships at code grain from <span className="mono">POST /api/v0/code/relationships</span>. Dead-code candidates from the same repository are shown as orphan analyzer nodes.</p></div>
        <select className="code-repo-select mono" value={liveMode ? ((selectedCandidate || {}).id || "") : repoId} onChange={(e) => liveMode ? selectCandidate(e.target.value) : setRepoId(e.target.value)}>
          {liveMode ? liveCandidates.map((d) => <option key={d.id} value={d.id}>{d.symbol} · {deadCodeRepoLabel(d)}</option>) : repos.map((s) => <option key={s.id} value={s.id}>{s.name}</option>)}
        </select>
      </div>

      <div className="grid g-4">
        <StatTile label="Modules" value={g.nodes.filter((n) => !n.dead).length} color="var(--teal)" sub={liveMode ? deadCodeRepoLabel(selectedCandidate) : (svc ? svc.repo : "")} />
        <StatTile label="Import edges" value={importEdges} color="var(--blue)" sub="module graph" />
        <StatTile label="Call edges" value={callEdges} color="var(--ember)" sub="function call-graph" />
        <StatTile label="Dead symbols" value={deadRows.length} color="var(--crit)" sub={deadRows.length ? "orphaned" : "none in repo"} onClick={() => { window.ESHU_ROUTES.setHash("deadcode"); }} cta="Dead code" />
      </div>

      <div className="explorer-layout mt">
        <div className="gcanvas-shell">
          <GraphCanvas graph={g} data={D} layout="layered" height={560} onSelect={selectGraphNode} selectedId={focusedNode && focusedNode.id} />
          {liveState.status === "error" ? <p className="src-err">{liveState.error}</p> : null}
          <div className="t-mut" style={{ fontSize: ".74rem", marginTop: 8 }}>{liveMode ? ((selectedCandidate && (selectedCandidate.symbol + " · " + selectedCandidate.file)) || "No live dead-code candidate selected.") : (svc ? svc.name + " · " + (D.lang[svc.lang] || {}).label + " · routes → service → model/lib, with cross-repo client imports" : "")}</div>
        </div>
        <Panel title="Analyzer" glyph={<Icon.spark />}>
          {liveMode && focusedNode ? (
            <>
              <div className="section-label">Selected symbol</div>
              <div className="selected-code-node">
                <div className="row" style={{ justifyContent: "space-between", gap: 8, alignItems: "center" }}>
                  <strong className="mono">{focusedNode.label}</strong>
                  <span className="dead-kind" style={{ "--dk": (DEADKIND[(focusedCandidate || {}).kind] || {}).color || "var(--muted)" }}>{(focusedCandidate && (focusedCandidate.classification || focusedCandidate.kind)) || focusedNode.kind}</span>
                </div>
                <div className="kv-list" style={{ marginTop: 10 }}>
                  <div className="kv"><span>Repository</span><strong>{focusedRepositoryLabel}</strong></div>
                  <div className="kv"><span>Location</span>{focusedSourceHref ? <a className="mono" href={focusedSourceHref}>{focusedLocationLabel}</a> : <strong className="mono">{focusedLocationLabel || focusedNode.sub || "source path unavailable"}</strong>}</div>
                  <div className="kv"><span>Graph degree</span><strong>{focusedDegree}</strong></div>
                  <div className="kv"><span>Evidence</span><strong>{(focusedCandidate && focusedCandidate.confidence) || focusedNode.truth || "derived"}</strong></div>
                </div>
                <div className="row" style={{ gap: 8, flexWrap: "wrap", marginTop: 12 }}>
                  {focusedSourceHref ? <a className="btn-ghost active" href={focusedSourceHref}>Open source</a> : null}
                  <a className="btn-ghost" href={window.ESHU_ROUTES.hashFor("explorer", "?q=" + encodeURIComponent(explorerQuery))}>Explore repo graph</a>
                </div>
                {focusedSourceStatus ? <p className="t-mut" style={{ fontSize: ".78rem", margin: "8px 0 0" }}>{focusedSourceStatus}</p> : null}
              </div>
            </>
          ) : <p className="empty" style={{ textAlign: "left" }}>Click a graph node to inspect evidence and next actions.</p>}
          <div className="section-label">Hotspots · most-referenced</div>
          <div className="kv-list">
            {hotspots.map((h) => <div className="kv" key={h.n.id}><span className="mono" style={{ fontSize: ".76rem" }}>{h.n.label}</span><strong>{h.c}</strong></div>)}
          </div>
          <div className="section-label" style={{ marginTop: 16 }}>Import cycles{g.cycles && g.cycles.length ? " · " + g.cycles.length : ""}</div>
          {liveMode ? <p className="t-mut" style={{ fontSize: ".8rem", margin: 0 }}><span style={{ color: "var(--teal)" }}>◆ not reported</span> by the current endpoint.</p> : g.cycles && g.cycles.length ? (
            <div className="conn-list">{g.cycles.slice(0, 6).map((c, i) => <div className="dead-row" key={i}><span className="mono" style={{ color: "var(--med)" }}>{Array.isArray(c) ? c.map((m) => String(m).split("/").pop()).join(" → ") : String(c.cycle || c.path || c)}</span></div>)}</div>
          ) : <p className="t-mut" style={{ fontSize: ".8rem", margin: 0 }}><span style={{ color: "var(--teal)" }}>◆ none detected</span> — module graph is acyclic.</p>}
          <div className="section-label" style={{ marginTop: 16 }}>Dead in this repo · {deadRows.length}</div>
          {deadRows.length ? (
            <div className="conn-list">
              {deadRows.map((d) => <button type="button" className="dead-row" key={d.id} onClick={() => liveMode ? selectCandidate(d.id) : window.ESHU_ROUTES.setHash("deadcode")}><span className="mono">{d.symbol}</span><span className="t-mut">{(DEADKIND[d.kind] || {}).label || d.classification}</span></button>)}
            </div>
          ) : <p className="empty" style={{ padding: "6px 0", textAlign: "left" }}>No dead code in {svc ? svc.name : "this repo"}.</p>}
        </Panel>
      </div>
    </div>
  );
}

Object.assign(window, { DeadCode, CodeGraph, buildCodeGraph });
