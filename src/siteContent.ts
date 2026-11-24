import { collectorCategories, collectors, readinessLegend } from "./siteContentCollectors";
import { commandDemos, demoTrace, heroTraces, quickstartTabs } from "./siteContentDemos";
import {
  architectureDocHref,
  demoDocHref,
  deployHref,
  docsHref,
  firstRunDocHref,
  githubHref,
  licenseHref,
  mcpDocHref,
  orgHref,
  readinessDocHref,
  replatformingDemoHref,
  repositoriesHref,
  securityHref,
  supplyChainDemoHref,
} from "./siteContentLinks";
import { capabilityTabs, metrics, pipeline, principles, surfaces } from "./siteContentProduct";
import { opsFacts, roleScenarios, rolloutPhases } from "./siteContentRoles";
import type { FooterGroup, NavItem } from "./siteContentTypes";

export type {
  CapabilityHighlight,
  CapabilityTab,
  CollectorCategory,
  CollectorItem,
  CollectorReadiness,
  CommandDemo,
  DemoNode,
  FooterGroup,
  HeroTrace,
  MetricStat,
  NavItem,
  OpsFact,
  PipelineStep,
  PrincipleItem,
  QuickstartTab,
  ReadinessLegendItem,
  RoleScenario,
  RolloutPhase,
  SurfaceItem,
} from "./siteContentTypes";

/** Complete, machine-aligned launch content assembled from focused modules. */
export const siteContent = {
  nav: [
    { label: "By role", href: "#roles" },
    { label: "Capabilities", href: "#capabilities" },
    { label: "Collectors", href: "#collectors" },
    { label: "Deploy", href: "#deploy" },
    { label: "Workbench", href: "#workbench" },
    { label: "Try it", href: "#try-it" },
    { label: "Docs", href: docsHref },
  ] satisfies readonly NavItem[],
  links: {
    github: githubHref,
    org: orgHref,
    deploy: deployHref,
    docs: docsHref,
    repositories: repositoriesHref,
    readiness: readinessDocHref,
  },
  hero: {
    eyebrow: "MIT-licensed · Self-hosted · Zero vendor telemetry",
    heading: "Your living organizational knowledge base — backed by evidence.",
    description:
      "Eshu continuously connects your repositories, dependencies, container images, SBOMs, Terraform, cloud posture, incidents, and docs into one self-hosted evidence graph. Engineers, SecOps, SRE, support, leadership, and AI assistants all query the same source of truth — and when evidence is missing, Eshu says so.",
    copyCommand: "eshu demo up",
    primaryCta: { label: "Try it locally", href: "#try-it" },
    secondaryCta: { label: "Read the docs", href: docsHref },
    footnote:
      "Useful on Day 1 with Git repos alone · Works with Claude Code, Cursor, VS Code, Codex & any MCP client",
  },
  metrics,
  demoTrace,
  heroTraces,
  roleScenarios,
  capabilityTabs,
  collectorCategories,
  readinessLegend,
  collectors,
  rolloutPhases,
  opsFacts,
  pipeline,
  commandDemos,
  surfaces,
  principles,
  quickstartTabs,
  closing: {
    heading: "Run your org's evidence graph on your own infrastructure.",
    description:
      "Start with a zero-credential demo or Git repos alone on Day 1, connect coding assistants over MCP, and opt into read-only security, cloud, and operations collectors at your own pace.",
  },
  footer: [
    {
      heading: "Documentation",
      links: [
        { label: "Public Docs", href: docsHref },
        { label: "Demo Mode", href: demoDocHref },
        { label: "First Successful Run", href: firstRunDocHref },
        { label: "Connect MCP", href: mcpDocHref },
        { label: "Architecture", href: architectureDocHref },
      ],
    },
    {
      heading: "Runbooks & Proof",
      links: [
        { label: "Collector & Reducer Readiness", href: readinessDocHref },
        { label: "Supply-Chain Demo Runbook", href: supplyChainDemoHref },
        { label: "AWS → Azure Re-Platforming Demo", href: replatformingDemoHref },
        { label: "Deployment (eshu-deploy)", href: deployHref },
      ],
    },
    {
      heading: "Open Source",
      links: [
        { label: "eshu-hq/eshu", href: githubHref },
        { label: "All 31 Repositories", href: repositoriesHref },
        { label: "GitHub Organization", href: orgHref },
        { label: "Security Policy", href: securityHref },
        { label: "MIT License", href: licenseHref },
      ],
    },
  ] satisfies readonly FooterGroup[],
} as const;
