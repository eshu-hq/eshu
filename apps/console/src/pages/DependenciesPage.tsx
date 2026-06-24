// pages/DependenciesPage.tsx
// Package dependency inventory browser backed by GET /api/v0/dependencies.
// Forward view: "what does this package depend on". Reverse view: "who depends
// on this package" (requires a package anchor). Bounded, keyset-paged, with
// honest empty/error states and truth/freshness chips.
import { useCallback, useEffect, useState } from "react";
import { useSearchParams } from "react-router-dom";

import type { EshuApiClient } from "../api/client";
import type { EshuTruth, TruthLevel } from "../api/envelope";
import type { DependencyRow, DependencyPage } from "../api/eshuConsoleLive";
import { loadDependencies } from "../api/eshuDependencies";
import { loadDependencyChains } from "../api/eshuDependencyChains";
import type { DependencyChain, DependencyChainPage } from "../api/eshuDependencyChains";
import { Panel, StatTile, Badge, TruthChip, FreshDot } from "../components/atoms";
import type { UiTruth, UiFresh } from "../console/types";
import "./liveInventory.css";

type Direction = "forward" | "reverse";
type Source = "loading" | "live" | "empty" | "unavailable";

const PAGE_LIMIT = 50;

// uiTruth maps the envelope truth level onto the chip vocabulary.
function uiTruth(level: TruthLevel | undefined): UiTruth {
  if (level === "exact") return "exact";
  if (level === "derived") return "derived";
  return "inferred";
}

// uiFresh maps the envelope freshness state onto the chip vocabulary.
function uiFresh(truth: EshuTruth | null): UiFresh {
  const state = truth?.freshness.state;
  if (state === "fresh") return "fresh";
  if (state === "building") return "lagging";
  return "stale";
}

export function DependenciesPage({
  client,
  sourceLabel = "live"
}: {
  readonly client?: EshuApiClient;
  readonly sourceLabel?: string;
}): React.JSX.Element {
  const [searchParams] = useSearchParams();
  const repoAnchor = searchParams.get("repo")?.trim() ?? "";
  if (repoAnchor !== "") {
    return <RepoDependencyChains client={client} repository={repoAnchor} sourceLabel={sourceLabel} />;
  }
  return <PackageDependencyBrowser client={client} sourceLabel={sourceLabel} />;
}

function PackageDependencyBrowser({
  client,
  sourceLabel = "live"
}: {
  readonly client?: EshuApiClient;
  readonly sourceLabel?: string;
}): React.JSX.Element {
  const [direction, setDirection] = useState<Direction>("forward");
  const [pkgInput, setPkgInput] = useState("");
  const [ecosystem, setEcosystem] = useState("");
  const [anchor, setAnchor] = useState<{ pkg: string; ecosystem: string } | null>(null);
  const [page, setPage] = useState<DependencyPage | null>(null);
  const [rows, setRows] = useState<readonly DependencyRow[]>([]);
  const [source, setSource] = useState<Source>("loading");
  const [err, setErr] = useState("");
  const [filter, setFilter] = useState("");

  const run = useCallback(
    async (dir: Direction, pkg: string, eco: string, cursor: DependencyPage["nextCursor"]) => {
      if (!client) { setSource("unavailable"); setPage(null); setRows([]); return; }
      setErr("");
      if (dir === "reverse" && pkg === "") {
        // Reverse needs a target anchor; show the empty-state guidance without
        // issuing a request that the API would reject with 400.
        setSource("empty"); setPage(null); setRows([]);
        return;
      }
      if (cursor === null) { setSource("loading"); setRows([]); setPage(null); }
      try {
        const result = await loadDependencies(client, {
          direction: dir,
          pkg: pkg || undefined,
          ecosystem: eco || undefined,
          afterName: cursor?.afterName,
          afterEdge: cursor?.afterEdge,
          limit: PAGE_LIMIT
        });
        setPage(result);
        setRows((prev) => (cursor === null ? result.rows : [...prev, ...result.rows]));
        setSource(result.rows.length === 0 && cursor === null ? "empty" : "live");
      } catch (e) {
        setErr(e instanceof Error ? e.message : "failed");
        setSource("unavailable"); setPage(null);
        if (cursor === null) setRows([]);
      }
    },
    [client]
  );

  // Initial load and reloads when the committed anchor or direction changes.
  useEffect(() => {
    void run(direction, anchor?.pkg ?? "", anchor?.ecosystem ?? "", null);
  }, [run, direction, anchor]);

  const submit = (e: React.FormEvent): void => {
    e.preventDefault();
    setAnchor({ pkg: pkgInput.trim(), ecosystem: ecosystem.trim() });
  };

  const visible = rows.filter((r) =>
    filter === "" ||
    `${r.relatedPackage} ${r.relatedPackageId} ${r.anchorPackage} ${r.range} ${r.dependencyType}`
      .toLowerCase()
      .includes(filter.toLowerCase())
  );
  const optionalCount = rows.filter((r) => r.optional).length;
  const relatedHeader = direction === "forward" ? "Depends on" : "Dependent";
  const anchorLabel = anchor?.pkg ? anchor.pkg : direction === "forward" ? "all packages" : "—";
  const sourceDisplay = source === "live" ? sourceLabel : source;

  return (
    <div className="page">
      <div className="page-intro">
        <h2>Dependencies</h2>
        <p>Package dependency inventory - <span className="mono">GET /api/v0/dependencies</span>. Forward lists what a package depends on; reverse lists who depends on it.</p>
      </div>

      <form className="evidence-toolbar" onSubmit={submit}>
        <div className="seg" role="group" aria-label="Direction">
          <button type="button" className={`btn-ghost${direction === "forward" ? " active" : ""}`} onClick={() => setDirection("forward")} aria-pressed={direction === "forward"}>Depends on</button>
          <button type="button" className={`btn-ghost${direction === "reverse" ? " active" : ""}`} onClick={() => setDirection("reverse")} aria-pressed={direction === "reverse"}>Dependents of</button>
        </div>
        <input className="popover-input mono" style={{ minWidth: 240 }} placeholder={direction === "reverse" ? "package name (required)" : "package name (optional)"} value={pkgInput} onChange={(e) => setPkgInput(e.target.value)} aria-label="Package name" />
        <input className="popover-input mono" style={{ minWidth: 120 }} placeholder="ecosystem" value={ecosystem} onChange={(e) => setEcosystem(e.target.value)} aria-label="Ecosystem" />
        <button type="submit" className="btn-ghost active">Look up</button>
      </form>

      <div className="grid g-4">
        <StatTile label="Edges" value={rows.length} color="var(--blue)" sub={page?.truncated ? "page truncated" : "complete page"} />
        <StatTile label="Direction" value={direction === "forward" ? "depends on" : "dependents of"} color="var(--teal)" sub={anchorLabel} />
        <StatTile label="Optional" value={optionalCount} color="var(--ember)" sub="optional edges" />
        <StatTile label="Source" value={sourceDisplay} color="var(--ember)" sub="dependency inventory" />
      </div>

      <div className="evidence-workbench evidence-workbench-rail mt" aria-label="Package graph workbench">
        <Panel className="flush" title={direction === "forward" ? "Forward dependencies" : "Reverse dependents"}
          sub={sourceDisplay}
          action={
            <div className="panel-action-stack">
              {page?.truth ? <TruthChip level={uiTruth(page.truth.level)} /> : null}
              {page?.truth ? <FreshDot state={uiFresh(page.truth)} /> : null}
              <div className="searchbox compact"><input placeholder="Filter rows…" value={filter} onChange={(e) => setFilter(e.target.value)} aria-label="Filter rows" /></div>
            </div>
          }>
          {source === "loading" ? (
            <div className="conn-state compact"><div className="conn-spinner" aria-hidden /><p>Loading dependencies...</p></div>
          ) : (
            <div className="table-scroll">
              <table className="tbl wide">
                <thead><tr><th>Anchor</th><th>Version</th><th>{relatedHeader}</th><th>Ecosystem</th><th>Range</th><th>Type</th><th>Optional</th></tr></thead>
                <tbody>
                  {visible.map((r) => (
                    <tr key={r.edgeId}>
                      <td className="t-name">{r.anchorPackage || "—"}</td>
                      <td className="t-mut mono" style={{ fontSize: ".76rem" }}>{r.declaringVersion || "—"}</td>
                      <td className="t-name mono" style={{ fontSize: ".82rem" }} title={r.relatedPackageId}>{r.relatedPackage}</td>
                      <td className="t-mut" style={{ fontSize: ".78rem" }}>{r.ecosystem || "—"}</td>
                      <td className="t-mut mono" style={{ fontSize: ".76rem" }}>{r.range || "—"}</td>
                      <td className="t-mut" style={{ fontSize: ".78rem" }}>{r.dependencyType || "—"}</td>
                      <td>{r.optional ? <Badge tone="warn">optional</Badge> : <span className="t-mut">no</span>}</td>
                    </tr>
                  ))}
                  {visible.length === 0 ? (
                    <tr><td colSpan={7} className="empty">{err ? `Failed to load: ${err}` : dependencyEmptyMessage(direction, anchor?.pkg ?? "")}</td></tr>
                  ) : null}
                </tbody>
              </table>
            </div>
          )}
          {page?.truncated && page.nextCursor ? (
            <div className="pager-row">
              <button className="btn-ghost" onClick={() => void run(direction, anchor?.pkg ?? "", anchor?.ecosystem ?? "", page.nextCursor)}>Load more</button>
            </div>
          ) : null}
        </Panel>
        <Panel title="Query context" sub="bounded package graph read">
          <dl className="surface-dl">
            <div><dt>Anchor</dt><dd className="mono">{anchorLabel}</dd></div>
            <div><dt>Rows loaded</dt><dd>{rows.length}</dd></div>
            <div><dt>Filtered rows</dt><dd>{visible.length}</dd></div>
            <div><dt>Page state</dt><dd>{page?.truncated ? "truncated" : source}</dd></div>
          </dl>
        </Panel>
      </div>
    </div>
  );
}

// RepoDependencyChains renders package-evidenced consumer-repo -> package ->
// publisher-repo chains for one repository. The consumption leg is canonical
// (manifest-backed); each publisher leg is an inferred, provenance-only link, so
// it is rendered with the inferred chip and never asserted as an exact
// repository dependency edge. Multiple candidate publishers render as ambiguous.
function RepoDependencyChains({
  client,
  repository,
  sourceLabel = "live"
}: {
  readonly client?: EshuApiClient;
  readonly repository: string;
  readonly sourceLabel?: string;
}): React.JSX.Element {
  const [page, setPage] = useState<DependencyChainPage | null>(null);
  const [source, setSource] = useState<Source>("loading");
  const [err, setErr] = useState("");

  useEffect(() => {
    let cancelled = false;
    if (!client) { setSource("unavailable"); setPage(null); return; }
    setSource("loading"); setErr("");
    void loadDependencyChains(client, repository)
      .then((result) => {
        if (cancelled) return;
        setPage(result);
        setSource(result.chains.length === 0 ? "empty" : "live");
      })
      .catch((e) => {
        if (cancelled) return;
        setErr(e instanceof Error ? e.message : "failed");
        setSource("unavailable"); setPage(null);
      });
    return () => { cancelled = true; };
  }, [client, repository]);

  const chains = page?.chains ?? [];
  const inferredPublishers = chains.reduce((sum, chain) => sum + chain.publishers.length, 0);
  const ambiguousCount = chains.filter((chain) => chain.ambiguous).length;
  const sourceDisplay = source === "live" ? sourceLabel : source;

  return (
    <div className="page">
      <div className="page-intro">
        <h2>Dependency chains</h2>
        <p>Package-evidenced repo-to-repo chains for <span className="mono">{repository}</span> - <span className="mono">GET /api/v0/package-registry/dependency-chains</span>. The consumption leg is canonical; publisher legs are inferred provenance-only links, not asserted repository edges.</p>
      </div>

      <div className="grid g-4">
        <StatTile label="Consumed packages" value={chains.length} color="var(--blue)" sub="canonical consumption" />
        <StatTile label="Inferred publishers" value={inferredPublishers} color="var(--ember)" sub="provenance-only links" />
        <StatTile label="Ambiguous" value={ambiguousCount} color="var(--violet)" sub="multiple candidate publishers" />
        <StatTile label="Source" value={sourceDisplay} color="var(--ember)" sub="dependency chains" />
      </div>

      <div className="evidence-workbench evidence-workbench-rail mt" aria-label="Dependency chain workbench">
        <Panel className="flush" title="Consumer → package → publisher"
          sub={sourceDisplay}
          action={
            <div className="panel-action-stack">
              {page?.truth ? <TruthChip level={uiTruth(page.truth.level)} /> : null}
              {page?.truth ? <FreshDot state={uiFresh(page.truth)} /> : null}
            </div>
          }>
          {source === "loading" ? (
            <div className="conn-state compact"><div className="conn-spinner" aria-hidden /><p>Loading dependency chains...</p></div>
          ) : (
            <div className="table-scroll">
              <table className="tbl wide">
                <thead><tr><th>Package</th><th>Ecosystem</th><th>Range</th><th>Publisher repo (inferred)</th></tr></thead>
                <tbody>
                  {chains.map((chain) => (
                    <ChainRows key={chain.consumptionCorrelationId || chain.packageId} chain={chain} />
                  ))}
                  {chains.length === 0 ? (
                    <tr><td colSpan={4} className="empty">{err ? `Failed to load: ${err}` : `No package-evidenced dependency chains for ${repository}. Requires admitted consumption correlations and package publisher hints.`}</td></tr>
                  ) : null}
                </tbody>
              </table>
            </div>
          )}
        </Panel>
        <Panel title="Truth model" sub="bounded read-side join">
          <dl className="surface-dl">
            <div><dt>Repository</dt><dd className="mono">{page?.repositoryId ?? repository}</dd></div>
            <div><dt>Consumption</dt><dd>canonical (manifest-backed)</dd></div>
            <div><dt>Publisher</dt><dd>inferred / provenance-only</dd></div>
            <div><dt>Page state</dt><dd>{page?.truncated ? "truncated" : source}</dd></div>
          </dl>
        </Panel>
      </div>
    </div>
  );
}

// ChainRows renders one consumed package and its publisher legs. A package with
// no publisher correlation terminates at the package (honest negative case); a
// package with multiple candidate publishers is marked ambiguous and never
// collapsed to a single asserted publisher.
function ChainRows({ chain }: { readonly chain: DependencyChain }): React.JSX.Element {
  if (chain.publishers.length === 0) {
    return (
      <tr>
        <td className="t-name mono" style={{ fontSize: ".82rem" }} title={chain.packageId}>{chain.packageName || chain.packageId || "—"}</td>
        <td className="t-mut" style={{ fontSize: ".78rem" }}>{chain.ecosystem || "—"}</td>
        <td className="t-mut mono" style={{ fontSize: ".76rem" }}>{chain.dependencyRange || "—"}</td>
        <td className="t-mut">no publisher correlation</td>
      </tr>
    );
  }
  return (
    <>
      {chain.publishers.map((publisher, index) => (
        <tr key={publisher.correlationId || `${chain.packageId}-${index}`}>
          <td className="t-name mono" style={{ fontSize: ".82rem" }} title={chain.packageId}>{index === 0 ? (chain.packageName || chain.packageId || "—") : ""}</td>
          <td className="t-mut" style={{ fontSize: ".78rem" }}>{index === 0 ? (chain.ecosystem || "—") : ""}</td>
          <td className="t-mut mono" style={{ fontSize: ".76rem" }}>{index === 0 ? (chain.dependencyRange || "—") : ""}</td>
          <td>
            <span className="row" style={{ gap: 6, alignItems: "center" }}>
              <span className="t-name">{publisher.repositoryName || publisher.repositoryId}</span>
              <TruthChip level="inferred" />
              {chain.ambiguous ? <Badge tone="warn">ambiguous</Badge> : null}
            </span>
          </td>
        </tr>
      ))}
    </>
  );
}

// dependencyEmptyMessage explains an empty result honestly for the active view.
function dependencyEmptyMessage(direction: Direction, pkg: string): string {
  if (direction === "reverse") {
    return pkg === ""
      ? "Enter a package name to find its dependents."
      : `No packages depend on ${pkg} in the indexed package graph.`;
  }
  return pkg === ""
    ? "No package dependencies in the indexed package graph yet - requires the package registry collector."
    : `${pkg} has no recorded dependencies in the indexed package graph.`;
}
