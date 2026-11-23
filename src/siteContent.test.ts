import { describe, expect, it } from "vitest";

import { siteContent } from "./siteContent";

describe("siteContent", () => {
  it("exposes accurate launch positioning, primary CTAs, and zero internal links", () => {
    expect(siteContent.hero.heading).toContain("organizational knowledge base");
    expect(siteContent.hero.copyCommand).toBe("eshu demo up");
    expect(siteContent.hero.primaryCta).toEqual({
      label: "Try it locally",
      href: "#try-it",
    });
    expect(siteContent.hero.secondaryCta.label).toBe("Read the docs");
    expect(siteContent.hero.secondaryCta.href).toBe(siteContent.links.docs);

    const serialized = JSON.stringify(siteContent);
    expect(serialized).not.toContain("docs/internal/");
    expect(serialized).not.toContain("147 MCP tools");
    expect(serialized).not.toContain("147 tools");
  });

  it("does not include named vendor comparisons", () => {
    const serialized = JSON.stringify(siteContent);
    for (const vendor of [
      "Snyk",
      "Wiz",
      "Sourcegraph",
      "Firefly",
      "Morpheus",
      "Firehydrant",
      "incident.io",
    ]) {
      expect(serialized).not.toContain(vendor);
    }
  });

  it("matches verified platform surface counts, readiness legend, and all 23 collector lanes", () => {
    const metricsByLabel = Object.fromEntries(siteContent.metrics.map((m) => [m.label, m.value]));
    expect(metricsByLabel["Read-only MCP tools"]).toBe("173");
    expect(metricsByLabel["HTTP API routes"]).toBe("281");
    expect(metricsByLabel["Correlation pipelines"]).toBe("88");
    expect(metricsByLabel["Console UI pages"]).toBe("42");
    expect(metricsByLabel["Source languages"]).toBe("22");
    expect(metricsByLabel["Read-only collectors"]).toBe("23");

    expect(siteContent.readinessLegend).toHaveLength(4);
    expect(siteContent.collectors).toHaveLength(23);
    const lanes = new Set(siteContent.collectors.map((c) => c.lane));
    expect(lanes.size).toBe(23);
  });

  it("includes organizational roles, rollout phases, Day-2 ops facts, and valid hero traces", () => {
    expect(siteContent.roleScenarios.map((r) => r.role)).toEqual([
      "Support & Sysadmin",
      "Software Engineer",
      "SecOps & AppSec",
      "SRE & On-Call",
      "Platform Engineer",
      "CTO & VP Engineering",
    ]);
    expect(siteContent.rolloutPhases.map((p) => p.phase)).toEqual(["Day 1", "Week 1", "Month 1"]);
    expect(siteContent.opsFacts).toHaveLength(4);

    const validNodeIds = new Set(siteContent.demoTrace.nodes.map((n) => n.id));
    for (const trace of siteContent.heroTraces) {
      expect(trace.sourceScope.length).toBeGreaterThan(0);
      for (const id of trace.activeNodeIds) {
        expect(validNodeIds.has(id)).toBe(true);
      }
    }
  });

  it("covers all six capability pillars, five surfaces, and quickstart prerequisites", () => {
    expect(siteContent.capabilityTabs).toHaveLength(6);
    expect(siteContent.surfaces.map((s) => s.title)).toEqual([
      "MCP Server",
      "Self-Hosted Console",
      "CLI",
      "HTTP API",
      "Go Collector SDK & Host",
    ]);
    for (const tab of siteContent.quickstartTabs) {
      expect(tab.prerequisites.length).toBeGreaterThan(0);
    }
    const deployCommands = siteContent.quickstartTabs[1].commands.join("\n");
    expect(deployCommands).toContain("eshu-deploy");
    expect(deployCommands).toContain("docker compose up -d");
  });
});
