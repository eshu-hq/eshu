/* Eshu Console — prototype repositories live parity overlay.
   Demo mode delegates to the original rich prototype repo browser. Live mode
   uses the production repository API family instead of catalog-derived rows. */
(function () {
  const DemoRepos = window.Repos;
  const { useEffect: useEffectR, useMemo: useMemoR, useState: useStateR } = React;

  function repoData(response) {
    if (response && response.error) throw new Error(response.error.message || response.error.code || "api error");
    return response && response.data && response.error !== undefined ? response.data : response;
  }

  function repoText(value) {
    return typeof value === "string" && value.trim() ? value.trim() : "";
  }

  function repoNumber(value) {
    return typeof value === "number" && Number.isFinite(value) ? value : null;
  }

  function repoSlugLeaf(slug) {
    const parts = slug.split(/[\\/]/).filter(Boolean);
    return parts.length ? parts[parts.length - 1] : "";
  }

  function repoDisplayName(repo) {
    const name = repoText(repo.name);
    if (name && !name.startsWith("repository:")) return name;
    return repoSlugLeaf(repoText(repo.repo_slug)) || name || repoText(repo.id);
  }

  function repoGroupKey(repo) {
    const name = repo.name.toLowerCase();
    const slugGroup = repo.repoSlug.split("/").find((part) => part.trim().length > 0);
    if (name.startsWith("lib-") || name === "dmm-clients") return "Shared Libraries";
    if (name.includes("forex")) return "FX";
    if (
      name === "api-node-boats" ||
      name === "api-node-boats-temp" ||
      name === "api-node-external-search" ||
      name === "api-node-saved-search" ||
      name === "api-node-make-model" ||
      name === "job-node-sitemaps-generator"
    ) return "Boat-Search";
    if (name.includes("fsbo")) return "FSBO";
    if (name.includes("conversation")) return "Messaging";
    if (name.includes("datax")) return "Data";
    if (name.includes("platform") || name.includes("provisioning") || name.includes("salesforce")) return "Platform";
    if (name.includes("boattrader") || name.includes("myboats") || name.includes("bw-home") || name === "boatsdotcom") return "Marketplace";
    if (name === "configd") return "Configuration";
    if (name.startsWith("iac-") || name.startsWith("terraform-") || name === "helm-charts") return "Cloud Platform";
    if (slugGroup) return titleGroup(slugGroup);
    return repo.isDependency ? "Dependencies" : "Unclassified";
  }

  function titleGroup(value) {
    return value
      .split(/[-_\s]+/)
      .filter(Boolean)
      .map((part) => part.slice(0, 1).toUpperCase() + part.slice(1))
      .join(" ");
  }

  async function loadLiveRepos(client) {
    const data = repoData(await client.get("/api/v0/repositories?limit=500&offset=0")) || {};
    return (data.repositories || []).map((repo) => ({
      id: repoText(repo.id) || repoText(repo.name),
      isDependency: repo.is_dependency === true,
      name: repoDisplayName(repo),
      remoteUrl: repoText(repo.remote_url),
      repoSlug: repoText(repo.repo_slug)
    })).filter((repo) => repo.id);
  }

  async function loadRepoDetail(client, id) {
    try {
      const stats = repoData(await client.get("/api/v0/repositories/" + encodeURIComponent(id) + "/stats")) || {};
      let highlights = [];
      try {
        const story = repoData(await client.get("/api/v0/repositories/" + encodeURIComponent(id) + "/story")) || {};
        const raw = story.highlights || story.sections || [];
        highlights = raw.map((item) => typeof item === "string" ? item : repoText(item && item.title)).filter(Boolean);
      } catch (_) {}
      return {
        id,
        name: repoText(stats.repository && stats.repository.name) || id,
        fileCount: repoNumber(stats.file_count),
        entityCount: repoNumber(stats.entity_count),
        languages: Array.isArray(stats.languages) ? stats.languages : [],
        entityTypes: Array.isArray(stats.entity_types) ? stats.entity_types : [],
        coverage: repoText(stats.coverage && stats.coverage.source_backend) || "unavailable",
        highlights,
        unavailable: false
      };
    } catch (_) {
      return { id, name: id, fileCount: null, entityCount: null, languages: [], entityTypes: [], coverage: "unavailable", highlights: [], unavailable: true };
    }
  }

  function groupRepos(repos, query) {
    const groups = new Map();
    repos.forEach((repo) => {
      const key = repoGroupKey(repo);
      if (!groups.has(key)) groups.set(key, []);
      groups.get(key).push(repo);
    });
    return Array.from(groups.entries())
      .map(([key, repositories]) => ({ key, repositories: repositories.filter((repo) => query === "" || (key + repo.name + repo.repoSlug).toLowerCase().includes(query)) }))
      .filter((group) => group.repositories.length)
      .sort((a, b) => b.repositories.length - a.repositories.length || a.key.localeCompare(b.key));
  }

  function Repos({ data, client, onOpenService }) {
    if (!client) return <DemoRepos data={data} onOpenService={onOpenService} />;
    return <LiveRepos client={client} />;
  }

  function LiveRepos({ client }) {
    const [repos, setRepos] = useStateR(null);
    const [err, setErr] = useStateR("");
    const [q, setQ] = useStateR("");
    const [view, setView] = useStateR("groups");
    const [selected, setSelected] = useStateR("");
    const [detail, setDetail] = useStateR(null);

    useEffectR(() => {
      let cancelled = false;
      setErr("");
      loadLiveRepos(client).then((rows) => { if (!cancelled) setRepos(rows); })
        .catch((e) => { if (!cancelled) { setRepos([]); setErr((e && e.message) || "failed"); } });
      return () => { cancelled = true; };
    }, [client]);

    useEffectR(() => {
      let cancelled = false;
      if (!selected) { setDetail(null); return () => { cancelled = true; }; }
      setDetail(null);
      loadRepoDetail(client, selected).then((next) => { if (!cancelled) setDetail(next); });
      return () => { cancelled = true; };
    }, [client, selected]);

    const query = q.trim().toLowerCase();
    const groups = useMemoR(() => groupRepos(repos || [], query), [repos, query]);
    const rows = groups.flatMap((group) => group.repositories);
    const depCount = rows.filter((repo) => repo.isDependency).length;

    return (
      <div className="page">
        <div className="page-intro"><h2>Repositories</h2><p>Live repository list from <span className="mono">GET /api/v0/repositories</span>. Select a repository to read its stats and story.</p></div>
        <div className="repo-toolbar">
          <div className="searchbox" style={{ minWidth: 260, height: 38, margin: 0, flex: 1 }}><Icon.search size={16} /><input placeholder="Find a group or repository..." value={q} onChange={(e) => setQ(e.target.value)} /></div>
          <div className="dep-toggle" style={{ margin: 0 }}><button className={view === "groups" ? "active" : ""} onClick={() => setView("groups")}>Groups</button><button className={view === "grid" ? "active" : ""} onClick={() => setView("grid")}>Grid</button></div>
        </div>
        <div className="grid g-4">
          <StatTile label="Repository groups" value={groups.length} color="var(--teal)" sub="clustered by domain evidence" />
          <StatTile label="Repositories" value={rows.length} color="var(--blue)" sub="GET /api/v0/repositories" />
          <StatTile label="Dependency repos" value={depCount} color="var(--ember)" sub="marked by the API" />
          <StatTile label="Most populated" value={(groups[0] && groups[0].key) || "-"} color="var(--violet)" sub="largest live group" />
        </div>
        {repos === null ? <div className="conn-state compact"><div className="conn-spinner" aria-hidden /><p>Loading repositories...</p></div> : null}
        {repos !== null && view === "groups" ? <LiveRepoGroups groups={groups} onSelect={setSelected} err={err} /> : null}
        {repos !== null && view === "grid" ? <LiveRepoGrid rows={rows} selected={selected} onSelect={setSelected} detail={detail} err={err} /> : null}
      </div>
    );
  }

  function LiveRepoGroups({ groups, onSelect, err }) {
    return <div className="repo-groups mt">{groups.map((group) => (
      <section className="repo-group" key={group.key}>
        <header className="repo-group-head"><div className="row" style={{ gap: 9 }}><span className="repo-group-dot" /><h3>{group.key}</h3><span className="repo-group-count">{group.repositories.length}</span></div><span className="findings-toggle">{group.repositories.filter((repo) => repo.isDependency).length} dependency repos</span></header>
        <div className="repo-group-repos">{group.repositories.map((repo) => <a className="repo-chip" key={repo.id} href={repoSourceHref(repo.id)} title={repo.repoSlug || repo.remoteUrl}><i />{repo.name}<span className={"tag-tier tier-" + (repo.isDependency ? "3" : "1")} style={{ marginLeft: 6 }}>{repo.isDependency ? "dep" : "src"}</span></a>)}</div>
      </section>
    ))}{groups.length === 0 ? <p className="empty">{err ? "Failed to load: " + err : "No repositories from this source."}</p> : null}</div>;
  }

  function repoSourceHref(id) {
    return window.ESHU_ROUTES.hashFor("reposource", "/" + encodeURIComponent(id) + "/source");
  }

  function LiveRepoGrid({ rows, selected, onSelect, detail, err }) {
    return <div className="evidence-workbench evidence-workbench-wide mt">
      <Panel className="flush" title={rows.length + " repositories"} sub="live API">
        <table className="tbl"><thead><tr><th>Repository</th><th>Group</th><th>Slug</th><th>Kind</th></tr></thead><tbody>{rows.map((repo) => <tr key={repo.id} className={selected === repo.id ? "is-sel" : ""} onClick={() => onSelect(repo.id)}><td className="t-name">{repo.name}</td><td className="mono t-mut">{repoGroupKey(repo)}</td><td className="mono t-mut">{repo.repoSlug || "-"}</td><td><Badge tone={repo.isDependency ? "neutral" : "teal"}>{repo.isDependency ? "dependency" : "source"}</Badge></td></tr>)}{rows.length === 0 ? <tr><td colSpan={4} className="empty">{err ? "Failed to load: " + err : "No repositories from this source."}</td></tr> : null}</tbody></table>
      </Panel>
      <Panel title="Repository detail" sub={detail ? detail.name : "select a repository"}>
        {!selected ? <p className="empty">Select a repository to see its stats and story.</p> : !detail ? <div className="conn-state compact"><div className="conn-spinner" aria-hidden /><p>Loading detail...</p></div> : detail.unavailable ? <p className="empty">Repository detail unavailable from this source.</p> : <RepoDetailCard detail={detail} />}
      </Panel>
    </div>;
  }

  function RepoDetailCard({ detail }) {
    return <><div className="grid g-2"><StatTile label="Files" value={detail.fileCount == null ? "-" : detail.fileCount} color="var(--teal)" sub={detail.coverage} /><StatTile label="Entities" value={detail.entityCount == null ? "-" : detail.entityCount} color="var(--blue)" sub={detail.entityTypes.length + " types"} /></div><div className="section-label" style={{ marginTop: 14 }}>Languages</div><div className="row" style={{ gap: 6, flexWrap: "wrap" }}>{detail.languages.length ? detail.languages.map((lang) => <Badge key={lang} tone="neutral">{lang}</Badge>) : <span className="t-mut">-</span>}</div>{detail.highlights.length ? <><div className="section-label" style={{ marginTop: 14 }}>Story highlights</div><ul className="plain-list">{detail.highlights.slice(0, 8).map((h, i) => <li key={i} className="t-mut">{h}</li>)}</ul></> : null}<div style={{ marginTop: 14 }}><a className="btn-ghost active" href={window.ESHU_ROUTES.hashFor("reposource", "/" + encodeURIComponent(detail.id) + "/source")}>Browse source</a></div></>;
  }

  Object.assign(window, { Repos });
})();
