// pages/RepositoriesPage.tsx
// Repository-centric browser: live repo list (GET /api/v0/repositories) with a
// filter, and per-repo detail (stats + story highlights). No fabricated file
// tree or contents here — source browsing is the separate code-viewer page.
import { useEffect, useState } from "react";
import { Link } from "react-router-dom";
import type { EshuApiClient } from "../api/client";
import { loadRepositories, loadRepositoryDetail } from "../api/repoCatalog";
import type { RepoDetail, RepoListItem } from "../api/repoCatalog";
import type { ConsoleModel } from "../console/types";
import { Panel, StatTile, Badge } from "../components/atoms";
import "./repositories.css";
import "./liveInventory.css";

type RepoView = "groups" | "grid";

interface RepoGroup {
  readonly key: string;
  readonly source: string;
  readonly truth: string;
  readonly kind: string;
  readonly reason: string;
  readonly repositories: readonly RepoListItem[];
}

export function RepositoriesPage({ client, model }: {
  readonly client?: EshuApiClient;
  readonly model?: ConsoleModel;
}): React.JSX.Element {
  const [repos, setRepos] = useState<readonly RepoListItem[] | null>(null);
  const [err, setErr] = useState("");
  const [q, setQ] = useState("");
  const [view, setView] = useState<RepoView>("groups");
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

  const query = q.trim().toLowerCase();
  const groups = filterRepositoryGroups(repositoryGroups(repos ?? []), query);
  const rows = groups.flatMap((group) => group.repositories);
  const dependencyCount = rows.filter((row) => row.isDependency).length;
  const mostPopulated = groups[0]?.key ?? "—";
  const sourceLabel = model?.source === "live" ? "live API" : "component model";

  return (
    <div className="page">
      <div className="page-intro">
        <h2>Repositories</h2>
        <p>Groups use source-backed repository grouping evidence from the live repository inventory. Rows without grouping evidence stay explicit. Grid browses repositories like a Git host.</p>
      </div>

      <div className="repo-toolbar">
        <div className="searchbox repo-search">
          <input aria-label="Find a group or repository" placeholder="Find a group or repository…" value={q} onChange={(e) => setQ(e.target.value)} />
        </div>
        <div className="seg" role="group" aria-label="Repository view">
          <button className={view === "groups" ? "active" : ""} onClick={() => setView("groups")}>Groups</button>
          <button className={view === "grid" ? "active" : ""} onClick={() => setView("grid")}>Grid</button>
        </div>
      </div>

      <div className="grid g-4">
        <StatTile label="Repository groups" value={groups.length} color="var(--teal)" sub="source-backed grouping" />
        <StatTile label="Repositories" value={rows.length} color="var(--blue)" sub={sourceLabel} />
        <Link to="/dependencies" className="repo-dependency-tile" aria-label="View dependency chains in the Dependencies view">
          <StatTile label="Dependency repos" value={dependencyCount} color="var(--ember)" sub="depended on by other repos →" />
        </Link>
        <StatTile label="Most populated" value={mostPopulated} color="var(--violet)" sub="largest live group" />
      </div>

      {repos === null ? (
        <div className="conn-state compact"><div className="conn-spinner" aria-hidden /><p>Loading repositories…</p></div>
      ) : view === "groups" ? (
        <div className="repo-group-grid mt" aria-label="Repository group workbench">
          {groups.map((group, index) => (
            <RepositoryGroupCard key={group.key} group={group} accent={groupAccent(index)} />
          ))}
          {groups.length === 0 ? <Panel><p className="empty">{err ? `Failed to load: ${err}` : "No repositories from this source."}</p></Panel> : null}
        </div>
      ) : (
        <div className="evidence-workbench evidence-workbench-wide mt" aria-label="Repository grid workbench">
          <Panel className="flush" title={`${rows.length} repositories`} sub="live">
            <div className="table-scroll">
              <table className="tbl wide">
                <thead><tr><th>Repository</th><th>Group</th><th>Slug</th><th>Kind</th></tr></thead>
                <tbody>
                  {rows.map((r) => (
                    <tr key={r.id} className={selected === r.id ? "is-sel" : undefined} onClick={() => setSelected(r.id)}>
                      <td className="t-name">{r.name}</td>
                      <td className="t-mut mono" style={{ fontSize: ".76rem" }}>{repositoryGroupKey(r)}</td>
                      <td className="t-mut mono" style={{ fontSize: ".76rem" }}>{r.repoSlug || "—"}</td>
                      <td>{r.isDependency ? <Badge tone="neutral">dependency</Badge> : <Badge tone="teal">source</Badge>}</td>
                    </tr>
                  ))}
                  {rows.length === 0 ? <tr><td colSpan={4} className="empty">{err ? `Failed to load: ${err}` : "No repositories from this source."}</td></tr> : null}
                </tbody>
              </table>
            </div>
          </Panel>

          <Panel title="Repository detail" sub={detail ? detail.name : "select a repository"}>
            {!selected ? (
              <p className="empty">Select a repository to see its stats and story.</p>
            ) : detailBusy || !detail ? (
              <div className="conn-state compact"><div className="conn-spinner" aria-hidden /><p>Loading detail…</p></div>
            ) : detail.provenance === "unavailable" ? (
              <p className="empty">Repository detail unavailable from this source.</p>
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
                <div className="row" style={{ marginTop: 14, gap: 8, flexWrap: "wrap" }}>
                  <Link to={`/repositories/${encodeURIComponent(detail.id)}/source`} className="btn-ghost active">Browse source →</Link>
                  <Link to={`/dependencies?repo=${encodeURIComponent(detail.id)}`} className="btn-ghost">Dependency chains →</Link>
                </div>
              </>
            )}
          </Panel>
        </div>
      )}
    </div>
  );
}

function RepositoryGroupCard({ group, accent }: {
  readonly group: RepoGroup;
  readonly accent: string;
}): React.JSX.Element {
  const dependencies = group.repositories.filter((repo) => repo.isDependency).length;
  return (
    <article className="repo-group-card" style={{ "--repo-accent": accent } as React.CSSProperties}>
      <div className="repo-group-head">
        <div className="row">
          <i className="repo-group-dot" />
          <h3>{group.key}</h3>
          <span className="repo-count">{group.repositories.length}</span>
        </div>
        <span className="repo-link-count mono">{dependencies} dependency repos</span>
      </div>
      <div className="repo-chip-grid">
        {group.repositories.slice(0, 8).map((repo) => (
          <Link key={repo.id} className="repo-chip" to={repositorySourcePath(repo.id)}>
            <span>{repo.name}</span>
            <Badge tone={repo.isDependency ? "neutral" : "teal"}>{repo.isDependency ? "dep" : "src"}</Badge>
          </Link>
        ))}
      </div>
      <div className="repo-group-foot">
        <span title={group.reason}>{groupEvidenceNote(group)}</span>
      </div>
    </article>
  );
}

function repositorySourcePath(id: string): string {
  return `/repositories/${encodeURIComponent(id)}/source`;
}

function repositoryGroups(repositories: readonly RepoListItem[]): readonly RepoGroup[] {
  const grouped = new Map<string, RepoListItem[]>();
  for (const repository of repositories) {
    const key = repositoryGroupKey(repository);
    grouped.set(key, [...(grouped.get(key) ?? []), repository]);
  }
  return [...grouped.entries()]
    .map(([key, groupRepos]) => ({
      key,
      source: repositoryGroupSource(groupRepos),
      truth: repositoryGroupTruth(groupRepos),
      kind: repositoryGroupKind(groupRepos),
      reason: repositoryGroupReason(groupRepos),
      repositories: groupRepos
    }))
    .sort((a, b) => b.repositories.length - a.repositories.length || a.key.localeCompare(b.key));
}

function filterRepositoryGroups(groups: readonly RepoGroup[], query: string): readonly RepoGroup[] {
  if (query === "") return groups;
  const filtered: RepoGroup[] = [];
  for (const group of groups) {
    if (group.key.toLowerCase().includes(query)) {
      filtered.push(group);
      continue;
    }
    const repositories = group.repositories.filter((repository) => repositoryMatchesQuery(repository, query));
    if (repositories.length > 0) filtered.push({ ...group, repositories });
  }
  return filtered;
}

function repositoryMatchesQuery(repository: RepoListItem, query: string): boolean {
  return `${repository.name} ${repository.repoSlug}`.toLowerCase().includes(query);
}

function repositoryGroupKey(repository: RepoListItem): string {
  return repository.groupKey.trim() || "Grouping evidence missing";
}

function repositoryGroupSource(repositories: readonly RepoListItem[]): string {
  return firstGroupField(repositories, (repository) => repository.groupSource) || "missing_evidence";
}

function repositoryGroupTruth(repositories: readonly RepoListItem[]): string {
  return firstGroupField(repositories, (repository) => repository.groupTruth) || "missing_evidence";
}

function repositoryGroupKind(repositories: readonly RepoListItem[]): string {
  return firstGroupField(repositories, (repository) => repository.groupKind) || "unknown";
}

function repositoryGroupReason(repositories: readonly RepoListItem[]): string {
  return firstGroupField(repositories, (repository) => repository.groupReason) || "No source-backed grouping evidence is available.";
}

function firstGroupField(repositories: readonly RepoListItem[], select: (repository: RepoListItem) => string): string {
  for (const repository of repositories) {
    const value = select(repository).trim();
    if (value !== "") return value;
  }
  return "";
}

function groupAccent(index: number): string {
  const colors = ["var(--teal)", "var(--ember)", "var(--blue)", "var(--violet)", "var(--med)"];
  return colors[index % colors.length];
}

function groupEvidenceNote(group: RepoGroup): string {
  return `${group.truth} · ${group.source}`;
}
