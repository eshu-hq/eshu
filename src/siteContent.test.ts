import { describe, expect, it } from "vitest";
import { siteContent } from "./siteContent";

describe("siteContent", () => {
  it("uses the public launch positioning and required developer surfaces", () => {
    expect(siteContent.hero.coreLine).toBe("One Graph. Every Layer. True Path.");
    expect(siteContent.hero.heading).toBe("Find the true path through your stack.");
    expect(siteContent.hero.primaryCta.label).toBe("View on GitHub");
    expect(siteContent.hero.secondaryCta.label).toBe("Read the docs");
    expect(siteContent.hero.secondaryCta.href).toBe(
      "https://github.com/eshu-hq/eshu/tree/main/docs/docs"
    );
    expect(siteContent.terminalCommands).toEqual([
      "eshu scan",
      "eshu trace service checkout",
      "eshu map --from terraform/aws_lb.main",
      "eshu docs verify"
    ]);
  });

  it("covers the requested product capabilities and use cases", () => {
    expect(siteContent.capabilities.map((capability) => capability.title)).toEqual([
      "Trace code to cloud",
      "Keep docs honest",
      "Find dead code and dead IaC",
      "Map dependencies",
      "Give humans and AI the same source of truth"
    ]);
    expect(siteContent.useCases.map((useCase) => useCase.question)).toEqual([
      "What owns this resource?",
      "Is this Terraform still used?",
      "Where does this service run?",
      "Which docs are stale?",
      "What changes if this dependency moves?"
    ]);
  });

  it("links docs navigation to a deployed external route", () => {
    expect(siteContent.nav.find((item) => item.label === "Docs")?.href).toBe(
      "https://github.com/eshu-hq/eshu/tree/main/docs/docs"
    );
    expect(siteContent.surfaces.map((surface) => surface.title)).toEqual([
      "CLI",
      "API",
      "MCP",
      "Docs checks"
    ]);
  });

  it("surfaces role prompts and parser coverage from the docs", () => {
    expect(siteContent.rolePrompts.map((prompt) => prompt.role)).toEqual([
      "Software engineering",
      "Platform engineering",
      "SRE and incident response",
      "Documentation and support"
    ]);
    expect(siteContent.coverage).toContain("SQL");
    expect(siteContent.coverage).toContain("Terraform");
    expect(siteContent.coverage).toContain("dead IaC");
  });

  it("highlights Kubernetes deployment coverage, indexing scale, and org-wide use", () => {
    expect(siteContent.proofPoints.map((point) => point.value)).toEqual([
      "Kubernetes",
      "nearly 900 repos",
      "organization-wide"
    ]);
    expect(siteContent.proofPoints[0].description).toContain("where software runs");
    expect(siteContent.proofPoints[1].description).toContain("under 15 minutes");
    expect(siteContent.proofPoints[2].description).toContain("whole organization");
  });

  it("defines the interactive open-source demo content", () => {
    expect(siteContent.demoTrace.nodes.map((node) => node.label)).toEqual([
      "Code",
      "SQL",
      "Terraform",
      "Kubernetes",
      "Cloud",
      "Runtime",
      "Docs"
    ]);
    expect(siteContent.commandDemos.map((command) => command.command)).toEqual([
      "eshu scan",
      "eshu trace service checkout",
      "eshu map --from terraform/aws_lb.main",
      "eshu docs verify"
    ]);
    expect(siteContent.personaDemos.map((persona) => persona.role)).toContain("Leadership");
    expect(siteContent.cleanupModes.map((mode) => mode.label)).toEqual(["Dead code", "Dead IaC"]);
  });
});
