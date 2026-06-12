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
    expect(guide).toContain("GET /api/v0/observability/coverage/correlations?provider=");
    expect(guide).not.toContain("Graph drill (next)");
    expect(guide).not.toContain("Copy `eshuConsoleLive.ts`");
    expect(guide).toContain("Use the production loaders in `apps/console/src/api/` as the current contract");
  });

  it("uses the same observability provider vocabulary as the live console", () => {
    const page = repoFile("apps/console/prototype/eshu-console/console/pages-cloud.jsx");

    expect(page).toContain("grafana, prometheus/mimir, loki, and tempo");
    expect(page).not.toContain("Prometheus, CloudWatch, OpenTelemetry, Loki, Datadog");
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
});
