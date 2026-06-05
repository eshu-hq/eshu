// pages/RepoSourcePage.tsx
// File-tree + code-viewer for a repository, wired to the merged tree (#1431) and
// content (#1432) endpoints. The tree/content reflect the single indexed ref;
// the multi-branch selector is gated on the branches API (#1433) and shown as a
// disabled note until then. No fabricated tree or contents.
import { useEffect, useState } from "react";
import { Link, useParams } from "react-router-dom";
import type { EshuApiClient } from "../api/client";
import { decodeRepoFile, loadRepoFile, loadRepoTree } from "../api/repoSource";
import type { RepoFile, RepoTree } from "../api/repoSource";
import { Panel, Badge } from "../components/atoms";

export function RepoSourcePage({ client }: { readonly client?: EshuApiClient }): React.JSX.Element {
  const { id = "" } = useParams<{ id: string }>();
  const [path, setPath] = useState("");
  const [tree, setTree] = useState<RepoTree | null>(null);
  const [treeErr, setTreeErr] = useState("");
  const [file, setFile] = useState<RepoFile | null>(null);
  const [fileBusy, setFileBusy] = useState(false);

  useEffect(() => {
    let cancelled = false;
    if (!client) { setTree(null); setTreeErr("requires a live connection"); return; }
    setTree(null); setTreeErr("");
    void loadRepoTree(client, id, path)
      .then((t) => { if (!cancelled) setTree(t); })
      .catch((e) => { if (!cancelled) setTreeErr(e instanceof Error ? e.message : "failed"); });
    return () => { cancelled = true; };
  }, [client, id, path]);

  function openFile(filePath: string): void {
    if (!client) return;
    setFileBusy(true); setFile(null);
    void loadRepoFile(client, id, filePath).then((f) => { setFile(f); setFileBusy(false); });
  }

  const crumbs = path ? path.split("/") : [];

  return (
    <div className="page" style={{ maxWidth: "none" }}>
      <div className="page-intro">
        <Link to="/repositories" className="link-btn">← Repositories</Link>
        <h2 style={{ marginTop: 8 }}>{id} <span className="t-mut" style={{ fontSize: "0.8rem", fontWeight: 400 }}>· source</span></h2>
        <p>File tree + viewer from <span className="mono">/repositories/{"{id}"}/tree</span> and <span className="mono">/content</span>. Branch selection is pending the branches API (#1433); showing the indexed ref{tree?.ref ? <> <Badge tone="neutral">{tree.ref.slice(0, 10)}</Badge></> : null}.</p>
      </div>

      <div className="explorer-filters" style={{ gap: 4 }}>
        <button className="link-btn" onClick={() => setPath("")}>root</button>
        {crumbs.map((c, i) => (
          <span key={i}><span className="t-mut">/</span> <button className="link-btn" onClick={() => setPath(crumbs.slice(0, i + 1).join("/"))}>{c}</button></span>
        ))}
      </div>

      <div className="grid" style={{ gridTemplateColumns: "minmax(0,1fr) minmax(0,2fr)", gap: "var(--gap)" }}>
        <Panel className="flush" title="Files" sub={tree ? `${tree.entries.length} entries${tree.truncated ? " (truncated)" : ""}` : "loading…"}>
          {treeErr ? <p className="empty" style={{ padding: 20 }}>Failed to load tree: {treeErr}</p>
            : !tree ? <div className="conn-state" style={{ padding: 32 }}><div className="conn-spinner" aria-hidden /><p>Loading tree…</p></div>
              : (
                <table className="tbl">
                  <tbody>
                    {tree.entries.map((e) => (
                      <tr key={e.path} style={{ cursor: "pointer" }} onClick={() => e.type === "dir" ? setPath(e.path) : openFile(e.path)}>
                        <td className="t-name">{e.type === "dir" ? "📁 " : "📄 "}{e.name}</td>
                        <td className="t-mut mono" style={{ fontSize: ".72rem", textAlign: "right" }}>{e.type === "dir" ? `${e.childCount ?? 0} files` : e.size != null ? `${e.size} lines` : ""}</td>
                      </tr>
                    ))}
                    {tree.entries.length === 0 ? <tr><td className="empty">Empty directory.</td></tr> : null}
                  </tbody>
                </table>
              )}
        </Panel>

        <Panel className="flush" title={file ? file.path : "Viewer"} sub={file?.language ?? (fileBusy ? "loading…" : "select a file")}>
          {fileBusy ? <div className="conn-state" style={{ padding: 40 }}><div className="conn-spinner" aria-hidden /><p>Loading file…</p></div>
            : !file ? <p className="empty" style={{ padding: 28 }}>Select a file to view its source.</p>
              : file.provenance === "unavailable" ? <p className="empty" style={{ padding: 28 }}>File content unavailable from this source.</p>
                : renderFile(file)}
        </Panel>
      </div>
    </div>
  );
}

function renderFile(file: RepoFile): React.JSX.Element {
  const { text, binary } = decodeRepoFile(file);
  if (binary) return <p className="empty" style={{ padding: 28 }}>Binary file ({file.size} bytes) — not shown.</p>;
  const lines = text.split("\n");
  return (
    <div className="code-view">
      {file.truncated ? <div className="prov-banner warn" style={{ padding: "6px 12px" }}>Truncated to the size cap.</div> : null}
      <pre className="code-pre"><code>{lines.map((ln, i) => (
        <span key={i} className="code-line"><span className="code-ln">{i + 1}</span>{ln}{"\n"}</span>
      ))}</code></pre>
    </div>
  );
}
