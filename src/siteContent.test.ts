import { describe, expect, it } from "vitest";

import { siteContent } from "./siteContent";

describe("siteContent", () => {
  it("uses the launch positioning and required developer surfaces", () => {
    expect(siteContent.hero.coreLine).toBe("One Graph. Every Layer. Every Role.");
    expect(siteContent.hero.heading).toBe(
      "The institutional knowledge layer now has an agentic answer surface."
    );
    expect(siteContent.hero.description).toContain("Ask Eshu");
    expect(siteContent.hero.description).toContain("evidence packets");
    expect(siteContent.hero.primaryCta.label).toBe("Try it locally");
    expect(siteContent.hero.primaryCta.href).toBe("#try-it");
    expect(siteContent.hero.secondaryCta.label).toBe("Read the docs");
    expect(siteContent.hero.secondaryCta.href).toBe(
      "https://github.com/eshu-hq/eshu/tree/main/docs/public"
    );
    expect(siteContent.terminalCommands).toEqual([
      "eshu scan",
      "POST /api/v0/ask",
      "eshu trace service checkout",
      "mcp: ask",
      "mcp: list_supply_chain_impact_findings",
      "mcp: compose_replatforming_plan"
    ]);
  });

  it("summarizes the six shipped surfaces that changed the launch story", () => {
    expect(siteContent.whatsNew.map((item) => item.title)).toEqual([
      "Ask Eshu",
      "Evidence packets v2",
      "Generated capability matrix",
      "Pre-change developer workflow",
      "Competitive parity gate",
      "Operator control plane"
    ]);
    const allSummaries = siteContent.whatsNew
      .map((item) => `${item.summary} ${item.detail}`)
      .join(" ");
    expect(allSummaries).toContain("provider-portable");
    expect(allSummaries).toContain("portable proof artifacts");
    expect(allSummaries).toContain("fails on drift");
    expect(allSummaries).toContain("developer_change_plan.v1");
    expect(allSummaries).toContain("CI-verified");
    expect(allSummaries).toContain("3 AM");
  });

  it("covers the launch capability surfaces plus the Ask Eshu layer", () => {
    expect(siteContent.capabilities.map((capability) => capability.title)).toEqual([
      "Agentic Q&A",
      "Supply chain traceability",
      "Code-to-cloud tracing",
      "Multi-cloud re-platforming",
      "Incident response context",
      "IaC governance and drift",
      "Code intelligence",
      "AI assistant context",
      "Institutional knowledge"
    ]);
    const askCapability = siteContent.capabilities.find(
      (capability) => capability.title === "Agentic Q&A"
    );
    expect(askCapability?.description).toContain("read-only Cypher");
    expect(askCapability?.description).toContain("SQL sandbox");
  });

  it("lists docs navigation and the surfaces section", () => {
    expect(siteContent.nav.find((item) => item.label === "Docs")?.href).toBe(
      "https://github.com/eshu-hq/eshu/tree/main/docs/public"
    );
    expect(
      siteContent.surfaces.map((surface) => surface.title).sort()
    ).toEqual(
      ["HTTP API", "CLI", "Console", "MCP", "SDK"].sort((a, b) => a.localeCompare(b))
    );
  });

  it("includes representative personas with the Ask Eshu user", () => {
    expect(siteContent.personaDemos.map((persona) => persona.role)).toEqual([
      "Ask Eshu user",
      "SRE / on-call",
      "Security analyst",
      "Platform engineer",
      "Engineer switching teams",
      "CTO",
      "Developer",
      "Sales engineer",
      "Data engineer"
    ]);
    const askPersona = siteContent.personaDemos[0];
    expect(askPersona.primaryTool).toBe("ask");
    expect(askPersona.answer).toContain("per-token streaming");
    expect(askPersona.answer).toContain("evidence");
    for (const persona of siteContent.personaDemos) {
      expect(persona.primaryTool).toMatch(/^[a-z_]+$/);
      expect(persona.context.length).toBeGreaterThan(0);
      expect(persona.question.length).toBeGreaterThan(0);
      expect(persona.answer.length).toBeGreaterThan(0);
    }
  });

  it("surfaces role prompts anchored to the supply-chain beachhead and re-platforming", () => {
    expect(siteContent.rolePrompts.map((prompt) => prompt.role)).toEqual([
      "New engineer",
      "SRE / on-call",
      "Security analyst",
      "Platform engineer",
      "Migration architect",
      "VP Engineering",
      "CTO",
      "Sales engineer"
    ]);
    const allPrompts = siteContent.rolePrompts.map((p) => p.prompt).join(" ");
    expect(allPrompts).toContain("ask");
    expect(allPrompts).toContain("CVE");
    expect(allPrompts).toContain("re-platforming");
    expect(allPrompts).toContain("blast radius");
  });

  it("names the organization-wide proof points including agentic Q&A", () => {
    expect(
      siteContent.proofPoints.map((point) => point.value).sort()
    ).toEqual(
      ["Ask Eshu", "Multi-cloud", "Open source", "Personas", "Supply chain"].sort()
    );
    const askPoint = siteContent.proofPoints.find((p) => p.value === "Ask Eshu");
    expect(askPoint?.description).toContain("self-hosted");
    expect(askPoint?.description).toContain("provider-portable");
    expect(askPoint?.description).toContain("read-only Cypher");
    const supplyChainPoint = siteContent.proofPoints.find(
      (p) => p.value === "Supply chain"
    );
    expect(supplyChainPoint?.description).toContain("promotion_state");
    expect(supplyChainPoint?.description).toContain("Refuses findings");
    const multiCloudPoint = siteContent.proofPoints.find(
      (p) => p.value === "Multi-cloud"
    );
    expect(multiCloudPoint?.description).toContain("compose_replatforming_plan");
  });

  it("defines the interactive demo content with the supply-chain pipeline", () => {
    expect(siteContent.demoTrace.nodes.map((node) => node.label)).toEqual([
      "Code",
      "Supply chain",
      "IaC",
      "Cloud",
      "Runtime",
      "Incidents"
    ]);
    expect(siteContent.commandDemos.map((command) => command.command)).toEqual([
      "eshu scan --json",
      "POST /api/v0/ask",
      "eshu trace service checkout",
      "mcp: ask",
      "mcp: list_supply_chain_impact_findings",
      "mcp: compose_replatforming_plan"
    ]);
    expect(
      siteContent.cleanupModes.map((mode) => mode.label).sort()
    ).toEqual(["Dead IaC", "Dead code", "Unmanaged resources"].sort());
  });

  it("keeps demo outputs aligned with shipped command output shapes", () => {
    const demosByCommand = Object.fromEntries(
      siteContent.commandDemos.map((demo) => [demo.command, demo.output])
    );

    expect(demosByCommand["eshu scan --json"]).toEqual(
      expect.arrayContaining([
        '"status": "ready",',
        '"succeeded": 8347,',
        '"queue_zero_ms": 853600,',
        '"freshness": "current"'
      ])
    );
    expect(demosByCommand["eshu scan --json"]).not.toContain("repos: 896 indexed");
    expect(demosByCommand["eshu scan --json"]).not.toContain("elapsed: 14m13.6s");

    expect(demosByCommand["POST /api/v0/ask"]).toEqual(
      expect.arrayContaining([
        "POST /api/v0/ask",
        "Question: which services are affected by CVE-2024-3094?",
        "Answer: partial",
        "Evidence packet: investigation_evidence_packet.v2",
        "Missing evidence: none for published finding"
      ])
    );

    expect(demosByCommand["eshu trace service checkout"]).toEqual(
      expect.arrayContaining([
        "Service: checkout-service",
        "Truth freshness: fresh",
        "Code to runtime:",
        "Trace status: partial",
        "Missing evidence: runtime"
      ])
    );

    expect(demosByCommand["mcp: list_supply_chain_impact_findings"]).toEqual(
      expect.arrayContaining([
        "Findings: 7",
        "Affected: npm:lodash@4.17.11",
        "Ecosystem: npm",
        "Confidence: exact (affected_exact)",
        "Promotion: vulnerability_intelligence -> implemented"
      ])
    );

    expect(demosByCommand["mcp: compose_replatforming_plan"]).toEqual(
      expect.arrayContaining([
        "Scope: aws/account=123456789012",
        "Plan: 4 items, ordered into migration waves",
        "Wave 1 (early-safe): 2 ready import candidates",
        "Read-only: never runs Terraform or mutates state"
      ])
    );

    expect(demosByCommand["mcp: ask"]).toEqual(
      expect.arrayContaining([
        "Tool: ask",
        "Streaming: SSE",
        "Sandbox: read-only Cypher + SQL",
        "Truth: derived with evidence handles"
      ])
    );
  });

  it("covers the breadth of the code-to-cloud ingestion surface in the coverage blurb", () => {
    expect(siteContent.coverage).toContain("22+ source languages");
    expect(siteContent.coverage).toContain("13+ package ecosystems");
    expect(siteContent.coverage).toContain("7 container registries");
    expect(siteContent.coverage).toContain("4 vulnerability sources");
    expect(siteContent.coverage).toContain("134 AWS service scanners");
    expect(siteContent.coverage).toContain("profile-aware");
    expect(siteContent.coverage).toContain("capability catalog");
  });

  it("states an MCP tool count within the machine-verified catalog, not inflated", () => {
    // The generated surface inventory holds 147 implemented mcp_tool records.
    // Keep the marketing claim exact and machine-verified.
    const inflatedToolCountPattern = new RegExp("14" + "0\\+");
    const mcpSurface = siteContent.surfaces.find((s) => s.title === "MCP");
    const aiContext = siteContent.capabilities.find(
      (c) => c.title === "AI assistant context"
    );
    for (const text of [mcpSurface?.description, aiContext?.description]) {
      expect(text).toBeDefined();
      expect(text).not.toMatch(inflatedToolCountPattern);
      expect(text).not.toMatch(/\d+\s+families/);
      expect(text).toContain("147");
      expect(text).toContain("capability catalog");
    }
    const serialized = JSON.stringify(siteContent);
    expect(serialized).not.toContain("11" + "5+");
    expect(serialized).not.toContain("12" + "5+");
    expect(serialized).not.toMatch(new RegExp("\\b1" + "2 families\\b"));
  });

  it("anchors the 'Try it' section to the runnable setup and the demo runbooks", () => {
    expect(siteContent.tryIt.heading.toLowerCase()).toContain("try it");
    expect(siteContent.tryIt.steps.length).toBeGreaterThanOrEqual(4);
    expect(siteContent.tryIt.steps.join(" ")).toContain("docker compose up");
    expect(siteContent.tryIt.steps.join(" ")).toContain("eshu mcp");
    expect(siteContent.tryIt.steps.join(" ")).toContain("POST /api/v0/ask");
    expect(siteContent.tryIt.steps.join(" ")).not.toContain("eshu ask");
    expect(siteContent.tryIt.firstQuestion).toContain(
      "which services are affected by CVE-2024-3094"
    );
    expect(siteContent.references.supplyChainDemo).toContain("supply-chain-demo.md");
    expect(siteContent.references.replatformingDemo).toContain("aws-to-azure");
    expect(siteContent.references.fullPersonaMatrix).toContain(
      "persona-question-tool-matrix"
    );
  });

  it("states the competitive difference as machine-verified parity", () => {
    const allClaims = siteContent.difference.points
      .map((point) => `${point.target} ${point.claim}`)
      .join(" ");
    expect(allClaims).toContain("CI-verified");
    expect(allClaims).toContain("competitive-parity validate");
    expect(allClaims).toContain("proof-bundle");
    expect(allClaims).toContain("capability surfaces");
  });
});
