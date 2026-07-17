import { useEffect, useMemo, useState } from "react";
import { Link } from "react-router-dom";

import {
  codeGraphSelectionKey,
  deadOnlyGraph,
  emptyImportCycleState,
  explorerQueryFor,
  findingForNode,
  hotspotRows,
  ImportCyclesPanel,
  locationFromFinding,
  locationFromNode,
  sourceHref,
  sourceHrefFromNode,
  sourceMetadataStatus,
  symbolFromFinding,
  withDeadSiblings,
} from "./CodeGraphPageSupport";
import type { ImportCycleState } from "./CodeGraphPageSupport";
import { CodeGraphSelectors } from "./CodeGraphSelectors";
import { RelationshipTruthPanel } from "./RelationshipTruthPanel";
import { useCodeGraphSelection } from "./useCodeGraphSelection";
import type { EshuApiClient } from "../api/client";
import { loadCodeGraph } from "../api/codeGraphLoader";
import { loadCodeImportCycles } from "../api/codeImports";
import type { CodeRelationshipStoryCoverage } from "../api/eshuGraph";
import type { RepoListItem } from "../api/repoCatalog";
import { Badge, Panel, StatTile } from "../components/atoms";
import { GraphCanvas } from "../components/GraphCanvas";
import type { ConsoleModel, GraphModel, GraphNode } from "../console/types";
import { fmt } from "../console/types";
import type { RepositoryCatalogState } from "../repositoryCatalogLifecycle";

export function CodeGraphPage({
  model,
  client,
  repositories,
  repositoryCatalog,
}: {
  readonly model: ConsoleModel;
  readonly client?: EshuApiClient;
  readonly repositories?: readonly RepoListItem[];
  readonly repositoryCatalog?: RepositoryCatalogState;
}): React.JSX.Element {
  const candidates = useMemo(
    () => model.findings.filter((finding) => finding.type === "Dead code"),
    [model.findings],
  );
  const selection = useCodeGraphSelection({
    client,
    deadCandidates: candidates,
    model,
    repositories,
    repositoryCatalog,
  });
  const selected = selection.selected;
  const selectedRepositoryId = selection.repository?.id ?? "";
  const selectableSymbols =
    selected &&
    !selection.symbols.some(
      (symbol) => (symbol.entityId ?? symbol.id) === (selected.entityId ?? selected.id),
    )
      ? [selected, ...selection.symbols]
      : selection.symbols;
  const evidenceFindings = selected
    ? [
        selected,
        ...candidates.filter(
          (finding) => (finding.entityId ?? finding.id) !== (selected.entityId ?? selected.id),
        ),
      ]
    : candidates;
  const [graph, setGraph] = useState<GraphModel>({ nodes: [], edges: [] });
  const selectedGraphKey = codeGraphSelectionKey(
    selectedRepositoryId,
    selected?.entityId ?? selected?.id ?? "",
  );
  const [graphOwnerKey, setGraphOwnerKey] = useState("");
  const [focusedNodeId, setFocusedNodeId] = useState<string | undefined>(selected?.entityId);
  const [busy, setBusy] = useState(false);
  const [graphError, setGraphError] = useState({ key: "", message: "" });
  const [graphRetryNonce, setGraphRetryNonce] = useState(0);
  const [cycleState, setCycleState] = useState<ImportCycleState>(emptyImportCycleState);
  const [cycleOwnerRepo, setCycleOwnerRepo] = useState("");
  const [relationshipCoverage, setRelationshipCoverage] = useState<
    CodeRelationshipStoryCoverage | undefined
  >(undefined);

  useEffect(() => {
    let cancelled = false;
    if (!client || !selected?.entityId) {
      setGraph(deadOnlyGraph(selected, candidates));
      setGraphOwnerKey(selectedGraphKey);
      setFocusedNodeId(selected?.entityId ?? selected?.id);
      setRelationshipCoverage(undefined);
      setGraphError({ key: selectedGraphKey, message: "" });
      setBusy(false);
      return () => {
        cancelled = true;
      };
    }
    setBusy(true);
    setGraphError({ key: selectedGraphKey, message: "" });
    const target = {
      entityId: selected.entityId,
      id: selected.id,
      name: symbolFromFinding(selected),
      repoId: selectedRepositoryId,
    };
    void loadCodeGraph(client, target)
      .then((loaded) => {
        if (cancelled) return;
        setGraph(withDeadSiblings(loaded.graph, selected, candidates));
        setGraphOwnerKey(selectedGraphKey);
        setRelationshipCoverage(loaded.coverage);
        setFocusedNodeId(selected.entityId ?? selected.id);
      })
      .catch((error) => {
        if (!cancelled) {
          setGraphError({
            key: selectedGraphKey,
            message: error instanceof Error ? error.message : "failed to load code graph",
          });
          setGraph({ nodes: [], edges: [] });
          setGraphOwnerKey(selectedGraphKey);
          setRelationshipCoverage(undefined);
        }
      })
      .finally(() => {
        if (!cancelled) setBusy(false);
      });
    return () => {
      cancelled = true;
    };
  }, [candidates, client, graphRetryNonce, selected, selectedGraphKey, selectedRepositoryId]);

  const selectedRepoScope = selectedRepositoryId;
  useEffect(() => {
    let cancelled = false;
    if (!client || selectedRepoScope === "") {
      setCycleState(emptyImportCycleState);
      setCycleOwnerRepo(selectedRepoScope);
      return () => {
        cancelled = true;
      };
    }
    setCycleState({ ...emptyImportCycleState, status: "loading" });
    setCycleOwnerRepo(selectedRepoScope);
    void loadCodeImportCycles(client, selectedRepoScope, 6)
      .then((page) => {
        if (!cancelled) {
          setCycleState({
            status: "ready",
            cycles: page.cycles,
            error: "",
            truncated: page.truncated,
            nextOffset: page.nextOffset,
          });
        }
      })
      .catch((error) => {
        if (!cancelled) {
          setCycleState({
            status: "error",
            cycles: [],
            error: error instanceof Error ? error.message : "failed to load import cycles",
            truncated: false,
            nextOffset: null,
          });
        }
      });
    return () => {
      cancelled = true;
    };
  }, [client, selectedRepoScope]);

  const visibleGraph = graphOwnerKey === selectedGraphKey ? graph : { nodes: [], edges: [] };
  const visibleGraphError = graphError.key === selectedGraphKey ? graphError.message : "";
  const visibleCoverage = graphOwnerKey === selectedGraphKey ? relationshipCoverage : undefined;
  const visibleCycleState =
    cycleOwnerRepo === selectedRepoScope
      ? cycleState
      : selectedRepoScope
        ? { ...emptyImportCycleState, status: "loading" as const }
        : emptyImportCycleState;
  const deadInRepo = candidates.filter((finding) =>
    findingBelongsToRepository(finding, selection.repository),
  );
  const importEdges = visibleGraph.edges.filter((edge) => edge.verb === "IMPORTS").length;
  const callEdges = visibleGraph.edges.filter((edge) => edge.verb === "CALLS").length;
  const hotspots = hotspotRows(visibleGraph);
  const focusedNode =
    visibleGraph.nodes.find((node) => node.id === focusedNodeId) ??
    visibleGraph.nodes.find((node) => node.id === selected?.entityId) ??
    visibleGraph.nodes[0];
  const focusedFinding = findingForNode(focusedNode, evidenceFindings);
  const focusedDegree = focusedNode
    ? visibleGraph.edges.filter((edge) => edge.s === focusedNode.id || edge.t === focusedNode.id)
        .length
    : 0;
  const focusedSourceHref = focusedFinding
    ? sourceHref(focusedFinding)
    : sourceHrefFromNode(focusedNode);
  const focusedRepository =
    focusedFinding?.entity ??
    focusedNode?.source?.repoName ??
    focusedNode?.source?.repoId ??
    selected?.entity ??
    "unknown";
  const explorerQuery = explorerQueryFor(focusedNode, focusedFinding, focusedRepository);
  const focusedLocation = focusedFinding
    ? locationFromFinding(focusedFinding)
    : locationFromNode(focusedNode);
  const focusedSourceStatus = sourceMetadataStatus(focusedNode, focusedFinding, focusedSourceHref);

  function selectGraphNode(node: GraphNode): void {
    setFocusedNodeId(node.id);
  }

  function selectCandidate(id: string): void {
    const next = candidates.find((finding) => finding.id === id);
    setFocusedNodeId(next ? `dead:${next.id}` : `dead:${id}`);
  }

  return (
    <div className="page" style={{ maxWidth: "none" }}>
      <div
        className="page-intro row"
        style={{
          justifyContent: "space-between",
          alignItems: "flex-end",
          flexWrap: "wrap",
          gap: 12,
        }}
      >
        <div>
          <h2>Code graph</h2>
          <p>
            Symbol and module relationships at code grain from{" "}
            <span className="mono">POST /api/v0/code/relationships/story</span>. Dead-code
            candidates from the same repository are shown as orphan analyzer nodes.
          </p>
        </div>
        <CodeGraphSelectors
          loading={selection.loading}
          onEntityChange={selection.selectEntity}
          onRepositoryChange={selection.selectRepository}
          repositories={selection.repositories}
          selectedEntityId={selected?.entityId ?? selected?.id ?? ""}
          selectedRepositoryId={selection.repository?.id ?? ""}
          symbols={selectableSymbols}
        />
      </div>

      <div className="grid g-4">
        <StatTile
          label="Modules"
          value={visibleGraph.nodes.filter((node) => !node.id.startsWith("dead:")).length}
          color="var(--teal)"
          sub={selection.repository?.name ?? "no repository"}
        />
        <StatTile label="Import edges" value={importEdges} color="var(--blue)" sub="module graph" />
        <StatTile
          label="Call edges"
          value={callEdges}
          color="var(--ember)"
          sub="function call graph"
        />
        <StatTile
          label="Dead symbols"
          value={deadInRepo.length}
          color="var(--crit)"
          sub={deadInRepo.length ? "orphaned" : "none in repo"}
        />
      </div>

      <div className="explorer-layout mt">
        <div className="gcanvas-shell">
          {selection.loading || busy ? (
            <div className="conn-state compact">
              <div className="conn-spinner" aria-hidden />
              <p>Loading code graph...</p>
            </div>
          ) : (
            <GraphCanvas
              graph={visibleGraph}
              layout="layered"
              height={560}
              selectedId={focusedNode?.id ?? selected?.entityId}
              onSelect={selectGraphNode}
            />
          )}
          {visibleGraphError ? (
            <div>
              <p className="src-err">{visibleGraphError}</p>
              <button
                className="btn-ghost"
                type="button"
                onClick={() => setGraphRetryNonce((current) => current + 1)}
              >
                Retry relationship graph
              </button>
            </div>
          ) : null}
          {selection.error ? (
            <div>
              <p className="src-err">{selection.error}</p>
              {selection.repository ? (
                <button className="btn-ghost" type="button" onClick={selection.retry}>
                  Retry repository graph
                </button>
              ) : null}
            </div>
          ) : null}
          <div className="t-mut" style={{ fontSize: ".74rem", marginTop: 8 }}>
            {selected
              ? `${symbolFromFinding(selected)} · ${selected.language ?? "code"} · ${selected.filePath ?? "source path unavailable"}`
              : selection.loading
                ? "Loading repository symbols."
                : selection.repository
                  ? `No modeled code symbols returned for ${selection.repository.name}.`
                  : "No code symbol selected."}
          </div>
          {!selection.loading &&
          !busy &&
          !visibleGraphError &&
          selected &&
          visibleGraph.edges.length === 0 ? (
            <p className="t-mut" style={{ fontSize: ".78rem", margin: "8px 0 0" }}>
              No modeled code relationships returned for{" "}
              {selection.repository?.name ?? "this repository"}.
            </p>
          ) : null}
        </div>
        <Panel title="Analyzer">
          <div className="section-label">Selected symbol</div>
          {focusedNode ? (
            <div className="selected-code-node">
              <div
                className="row"
                style={{ justifyContent: "space-between", gap: 8, alignItems: "center" }}
              >
                <strong className="mono">{focusedNode.label}</strong>
                <Badge tone={focusedFinding?.type === "Dead code" ? "crit" : "neutral"}>
                  {focusedFinding?.classification ?? focusedNode.kind}
                </Badge>
              </div>
              <div className="kv-list" style={{ marginTop: 10 }}>
                <div className="kv">
                  <span>Repository</span>
                  <strong>{focusedRepository}</strong>
                </div>
                <div className="kv">
                  <span>Location</span>
                  {focusedSourceHref ? (
                    <Link className="mono" to={focusedSourceHref}>
                      {focusedLocation}
                    </Link>
                  ) : (
                    <strong className="mono">{focusedLocation}</strong>
                  )}
                </div>
                <div className="kv">
                  <span>Graph degree</span>
                  <strong>{focusedDegree}</strong>
                </div>
                <div className="kv">
                  <span>Evidence</span>
                  <strong>{focusedFinding?.truth ?? focusedNode.truth ?? "derived"}</strong>
                </div>
              </div>
              <div className="row" style={{ gap: 8, flexWrap: "wrap", marginTop: 12 }}>
                {focusedSourceHref ? (
                  <Link className="btn-ghost active" to={focusedSourceHref}>
                    Open source
                  </Link>
                ) : null}
                <Link className="btn-ghost" to={`/explorer?q=${encodeURIComponent(explorerQuery)}`}>
                  Explore repo graph
                </Link>
                {focusedFinding?.type === "Dead code" && focusedFinding.filePath ? (
                  <Link
                    className="btn-ghost"
                    to={`/dead-code?q=${encodeURIComponent(focusedFinding.filePath)}`}
                  >
                    Filter dead code
                  </Link>
                ) : null}
              </div>
              {focusedSourceStatus ? (
                <p className="t-mut" style={{ fontSize: ".78rem", margin: "8px 0 0" }}>
                  {focusedSourceStatus}
                </p>
              ) : null}
            </div>
          ) : (
            <p className="empty" style={{ textAlign: "left" }}>
              Click a graph node to inspect evidence and next actions.
            </p>
          )}

          <div className="section-label">Hotspots · most-referenced</div>
          <div className="kv-list">
            {hotspots.map((row) => (
              <div className="kv" key={row.id}>
                <span className="mono" style={{ fontSize: ".76rem" }}>
                  {row.label}
                </span>
                <strong>{row.count}</strong>
              </div>
            ))}
            {hotspots.length === 0 ? (
              <p className="empty" style={{ textAlign: "left" }}>
                No relationship hotspots returned.
              </p>
            ) : null}
          </div>
          <RelationshipTruthPanel graph={visibleGraph} coverage={visibleCoverage} />
          <ImportCyclesPanel state={visibleCycleState} />
          <div className="section-label" style={{ marginTop: 16 }}>
            Dead in this repo · {deadInRepo.length}
          </div>
          {deadInRepo.length ? (
            <div className="conn-list">
              {deadInRepo.map((finding) => (
                <button
                  type="button"
                  className="dead-row"
                  key={finding.id}
                  onClick={() => selectCandidate(finding.id)}
                >
                  <span className="mono">{symbolFromFinding(finding)}</span>
                  <span className="t-mut">{finding.classification ?? "candidate"}</span>
                </button>
              ))}
            </div>
          ) : (
            <p className="empty" style={{ padding: "6px 0", textAlign: "left" }}>
              No dead code in this repository.
            </p>
          )}
          <div className="section-label" style={{ marginTop: 16 }}>
            Scan window
          </div>
          <div className="kv-list">
            <div className="kv">
              <span>Dead candidates</span>
              <strong>{fmt(candidates.length)}</strong>
            </div>
            <div className="kv">
              <span>Selected repo</span>
              <strong>{deadInRepo.length}</strong>
            </div>
            {selection.truncated ? (
              <div className="kv">
                <span>Structural inventory</span>
                <strong>first 100 symbols</strong>
              </div>
            ) : null}
          </div>
        </Panel>
      </div>
    </div>
  );
}

function findingBelongsToRepository(
  finding: ConsoleModel["findings"][number],
  repository: RepoListItem | undefined,
): boolean {
  if (!repository) return false;
  const scope = finding.repoId?.trim() || finding.entity.trim();
  return scope === repository.id || scope === repository.name || scope === repository.repoSlug;
}
