import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

function repoRoot(): string {
  return process.cwd().endsWith("apps/console") ? resolve(process.cwd(), "../..") : process.cwd();
}

function repoFile(path: string): string {
  return readFileSync(resolve(repoRoot(), path), "utf8");
}

describe("prototype documentation parity", () => {
  it("documents the live parity endpoints exposed by the prototype loader", () => {
    const guide = repoFile("apps/console/prototype/eshu-console/port/PORT-TO-CONSOLE.md");

    expect(guide).toContain("GET /api/v0/images");
    expect(guide).toContain("GET /api/v0/iac/resources");
    expect(guide).toContain("GET /api/v0/supply-chain/sbom-attestations/attachments");
    expect(guide).toContain("GET /api/v0/supply-chain/advisories");
    expect(guide).toContain("GET /api/v0/dependencies");
    expect(guide).toContain("GET /api/v0/metrics/timeseries");
    expect(guide).toContain("`dead_letters`, `graph_nodes`, `graph_edges`, `query_p50`, `query_p95`, and");
    expect(guide).toContain("issue #2216 defines named live contracts");
    expect(guide).toContain("issue #2217 proves the live runtime collector path");
    expect(guide).toContain("GET /api/v0/observability/coverage/correlations?provider=");
    expect(guide).not.toContain("Graph drill (next)");
    expect(guide).not.toContain("no historical series endpoint");
    expect(guide).not.toContain("Copy `eshuConsoleLive.ts`");
    expect(guide).toContain("Use the production loaders in `apps/console/src/api/` as the current contract");
  });

  it("uses the same observability provider vocabulary as the live console", () => {
    const html = repoFile("apps/console/prototype/eshu-console/Eshu Console.html");
    const app = repoFile("apps/console/prototype/eshu-console/console/app.jsx");
    const page = repoFile("apps/console/prototype/eshu-console/console/pages-cloud.jsx");
    const overlay = repoFile("apps/console/prototype/eshu-console/console/pages-observability-parity.jsx");
    const loader = repoFile("apps/console/prototype/eshu-console/console/live-parity-loader.js");

    expect(html).toContain("console/pages-observability-parity.jsx");
    expect(app).toContain("<Observability data={data} client={liveClient}");
    expect(page).toContain("grafana, prometheus/mimir, loki, and tempo");
    expect(page).toContain("obsCoverageSnapshot");
    expect(page).toContain("Coverage correlations");
    expect(page).toContain("Provider coverage");
    expect(overlay).toContain("provider=grafana");
    expect(overlay).toContain("provider=prometheus");
    expect(overlay).toContain("provider=loki");
    expect(overlay).toContain("provider=tempo");
    expect(overlay).toContain("after_correlation_id");
    expect(overlay).toContain("No live observability coverage correlations");
    expect(overlay).toContain("Live observability coverage unavailable");
    expect(loader).toContain("obsCoverageSnapshot");
    expect(loader).toContain("providerResults");
    expect(page).not.toContain("Prometheus, CloudWatch, OpenTelemetry, Loki, Datadog");
  });

  it("keeps the prototype cloud route on the live cloud resources contract", () => {
    const html = repoFile("apps/console/prototype/eshu-console/Eshu Console.html");
    const app = repoFile("apps/console/prototype/eshu-console/console/app.jsx");
    const page = repoFile("apps/console/prototype/eshu-console/console/pages-cloud.jsx");
    const overlay = repoFile("apps/console/prototype/eshu-console/console/pages-cloud-parity.jsx");
    const guide = repoFile("apps/console/prototype/eshu-console/port/PORT-TO-CONSOLE.md");

    expect(html).toContain("console/pages-cloud-parity.jsx");
    expect(app).toContain("<Cloud data={data} client={liveClient}");
    expect(page).toContain("/api/v0/cloud/inventory");
    expect(page).toContain("Canonical inventory");
    expect(page).toContain("buildCanonicalCloudNetwork");
    expect(page).toContain("HAS_RESOURCE");
    expect(page).toContain("No canonical inventory rows");
    expect(overlay).toContain("/api/v0/cloud/resources");
    expect(overlay).toContain("after_resource_type");
    expect(overlay).toContain("after_id");
    expect(overlay).toContain("No cloud resources match this scope");
    expect(guide).toContain("GET /api/v0/cloud/inventory");
    expect(guide).toContain("GET /api/v0/cloud/resources");
  });

  it("keeps the prototype dashboard atlas on live entity-map contracts", () => {
    const html = repoFile("apps/console/prototype/eshu-console/Eshu Console.html");
    const app = repoFile("apps/console/prototype/eshu-console/console/app.jsx");
    const page = repoFile("apps/console/prototype/eshu-console/console/pages-dashboard-parity.jsx");

    expect(html).toContain("console/pages-dashboard-parity.jsx");
    expect(app).toContain("<Dashboard data={data} client={liveClient}");
    expect(page).toContain("/api/v0/impact/entity-map");
    expect(page).toContain("MEANINGFUL_DASHBOARD_EDGES");
    expect(page).toContain("No live relationship atlas");
  });

  it("keeps the verified-evidence shield behavior aligned across live and prototype shells", () => {
    const liveApp = repoFile("apps/console/src/App.tsx");
    const prototypeApp = repoFile("apps/console/prototype/eshu-console/console/app.jsx");

    expect(liveApp).toContain('aria-label="Show verified evidence only"');
    expect(liveApp).toContain("verifiedConsoleModel");
    expect(liveApp).toContain('finding.truth !== "fallback"');
    expect(prototypeApp).toContain("verifiedOnly");
    expect(prototypeApp).toContain("Show verified evidence only");
  });

  it("keeps prototype topbar search wired to live-style vulnerability routing", () => {
    const liveApp = repoFile("apps/console/src/App.tsx");
    const prototypeApp = repoFile("apps/console/prototype/eshu-console/console/app.jsx");

    expect(liveApp).toContain("vulnerabilitySearchTarget");
    expect(liveApp).toContain("navigate(`/vulnerabilities/${encodeURIComponent(vulnerabilityId)}`)");
    expect(prototypeApp).toContain("onSubmit={submitSearch}");
    expect(prototypeApp).toContain("prototypeVulnerabilitySearchTarget");
    expect(prototypeApp).toContain('setRouteHash("vulnerabilities", "?cve=" + encodeURIComponent(cve))');
  });

  it("keeps prototype topbar search wired to live-style repository routing", () => {
    const liveApp = repoFile("apps/console/src/App.tsx");
    const prototypeApp = repoFile("apps/console/prototype/eshu-console/console/app.jsx");

    expect(liveApp).toContain("repositorySearchTarget");
    expect(liveApp).toContain("loadRepositories(next)");
    expect(liveApp).toContain("navigate(`/repositories/${encodeURIComponent(repositoryId)}/source`)");
    expect(liveApp).toContain("onKeyDown={submitSearchKey}");
    expect(liveApp).toContain("onClick={submitSearchButton}");
    expect(liveApp).toContain('className="search-submit"');
    expect(prototypeApp).toContain("prototypeRepositorySearchTarget");
    expect(prototypeApp).toContain('setRouteHash("reposource"');
    expect(prototypeApp).toContain("onKeyDown={submitSearchKey}");
    expect(prototypeApp).toContain("onClick={submitSearchButton}");
    expect(prototypeApp).toContain('className="search-submit"');
  });

  it("keeps the prototype repositories route on the live repository contract", () => {
    const html = repoFile("apps/console/prototype/eshu-console/Eshu Console.html");
    const app = repoFile("apps/console/prototype/eshu-console/console/app.jsx");
    const page = repoFile("apps/console/prototype/eshu-console/console/pages-repositories-parity.jsx");
    const guide = repoFile("apps/console/prototype/eshu-console/port/PORT-TO-CONSOLE.md");

    expect(html).toContain("console/pages-repositories-parity.jsx");
    expect(app).toContain("<Repos data={data} client={liveClient}");
    expect(page).toContain("/api/v0/repositories?limit=500&offset=0");
    expect(page).toContain("/stats");
    expect(page).toContain("/story");
    expect(page).toContain("clustered by domain evidence");
    expect(page).toContain('name === "api-node-boats"');
    expect(page).toContain('name === "job-node-sitemaps-generator"');
    expect(page).toContain("Marketplace");
    expect(page).toContain("Configuration");
    expect(page).toContain("repoSourceHref");
    expect(page).not.toContain('name.includes("boat") || name.includes("search") || name.includes("sitemap")');
    expect(page).toContain("Repository detail unavailable");
    expect(guide).toContain("GET /api/v0/repositories");
    expect(guide).toContain("GET /api/v0/repositories/{id}/stats");
  });

  it("keeps the prototype vulnerability surface split like the live console", () => {
    const html = repoFile("apps/console/prototype/eshu-console/Eshu Console.html");
    const app = repoFile("apps/console/prototype/eshu-console/console/app.jsx");
    const page = repoFile("apps/console/prototype/eshu-console/console/pages-vulnerability-parity.jsx");
    const guide = repoFile("apps/console/prototype/eshu-console/port/PORT-TO-CONSOLE.md");

    expect(html).toContain("console/pages-vulnerability-parity.jsx");
    expect(app).toContain("<Vulnerabilities data={data} client={liveClient}");
    expect(page).toContain("Reachable in services");
    expect(page).toContain("Known intelligence");
    expect(page).toContain("advisoryCatalog");
    expect(page).toContain("GET /api/v0/supply-chain/advisories");
    expect(page).toContain("/api/v0/supply-chain/vulnerabilities/");
    expect(page).toContain("Extended advisory evidence");
    expect(guide).toContain("GET /api/v0/supply-chain/vulnerabilities/{id}");
  });

  it("keeps the prototype graph explorer on the live query contracts", () => {
    const html = repoFile("apps/console/prototype/eshu-console/Eshu Console.html");
    const app = repoFile("apps/console/prototype/eshu-console/console/app.jsx");
    const page = repoFile("apps/console/prototype/eshu-console/console/pages-explorer-parity.jsx");

    expect(html).toContain("console/pages-explorer-parity.jsx");
    expect(app).toContain("client={liveClient}");
    expect(page).toContain("/api/v0/entities/resolve");
    expect(page).toContain("/api/v0/code/relationships");
    expect(page).toContain("max_depth: 1");
    expect(page).toContain("/api/v0/services/");
    expect(page).toContain("/api/v0/repositories/");
    expect(page).toContain("/api/v0/impact/entity-map");
    expect(page).toContain("Direct");
    expect(page).toContain("Neighborhood");
    expect(page).toContain("DEPLOYS_FROM");
    expect(page).toContain("DEPLOYS_HELM");
    expect(page).toContain("PACKAGES");
    expect(page).toContain("loadRepositoryDeploymentStoryGraph");
    expect(page).toContain("relationshipNodeKind");
    expect(page).toContain("relationshipNodeSub");
    expect(page).toContain("sourceLocationFromEdge");
    expect(page).toContain("sourceHref(value)");
    expect(page).toContain("repoId");
    expect(page).toContain("sourceLocation");
    expect(page).toContain('hashFor("reposource"');
    expect(page).toContain("Open source");
  });

  it("documents Graph Explorer as a live-hydrated prototype route", () => {
    const guide = repoFile("apps/console/prototype/eshu-console/port/PORT-TO-CONSOLE.md");

    expect(guide).toContain("Graph Explorer");
    expect(guide).toContain("POST /api/v0/entities/resolve");
    expect(guide).toContain("POST /api/v0/code/relationships");
    expect(guide).toContain("POST /api/v0/impact/entity-map");
    expect(guide).not.toContain("Demo-mode graph edges");
  });

  it("keeps the prototype workspace dossier route on the live query contracts", () => {
    const html = repoFile("apps/console/prototype/eshu-console/Eshu Console.html");
    const app = repoFile("apps/console/prototype/eshu-console/console/app.jsx");
    const page = repoFile("apps/console/prototype/eshu-console/console/pages-workspace-parity.jsx");

    expect(html).toContain("console/pages-workspace-parity.jsx");
    expect(app).toContain('route === "workspace"');
    expect(page).toContain("/api/v0/repositories/");
    expect(page).toContain("/api/v0/services/");
    expect(page).toContain("Deployment evidence map");
    expect(page).toContain("Typed deployment chain");
    expect(page).toContain("Evidence story");
    expect(page).toContain("Workspace unavailable");
    expect(page).toContain("DEPLOYS_HELM");
    expect(page).toContain("PACKAGES");
  });

  it("keeps the prototype repository source route on the live query contracts", () => {
    const html = repoFile("apps/console/prototype/eshu-console/Eshu Console.html");
    const app = repoFile("apps/console/prototype/eshu-console/console/app.jsx");
    const page = repoFile("apps/console/prototype/eshu-console/console/pages-source-parity.jsx");

    expect(html).toContain("console/pages-source-parity.jsx");
    expect(app).toContain('route === "reposource"');
    expect(page).toContain("/api/v0/repositories/");
    expect(page).toContain("/tree");
    expect(page).toContain("/content?path=");
    expect(page).toContain("/branches");
    expect(page).toContain("Indexed ref");
    expect(page).toContain("Repository source unavailable");
    expect(page).toContain("demo-indexed-ref");
    expect(page).toContain("repoSourceDisplayName");
    expect(page).not.toContain("Branch selection is pending");
    expect(page).not.toContain("<h2 style={{ marginTop: 8 }}>{repoId}");
  });

  it("keeps prototype live page overlays rejecting API error envelopes", () => {
    const explorer = repoFile("apps/console/prototype/eshu-console/console/pages-explorer-parity.jsx");
    const workspace = repoFile("apps/console/prototype/eshu-console/console/pages-workspace-parity.jsx");
    const source = repoFile("apps/console/prototype/eshu-console/console/pages-source-parity.jsx");

    for (const page of [explorer, workspace, source]) {
      expect(page).toContain("function apiData(env)");
      expect(page).toContain("env && env.error");
      expect(page).toContain("throw new Error(message)");
    }
  });

  it("keeps prototype live unwrap helpers rejecting API error envelopes", () => {
    const helperPages = [
      repoFile("apps/console/prototype/eshu-console/console/pages-repositories-parity.jsx"),
      repoFile("apps/console/prototype/eshu-console/console/pages-cloud-parity.jsx"),
      repoFile("apps/console/prototype/eshu-console/console/pages-observability-parity.jsx"),
      repoFile("apps/console/prototype/eshu-console/console/pages-dashboard-parity.jsx"),
      repoFile("apps/console/prototype/eshu-console/console/pages-live-parity.jsx"),
      repoFile("apps/console/prototype/eshu-console/console/pages-cloud.jsx")
    ];

    for (const page of helperPages) {
      expect(page).toContain("response && response.error");
      expect(page).toContain("throw new Error");
    }
  });

  it("keeps prototype dead-code locations wired to repository source deep links", () => {
    const html = repoFile("apps/console/prototype/eshu-console/Eshu Console.html");
    const page = repoFile("apps/console/prototype/eshu-console/console/pages-code.jsx");
    const deadCodeLoader = repoFile("apps/console/prototype/eshu-console/console/live-dead-code-loader.js");
    const loader = repoFile("apps/console/prototype/eshu-console/console/data.js");

    expect(html).toContain("console/live-dead-code-loader.js");
    expect(page).toContain('hashFor("reposource"');
    expect(page).toContain('hashFor("codegraph", "?candidate="');
    expect(page).toContain("lineStart");
    expect(page).toContain("Open source");
    expect(deadCodeLoader).toContain("/api/v0/code/dead-code");
    expect(deadCodeLoader).toContain("loadRepositoryNameLookup");
    expect(deadCodeLoader).toContain("repoDisplayName");
    expect(deadCodeLoader).not.toContain("row.entityId && row.file");
    expect(loader).toContain("repoNameById");
  });

  it("keeps the prototype code graph on current live code contracts", () => {
    const app = repoFile("apps/console/prototype/eshu-console/console/app.jsx");
    const page = repoFile("apps/console/prototype/eshu-console/console/pages-code.jsx");

    expect(app).toContain("<CodeGraph data={data} client={liveClient}");
    expect(page).toContain("/api/v0/code/relationships");
    expect(page).toContain("max_depth");
    expect(page).toContain("sourceHref");
    expect(page).toContain("deadCodeSourceRepo");
    expect(page).toContain("focusedNode");
    expect(page).toContain("locationLabel");
    expect(page).toContain("codeGraphCandidateParam");
    expect(page).toContain("relationshipNodeKind");
    expect(page).toContain("relationshipNodeSub");
    expect(page).toContain("sourceLocationFromCodeEdge");
    expect(page).toContain("sourceHrefFromNode");
    expect(page).toContain("locationLabelFromNode");
    expect(page).toContain("focusedNodeSourceHref");
    expect(page).toContain("focusedRepositoryLabel");
    expect(page).toContain("function apiData(env)");
    expect(page).toContain("env && env.error");
    expect(page).toContain("sourceAvailable");
  });

  it("keeps the prototype topology route on current live service topology contracts", () => {
    const html = repoFile("apps/console/prototype/eshu-console/Eshu Console.html");
    const app = repoFile("apps/console/prototype/eshu-console/console/app.jsx");
    const chain = repoFile("apps/console/prototype/eshu-console/console/deployment-chain-parity.js");
    const page = repoFile("apps/console/prototype/eshu-console/console/pages-live-parity.jsx");
    const guide = repoFile("apps/console/prototype/eshu-console/port/PORT-TO-CONSOLE.md");

    expect(html).toContain("console/deployment-chain-parity.js");
    expect(app).toContain("<Topology data={data} client={liveClient}");
    expect(app).toContain("data.servicesById = {}");
    expect(page).toContain("/api/v0/services/");
    expect(page).toContain("/story");
    expect(page).toContain("/context");
    expect(page).toContain("traffic evidence unavailable");
    expect(page).toContain("liveDeploymentChainGraph");
    expect(chain).toContain("DEPLOYS_HELM");
    expect(chain).toContain("PACKAGES");
    expect(chain).toContain("deployment_evidence.artifacts");
    expect(guide).toContain("GET /api/v0/services/{name}/story");
    expect(guide).toContain("GET /api/v0/services/{name}/context");
  });
});
