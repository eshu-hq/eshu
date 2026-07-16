import { readFileSync } from "node:fs";
import { resolve } from "node:path";

import { describe, expect, it } from "vitest";

function repoRoot(): string {
  return process.cwd().endsWith("apps/console") ? resolve(process.cwd(), "../..") : process.cwd();
}

function repoFile(path: string): string {
  return readFileSync(resolve(repoRoot(), path), "utf8");
}

describe("prototype graph and topology documentation parity", () => {
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
    expect(page).toContain("Related symbol source metadata unavailable");
    expect(page).toContain("function apiData(env)");
    expect(page).toContain("env && env.error");
    expect(page).toContain("sourceAvailable");
  });

  it("keeps the prototype topology route on current live service topology contracts", () => {
    const html = repoFile("apps/console/prototype/eshu-console/Eshu Console.html");
    const app = repoFile("apps/console/prototype/eshu-console/console/app.jsx");
    const chain = repoFile(
      "apps/console/prototype/eshu-console/console/deployment-chain-parity.js",
    );
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
    expect(chain).toContain("function artifactEdgeEvidence(artifact)");
    expect(chain).toContain("evidence: artifactEdgeEvidence(artifact)");
    expect(chain).toContain("evidence: artifactEdgeEvidence(deployArtifacts[0])");
    expect(chain).toContain(
      "Object.assign(window, { liveDeploymentChainGraph, artifactEdgeEvidence })",
    );
    expect(guide).toContain("GET /api/v0/services/{name}/story");
    expect(guide).toContain("GET /api/v0/services/{name}/context");
  });
});
