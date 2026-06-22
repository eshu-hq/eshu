/* Eshu Console — demo repository model helpers. */
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
  const svc = r.name.replace(/^svc-|^job-/, "");
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
  "name": "@acme/${r.name}",
  "version": "${r.version || "1.0.0"}",
  "private": true,
  "scripts": { "dev": "tsx watch src/index.ts", "test": "vitest", "build": "tsc" },
  "dependencies": {
${(r.deps || []).slice(0, 4).map((d) => `    "@acme/${d}": "workspace:*"`).join(",\n") || '    "express": "^4.19.0"'}
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

const SYSTEM_COLOR = { "Catalog-Search": "#14b8a6", "Platform": "#4f8cff", "Marketplace": "#ff8a00", "Messaging": "#8b5cf6", "FX": "#22d3ee", "Data": "#f59e0b", "Storefront": "#f0506e", "Shared Libraries": "#c4b59a" };
