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
      "896 repos",
      "organization-wide"
    ]);
    expect(siteContent.proofPoints[0].description).toContain("where software runs");
    expect(siteContent.proofPoints[1].description).toContain("14m13.6s");
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
      "eshu scan --json",
      "eshu trace service checkout",
      "eshu map --from terraform/aws_lb.main",
      "eshu docs verify"
    ]);
    expect(siteContent.personaDemos.map((persona) => persona.role)).toContain("Leadership");
    expect(siteContent.cleanupModes.map((mode) => mode.label)).toEqual(["Dead code", "Dead IaC"]);
  });

  it("keeps demo outputs aligned with shipped command output shapes", () => {
    const demosByCommand = Object.fromEntries(
      siteContent.commandDemos.map((demo) => [demo.command, demo.output])
    );

    expect(demosByCommand["eshu scan --json"]).toEqual(
      expect.arrayContaining([
        "\"status\": \"ready\",",
        "\"succeeded\": 8347,",
        "\"queue_zero_ms\": 853600,",
        "\"freshness\": \"current\""
      ])
    );
    expect(demosByCommand["eshu scan --json"]).not.toContain("repos: 896 indexed");
    expect(demosByCommand["eshu scan --json"]).not.toContain("elapsed: 14m13.6s");

    expect(demosByCommand["eshu trace service checkout"]).toEqual(
      expect.arrayContaining([
        "Service: checkout-service",
        "Truth freshness: fresh",
        "Code to runtime:",
        "Trace status: partial",
        "Missing evidence: runtime"
      ])
    );

    expect(demosByCommand["eshu map --from terraform/aws_lb.main"]).toEqual(
      expect.arrayContaining([
        "Map: terraform/aws_lb.main",
        "Resolved: TerraformResource tfstate:aws_lb.main (aws_lb.main)",
        "Evidence: 2 relationships"
      ])
    );

    expect(demosByCommand["eshu docs verify"]).toEqual(
      expect.arrayContaining([
        "Docs verify: documents=3 claims=6 valid=4 contradicted=1 missing_evidence=1 unsupported=0 truncated=false",
        "- contradicted terraform_address aws_sqs_queue.missing",
        "- missing_evidence image_ref ghcr.io/acme/checkout:1.2.3"
      ])
    );
    expect(demosByCommand["eshu docs verify"]).not.toContain(
      "result: fail-on contradicted would exit 1"
    );
    expect(demosByCommand["eshu docs verify"]).not.toContain(
      "docs/architecture/payments.md: missing runtime edge"
    );
  });

  it("labels broad coverage as profile-aware and derived where appropriate", () => {
    expect(siteContent.coverage).toContain("exact and derived");
    expect(siteContent.coverage).toContain("profile");
    expect(siteContent.coverage).toContain("candidate");
  });
});
