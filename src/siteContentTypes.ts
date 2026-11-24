/** Navigation link rendered in the top-level site header. */
export interface NavItem {
  readonly label: string;
  readonly href: string;
}

/** High-level verified platform metric shown beneath the hero. */
export interface MetricStat {
  readonly value: string;
  readonly label: string;
  readonly detail: string;
}

/** Node in the hero source-to-runtime graph illustration. */
export interface DemoNode {
  readonly id: string;
  readonly label: string;
  readonly detail: string;
}

/** Interactive workflow trace selectable inside the hero graph card. */
export interface HeroTrace {
  readonly id: string;
  readonly label: string;
  readonly sourceScope: string;
  readonly question: string;
  readonly command: string;
  readonly truthLabel: string;
  readonly freshness: string;
  readonly activeNodeIds: readonly string[];
  readonly evidence: readonly string[];
}

/** Real-world organizational role showcase entry. */
export interface RoleScenario {
  readonly id: string;
  readonly role: string;
  readonly context: string;
  readonly interfaceLabel: string;
  readonly prompt: string;
  readonly answerSummary: string;
  readonly evidencePoints: readonly string[];
}

/** One phase of the incremental rollout path. */
export interface RolloutPhase {
  readonly phase: string;
  readonly timeframe: string;
  readonly title: string;
  readonly requirements: string;
  readonly unlocks: readonly string[];
}

/** Day-2 operational reassurance card for sysadmins and operators. */
export interface OpsFact {
  readonly title: string;
  readonly detail: string;
}

/** Single capability highlight inside a tabbed platform pillar. */
export interface CapabilityHighlight {
  readonly title: string;
  readonly description: string;
  readonly tag?: string;
}

/** One platform pillar rendered in the tabbed capability explorer. */
export interface CapabilityTab {
  readonly id: string;
  readonly label: string;
  readonly headline: string;
  readonly summary: string;
  readonly highlights: readonly CapabilityHighlight[];
}

/** Readiness lane matching surface-inventory.generated.json verbatim. */
export type CollectorReadiness = "implemented" | "partial" | "gated" | "foundation_only";

/** Plain-English explanation for one collector readiness badge. */
export interface ReadinessLegendItem {
  readonly key: CollectorReadiness;
  readonly label: string;
  readonly meaning: string;
}

/** Logical grouping for the filterable collector ecosystem grid. */
export type CollectorCategory =
  | "Core & Intake"
  | "Supply Chain & Security"
  | "Cloud & IaC"
  | "Delivery & Docs"
  | "Observability & Semantic";

/** Single collector lane with honest readiness status. */
export interface CollectorItem {
  readonly name: string;
  readonly lane: string;
  readonly category: CollectorCategory;
  readonly readiness: CollectorReadiness;
  readonly summary: string;
  readonly privacyNote?: string;
}

/** One step in the facts-first architecture pipeline. */
export interface PipelineStep {
  readonly step: string;
  readonly label: string;
  readonly detail: string;
}

/** Interactive CLI, API, or MCP command example in the workbench. */
export interface CommandDemo {
  readonly id: string;
  readonly command: string;
  readonly surface: "CLI" | "MCP" | "HTTP API";
  readonly summary: string;
  readonly output: readonly string[];
}

/** Public product surface (MCP, Console, CLI, HTTP API, SDK). */
export interface SurfaceItem {
  readonly title: string;
  readonly metric: string;
  readonly description: string;
  readonly bullets: readonly string[];
}

/** Calm, factual engineering principle explaining why Eshu behaves as it does. */
export interface PrincipleItem {
  readonly title: string;
  readonly description: string;
}

/** One runnable local or deployed quickstart path. */
export interface QuickstartTab {
  readonly id: string;
  readonly label: string;
  readonly badge: string;
  readonly prerequisites: string;
  readonly description: string;
  readonly commands: readonly string[];
  readonly note: string;
}

/** Footer link column. */
export interface FooterGroup {
  readonly heading: string;
  readonly links: readonly NavItem[];
}
