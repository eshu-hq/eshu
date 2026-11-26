import { describe, expect, it } from "vitest";

import {
  attentionItems,
  coverageAreas,
  exampleService,
  health,
  knowledgeSummary,
  orderForRole,
  teams,
} from "./homeModel";
import { roleLens, suggestedQuestions } from "./roles";
import {
  demoCatalog,
  demoCollectorReadiness,
  demoDocumentationCount,
  demoEcosystemOverview,
  demoImpactCount,
  demoIndexStatus,
  demoWorkflows,
} from "../../api/demoHomeFixtures";
import type { KnowledgeHomeData } from "../../api/knowledgeHome";

const readyData: KnowledgeHomeData = {
  overview: { status: "ready", data: demoEcosystemOverview, truth: null },
  index: { status: "ready", data: demoIndexStatus, truth: null },
  collectors: { status: "ready", data: demoCollectorReadiness, truth: null },
  vulnerabilities: { status: "ready", data: demoImpactCount, truth: null },
  docs: { status: "ready", data: demoDocumentationCount, truth: null },
  workflows: { status: "ready", data: demoWorkflows, truth: null },
  catalog: { status: "ready", data: demoCatalog, truth: null },
};

describe("Home model", () => {
  it("summarizes what Eshu knows in one plain sentence", () => {
    expect(knowledgeSummary(readyData)).toBe(
      "Eshu is connected to 128 repositories, 64 services and 212 running instances across your organization.",
    );
  });

  it("reports healthy knowledge when the index is healthy", () => {
    expect(health(readyData)).toEqual({ state: "healthy", summary: "Knowledge is up to date." });
  });

  it("explains a degraded index in plain language", () => {
    const degraded: KnowledgeHomeData = {
      ...readyData,
      index: {
        status: "ready",
        truth: null,
        data: {
          ...demoIndexStatus,
          status: "degraded",
          queue: { ...demoIndexStatus.queue, dead_letter: 3952 },
          reasons: ["3952 work items are dead-lettered", "2411 generations are failed"],
        },
      },
    };
    const result = health(degraded);
    expect(result.state).toBe("degraded");
    expect(result.state === "degraded" && result.notes).toEqual([
      "3,952 updates could not be processed.",
      "Some repositories failed their last refresh, so their answers are older.",
    ]);
  });

  it("raises urgent vulnerabilities and contradicted docs as attention items", () => {
    const items = attentionItems(readyData);
    expect(items.map((item) => item.id)).toEqual(["vulns", "docs"]);
    expect(items[0]).toMatchObject({
      tone: "critical",
      title: "6 urgent vulnerabilities affect software we run",
    });
    expect(items[1].title).toBe("4 documentation claims no longer match the code");
  });

  it("turns an unavailable vulnerability read into an explanatory item, not a zero", () => {
    const items = attentionItems({
      ...readyData,
      vulnerabilities: {
        status: "unavailable",
        reason: "slow",
        detail: "upstream request timeout",
      },
    });
    expect(items[0]).toMatchObject({ id: "vulns-unavailable", tone: "info" });
    expect(items[0].detail).toMatch(/took too long/);
  });

  it("puts the role's items first without hiding others", () => {
    const pipeline = attentionItems({
      ...readyData,
      index: {
        status: "ready",
        truth: null,
        data: { ...demoIndexStatus, queue: { dead_letter: 12 } },
      },
    });
    const forPlatform = orderForRole(pipeline, "platform").map((item) => item.id);
    expect(forPlatform[0]).toBe("pipeline");
    expect(forPlatform).toHaveLength(pipeline.length);
  });

  it("groups data sources into plain coverage areas", () => {
    const areas = coverageAreas(demoCollectorReadiness);
    const byId = Object.fromEntries(areas.map((area) => [area.id, area.state]));
    expect(byId).toMatchObject({
      code: "connected",
      delivery: "partial",
      operations: "partial",
      knowledge: "partial",
    });
  });

  it("groups services by owning team, largest first", () => {
    const rows = teams(demoCatalog);
    expect(rows[0]).toMatchObject({ name: "team-discovery", critical: 1 });
    expect(rows.map((row) => row.name)).toContain("team-payments");
  });

  it("personalizes suggested questions with a real service and server starter prompts", () => {
    const service = exampleService(demoCatalog);
    expect(service).toBe("checkout-service");
    const questions = suggestedQuestions(roleLens("security"), service, demoWorkflows);
    expect(questions).toContain("Is checkout-service exposed to the internet?");
    expect(questions).toContain("Which services are affected by CVE-2025-13465?");
    expect(questions.length).toBeLessThanOrEqual(4);
  });
});
