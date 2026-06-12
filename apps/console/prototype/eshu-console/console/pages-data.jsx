/* Eshu Console — data pages: Explorer, Catalog, Findings, Vulnerabilities, Admin. */
const { useState: useStateD, useMemo: useMemoD } = React;

/* ================================================================== REPOS */
/* GitHub/GitLab-like repository browser, derived from the indexed services. */
function deriveRepos(D) {
  const langDays = { ts: 2, go: 5, py: 9, rust: 14, java: 21 };
  return D.services.filter((s) => s.repo).map((s) => {
    const branches = 3 + (s.callers % 9);
    const openPrs = (s.high + s.med) % 7;
    const issues = s.crit + s.high;
    const li = D.lang[s.lang] || { label: s.lang, color: "#9aa4af" };
    return {
      id: s.id, name: s.repo.split("/").pop(), slug: s.repo, host: s.host || "bitbucket",
      desc: s.story ? s.story.split(". ")[0] + "." : s.name,
      lang: s.lang, langLabel: li.label, langColor: li.color,
      kind: s.kind, system: s.system, owner: s.owner, tier: s.tier,
      version: s.version, branches, openPrs, issues, facts: 1200 + s.callers * 137 + s.calls * 53,
      updated: (langDays[s.lang] || 7) + (s.id.length % 6) + "h", coverage: s.coverage,
      truth: s.truth, freshness: s.freshness, deps: s.deps, defaultBranch: "main"
    };
  });
}

/* a believable, deeply-nested file tree with file contents for a repo */
function repoTree(r) {
  const ext = r.lang === "go" ? "go" : r.lang === "py" ? "py" : r.lang === "rust" ? "rs" : r.lang === "java" ? "java" : "ts";
  const svc = r.name.replace(/^api-node-|^job-node-/, "");
  const mod = svc.replace(/-/g, "_");
  const cmt = (t) => ext === "py" ? "# " + t : "// " + t;
  const F = (name, content) => ({ type: "file", name, content });

  const handler =
`${cmt(r.slug + " · " + name(svc) + " route")}
import { Router } from "express";
import { ${mod}Service } from "../services/${svc}.service";

export const router = Router();

router.get("/${svc}", async (req, res) => {
  const result = await ${mod}Service.search(req.query);
  res.json({ data: result, truth: { level: "exact" } });
});

router.get("/${svc}/:id", async (req, res) => {
  const item = await ${mod}Service.byId(req.params.id);
  if (!item) return res.status(404).json({ error: { code: "not_found" } });
  res.json({ data: item });
});`;

  const service =
`${cmt(name(svc) + " service — core business logic")}
import { db } from "../lib/db";
import { logger } from "../lib/logger";
import type { ${cap(mod)} } from "../models/${svc}";

export const ${mod}Service = {
  async search(query: Record<string, unknown>): Promise<${cap(mod)}[]> {
    logger.info("search", { query });
    return db.${mod}.findMany({ where: query, take: 50 });
  },
  async byId(id: string): Promise<${cap(mod)} | null> {
    return db.${mod}.findUnique({ where: { id } });
  }
};`;

  const model =
`${cmt(name(svc) + " domain model")}
export interface ${cap(mod)} {
  id: string;
  name: string;
  status: "active" | "archived";
  createdAt: string;
  updatedAt: string;
}`;

  const indexFile =
`${cmt("entrypoint — wires the HTTP server")}
import { app } from "./app";

const port = process.env.PORT ?? ${r.port || 3000};
app.listen(port, () => console.log("${r.name} listening on " + port));`;

  const appFile =
`${cmt("express app + middleware")}
import express from "express";
import { router } from "./routes";

export const app = express();
app.use(express.json());
app.use("/v1", router);
app.get("/healthz", (_req, res) => res.json({ ok: true }));`;

  const pkg =
`{
  "name": "@dmm/${r.name}",
  "version": "${r.version || "1.0.0"}",
  "private": true,
  "scripts": { "dev": "tsx watch src/index.ts", "test": "vitest", "build": "tsc" },
  "dependencies": {
${(r.deps || []).slice(0, 4).map((d) => `    "@dmm/${d}": "workspace:*"`).join(",\n") || '    "express": "^4.19.0"'}
  }
}`;

  const dockerfile =
`FROM node:20-alpine AS build
WORKDIR /app
COPY package*.json ./
RUN npm ci
COPY . .
RUN npm run build

FROM node:20-alpine
WORKDIR /app
COPY --from=build /app/dist ./dist
EXPOSE ${r.port || 3000}
CMD ["node", "dist/index.js"]`;

  const ci =
`name: ci
on: [push, pull_request]
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-node@v4
        with: { node-version: 20 }
      - run: npm ci
      - run: npm test
      - run: npm run build`;

  const values =
`# ArgoCD / Helm values for ${r.name}
replicaCount: 3
image:
  repository: ecr/${r.name}
  tag: "${r.version || "1.0.0"}"
service:
  port: ${r.port || 3000}
resources:
  requests: { cpu: 100m, memory: 256Mi }
  limits:   { cpu: 500m, memory: 512Mi }`;

  const readme =
`# ${r.name}

${r.desc}

- **Owner:** ${r.owner}
- **System:** ${r.system}
- **Language:** ${r.langLabel}
- **Runtime:** EKS · namespace \`api-node\`

## Development
\`\`\`
npm install
npm run dev
\`\`\``;

  const test =
`${cmt(name(svc) + " service tests")}
import { describe, it, expect } from "vitest";
import { ${mod}Service } from "../src/services/${svc}.service";

describe("${mod}Service", () => {
  it("returns results for a query", async () => {
    const r = await ${mod}Service.search({});
    expect(Array.isArray(r)).toBe(true);
  });
});`;

  return [
  { type: "dir", name: "src", items: [
    { type: "dir", name: "routes", items: [
      F("index." + ext, handler),
      F(svc + "." + ext, handler),
      F("health." + ext, cmt("liveness + readiness probes") + "\nexport const health = () => ({ ok: true });") ] },
    { type: "dir", name: "services", items: [
      F(svc + ".service." + ext, service),
      { type: "dir", name: "providers", items: [
        F("elasticsearch." + ext, cmt("Elasticsearch client") + "\nimport { Client } from \"@elastic/elasticsearch\";\nexport const es = new Client({ node: process.env.ES_URL });"),
        F("cache." + ext, cmt("ElastiCache / Redis") + "\nimport Redis from \"ioredis\";\nexport const cache = new Redis(process.env.REDIS_URL);") ] } ] },
    { type: "dir", name: "models", items: [F(svc + "." + ext, model)] },
    { type: "dir", name: "lib", items: [
      F("logger." + ext, cmt("structured logger") + "\nexport const logger = { info: (m, x) => console.log(JSON.stringify({ m, ...x })) };"),
      F("db." + ext, cmt("db client (Prisma)") + "\nimport { PrismaClient } from \"@prisma/client\";\nexport const db = new PrismaClient();") ] },
    F("index." + ext, indexFile),
    F("app." + ext, appFile) ] },
  { type: "dir", name: "test", items: [F(svc + ".test." + ext, test)] },
  { type: "dir", name: ".github", items: [
    { type: "dir", name: "workflows", items: [F("ci.yml", ci)] } ] },
  { type: "dir", name: "helm", items: [F("values.yaml", values), F("Chart.yaml", "apiVersion: v2\nname: " + r.name + "\nversion: " + (r.version || "1.0.0"))] },
  F(r.lang === "go" ? "go.mod" : r.lang === "py" ? "pyproject.toml" : r.lang === "rust" ? "Cargo.toml" : "package.json", pkg),
  F("Dockerfile", dockerfile),
  F("README.md", readme)];

}
function name(s) { return s.replace(/-/g, " "); }
function cap(s) { return s.charAt(0).toUpperCase() + s.slice(1); }

/* group repos into dependency clusters by system, with cross-group dependency counts */
function deriveRepoGroups(D, repos) {
  const sysOf = (sid) => { const r = repos.find((x) => x.id === sid); if (r) return r.system; const svc = D.servicesById[sid]; return svc ? svc.system : null; };
  const groups = {};
  repos.forEach((r) => { (groups[r.system] = groups[r.system] || { system: r.system, repos: [], internal: 0, deps: {}, dependents: {} }).repos.push(r); });
  repos.forEach((r) => (r.deps || []).forEach((dep) => {
    const ds = sysOf(dep); if (!ds || !groups[r.system]) return;
    if (ds === r.system) groups[r.system].internal++;
    else { groups[r.system].deps[ds] = (groups[r.system].deps[ds] || 0) + 1; if (groups[ds]) groups[ds].dependents[r.system] = (groups[ds].dependents[r.system] || 0) + 1; }
  }));
  return Object.values(groups).sort((a, b) => b.repos.length - a.repos.length);
}

/* mini dependency graph for one group: member repos + their direct dependencies */
function buildGroupGraph(D, repos, group) {
  const memberIds = new Set(group.repos.map((r) => r.id));
  const nodes = [], edges = [], seen = new Set();
  const addNode = (id) => {
    if (seen.has(id)) return; seen.add(id);
    const repo = repos.find((x) => x.id === id); const svc = D.servicesById[id];
    const member = memberIds.has(id); const isLib = svc && svc.kind === "lib";
    nodes.push({ id, kind: member ? "repo" : isLib ? "library" : "service", label: (repo && repo.name) || (svc && svc.name) || id, sub: member ? (svc && svc.tier || "repo") : (svc && svc.system || ""), hero: member && id === group.repos[0].id });
  };
  group.repos.forEach((r) => addNode(r.id));
  group.repos.forEach((r) => (r.deps || []).forEach((dep) => {
    if (!D.servicesById[dep]) return; addNode(dep);
    edges.push({ s: r.id, t: dep, verb: D.servicesById[dep].kind === "lib" ? "IMPORTS" : "DEPENDS_ON", layer: D.servicesById[dep].kind === "lib" ? "code" : "runtime" });
  }));
  return { nodes, edges };
}

const SYSTEM_COLOR = { "Boat-Search": "#14b8a6", "Platform": "#4f8cff", "Marketplace": "#ff8a00", "Messaging": "#8b5cf6", "FX": "#22d3ee", "Data": "#f59e0b", "Storefront": "#f0506e", "Shared Libraries": "#c4b59a" };

function RepoGroups({ D, repos, q, onOpenRepo, onOpenService }) {
  const groups = useMemoD(() => deriveRepoGroups(D, repos), [D, repos]);
  const [expanded, setExpanded] = useStateD(null);
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
  const repos = useMemoD(() => deriveRepos(D), [D]);
  const [q, setQ] = useStateD("");
  const [lang, setLang] = useStateD("all");
  const [sort, setSort] = useStateD("updated");
  const [view, setView] = useStateD("groups");
  const [openRepo, setOpenRepo] = useStateD(null);
  const langs = Array.from(new Set(repos.map((r) => r.lang)));
  let rows = repos.filter((r) => (lang === "all" || r.lang === lang) && (q === "" || (r.name + r.slug + r.system + r.owner).toLowerCase().includes(q.toLowerCase())));
  rows = rows.slice().sort((a, b) => sort === "name" ? a.name.localeCompare(b.name) : sort === "facts" ? b.facts - a.facts : parseInt(a.updated) - parseInt(b.updated));
  const r = openRepo ? repos.find((x) => x.id === openRepo) : null;

  if (r) return <RepoDetail r={r} D={D} onBack={() => setOpenRepo(null)} onOpenService={onOpenService} />;

  return (
    <div className="page">
      <div className="page-intro"><h2>Repositories</h2><p>Every source repository Eshu has indexed. <strong>Groups</strong> clusters repos by system and draws the <span className="mono">@dmm/*</span> dependencies between them; <strong>Grid</strong> browses them like your Git host. Select a repo to inspect its tree, branches and graph footprint.</p></div>
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
  const tree = useMemoD(() => repoTree(r), [r]);
  const allDirs = useMemoD(() => collectDirs(tree), [tree]);
  const [branch, setBranch] = useStateD(r.defaultBranch);
  const [view, setView] = useStateD("files");
  const readme = useMemoD(() => findFile(tree, "README.md"), [tree]);
  const [file, setFile] = useStateD(() => readme);
  const [openPaths, setOpenPaths] = useStateD(() => new Set(["src"]));
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
            <p className="t-mut" style={{ fontSize: ".8rem", margin: "0 0 12px", lineHeight: 1.5 }}>Internal <span className="mono">@dmm/*</span> import graph rooted at <span className="mono">{r.name}</span> — every node is clickable to open that service, repeated branches collapse and cycles are flagged.</p>
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
  const [open, setOpen] = useStateD({});
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

/* ================================================================ EXPLORER */
function Explorer({ onOpenService, onOpenNode, graphStyle, setGraphStyle, verifiedOnly, data }) {
  const D = data || ESHU;
  const [scope, setScope] = useStateD("focus");
  const estate = useMemoD(() => D.buildEstateGraph(), []);
  const baseGraph = scope === "estate" ? estate : D.graph;
  const heroNode = baseGraph.nodes.find((n) => n.hero) || baseGraph.nodes[0];

  const [sel, setSel] = useStateD({ type: "node", node: heroNode });
  const [layers, setLayers] = useStateD(() => {const o = {};Object.keys(D.layerColor).forEach((k) => o[k] = true);return o;});
  const [focusRoot, setFocusRoot] = useStateD(null);
  const [visibleIds, setVisibleIds] = useStateD(() => new Set());
  const [crumbs, setCrumbs] = useStateD([]);
  const [isolatedVerb, setIsolatedVerb] = useStateD(null);
  const [pinnedEdge, setPinnedEdge] = useStateD(null);
  const [traceMode, setTraceMode] = useStateD(false);
  const [traceA, setTraceA] = useStateD(null);
  const [traceB, setTraceB] = useStateD(null);
  const layerKeys = Object.keys(D.layerColor);

  const baseAdj = useMemoD(() => {
    const m = {};
    baseGraph.edges.forEach((e) => { (m[e.s] = m[e.s] || new Set()).add(e.t); (m[e.t] = m[e.t] || new Set()).add(e.s); });
    return m;
  }, [baseGraph]);
  const neighborsOf = (id) => baseAdj[id] ? Array.from(baseAdj[id]) : [];

  // reset drill state whenever the scope (focus vs estate) changes
  React.useEffect(() => {
    setFocusRoot(null); setVisibleIds(new Set()); setCrumbs([]); setIsolatedVerb(null);
    setPinnedEdge(null); setTraceMode(false); setTraceA(null); setTraceB(null);
    const h = baseGraph.nodes.find((n) => n.hero) || baseGraph.nodes[0];
    setSel({ type: "node", node: h });
  }, [scope]);

  const selectNode = (n) => {
    if (!n) return;
    if (traceMode) {
      if (!traceA) setTraceA(n.id);
      else if (!traceB && n.id !== traceA) setTraceB(n.id);
      else { setTraceA(n.id); setTraceB(null); }
    }
    setSel({ type: "node", node: n });
  };
  const selectEdge = (e) => e && setSel({ type: "edge", edge: e });
  function pinEdge(e) { setPinnedEdge((p) => (p && edgeKey(p) === edgeKey(e)) ? null : e); }
  function startTrace() { setTraceMode((m) => { const nx = !m; if (!nx) { setTraceA(null); setTraceB(null); } return nx; }); }

  function focusHere(n) {
    const s = new Set([n.id]); neighborsOf(n.id).forEach((id) => s.add(id));
    setFocusRoot(n.id); setVisibleIds(s);
    setCrumbs((c) => [...c, { id: n.id, label: n.label, kind: n.kind }]);
    setSel({ type: "node", node: n });
  }
  function expandNode(n) {
    if (!focusRoot) { focusHere(n); return; }
    setVisibleIds((prev) => { const s = new Set(prev); s.add(n.id); neighborsOf(n.id).forEach((id) => s.add(id)); return s; });
  }
  function refocusIndex(i) {
    if (i === 0) { setFocusRoot(null); setVisibleIds(new Set()); setCrumbs([]); return; }
    const newCrumbs = crumbs.slice(0, i);
    const last = newCrumbs[newCrumbs.length - 1];
    const node = baseGraph.nodes.find((n) => n.id === last.id);
    const s = new Set([node.id]); neighborsOf(node.id).forEach((id) => s.add(id));
    setFocusRoot(node.id); setVisibleIds(s); setCrumbs(newCrumbs); setSel({ type: "node", node });
  }
  function isolate(edge) { setIsolatedVerb((v) => v === edge.verb ? null : edge.verb); }

  const scopedNodes = useMemoD(() => focusRoot ? baseGraph.nodes.filter((n) => visibleIds.has(n.id)) : baseGraph.nodes, [baseGraph, focusRoot, visibleIds]);

  const filteredGraph = useMemoD(() => {
    if (pinnedEdge) {
      const ids = new Set([pinnedEdge.s, pinnedEdge.t]);
      return { nodes: baseGraph.nodes.filter((n) => ids.has(n.id)), edges: [pinnedEdge] };
    }
    let nodes = scopedNodes.slice();
    if (verifiedOnly) nodes = nodes.filter((n) => n.truth !== "inferred");
    const nodeIds = new Set(nodes.map((n) => n.id));
    let edges = baseGraph.edges.filter((e) => layers[e.layer] && nodeIds.has(e.s) && nodeIds.has(e.t));
    if (isolatedVerb) edges = edges.filter((e) => e.verb === isolatedVerb);
    if (isolatedVerb) {
      const keep = new Set(); edges.forEach((e) => { keep.add(e.s); keep.add(e.t); });
      nodes = nodes.filter((n) => keep.has(n.id));
    } else if (!focusRoot) {
      const keep = new Set(); edges.forEach((e) => { keep.add(e.s); keep.add(e.t); });
      nodes.forEach((n) => { if (n.hero) keep.add(n.id); });
      nodes = nodes.filter((n) => keep.has(n.id) || baseGraph.edges.length === 0);
    }
    return { nodes, edges };
  }, [scopedNodes, layers, verifiedOnly, isolatedVerb, focusRoot, baseGraph, pinnedEdge]);

  const tracePathResult = useMemoD(() => (traceMode && traceA && traceB) ? tracePath(filteredGraph, traceA, traceB) : null, [traceMode, traceA, traceB, filteredGraph]);
  const traceLabel = (id) => { const n = filteredGraph.nodes.find((x) => x.id === id) || baseGraph.nodes.find((x) => x.id === id); return n ? n.label : id; };

  // which nodes still have hidden neighbours (drive the + expand badge)
  const expandedSet = useMemoD(() => {
    const s = new Set();
    filteredGraph.nodes.forEach((n) => {
      const all = neighborsOf(n.id).every((id) => focusRoot ? visibleIds.has(id) : true);
      if (all) s.add(n.id);
    });
    return s;
  }, [filteredGraph, visibleIds, focusRoot, baseAdj]);

  const trail = focusRoot ? [{ label: scope === "estate" ? "Full estate" : "Focus graph", kind: null }].concat(crumbs.map((c) => ({ label: c.label, kind: c.kind }))) : [];

  return (
    <div className="page" style={{ maxWidth: "none" }}>
      <div className="page-intro row" style={{ justifyContent: "space-between", alignItems: "flex-end", flexWrap: "wrap", gap: 12 }}>
        <div><h2>Graph Explorer</h2><p>Pan, zoom and drill into the live NornicDB graph. Click any <strong>node</strong> for its evidence and connections, click any <strong>edge</strong> to read the typed relationship facts, then expand neighbours to walk the graph outward.</p></div>
        <div className="row" style={{ gap: 8 }}>
          <div className="seg"><button className={scope === "focus" ? "active" : ""} onClick={() => setScope("focus")}>Focus</button><button className={scope === "estate" ? "active" : ""} onClick={() => {setScope("estate");setGraphStyle("radial");}}>Full estate</button></div>
          <div className="seg"><button className={graphStyle === "layered" ? "active" : ""} onClick={() => setGraphStyle("layered")}>Layered</button><button className={graphStyle === "radial" ? "active" : ""} onClick={() => setGraphStyle("radial")}>Radial</button></div>
          <button className={cx("trace-toggle", traceMode && "on")} onClick={startTrace} title="Trace the shortest path between two nodes"><Icon.branch size={14} /> Trace path</button>
        </div>
      </div>

      <div className="explorer-filters">
        <span className="row" style={{ gap: 7, color: "var(--subtle)", fontSize: ".78rem", fontWeight: 700, textTransform: "uppercase", letterSpacing: ".08em", marginRight: 4 }}><Icon.filter size={15} />Layers</span>
        {layerKeys.map((k) => {
          const n = D.relationships.filter((r) => r.layer === k).reduce((a, r) => a + r.count, 0);
          return (
            <button key={k} className={cx("layer-toggle", layers[k] ? "on" : "off")} style={{ "--lc": D.layerColor[k] }} onClick={() => setLayers((l) => ({ ...l, [k]: !l[k] }))}>
              <i style={{ background: D.layerColor[k] }} /><span style={{ textTransform: "capitalize" }}>{k}</span><span className="lt-n">{fmt(n)}</span>
            </button>);

        })}
      </div>

      {isolatedVerb ? (
        <div className="isolate-bar"><Icon.branch size={15} /> Isolated to <span className="iso-verb">{isolatedVerb}</span> edges — showing only nodes joined by this relationship. <button onClick={() => setIsolatedVerb(null)}>Clear</button></div>
      ) : null}
      {pinnedEdge ? (
        <div className="isolate-bar pin-bar"><Icon.filter size={15} /> Pinned to <span className="iso-verb">{pinnedEdge.verb}</span> — <span className="mono">{traceLabel(pinnedEdge.s)}</span> → <span className="mono">{traceLabel(pinnedEdge.t)}</span>. Graph filtered to just this relationship. <button onClick={() => setPinnedEdge(null)}>Clear</button></div>
      ) : null}
      {traceMode ? (
        <div className="isolate-bar trace-bar">
          <Icon.branch size={15} />
          {!traceA ? <span>Trace mode · click the <strong>source</strong> node…</span>
            : !traceB ? <span>Source <span className="mono trace-end">{traceLabel(traceA)}</span> · now click the <strong>target</strong> node…</span>
            : tracePathResult ? <span>Path <span className="mono trace-end">{traceLabel(traceA)}</span> {tracePathResult.seq.slice(1).map((id, i) => <React.Fragment key={id}><span className="trace-arrow">→</span><button className="trace-hop" onClick={() => onOpenNode(filteredGraph.nodes.find((n) => n.id === id), filteredGraph)}>{traceLabel(id)}</button></React.Fragment>)} · <strong>{tracePathResult.hops} hops</strong></span>
            : <span>No path between <span className="mono trace-end">{traceLabel(traceA)}</span> and <span className="mono trace-end">{traceLabel(traceB)}</span> in this view — widen layers or scope.</span>}
          <button onClick={() => { setTraceA(null); setTraceB(null); }}>Reset</button>
          <button onClick={startTrace}>Exit</button>
        </div>
      ) : null}

      <div className="explorer-layout">
        <div className="gcanvas-shell">
          <GraphCanvas graph={filteredGraph} layout={graphStyle} height={640}
            onSelect={selectNode} onSelectEdge={selectEdge} onExpand={expandNode} onClear={() => {}}
            selectedId={sel && sel.type === "node" ? sel.node.id : null}
            selectedEdge={sel && sel.type === "edge" ? sel.edge : null}
            expandedIds={expandedSet} tracePath={tracePathResult} />
          <div className="t-mut" style={{ fontSize: ".74rem", marginTop: 8 }}>{focusRoot ? "Focused walk · double-click a node or press ＋ to expand its neighbours · " + filteredGraph.nodes.length + " nodes shown" : scope === "estate" ? D.services.length + " indexed services & libraries · real @dmm/* dependency edges" : "api-node-boats neighbourhood · curated evidence · double-click a node to focus & walk outward"}</div>
        </div>
        <Panel title="Inspector" glyph={<Icon.search />}>
          <GraphInspector sel={sel} graph={filteredGraph} onOpenService={onOpenService} onOpenNode={onOpenNode}
            onSelectNode={selectNode} onSelectEdge={selectEdge} onExpand={expandNode} onFocus={focusHere}
            onIsolate={isolate} isolatedVerb={isolatedVerb} expandedIds={expandedSet}
            onPin={pinEdge} pinnedEdge={pinnedEdge}
            breadcrumb={trail} onCrumb={refocusIndex}
            emptyHint="Select any node or relationship edge." />
          <div className="section-label" style={{ marginTop: 18 }}>Node kinds</div>
          <div className="grid g-2" style={{ gap: 7 }}>
            {Object.entries(D.kindStyle).map(([k, v]) => <span key={k} className="row" style={{ gap: 8, fontSize: ".76rem", color: "var(--muted)" }}><i style={{ width: 8, height: 8, borderRadius: 2, background: v.color, flex: "none" }} />{v.label}</span>)}
          </div>
        </Panel>
      </div>
    </div>);

}

/* ================================================================= CATALOG */
function Catalog({ onOpenService, data }) {
  const D = data || ESHU;
  const [q, setQ] = useStateD("");
  const [tier, setTier] = useStateD("all");
  const rows = D.services.filter((s) => (tier === "all" || s.tier === tier) && (q === "" || (s.name + s.repo + s.owner).toLowerCase().includes(q.toLowerCase())));
  return (
    <div className="page">
      <div className="page-intro"><h2>Catalog</h2><p>Every indexed service, repository and workload with coverage, freshness and truth level. Select a row to open its spotlight.</p></div>
      <Panel className="flush" title={rows.length + " services"} sub="Sorted by deployment criticality"
      action={
      <div className="row" style={{ gap: 8 }}>
            <div className="searchbox" style={{ minWidth: 220, height: 34 }}><Icon.search size={15} /><input placeholder="Filter catalog…" value={q} onChange={(e) => setQ(e.target.value)} /></div>
            <div className="seg">{["all", "tier-1", "tier-2", "tier-3", "lib"].map((t) => <button key={t} className={tier === t ? "active" : ""} onClick={() => setTier(t)}>{t === "all" ? "All" : t === "lib" ? "Libs" : t}</button>)}</div>
          </div>
      }>
        <table className="tbl">
          <thead><tr><th>Service</th><th>Tier</th><th>System</th><th>Owner</th><th>Language</th><th>Runtime</th><th>Security</th><th>Coverage</th><th>Truth</th><th>Freshness</th></tr></thead>
          <tbody>
            {rows.map((s) =>
            <tr key={s.id} onClick={() => onOpenService(s.id)}>
                <td className="cell-stack"><span className="t-name">{s.name}</span><small>{s.repo}</small></td>
                <td><span className={"tag-tier tier-" + s.tier}>{s.tier === "lib" ? "library" : s.tier}</span></td>
                <td className="t-mut" style={{ fontSize: ".8rem" }}>{s.system}</td>
                <td className="t-mut">{s.owner}</td>
                <td><span className="row" style={{ gap: 7 }}><i style={{ width: 8, height: 8, borderRadius: 9, background: D.lang[s.lang].color }} />{D.lang[s.lang].label}</span></td>
                <td className="t-mut mono" style={{ fontSize: ".76rem" }}>{s.kind === "lib" ? "—" : s.envs.length + " env"}</td>
                <td style={{ minWidth: 130 }}><SeverityBar counts={{ critical: s.crit, high: s.high, medium: s.med, low: s.low }} sev={D.sev} />
                  <div className="row" style={{ gap: 8, marginTop: 6 }}>{s.crit ? <span className="sev-tag" style={{ color: D.sev.critical, fontSize: ".68rem" }}><i style={{ background: D.sev.critical }} />{s.crit}</span> : null}{s.high ? <span className="sev-tag" style={{ color: D.sev.high, fontSize: ".68rem" }}><i style={{ background: D.sev.high }} />{s.high}</span> : null}</div>
                </td>
                <td className="mono" style={{ fontSize: ".82rem" }}>{Math.round(s.coverage * 100)}%</td>
                <td><TruthChip level={s.truth} /></td>
                <td><FreshDot state={s.freshness} /></td>
              </tr>
            )}
          </tbody>
        </table>
      </Panel>
    </div>);

}

/* ================================================================ FINDINGS */
/* shared sub-nav so Findings (worklist) and the CVE register feel like one surface */
function FindingsTabs({ active }) {
  return (
    <div className="dep-toggle" style={{ marginBottom: 14 }}>
      <button className={active === "worklist" ? "active" : ""} onClick={() => { location.hash = "findings"; }}>Worklist</button>
      <button className={active === "cves" ? "active" : ""} onClick={() => { location.hash = "vulnerabilities"; }}>CVE register</button>
    </div>
  );
}
const SEVRANK = { critical: 4, high: 3, medium: 2, low: 1 };

function Findings({ onOpenService, onOpenVuln, verifiedOnly, data }) {
  const D = data || ESHU;
  const [type, setType] = useStateD("all");
  const [sev, setSev] = useStateD("all");
  const fpool = verifiedOnly ? D.findings.filter((f) => f.truth !== "inferred") : D.findings;
  const vpool = verifiedOnly ? D.vulns.filter((v) => v.prov !== "inferred") : D.vulns;
  // CVEs join the unified worklist as first-class rows; deep detail lives in the CVE register
  const vulnRows = vpool.map((v) => ({
    id: "cve-" + v.cve, type: "Vulnerability", severity: v.severity,
    title: v.cve + " · " + v.title,
    detail: v.pkg + "@" + v.version + " · " + v.ecosystem + (v.fixAvailable ? " · fix " + v.fixed : " · no fix"),
    entity: (v.services && v.services[0]) || "—", source: v.source,
    truth: v.prov === "inferred" ? "inferred" : "derived", age: v.firstSeen || "live",
    _cve: v.cve, _kev: v.kev, _cvss: v.cvss
  }));
  const pool = fpool.concat(vulnRows);
  const hidden = (D.findings.length - fpool.length) + (D.vulns.length - vpool.length);
  const types = ["all"].concat(Array.from(new Set(pool.map((f) => f.type))));
  const rows = pool.filter((f) => (type === "all" || f.type === type) && (sev === "all" || f.severity === sev)).sort((a, b) => (SEVRANK[b.severity] || 0) - (SEVRANK[a.severity] || 0));
  const byType = {};pool.forEach((f) => byType[f.type] = (byType[f.type] || 0) + 1);
  const sevCount = {};pool.forEach((f) => sevCount[f.severity] = (sevCount[f.severity] || 0) + 1);
  const kevCount = vpool.filter((v) => v.kev).length;

  return (
    <div className="page">
      <div className="page-intro"><h2>Findings</h2><p>One worklist for everything that needs attention — drift, version skew, legacy dependencies, missing evidence, incidents and <strong>vulnerabilities</strong> — each carrying its truth level and source. The <strong>CVE register</strong> tab keeps the deep security view with EPSS, KEV &amp; blast-radius.</p></div>
      <FindingsTabs active="worklist" />
      <div className="grid g-4">
        <StatTile label="Open items" value={pool.length} color="var(--ember)" sub={hidden ? hidden + " inferred hidden" : "across the fleet"} />
        <StatTile label="Critical" value={sevCount.critical || 0} color="var(--crit)" sub="immediate action" />
        <StatTile label="Vulnerabilities" value={byType.Vulnerability || 0} color="var(--crit)" sub={kevCount + " KEV-listed"} onClick={() => { location.hash = "vulnerabilities"; }} cta="CVE register" />
        <StatTile label="Drift / evidence" value={(byType.Drift || 0) + (byType["Missing evidence"] || 0) + (byType["Stale answer"] || 0)} color="var(--violet)" sub="freshness & drift gaps" />
      </div>
      <Panel className="flush mt" title="Unified worklist" sub={hidden ? hidden + " inferred item(s) hidden" : undefined}
      action={<div className="row" style={{ gap: 8 }}>
          <div className="seg">{["all", "critical", "high", "medium", "low"].map((s) => <button key={s} className={sev === s ? "active" : ""} onClick={() => setSev(s)}>{s === "all" ? "All" : s}</button>)}</div>
        </div>}>
        <div className="row wrap" style={{ gap: 7, padding: "14px var(--pad) 0" }}>
          {types.map((t) => <button key={t} className={cx("btn-ghost", type === t && "active")} onClick={() => setType(t)}>{t === "all" ? "All types" : t}{t !== "all" ? " · " + byType[t] : ""}</button>)}
        </div>
        <table className="tbl mt">
          <thead><tr><th>Severity</th><th>Finding</th><th>Type</th><th>Entity</th><th>Source</th><th>Truth</th><th>Age</th><th></th></tr></thead>
          <tbody>
            {rows.map((f) =>
            <tr key={f.id} onClick={() => { if (f._cve) { onOpenVuln && onOpenVuln(f._cve); return; } const svc = D.services.find((s) => s.id === f.entity); if (svc) onOpenService(svc.id); }} style={{ cursor: "pointer" }}>
                <td><span className="sev-tag" style={{ color: D.sev[f.severity] }}><i style={{ background: D.sev[f.severity] }} />{f.severity}</span></td>
                <td className="cell-stack" style={{ maxWidth: 360 }}><span className="row" style={{ gap: 7 }}><span style={{ color: "var(--bone)", fontWeight: 600 }}>{f.title}</span>{f._kev ? <span className="kev-flag">KEV</span> : null}</span><small>{f.detail}</small></td>
                <td><span className="row" style={{ gap: 7 }}><Badge tone={f.type === "Vulnerability" ? "crit" : "neutral"}>{f.type}</Badge>{f._cvss != null ? <span className="mono" style={{ fontSize: ".74rem", color: D.sev[f.severity] }}>{f._cvss}</span> : null}</span></td>
                <td className="t-name" style={{ fontSize: ".8rem" }}>{f.entity}</td>
                <td className="t-mut mono" style={{ fontSize: ".76rem" }}>{f.source}</td>
                <td><TruthChip level={f.truth} /></td>
                <td className="t-mut mono" style={{ fontSize: ".78rem" }}>{f.age}</td>
                <td style={{ color: "var(--subtle)" }}><Icon.arrow size={15} /></td>
              </tr>
            )}
          </tbody>
        </table>
      </Panel>
    </div>);

}

/* =========================================================== VULNERABILITIES */
/* CVE -> affected nodes graph: the vulnerability at the centre. */
function buildVulnGraph(D, v) {
  const center = { id: "vuln:" + v.cve, kind: "vuln", label: v.cve, sub: (v.kev ? "KEV · " : "") + "CVSS " + v.cvss, hero: true, truth: "exact" };
  const pkg = { id: "pkg:" + v.pkg, kind: "library", label: v.pkg, sub: v.version + " · " + v.ecosystem, truth: "exact" };
  const nodes = [center, pkg];
  const edges = [{ s: "pkg:" + v.pkg, t: center.id, verb: "AFFECTED_BY", layer: "security" }];
  (v.services || []).forEach((sid) => {
    const svc = D.servicesById && D.servicesById[sid];
    nodes.push({ id: sid, kind: svc && svc.kind === "lib" ? "library" : "service", label: svc ? svc.name : sid, sub: svc ? (svc.tier || "") + (svc.system ? " · " + svc.system : "") : "", truth: svc ? svc.truth : "derived" });
    edges.push({ s: sid, t: "pkg:" + v.pkg, verb: "DEPENDS_ON", layer: "runtime" });
  });
  return { nodes, edges };
}

function VulnDetail({ v, D, onBack, onOpenService, onOpenNode }) {
  const graph = useMemoD(() => buildVulnGraph(D, v), [D, v]);
  const affected = (v.services || []).map((sid) => D.servicesById && D.servicesById[sid] || { id: sid, name: sid });
  return (
    <div className="page">
      <button className="repo-back" onClick={onBack}><Icon.arrow size={14} style={{ transform: "rotate(180deg)" }} /> All vulnerabilities</button>
      <div className="repo-detail-head">
        <div>
          <div className="row" style={{ gap: 10, flexWrap: "wrap" }}>
            <span className="repo-icon" style={{ background: "color-mix(in oklab, var(--crit) 16%, transparent)", color: "var(--crit)" }}><Icon.vuln size={18} /></span>
            <h2 style={{ fontFamily: "var(--mono)", fontSize: "1.3rem" }}>{v.cve}</h2>
            {v.kev ? <span className="kev-flag">KEV</span> : null}
            <span className="sev-tag" style={{ color: D.sev[v.severity] }}><i style={{ background: D.sev[v.severity] }} />{v.severity}</span>
          </div>
          <p className="t-mut" style={{ fontSize: ".9rem", margin: "10px 0 0", maxWidth: "78ch", lineHeight: 1.5 }}>{v.title}</p>
        </div>
        {v.fixAvailable ? <Badge tone="teal">fix: {v.fixed}</Badge> : <Badge tone="crit">no fix</Badge>}
      </div>

      <div className="repo-stats">
        <div><dt>CVSS</dt><dd style={{ color: D.sev[v.severity] }}>{v.cvss}</dd></div>
        <div><dt>EPSS</dt><dd>{Math.round((v.epss || 0) * 100)}%</dd></div>
        <div><dt>KEV</dt><dd style={{ color: v.kev ? "var(--crit)" : "var(--muted)" }}>{v.kev ? "listed" : "no"}</dd></div>
        <div><dt>Package</dt><dd style={{ fontSize: ".92rem" }}>{v.pkg}</dd></div>
        <div><dt>Affected version</dt><dd style={{ fontSize: ".92rem" }}>{v.version}</dd></div>
        <div><dt>Fixed in</dt><dd style={{ fontSize: ".92rem", color: v.fixAvailable ? "var(--teal)" : "var(--crit)" }}>{v.fixAvailable ? v.fixed : "none"}</dd></div>
        <div><dt>Ecosystem</dt><dd style={{ fontSize: ".92rem" }}>{v.ecosystem}</dd></div>
        <div><dt>Source</dt><dd style={{ fontSize: ".92rem" }}>{v.source}</dd></div>
      </div>

      <Panel title="Blast radius" sub={"AFFECTED_BY graph — " + v.cve + " at the centre, reachable services around it · click any node to drill"} glyph={<Icon.graph />}>
        <GraphCanvas graph={graph} layout="radial" height={420} onSelect={(n) => { if (onOpenNode) onOpenNode(n, graph); else if (D.servicesById && D.servicesById[n.id]) onOpenService(n.id); }} selectedId={"vuln:" + v.cve} />
      </Panel>

      <Panel className="flush mt" title={"Affected services (" + affected.length + ")"} sub="Reachable via a vulnerable dependency — click to open">
        <table className="tbl">
          <thead><tr><th>Service</th><th>Tier</th><th>System</th><th>Truth</th></tr></thead>
          <tbody>
            {affected.map((s) =>
            <tr key={s.id} onClick={() => onOpenService(s.id)} style={{ cursor: "pointer" }}>
                <td className="t-name">{s.name}</td>
                <td>{s.tier ? <span className={"tag-tier tier-" + s.tier}>{s.tier === "lib" ? "library" : s.tier}</span> : "—"}</td>
                <td className="t-mut" style={{ fontSize: ".8rem" }}>{s.system || "—"}</td>
                <td>{s.truth ? <TruthChip level={s.truth} /> : "—"}</td>
              </tr>
            )}
          </tbody>
        </table>
      </Panel>
    </div>);

}

function Vulnerabilities({ onOpenService, onOpenNode, chartStyle, verifiedOnly, data }) {
  const D = data || ESHU;
  const [sev, setSev] = useStateD("all");
  const hashCve = (((location.hash || "").split("?")[1] || "").match(/cve=([^&]+)/) || [])[1];
  const [openVuln, setOpenVuln] = useStateD(hashCve ? decodeURIComponent(hashCve) : null);
  const pool = verifiedOnly ? D.vulns.filter((v) => v.prov !== "inferred") : D.vulns;
  const hidden = D.vulns.length - pool.length;
  const rows = pool.filter((v) => sev === "all" || v.severity === sev).slice().sort((a, b) => b.cvss - a.cvss);
  const sevCount = { critical: 0, high: 0, medium: 0, low: 0 };
  pool.forEach((v) => sevCount[v.severity]++);
  const kevCount = pool.filter((v) => v.kev).length;
  const fixable = pool.filter((v) => v.fixAvailable).length;
  const byEco = {};pool.forEach((v) => byEco[v.ecosystem] = (byEco[v.ecosystem] || 0) + 1);
  const ecoColor = { go: "#14b8a6", npm: "#f0506e", pypi: "#a78bfa", maven: "#f472b6", deb: "#ff9d2e", apk: "#4f8cff" };
  const activeVuln = openVuln ? D.vulns.find((v) => v.cve === openVuln) : null;
  if (activeVuln) return <VulnDetail v={activeVuln} D={D} onBack={() => { setOpenVuln(null); if ((location.hash || "").indexOf("?") >= 0) location.hash = "vulnerabilities"; }} onOpenService={onOpenService} onOpenNode={onOpenNode} />;

  return (
    <div className="page">
      <div className="page-intro"><h2>Vulnerabilities</h2><p>CVE register — vulnerability intelligence correlated to deployed images and reachable services, sourced from CISA KEV, FIRST EPSS, NVD, OSV and GHSA, and joined to the graph by <span className="mono">AFFECTED_BY</span> edges. This is the deep security view of the Findings worklist.</p></div>
      <FindingsTabs active="cves" />

      <div className="grid g-4">
        <StatTile label="Open CVEs" value={pool.length} color="var(--crit)" sub={sevCount.critical + " critical · " + sevCount.high + " high" + (hidden ? " · " + hidden + " inferred hidden" : "")} />
        <StatTile label="KEV-listed" value={kevCount} color="var(--crit)" trend={kevCount ? { dir: "down", text: "act now" } : undefined} sub="known exploited" />
        <StatTile label="Fix available" value={fixable + "/" + pool.length} color="var(--teal)" sub="patch path exists" />
        <StatTile label="New (14d)" value={D.metrics.newVulns.reduce((a, b) => a + b, 0)} spark={D.metrics.newVulns} color="var(--ember)" sub="intake from feeds" />
      </div>

      <div className="grid mt" style={{ gridTemplateColumns: "minmax(0,1.4fr) minmax(0,1fr) minmax(0,1fr)", gap: "var(--gap)" }}>
        <Panel title="New vulnerabilities" sub="Daily intake over the last 14 days" glyph={<Icon.pulse />}>
          <AreaChart data={D.metrics.newVulns} color="var(--crit)" h={170} unit=" CVEs" />
        </Panel>
        <Panel title="By severity" glyph={<Icon.shield />}>
          <div style={{ display: "grid", placeItems: "center" }}>
            <Donut size={132} thickness={17} segments={["critical", "high", "medium", "low"].map((k) => ({ label: k, value: sevCount[k], color: D.sev[k] }))} center={{ value: pool.length, label: "CVEs" }} />
          </div>
          <div className="row wrap" style={{ gap: 12, justifyContent: "center", marginTop: 12 }}>{["critical", "high", "medium", "low"].map((k) => <span key={k} className="sev-tag" style={{ color: D.sev[k] }}><i style={{ background: D.sev[k] }} />{sevCount[k]}</span>)}</div>
        </Panel>
        <Panel title="By ecosystem" glyph={<Icon.box />}>
          <BarRows rows={Object.entries(byEco).map(([k, v]) => ({ label: k, value: v, color: ecoColor[k] || "var(--teal)" })).sort((a, b) => b.value - a.value)} />
        </Panel>
      </div>

      <Panel className="flush mt" title="CVE register" sub="Sorted by CVSS — joined to reachable services"
      action={<div className="seg">{["all", "critical", "high", "medium"].map((s) => <button key={s} className={sev === s ? "active" : ""} onClick={() => setSev(s)}>{s === "all" ? "All" : s}</button>)}</div>}>
        <table className="tbl">
          <thead><tr><th>CVE</th><th>Severity</th><th>CVSS</th><th>EPSS</th><th>Package</th><th>Affected services</th><th>Fix</th><th>Source</th></tr></thead>
          <tbody>
            {rows.map((v) =>
            <tr key={v.cve} onClick={() => setOpenVuln(v.cve)} style={{ cursor: "pointer" }}>
                <td className="cell-stack"><span className="row" style={{ gap: 7 }}><span className="t-name vuln-link" style={{ fontSize: ".8rem" }}>{v.cve}</span>{v.kev ? <span className="kev-flag">KEV</span> : null}</span><small style={{ maxWidth: 260 }}>{v.title}</small></td>
                <td><span className="sev-tag" style={{ color: D.sev[v.severity] }}><i style={{ background: D.sev[v.severity] }} />{v.severity}</span></td>
                <td><span className="mono" style={{ fontSize: ".82rem", color: D.sev[v.severity] }}>{v.cvss}</span></td>
                <td><span className="score-bar"><i style={{ width: v.epss * 100 + "%", background: v.epss > 0.5 ? "var(--crit)" : "var(--med)" }} /></span> <span className="mono" style={{ fontSize: ".72rem", color: "var(--muted)" }}>{(v.epss * 100).toFixed(0)}%</span></td>
                <td className="cell-stack"><span className="t-mut mono" style={{ fontSize: ".78rem" }}>{v.pkg}</span><small>{v.version} · {v.ecosystem}</small></td>
                <td><div className="row wrap" style={{ gap: 5 }}>{v.services.slice(0, 3).map((s) => <button key={s} className="dep-chip" style={{ fontSize: ".7rem", padding: "3px 7px" }} onClick={(e) => {e.stopPropagation();onOpenService(s);}}>{s}</button>)}{v.services.length > 3 ? <span className="t-mut" style={{ fontSize: ".72rem" }}>+{v.services.length - 3}</span> : null}</div></td>
                <td>{v.fixAvailable ? <Badge tone="teal">{v.fixed}</Badge> : <Badge tone="crit">none</Badge>}</td>
                <td className="t-mut mono" style={{ fontSize: ".74rem" }}>{v.source}</td>
              </tr>
            )}
          </tbody>
        </table>
      </Panel>
    </div>);

}

/* =================================================================== ADMIN */
function Admin({ source, data, onOpenCollector, onOpenNode }) {
  const D = data || ESHU,m = D.metrics,r = D.runtime;
  const apps = D.argocdApps;
  const indexedCount = apps.filter((a) => a.indexed).length;
  const live = source && source.mode === "live" && source.status === "connected";
  return (
    <div className="page">
      <div className="page-intro"><h2>Operations</h2><p>Eshu runtime and NornicDB graph-backend health. Ingestion pipeline, reducer queues, graph writes and query performance. Data source: <strong style={{ color: live ? "var(--teal)" : "var(--bone)" }}>{source ? source.mode === "demo" ? "demo (static extraction)" : live ? "live Eshu API" : "live (unreachable — demo fallback)" : "demo"}</strong>.</p></div>

      <div className="grid g-4">
        <StatTile label="Write throughput" value={fmt(m.writeTps.at(-1)) + "/s"} spark={m.writeTps} color="var(--teal)" trend={{ dir: "up", text: "steady" }} sub="graph mutations" />
        <StatTile label="Query p99" value={m.queryP99.at(-1) + "ms"} spark={m.queryP99} color="var(--ember)" trend={{ dir: "flat", text: "within SLO" }} sub="NornicDB read path" />
        <StatTile label="Cache hit" value={m.cacheHit.at(-1) + "%"} spark={m.cacheHit} color="var(--blue)" trend={{ dir: "up", text: "+0.4%" }} sub="adjacency cache" />
        <StatTile label="Dead letters" value={r.deadLetters} spark={m.deadLetters} color="var(--violet)" trend={{ dir: "down", text: "−2" }} sub="needs replay" />
      </div>

      <div className="grid g-2 mt">
        <Panel title="Reducer queue depth" sub="Outstanding work items awaiting reduction" glyph={<Icon.layers />}>
          <AreaChart data={m.queueDepth} color="var(--violet)" h={180} unit=" items" />
        </Panel>
        <Panel title="Graph growth" sub="Total nodes & relationships in NornicDB" glyph={<Icon.db />}>
          <MultiLine seriesList={[{ label: "edges", data: m.graphEdges, color: "var(--ember)" }, { label: "nodes", data: m.graphNodes, color: "var(--teal)" }]} h={180} unit="" />
          <div className="chart-legend"><span><i style={{ background: "var(--teal)" }} />{fmt(r.nodes)} nodes</span><span><i style={{ background: "var(--ember)" }} />{fmt(r.edges)} edges</span></div>
        </Panel>
      </div>

      <Panel className="flush mt" title="ArgoCD deployed workloads" sub={apps.length + " applications · " + indexedCount + " with source indexed in this workspace"} glyph={<Icon.layers />}
      action={<span className="t-mut mono" style={{ fontSize: ".74rem" }}>helm-charts/argocd</span>}>
        <div className="argocd-grid">
          {apps.map((a) =>
          <div className={cx("argocd-app", a.indexed && "indexed")} key={a.name} title={a.indexed ? "Source indexed" : "Deploy-only — source not in workspace"}>
              <span className="row" style={{ gap: 7, minWidth: 0 }}><i style={{ width: 7, height: 7, borderRadius: 9, background: a.indexed ? "var(--teal)" : "var(--subtle)", flex: "none" }} /><span className="argocd-name">{a.name}</span></span>
              {a.kind === "portal" ? <span className="argocd-tag">portal</span> : null}
            </div>
          )}
        </div>
      </Panel>

      <Panel className="flush mt" title="Collectors" sub={D.collectors.length + " fact sources feeding the graph · click any collector to see what it produces"} glyph={<Icon.cloud />}>
        <div className="domain-strip">
          {Object.keys(COLLECTOR_DOMAIN).map((dom) => {
            const list = D.collectors.filter((c) => COLLECTOR_DOMAIN[dom].includes(c.kind));
            if (!list.length) return null;
            const cc = list.reduce((a, c) => { a[c.status] = (a[c.status] || 0) + 1; return a; }, {});
            return (
              <div className="domain-card" key={dom}>
                <div className="domain-card-top"><span className="domain-name">{dom}</span><span className="domain-count">{list.length}</span></div>
                <div className="domain-dots">
                  {cc.healthy ? <span style={{ color: "var(--teal)" }}><i style={{ background: "var(--teal)" }} />{cc.healthy}</span> : null}
                  {cc.degraded ? <span style={{ color: "var(--med)" }}><i style={{ background: "var(--med)" }} />{cc.degraded}</span> : null}
                  {cc.stale ? <span style={{ color: "var(--crit)" }}><i style={{ background: "var(--crit)" }} />{cc.stale}</span> : null}
                </div>
              </div>
            );
          })}
        </div>
        <table className="tbl collectors-tbl">
          <thead><tr><th>Collector</th><th>Instance</th><th>Status</th><th>Facts</th><th>Scopes</th><th>Latency</th><th>Cadence</th><th>Last run</th><th></th></tr></thead>
          <tbody>
            {Object.keys(COLLECTOR_DOMAIN).map((dom) => {
              const list = D.collectors.filter((c) => COLLECTOR_DOMAIN[dom].includes(c.kind));
              if (!list.length) return null;
              return (
                <React.Fragment key={dom}>
                  <tr className="group-row"><td colSpan={9}><span className="group-label">{dom}</span><span className="group-meta">{list.length} {list.length === 1 ? "collector" : "collectors"} · {fmt(list.reduce((a, c) => a + c.facts, 0))} facts</span></td></tr>
                  {list.map((c) => {
                    const k = D.collectorKinds[c.kind];
                    return (
                      <tr key={c.instance} className="collector-row" onClick={() => onOpenCollector && onOpenCollector(c)} style={{ cursor: "pointer" }}>
                        <td><span className="row" style={{ gap: 10 }}><CollectorGlyph kind={c.kind} /><span className="cell-stack"><span style={{ fontWeight: 600 }}>{k.label}</span><small>{c.note}</small></span></span></td>
                        <td className="t-mut mono" style={{ fontSize: ".76rem" }}>{c.instance}</td>
                        <td><span className="status-pill" style={{ color: D.statusColor[c.status] }}><i style={{ background: D.statusColor[c.status] }} />{c.status}</span></td>
                        <td className="mono" style={{ fontSize: ".82rem" }}>{fmt(c.facts)}</td>
                        <td className="t-mut mono" style={{ fontSize: ".8rem" }}>{c.scopes}</td>
                        <td className="t-mut mono" style={{ fontSize: ".8rem" }}>{c.latencyMs ? c.latencyMs + "ms" : "—"}</td>
                        <td className="t-mut" style={{ fontSize: ".78rem" }}>{c.cadence}</td>
                        <td><FreshDot state={c.freshness} /><div className="t-mut mono" style={{ fontSize: ".72rem", marginTop: 2 }}>{c.lastRun}</div></td>
                        <td style={{ color: "var(--subtle)" }}><Icon.arrow size={15} /></td>
                      </tr>);
                  })}
                </React.Fragment>
              );
            })}
          </tbody>
        </table>
      </Panel>
    </div>);

}

Object.assign(window, { Explorer, Catalog, Findings, Vulnerabilities, Admin, Repos });
