import { cleanupModes, commandDemos, demoTrace, terminalCommands } from "./siteContentDemos";
import {
  docsHref,
  githubHref,
  lightweightAuditHref,
  personaMatrixHref,
  replatformingDemoHref,
  supplyChainDemoHref,
} from "./siteContentLinks";
import { personaDemos, proofPoints, rolePrompts } from "./siteContentOrg";
import { capabilities, coverage, pipeline, surfaces, useCases } from "./siteContentProduct";
import type { NavItem } from "./siteContentTypes";
import { whatsNew } from "./siteContentWhatsNew";

export type {
  Capability,
  CleanupMode,
  CommandDemo,
  DemoNode,
  PersonaDemo,
  PipelineStep,
  ProofPoint,
  RolePrompt,
  Surface,
  UseCase,
  WhatsNewItem,
} from "./siteContentTypes";

/** Public launch-page content assembled from focused content modules. */
export const siteContent = {
  nav: [
    { label: "Product", href: "#product" },
    { label: "What's new", href: "#whats-new" },
    { label: "How it works", href: "#how-it-works" },
    { label: "Personas", href: "#personas" },
    { label: "Try it", href: "#try-it" },
    { label: "Use cases", href: "#use-cases" },
    { label: "Docs", href: docsHref },
  ] satisfies readonly NavItem[],
  hero: {
    coreLine: "One Graph. Every Layer. Every Role.",
    heading: "The institutional knowledge layer now has an agentic answer surface.",
    description:
      "Eshu connects code, dependencies, supply chain, infrastructure, and runtime into one evidence-backed graph. Ask Eshu turns that graph into a self-hosted answer surface with streaming responses, read-only query tools, and evidence packets when the answer needs proof. When evidence is missing, Eshu says so.",
    primaryCta: { label: "Try it locally", href: "#try-it" },
    secondaryCta: { label: "Read the docs", href: docsHref },
  },
  whatsNew,
  capabilities,
  pipeline,
  terminalCommands,
  demoTrace,
  commandDemos,
  personaDemos,
  cleanupModes,
  surfaces,
  coverage,
  proofPoints,
  rolePrompts,
  useCases,
  tryIt: {
    heading: "Try it in under a minute.",
    steps: [
      "git clone https://github.com/eshu-hq/eshu",
      "cd eshu",
      "docker compose up --build",
      "eshu mcp setup    # prints the client snippet for Claude Code, Codex, Cursor, or VS Code",
      "eshu mcp start   # boots the local MCP server",
      "POST /api/v0/ask # HTTP Ask Eshu endpoint for app integrations",
    ],
    firstQuestion:
      'Then in your MCP client or POST /api/v0/ask: "which services are affected by CVE-2024-3094?"',
    ctaLabel: "View on GitHub",
    ctaHref: githubHref,
  },
  difference: {
    heading: "What makes Eshu different.",
    points: [
      {
        target: "Snyk",
        claim:
          "Returns findings from KEV + EPSS alone. Eshu refuses unless owned evidence proves the path.",
      },
      {
        target: "Wiz",
        claim:
          "Knows cloud security. Eshu knows cloud security, the code, and the package chain that produced the deploy.",
      },
      {
        target: "Sourcegraph",
        claim:
          "Finds code. Eshu finds code, the deployment chain, the blast radius, and the supply-chain risk.",
      },
      {
        target: "Firefly / Morpheus",
        claim:
          "Do cloud governance. Eshu does cloud governance, the re-platforming plan, and the institutional knowledge layer.",
      },
      {
        target: "Firehydrant / incident.io",
        claim:
          "Do incident response. Eshu does incident response, supply-chain context, and deployment evidence.",
      },
      {
        target: "Parity gate",
        claim:
          "Eshu parity is CI-verified, not marketing-verified: `eshu competitive-parity validate` scores report, proof-bundle, and capability surfaces against named competitors.",
      },
      {
        target: "The unification",
        claim:
          "No competitor ships all of this in one graph, behind one MCP server, with one set of truth envelopes, MIT-licensed and self-hosted.",
      },
    ],
  },
  references: {
    fullPersonaMatrix: personaMatrixHref,
    supplyChainDemo: supplyChainDemoHref,
    replatformingDemo: replatformingDemoHref,
    lightweightAudit: lightweightAuditHref,
  },
  closing: {
    heading: "Stop losing institutional knowledge to tenure.",
    description:
      "Eshu is the answer: a queryable, evidence-backed knowledge layer that builds itself from your actual artifacts. New hires ramp in days. Engineers switch teams without losing context. Customer-facing teams answer questions from the same source of truth as your engineers. Security, platform, and SRE work from the same data.",
  },
} as const;
