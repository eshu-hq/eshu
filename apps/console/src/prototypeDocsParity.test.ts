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
    expect(guide).toContain("GET /api/v0/dependencies");
    expect(guide).toContain("GET /api/v0/observability/coverage/correlations?provider=");
    expect(guide).not.toContain("Graph drill (next)");
  });

  it("uses the same observability provider vocabulary as the live console", () => {
    const page = repoFile("apps/console/prototype/eshu-console/console/pages-cloud.jsx");

    expect(page).toContain("grafana, prometheus/mimir, loki, and tempo");
    expect(page).not.toContain("Prometheus, CloudWatch, OpenTelemetry, Loki, Datadog");
  });
});
