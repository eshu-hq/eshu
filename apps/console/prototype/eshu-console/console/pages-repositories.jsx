/* Eshu Console — demo repository browser pages. */
const { useState: useStateRepos, useMemo: useMemoRepos } = React;
function RepoGroups({ D, repos, q, onOpenRepo, onOpenService }) {
  const groups = useMemoRepos(() => deriveRepoGroups(D, repos), [D, repos]);
  const [expanded, setExpanded] = useStateRepos(null);
  const shown = groups.filter((g) => q === "" || g.system.toLowerCase().includes(q.toLowerCase()) || g.repos.some((r) => r.name.toLowerCase().includes(q.toLowerCase())));
  const totalCross = groups.reduce((a, g) => a + Object.keys(g.deps).length, 0);
  return (
    <>
      <div className="grid g-4">
        <StatTile label="Dependency groups" value={groups.length} color="var(--teal)" sub="clustered by system" />
        <StatTile label="Repositories" value={repos.length} color="var(--blue)" sub="across all groups" />
        <StatTile label="Cross-group links" value={totalCross} color="var(--ember)" sub="inter-system dependencies" />
        <StatTile label="Most depended-on" value={(groups.slice().sort((a, b) => Object.values(b.dependents).reduce((x, y) => x + y, 0) - Object.values(a.dependents).reduce((x, y) => x + y, 0))[0] || {}).system || "—"} color="var(--violet)" sub="hub group" />
      </div>
      <div className="repo-groups mt">
        {shown.map((g) => {
          const col = SYSTEM_COLOR[g.system] || "var(--accent)";
          const open = expanded === g.system;
          const depEntries = Object.entries(g.deps).sort((a, b) => b[1] - a[1]);
          const gg = open ? buildGroupGraph(D, repos, g) : null;
          return (
            <section className={cx("repo-group", open && "is-open")} key={g.system} id={"grp-" + g.system.replace(/\W/g, "")} style={{ "--gc": col }}>
              <header className="repo-group-head">
                <div className="row" style={{ gap: 9, minWidth: 0 }}>
                  <span className="repo-group-dot" style={{ background: col }} />
                  <h3>{g.system}</h3>
                  <span className="repo-group-count">{g.repos.length}</span>
                </div>
                <button type="button" className="findings-toggle" onClick={() => setExpanded(open ? null : g.system)}>{g.internal} internal {g.internal === 1 ? "link" : "links"} <span>{open ? "▾" : "▸"}</span></button>
              </header>
              <div className="repo-group-repos">
                {g.repos.map((r) => (
                  <button type="button" className="repo-chip" key={r.id} onClick={() => onOpenRepo(r.id)} title={r.slug}>
                    <i style={{ background: r.langColor }} />{r.name}<span className={"tag-tier tier-" + r.tier} style={{ marginLeft: 6 }}>{r.tier}</span>
                  </button>
                ))}
              </div>
              {depEntries.length ? (
                <div className="repo-group-deps">
                  <span className="rgd-label">depends on</span>
                  {depEntries.map(([sys, n]) => <button type="button" className="rgd-chip" key={sys} style={{ "--dc": SYSTEM_COLOR[sys] || "var(--muted)" }} onClick={() => { const el = document.getElementById("grp-" + sys.replace(/\W/g, "")); if (el) el.scrollIntoView({ block: "center" }); setExpanded(sys); }}><i />{sys}<em>{n}</em></button>)}
                </div>
              ) : <div className="repo-group-deps"><span className="rgd-label" style={{ color: "var(--teal)" }}>◆ foundational — no outbound dependencies</span></div>}
              {open && gg ? (
                <div className="node-hood" style={{ marginTop: 12 }}>
                  <GraphCanvas graph={gg} layout="radial" height={300} onSelect={(n) => { if (repos.find((x) => x.id === n.id)) onOpenRepo(n.id); else if (D.servicesById[n.id]) onOpenService(n.id); }} />
                </div>
              ) : null}
            </section>
          );
        })}
        {shown.length === 0 ? <p className="empty">No groups match.</p> : null}
      </div>
    </>
  );
}

function Repos({ data, onOpenService }) {
  const D = data || ESHU;
  const repos = useMemoRepos(() => deriveRepos(D), [D]);
  const [q, setQ] = useStateRepos("");
  const [lang, setLang] = useStateRepos("all");
  const [sort, setSort] = useStateRepos("updated");
  const [view, setView] = useStateRepos("groups");
  const [openRepo, setOpenRepo] = useStateRepos(null);
  const langs = Array.from(new Set(repos.map((r) => r.lang)));
  let rows = repos.filter((r) => (lang === "all" || r.lang === lang) && (q === "" || (r.name + r.slug + r.system + r.owner).toLowerCase().includes(q.toLowerCase())));
  rows = rows.slice().sort((a, b) => sort === "name" ? a.name.localeCompare(b.name) : sort === "facts" ? b.facts - a.facts : parseInt(a.updated) - parseInt(b.updated));
  const r = openRepo ? repos.find((x) => x.id === openRepo) : null;

  if (r) return <RepoDetail r={r} D={D} onBack={() => setOpenRepo(null)} onOpenService={onOpenService} />;

  return (
    <div className="page">
      <div className="page-intro"><h2>Repositories</h2><p>Every source repository Eshu has indexed. <strong>Groups</strong> clusters repos by system and draws the <span className="mono">@acme/*</span> dependencies between them; <strong>Grid</strong> browses them like your Git host. Select a repo to inspect its tree, branches and graph footprint.</p></div>
      <div className="repo-toolbar">
        <div className="searchbox" style={{ minWidth: 260, height: 38, margin: 0, flex: 1 }}><Icon.search size={16} /><input placeholder={view === "groups" ? "Find a group or repository…" : "Find a repository…"} value={q} onChange={(e) => setQ(e.target.value)} /></div>
        <div className="dep-toggle" style={{ margin: 0 }}>
          <button className={view === "groups" ? "active" : ""} onClick={() => setView("groups")}>Groups</button>
          <button className={view === "grid" ? "active" : ""} onClick={() => setView("grid")}>Grid</button>
        </div>
        {view === "grid" ? <div className="seg">{["all"].concat(langs).map((l) => <button key={l} className={lang === l ? "active" : ""} onClick={() => setLang(l)}>{l === "all" ? "All" : D.lang[l] && D.lang[l].label || l}</button>)}</div> : null}
        {view === "grid" ? <div className="seg">{[["updated", "Updated"], ["name", "Name"], ["facts", "Facts"]].map(([k, lbl]) => <button key={k} className={sort === k ? "active" : ""} onClick={() => setSort(k)}>{lbl}</button>)}</div> : null}
      </div>
      {view === "groups" ? <RepoGroups D={D} repos={repos} q={q} onOpenRepo={setOpenRepo} onOpenService={onOpenService} /> : (
      <div className="repo-grid">
        {rows.map((repo) =>
        <button type="button" className="repo-card" key={repo.id} onClick={() => setOpenRepo(repo.id)}>
            <div className="repo-card-top">
              <span className="repo-icon"><Icon.catalog size={16} /></span>
              <span className="repo-name">{repo.name}</span>
              <span className={"tag-tier tier-" + repo.tier}>{repo.tier}</span>
            </div>
            <p className="repo-desc">{repo.desc}</p>
            <div className="repo-meta">
              <span className="repo-lang"><i style={{ background: repo.langColor }} />{repo.langLabel}</span>
              <span title="branches"><Icon.branch size={13} /> {repo.branches}</span>
              <span title="open PRs">⇡ {repo.openPrs}</span>
              <span title="graph facts">◆ {fmt(repo.facts)}</span>
              <span className="repo-updated">{repo.updated} ago</span>
            </div>
          </button>
        )}
        {rows.length === 0 ? <p className="empty">No repositories match.</p> : null}
      </div>
      )}
    </div>
  );
}

/* all directory paths in a tree (for expand-all) */
function collectDirs(nodes, prefix, out) {
  prefix = prefix || ""; out = out || [];
  nodes.forEach((n) => { if (n.type === "dir") { const p = prefix ? prefix + "/" + n.name : n.name; out.push(p); collectDirs(n.items || [], p, out); } });
  return out;
}
/* ancestor dir paths of a file path (for breadcrumb reveal) */
function ancestorDirs(filePath) {
  const segs = filePath.split("/"); const out = []; let acc = "";
  for (let i = 0; i < segs.length - 1; i++) { acc = acc ? acc + "/" + segs[i] : segs[i]; out.push(acc); }
  return out;
}

function RepoDetail({ r, D, onBack, onOpenService }) {
  const tree = useMemoRepos(() => repoTree(r), [r]);
  const allDirs = useMemoRepos(() => collectDirs(tree), [tree]);
  const [branch, setBranch] = useStateRepos(r.defaultBranch);
  const [view, setView] = useStateRepos("files");
  const readme = useMemoRepos(() => findFile(tree, "README.md"), [tree]);
  const [file, setFile] = useStateRepos(() => readme);
  const [openPaths, setOpenPaths] = useStateRepos(() => new Set(["src"]));
  const treeRef = React.useRef(null);
  const branchNames = ["main", "develop", "release/" + r.version, "feat/graph-sync"].slice(0, Math.min(4, r.branches));
  const svc = D.services.find((x) => x.id === r.id);
  const lines = (file && file.content || "").split("\n");

  function toggle(path) { setOpenPaths((s) => { const n = new Set(s); n.has(path) ? n.delete(path) : n.add(path); return n; }); }
  function expandAll() { setOpenPaths(new Set(allDirs)); }
  function collapseAll() { setOpenPaths(new Set()); }
  function revealPath(filePath) { setOpenPaths((s) => { const n = new Set(s); ancestorDirs(filePath).forEach((d) => n.add(d)); return n; }); }
  function selectFile(f) { setFile(f); revealPath(f._path || f.name); }
  function onTreeKeyNav(e) {
    if (e.key !== "ArrowDown" && e.key !== "ArrowUp") return;
    const items = [...treeRef.current.querySelectorAll("[role=treeitem]")];
    const idx = items.indexOf(document.activeElement);
    e.preventDefault();
    const next = e.key === "ArrowDown" ? Math.min(items.length - 1, idx + 1) : Math.max(0, idx - 1);
    items[next] && items[next].focus();
  }
  const crumbs = file ? (file._path || file.name).split("/") : [];

  return (
    <div className="page">
      <button className="repo-back" onClick={onBack}><Icon.arrow size={14} style={{ transform: "rotate(180deg)" }} /> All repositories</button>
      <div className="repo-detail-head">
        <div>
          <div className="row" style={{ gap: 10, flexWrap: "wrap" }}>
            <span className="repo-icon"><Icon.catalog size={18} /></span>
            <h2 style={{ fontFamily: "var(--mono)", fontSize: "1.3rem" }}>{r.name}</h2>
            <span className={"tag-tier tier-" + r.tier}>{r.tier}</span>
            <TruthChip level={r.truth} /><FreshDot state={r.freshness} />
          </div>
          <p className="t-mut mono" style={{ fontSize: ".8rem", margin: "8px 0 0" }}>{r.host}:{r.slug}</p>
        </div>
        <button className="btn-ghost active" onClick={() => onOpenService(r.id)}>Open service spotlight →</button>
      </div>
      <p style={{ color: "var(--muted)", lineHeight: 1.6, maxWidth: "80ch" }}>{svc ? svc.story : r.desc}</p>

      <div className="repo-stats">
        <div><dt>Language</dt><dd><span className="repo-lang"><i style={{ background: r.langColor }} />{r.langLabel}</span></dd></div>
        <div><dt>Branches</dt><dd>{r.branches}</dd></div>
        <div><dt>Open PRs</dt><dd>{r.openPrs}</dd></div>
        <div><dt>Open issues</dt><dd>{r.issues}</dd></div>
        <div><dt>Graph facts</dt><dd>{fmt(r.facts)}</dd></div>
        <div><dt>Coverage</dt><dd>{Math.round(r.coverage * 100)}%</dd></div>
      </div>

      <div className="repo-browser">
        <div className="repo-browser-head">
          <div className="seg branch-seg"><Icon.branch size={14} />{branchNames.map((b) => <button key={b} className={branch === b ? "active" : ""} onClick={() => setBranch(b)}>{b}</button>)}</div>
          <span className="t-mut mono" style={{ fontSize: ".74rem" }}>{r.version ? "v" + r.version : ""} · indexed {r.updated} ago</span>
        </div>
        <div className="tree-toolbar">
          <div className="dep-toggle" style={{ margin: 0 }}>
            <button className={view === "files" ? "active" : ""} onClick={() => setView("files")}>Files</button>
            <button className={view === "deps" ? "active" : ""} onClick={() => setView("deps")}>Dependency tree</button>
          </div>
          {view === "files" ? (
            <div className="seg">
              <button className="tree-btn" onClick={expandAll}>⊞ Expand all</button>
              <button className="tree-btn" onClick={collapseAll}>⊟ Collapse all</button>
            </div>
          ) : null}
        </div>
        {view === "files" ? (
          <div className="repo-split">
            <div className="repo-tree" role="tree" ref={treeRef} onKeyDown={onTreeKeyNav}>
              {tree.map((node, i) => <TreeRow key={i} node={node} depth={0} path="" openPaths={openPaths} onToggle={toggle} onSelectFile={selectFile} onJump={() => onOpenService(r.id)} activePath={file && file._path} />)}
            </div>
            <div className="code-pane">
              {file ? (
                <>
                  <div className="code-head">
                    <span className="code-crumbs">
                      {crumbs.map((c, i) => <React.Fragment key={i}>{i > 0 ? <span className="cc-sep">/</span> : null}{i === crumbs.length - 1 ? <span className="cc-cur">{c}</span> : <button onClick={() => revealPath(file._path)}>{c}</button>}</React.Fragment>)}
                    </span>
                    <span className="t-mut mono" style={{ fontSize: ".72rem" }}>{lines.length} lines · {file.name.split(".").pop()}</span>
                  </div>
                  <div className="code-body">
                    <pre className="code-pre">{lines.map((ln, i) => <div className="code-line" key={i}><span className="code-ln">{i + 1}</span><code>{ln || " "}</code></div>)}</pre>
                  </div>
                </>
              ) : <p className="empty">Select a file to view its contents.</p>}
            </div>
          </div>
        ) : (
          <div style={{ padding: "16px var(--pad)" }}>
            <p className="t-mut" style={{ fontSize: ".8rem", margin: "0 0 12px", lineHeight: 1.5 }}>Internal <span className="mono">@acme/*</span> import graph rooted at <span className="mono">{r.name}</span> — every node is clickable to open that service, repeated branches collapse and cycles are flagged.</p>
            {svc ? <DepTree rootId={r.id} D={D} onOpenService={onOpenService} /> : <p className="empty">No dependency manifest indexed.</p>}
          </div>
        )}
      </div>

      {r.deps && r.deps.length ?
      <Panel className="mt" title="Internal dependencies" sub="Imports resolved from the manifest" glyph={<Icon.branch />}>
        <div className="row wrap" style={{ gap: 8 }}>{r.deps.map((d) => <button key={d} className="dep-chip" onClick={() => onOpenService(d)}><i style={{ width: 6, height: 6, borderRadius: 9, background: "var(--teal)" }} />{D.servicesById && D.servicesById[d] && D.servicesById[d].name || d}</button>)}</div>
      </Panel> : null}
    </div>
  );
}

/* recursive, collapsible internal-dependency tree — every node opens its service */
function DepTree({ rootId, D, onOpenService }) {
  const [open, setOpen] = useStateRepos({});
  function render(id, depth, ancestry, keyPath) {
    const svc = D.servicesById[id];
    if (!svc) return null;
    const deps = svc.deps || [];
    const cycle = ancestry.includes(id);
    const hasChildren = deps.length > 0 && !cycle;
    const isOpen = keyPath in open ? open[keyPath] : depth < 1;
    return (
      <React.Fragment key={keyPath}>
        <div className="deptree-row" style={{ paddingLeft: 8 + depth * 18 }}>
          {hasChildren ?
            <button type="button" className="deptree-glyph" onClick={() => setOpen((o) => ({ ...o, [keyPath]: !isOpen }))} title="Expand">{isOpen ? "▾" : "▸"}</button> :
            <span className="deptree-glyph">·</span>}
          <span className="deptree-dot" style={{ background: svc.kind === "lib" ? "#c4b59a" : svc.kind === "web" ? "var(--blue)" : svc.kind === "job" ? "var(--violet)" : "var(--teal)" }} />
          <button type="button" className="deptree-name" style={{ background: "none", border: 0, padding: 0, font: "inherit", cursor: "pointer", flex: 1, textAlign: "left" }} onClick={() => onOpenService(id)}>{svc.name}</button>
          {cycle ? <span className="deptree-cycle">cycle ↺</span> : <span className="deptree-tier">{svc.tier === "lib" ? "lib" : svc.tier}{deps.length ? " · " + deps.length : ""}</span>}
        </div>
        {hasChildren && isOpen ? deps.map((d) => render(d, depth + 1, ancestry.concat(id), keyPath + "/" + d)) : null}
      </React.Fragment>
    );
  }
  return <div className="deptree">{render(rootId, 0, [], rootId)}</div>;
}

/* find a file node by name anywhere in the tree (for default README) */
function findFile(nodes, target, prefix) {
  prefix = prefix || "";
  for (const n of nodes) {
    const p = prefix ? prefix + "/" + n.name : n.name;
    if (n.type === "file" && n.name === target) return Object.assign({}, n, { _path: p });
    if (n.type === "dir" && n.items) { const f = findFile(n.items, target, p); if (f) return f; }
  }
  return null;
}

function TreeRow({ node, depth, path, openPaths, onToggle, onSelectFile, onJump, activePath }) {
  const fullPath = path ? path + "/" + node.name : node.name;
  const isDir = node.type === "dir";
  const open = isDir && openPaths.has(fullPath);
  const isActive = !isDir && activePath === fullPath;
  function activate() { isDir ? onToggle(fullPath) : onSelectFile(Object.assign({}, node, { _path: fullPath })); }
  function onKey(e) {
    if (e.key === "Enter" || e.key === " ") { e.preventDefault(); activate(); }
    else if (e.key === "ArrowRight" && isDir && !open) { e.preventDefault(); onToggle(fullPath); }
    else if (e.key === "ArrowLeft" && isDir && open) { e.preventDefault(); onToggle(fullPath); }
  }
  return (
    <>
      <div className={"tree-row" + (isDir ? " tree-dir" : " tree-file") + (isActive ? " is-active" : "")} style={{ paddingLeft: 12 + depth * 18 }}
        role="treeitem" tabIndex={0} aria-expanded={isDir ? open : undefined} onClick={activate} onKeyDown={onKey}>
        <span className="tree-glyph">{isDir ? open ? "▾" : "▸" : ""}</span>
        <span className="tree-icon">{isDir ? open ? "📂" : "📁" : "📄"}</span>
        <span className="tree-name">{node.name}</span>
        {!isDir ? <button type="button" className="tree-jump" onClick={(e) => { e.stopPropagation(); onJump(fullPath, node); }} title="Open this service's graph footprint">graph ↗</button> : node.note ? <span className="tree-note">{node.note}</span> : null}
      </div>
      {isDir && open && node.items ? node.items.map((c, i) => <TreeRow key={i} node={c} depth={depth + 1} path={fullPath} openPaths={openPaths} onToggle={onToggle} onSelectFile={onSelectFile} onJump={onJump} activePath={activePath} />) : null}
    </>
  );
}

Object.assign(window, { Repos });
