// pages/RepoSourcePage.tsx
// File-tree + code-viewer for a repository, wired to the merged tree (#1431) and
// content (#1432) endpoints plus source-backed branch refs (#1433). No
// fabricated tree, refs, or contents.
import { useEffect, useState } from "react";
import { Link, useNavigate, useParams, useSearchParams } from "react-router-dom";

import type { EshuApiClient } from "../api/client";
import { loadRepositoryNameMap } from "../api/repoCatalog";
import { decodeRepoFile, loadRepoBranches, loadRepoFile, loadRepoTree } from "../api/repoSource";
import type { RepoBranch, RepoBranches, RepoFile, RepoTree } from "../api/repoSource";
import { Panel, Badge } from "../components/atoms";

export function RepoSourcePage({ client }: { readonly client?: EshuApiClient }): React.JSX.Element {
  const { id = "" } = useParams<{ id: string }>();
  const navigate = useNavigate();
  const [searchParams] = useSearchParams();
  const requestedFile = searchParams.get("path") ?? "";
  const selectedRef = searchParams.get("ref") ?? "";
  const highlightStart = parseLineParam(searchParams.get("lineStart"));
  const highlightEnd = parseLineParam(searchParams.get("lineEnd")) ?? highlightStart;
  const [path, setPath] = useState(parentPath(requestedFile));
  const [tree, setTree] = useState<RepoTree | null>(null);
  const [treeErr, setTreeErr] = useState("");
  const [file, setFile] = useState<RepoFile | null>(null);
  const [fileBusy, setFileBusy] = useState(false);
  const [repositoryLabel, setRepositoryLabel] = useState(id);
  const [branches, setBranches] = useState<RepoBranches | null>(null);
  const [branchesErr, setBranchesErr] = useState("");
  const [langFilter, setLangFilter] = useState("");

  useEffect(() => {
    let cancelled = false;
    setRepositoryLabel(id);
    if (!client || id === "")
      return () => {
        cancelled = true;
      };
    void loadRepositoryNameMap(client)
      .then((repoNames) => {
        if (!cancelled) setRepositoryLabel(repoNames.get(id) ?? id);
      })
      .catch(() => {
        if (!cancelled) setRepositoryLabel(id);
      });
    return () => {
      cancelled = true;
    };
  }, [client, id]);

  useEffect(() => {
    let cancelled = false;
    if (!client) {
      setTree(null);
      setTreeErr("requires a live connection");
      return;
    }
    setTree(null);
    setTreeErr("");
    void loadRepoTree(client, id, path, selectedRef)
      .then((t) => {
        if (!cancelled) setTree(t);
      })
      .catch((e) => {
        if (!cancelled) setTreeErr(e instanceof Error ? e.message : "failed");
      });
    return () => {
      cancelled = true;
    };
  }, [client, id, path, selectedRef]);

  useEffect(() => {
    let cancelled = false;
    if (!client) {
      setBranches(null);
      setBranchesErr("requires a live connection");
      return;
    }
    setBranches(null);
    setBranchesErr("");
    void loadRepoBranches(client, id)
      .then((refs) => {
        if (!cancelled) setBranches(refs);
      })
      .catch((e) => {
        if (!cancelled) setBranchesErr(e instanceof Error ? e.message : "failed");
      });
    return () => {
      cancelled = true;
    };
  }, [client, id]);

  useEffect(() => {
    let cancelled = false;
    if (!client || requestedFile === "")
      return () => {
        cancelled = true;
      };
    setPath(parentPath(requestedFile));
    setFileBusy(true);
    setFile(null);
    void loadRepoFile(client, id, requestedFile, selectedRef).then((f) => {
      if (!cancelled) {
        setFile(f);
        setFileBusy(false);
      }
    });
    return () => {
      cancelled = true;
    };
  }, [client, id, requestedFile, selectedRef]);

  function openFile(filePath: string): void {
    const params = new URLSearchParams({ path: filePath });
    if (selectedRef) params.set("ref", selectedRef);
    navigate(`/repositories/${encodeURIComponent(id)}/source?${params.toString()}`);
  }

  function selectRef(ref: string): void {
    const params = new URLSearchParams(searchParams);
    if (ref) params.set("ref", ref);
    else params.delete("ref");
    const query = params.toString();
    navigate(`/repositories/${encodeURIComponent(id)}/source${query ? `?${query}` : ""}`);
  }

  const crumbs = path ? path.split("/") : [];
  const branchOptions =
    branches?.branches.filter((branch) => branch.name !== "" || branch.headSha !== "") ?? [];
  const namedBranches = branchOptions.filter((branch) => branch.name !== "");
  const selectedBranchValue =
    selectedRef ||
    branchValueForTree(branchOptions, tree?.ref ?? "", branches?.defaultBranch ?? "");
  const selectedBranch = branchByValue(branchOptions, selectedBranchValue);
  const indexedRef = tree?.ref || selectedBranch?.headSha || branchOptions[0]?.headSha || "";
  const indexedBranchName =
    selectedBranch?.name || branchOptions[0]?.name || branches?.defaultBranch || "";
  const lastIndexedAt = selectedBranch?.lastIndexedAt ?? branchOptions[0]?.lastIndexedAt ?? null;

  return (
    <div className="page" style={{ maxWidth: "none" }}>
      <div className="page-intro">
        <Link to="/repositories" className="link-btn">
          ← Repositories
        </Link>
        <h2 style={{ marginTop: 8 }}>
          {repositoryLabel}{" "}
          <span className="t-mut" style={{ fontSize: "0.8rem", fontWeight: 400 }}>
            · source
          </span>
        </h2>
        <p>
          File tree + viewer from <span className="mono">/repositories/{"{id}"}/tree</span>,{" "}
          <span className="mono">/content</span>, and <span className="mono">/branches</span>.
        </p>
        <div className="explorer-filters" style={{ gap: 8, marginTop: 10 }}>
          <span className="t-mut">Indexed ref</span>
          {indexedRef ? (
            <Badge tone="neutral">{indexedRef.slice(0, 10)}</Badge>
          ) : (
            <Badge tone="neutral">unavailable</Badge>
          )}
          {namedBranches.length > 1 ? (
            <label className="t-mut">
              Branch
              <select
                aria-label="Repository ref"
                className="code-repo-select mono"
                value={selectedBranchValue}
                onChange={(event) => selectRef(event.target.value)}
                style={{ marginLeft: 6 }}
              >
                {branchOptions.map((branch) => (
                  <option
                    key={`${branch.name}:${branch.headSha}`}
                    value={branchSelectorValue(branch)}
                  >
                    {branchOptionLabel(branch)}
                  </option>
                ))}
              </select>
            </label>
          ) : null}
          {indexedBranchName ? <span className="t-mut mono">{indexedBranchName}</span> : null}
          {lastIndexedAt ? (
            <span className="t-mut mono">{new Date(lastIndexedAt).toLocaleString()}</span>
          ) : null}
          {branchesErr ? <span className="t-mut">ref list unavailable: {branchesErr}</span> : null}
        </div>
      </div>

      <div className="explorer-filters" style={{ gap: 4 }}>
        <button className="link-btn" onClick={() => setPath("")}>
          root
        </button>
        {crumbs.map((c, i) => (
          <span key={i}>
            <span className="t-mut">/</span>{" "}
            <button className="link-btn" onClick={() => setPath(crumbs.slice(0, i + 1).join("/"))}>
              {c}
            </button>
          </span>
        ))}
      </div>

      <div
        className="grid"
        style={{ gridTemplateColumns: "minmax(0,1fr) minmax(0,2fr)", gap: "var(--gap)" }}
      >
        <Panel
          className="flush"
          title="Files"
          sub={
            tree
              ? `${tree.entries.length} entries${tree.truncated ? " (truncated)" : ""}`
              : "loading…"
          }
        >
          {treeErr ? (
            <p className="empty" style={{ padding: 20 }}>
              Failed to load tree: {treeErr}
            </p>
          ) : !tree ? (
            <div className="conn-state" style={{ padding: 32 }}>
              <div className="conn-spinner" aria-hidden />
              <p>Loading tree…</p>
            </div>
          ) : (
            (() => {
              const fileLanguages = [
                ...new Set(
                  tree.entries.flatMap((e) => (e.type === "file" && e.language ? [e.language] : [])),
                ),
              ].sort();
              const visibleEntries = tree.entries.filter(
                (e) => langFilter === "" || e.type === "dir" || e.language === langFilter,
              );
              return (
                <>
                  {fileLanguages.length > 0 ? (
                    <div className="searchbox" style={{ padding: "8px 12px" }}>
                      <label htmlFor="tree-lang-filter" className="t-mut" style={{ fontSize: ".72rem" }}>
                        Language
                      </label>{" "}
                      <select
                        id="tree-lang-filter"
                        aria-label="Filter files by language"
                        value={langFilter}
                        onChange={(ev) => setLangFilter(ev.target.value)}
                      >
                        <option value="">All</option>
                        {fileLanguages.map((lang) => (
                          <option key={lang} value={lang}>
                            {lang}
                          </option>
                        ))}
                      </select>
                    </div>
                  ) : null}
                  <table className="tbl">
                    <tbody>
                      {visibleEntries.map((e) => (
                        <tr
                          key={e.path}
                          style={{ cursor: "pointer" }}
                          onClick={() => (e.type === "dir" ? setPath(e.path) : openFile(e.path))}
                        >
                          <td className="t-name">
                            {e.type === "dir" ? "📁 " : "📄 "}
                            {e.name}
                            {e.type === "file" && e.language ? (
                              <>
                                {" "}
                                <Badge tone="violet">{e.language}</Badge>
                              </>
                            ) : null}
                          </td>
                          <td className="t-mut mono" style={{ fontSize: ".72rem", textAlign: "right" }}>
                            {e.type === "dir"
                              ? `${e.childCount ?? 0} files`
                              : e.size != null
                                ? `${e.size} lines`
                                : ""}
                          </td>
                        </tr>
                      ))}
                      {visibleEntries.length === 0 ? (
                        <tr>
                          <td className="empty">
                            {langFilter === "" ? "Empty directory." : `No ${langFilter} files here.`}
                          </td>
                        </tr>
                      ) : null}
                    </tbody>
                  </table>
                </>
              );
            })()
          )}
        </Panel>

        <Panel
          className="flush"
          title={file ? file.path : "Viewer"}
          sub={file?.language ?? (fileBusy ? "loading…" : "select a file")}
        >
          {fileBusy ? (
            <div className="conn-state" style={{ padding: 40 }}>
              <div className="conn-spinner" aria-hidden />
              <p>Loading file…</p>
            </div>
          ) : !file ? (
            <p className="empty" style={{ padding: 28 }}>
              Select a file to view its source.
            </p>
          ) : file.provenance === "unavailable" ? (
            <p className="empty" style={{ padding: 28 }}>
              File content unavailable from this source.
            </p>
          ) : (
            renderFile(
              file,
              file.path === requestedFile
                ? { start: highlightStart, end: highlightEnd }
                : emptyHighlight,
            )
          )}
        </Panel>
      </div>
    </div>
  );
}

interface HighlightRange {
  readonly start: number | null;
  readonly end: number | null;
}

const emptyHighlight: HighlightRange = { start: null, end: null };

function parentPath(filePath: string): string {
  const idx = filePath.lastIndexOf("/");
  return idx > 0 ? filePath.slice(0, idx) : "";
}

function parseLineParam(value: string | null): number | null {
  if (value === null) return null;
  const line = Number(value);
  return Number.isInteger(line) && line > 0 ? line : null;
}

function branchSelectorValue(branch: RepoBranch): string {
  return branch.name || branch.headSha;
}

function branchByValue(branches: readonly RepoBranch[], value: string): RepoBranch | null {
  return branches.find((branch) => branch.name === value || branch.headSha === value) ?? null;
}

function branchValueForTree(
  branches: readonly RepoBranch[],
  treeRef: string,
  defaultBranch: string,
): string {
  const indexedBranch = branches.find((branch) => treeRef !== "" && branch.headSha === treeRef);
  if (indexedBranch) return branchSelectorValue(indexedBranch);
  const defaultBranchRow = branches.find(
    (branch) => defaultBranch !== "" && branch.name === defaultBranch,
  );
  if (defaultBranchRow) return branchSelectorValue(defaultBranchRow);
  return branches[0] ? branchSelectorValue(branches[0]) : "";
}

function branchOptionLabel(branch: RepoBranch): string {
  const label = branch.name || branch.headSha;
  return branch.headSha ? `${label} · ${branch.headSha.slice(0, 10)}` : label;
}

function isHighlighted(line: number, range: HighlightRange): boolean {
  if (range.start === null) return false;
  return line >= range.start && line <= (range.end ?? range.start);
}

function renderFile(file: RepoFile, highlight: HighlightRange): React.JSX.Element {
  const { text, binary } = decodeRepoFile(file);
  if (binary)
    return (
      <p className="empty" style={{ padding: 28 }}>
        Binary file ({file.size} bytes) — not shown.
      </p>
    );
  const lines = text.split("\n");
  return (
    <div className="code-view" tabIndex={0}>
      {file.truncated ? (
        <div className="prov-banner warn" style={{ padding: "6px 12px" }}>
          Truncated to the size cap.
        </div>
      ) : null}
      <pre className="code-pre">
        <code>
          {lines.map((ln, i) => (
            <span
              key={i}
              id={`L${i + 1}`}
              data-testid={`source-line-${i + 1}`}
              className={`code-line${isHighlighted(i + 1, highlight) ? " is-highlighted" : ""}`}
            >
              <span className="code-ln">{i + 1}</span>
              {ln}
              {"\n"}
            </span>
          ))}
        </code>
      </pre>
    </div>
  );
}
