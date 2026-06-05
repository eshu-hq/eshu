// pages/RepositoriesPage.tsx
// Repository-centric browser: live repo list (GET /api/v0/repositories) with a
// filter, and per-repo detail (stats + story highlights). No fabricated file
// tree or contents here — source browsing is the separate code-viewer page.
import { useEffect, useState } from "react";
import type { EshuApiClient } from "../api/client";
import { loadRepositories, loadRepositoryDetail } from "../api/repoCatalog";
import type { RepoDetail, RepoListItem } from "../api/repoCatalog";
import { Panel, StatTile, Badge } from "../components/atoms";

export function RepositoriesPage({ client }: { readonly client?: EshuApiClient }): React.JSX.Element {
  const [repos, setRepos] = useState<readonly RepoListItem[] | null>(null);
  const [err, setErr] = useState("");
  const [q, setQ] = useState("");
  const [selected, setSelected] = useState<string | null>(null);
  const [detail, setDetail] = useState<RepoDetail | null>(null);
  const [detailBusy, setDetailBusy] = useState(false);

  useEffect(() => {
    let cancelled = false;
    if (!client) { setRepos([]); return; }
    setErr("");
    void loadRepositories(client)
      .then((r) => { if (!cancelled) setRepos(r); })
      .catch((e) => { if (!cancelled) { setRepos([]); setErr(e instanceof Error ? e.message : "failed"); } });
    return () => { cancelled = true; };
  }, [client]);

  useEffect(() => {
    let cancelled = false;
    if (!client || !selected) { setDetail(null); return; }
    setDetailBusy(true);
    void loadRepositoryDetail(client, selected).then((d) => {
      if (!cancelled) { setDetail(d); setDetailBusy(false); }
    });
    return () => { cancelled = true; };
  }, [client, selected]);

  const rows = (repos ?? []).filter((r) => q === "" || `${r.name} ${r.repoSlug}`.toLowerCase().includes(q.toLowerCase()));

  return (
    <div className="page">
      <div className="page-intro"><h2>Repositories</h2><p>Indexed repositories from <span className="mono">GET /api/v0/repositories</span>. Select a repository for its stats and story.</p></div>

      <div className="grid" style={{ gridTemplateColumns: "minmax(0,1fr) minmax(0,1.4fr)", gap: "var(--gap)" }}>
        <Panel className="flush" title={`${rows.length} repositories`} sub={repos === null ? "loading…" : "live"}
          action={<div className="searchbox" style={{ minWidth: 200, height: 34 }}><input placeholder="Filter repositories…" value={q} onChange={(e) => setQ(e.target.value)} /></div>}>
          {repos === null ? (
            <div className="conn-state" style={{ padding: 40 }}><div className="conn-spinner" aria-hidden /><p>Loading repositories…</p></div>
          ) : (
            <table className="tbl">
              <thead><tr><th>Repository</th><th>Slug</th><th>Kind</th></tr></thead>
              <tbody>
                {rows.map((r) => (
                  <tr key={r.id} onClick={() => setSelected(r.id)} style={{ cursor: "pointer", background: selected === r.id ? "var(--bg-raised)" : undefined }}>
                    <td className="t-name">{r.name}</td>
                    <td className="t-mut mono" style={{ fontSize: ".76rem" }}>{r.repoSlug || "—"}</td>
                    <td>{r.isDependency ? <Badge tone="neutral">dependency</Badge> : <Badge tone="teal">source</Badge>}</td>
                  </tr>
                ))}
                {rows.length === 0 ? <tr><td colSpan={3} className="empty">{err ? `Failed to load: ${err}` : "No repositories from this source."}</td></tr> : null}
              </tbody>
            </table>
          )}
        </Panel>

        <Panel title="Repository detail" sub={detail ? detail.name : "select a repository"}>
          {!selected ? (
            <p className="empty" style={{ padding: 28 }}>Select a repository to see its stats and story.</p>
          ) : detailBusy || !detail ? (
            <div className="conn-state" style={{ padding: 40 }}><div className="conn-spinner" aria-hidden /><p>Loading detail…</p></div>
          ) : detail.provenance === "unavailable" ? (
            <p className="empty" style={{ padding: 28 }}>Repository detail unavailable from this source.</p>
          ) : (
            <>
              <div className="grid g-2">
                <StatTile label="Files" value={detail.stats.fileCount ?? "—"} color="var(--teal)" sub={detail.stats.coverageState} />
                <StatTile label="Entities" value={detail.stats.entityCount ?? "—"} color="var(--blue)" sub={`${detail.stats.entityTypes.length} types`} />
              </div>
              <div className="section-label" style={{ marginTop: 14 }}>Languages</div>
              <div className="row" style={{ gap: 6, flexWrap: "wrap" }}>
                {detail.stats.languages.length ? detail.stats.languages.map((l) => <Badge key={l} tone="neutral">{l}</Badge>) : <span className="t-mut">—</span>}
              </div>
              {detail.highlights.length ? (
                <>
                  <div className="section-label" style={{ marginTop: 14 }}>Story highlights</div>
                  <ul className="plain-list">{detail.highlights.slice(0, 8).map((h, i) => <li key={i} className="t-mut">{h}</li>)}</ul>
                </>
              ) : null}
              <p className="t-mut" style={{ fontSize: ".72rem", marginTop: 14 }}>Source browsing (file tree + contents) is the code viewer — pending the repository content API.</p>
            </>
          )}
        </Panel>
      </div>
    </div>
  );
}
