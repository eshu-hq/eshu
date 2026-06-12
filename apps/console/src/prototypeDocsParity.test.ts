import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

function repoFile(path: string): string {
  return readFileSync(resolve(process.cwd(), path), "utf8");
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
    expect(guide).toContain("GET /api/v0/observability/coverage/correlations?provider=");
    expect(guide).not.toContain("Graph drill (next)");
    expect(guide).not.toContain("no historical series endpoint");
    expect(guide).not.toContain("Copy `eshuConsoleLive.ts`");
    expect(guide).toContain("Use the production loaders in `apps/console/src/api/` as the current contract");
  });

  it("uses the same observability provider vocabulary as the live console", () => {
    const page = repoFile("apps/console/prototype/eshu-console/console/pages-cloud.jsx");

    expect(page).toContain("grafana, prometheus/mimir, loki, and tempo");
    expect(page).not.toContain("Prometheus, CloudWatch, OpenTelemetry, Loki, Datadog");
  });

  it("keeps the prototype cloud route on the canonical inventory contract", () => {
    const app = repoFile("apps/console/prototype/eshu-console/console/app.jsx");
    const page = repoFile("apps/console/prototype/eshu-console/console/pages-cloud.jsx");
    const guide = repoFile("apps/console/prototype/eshu-console/port/PORT-TO-CONSOLE.md");

    expect(app).toContain("<Cloud data={data} client={liveClient}");
    expect(page).toContain("/api/v0/cloud/inventory");
    expect(page).toContain("Canonical inventory");
    expect(page).toContain("No canonical inventory rows");
    expect(guide).toContain("GET /api/v0/cloud/inventory");
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
    expect(page).toContain("Repository detail unavailable");
    expect(guide).toContain("GET /api/v0/repositories");
    expect(guide).toContain("GET /api/v0/repositories/{id}/stats");
  });

  it("keeps the prototype vulnerability surface split like the live console", () => {
    const html = repoFile("apps/console/prototype/eshu-console/Eshu Console.html");
    const page = repoFile("apps/console/prototype/eshu-console/console/pages-vulnerability-parity.jsx");

    expect(html).toContain("console/pages-vulnerability-parity.jsx");
    expect(page).toContain("Reachable in services");
    expect(page).toContain("Known intelligence");
    expect(page).toContain("advisoryCatalog");
    expect(page).toContain("GET /api/v0/supply-chain/advisories");
  });

  it("keeps the prototype graph explorer on the live query contracts", () => {
    const html = repoFile("apps/console/prototype/eshu-console/Eshu Console.html");
    const app = repoFile("apps/console/prototype/eshu-console/console/app.jsx");
    const page = repoFile("apps/console/prototype/eshu-console/console/pages-explorer-parity.jsx");

    expect(html).toContain("console/pages-explorer-parity.jsx");
    expect(app).toContain("client={liveClient}");
    expect(page).toContain("/api/v0/entities/resolve");
    expect(page).toContain("/api/v0/code/relationships");
    expect(page).toContain("/api/v0/services/");
    expect(page).toContain("/api/v0/impact/entity-map");
    expect(page).toContain("Direct");
    expect(page).toContain("Neighborhood");
    expect(page).toContain("DEPLOYS_FROM");
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
    expect(page).toContain("Evidence story");
    expect(page).toContain("Workspace unavailable");
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
    expect(page).toContain("Repository source unavailable");
    expect(page).toContain("indexed ref");
  });

  it("keeps prototype dead-code locations wired to repository source deep links", () => {
    const page = repoFile("apps/console/prototype/eshu-console/console/pages-code.jsx");

    expect(page).toContain('hashFor("reposource"');
    expect(page).toContain("lineStart");
    expect(page).toContain("Open source");
  });

  it("keeps the prototype code graph on current live code contracts", () => {
    const app = repoFile("apps/console/prototype/eshu-console/console/app.jsx");
    const page = repoFile("apps/console/prototype/eshu-console/console/pages-code.jsx");

    expect(app).toContain("<CodeGraph data={data} client={liveClient}");
    expect(page).toContain("/api/v0/code/relationships");
    expect(page).toContain("max_depth");
    expect(page).toContain("sourceHref");
  });

  it("keeps the prototype topology route on current live service topology contracts", () => {
    const app = repoFile("apps/console/prototype/eshu-console/console/app.jsx");
    const page = repoFile("apps/console/prototype/eshu-console/console/pages-live-parity.jsx");
    const guide = repoFile("apps/console/prototype/eshu-console/port/PORT-TO-CONSOLE.md");

    expect(app).toContain("<Topology data={data} client={liveClient}");
    expect(app).toContain("data.servicesById = {}");
    expect(page).toContain("/api/v0/services/");
    expect(page).toContain("/story");
    expect(page).toContain("/context");
    expect(page).toContain("traffic evidence unavailable");
    expect(guide).toContain("GET /api/v0/services/{name}/story");
    expect(guide).toContain("GET /api/v0/services/{name}/context");
  });
});
