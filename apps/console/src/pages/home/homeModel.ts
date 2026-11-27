// pages/home/homeModel.ts
// Pure derivations that turn Home's contract-typed reads into plain-language
// content anyone in the company can act on. No React, no fetching: every
// function here is unit-tested against the demo fixtures.
import type { CatalogWire } from "../../api/contracts/wire/CatalogWire";
import type { StatusCollectorReadinessWire } from "../../api/contracts/wire/StatusCollectorReadinessWire";
import type { KnowledgeHomeData, Section } from "../../api/knowledgeHome";

export type RoleId =
  | "everyone"
  | "engineering"
  | "security"
  | "sre"
  | "platform"
  | "support"
  | "leadership";

export type Tone = "critical" | "warning" | "info";

export interface AttentionItem {
  readonly id: string;
  readonly tone: Tone;
  readonly title: string;
  readonly detail: string;
  readonly href: string;
  readonly action: string;
  readonly audiences: readonly RoleId[];
}

export type Health =
  | { readonly state: "unknown" }
  | { readonly state: "healthy"; readonly summary: string }
  | { readonly state: "degraded"; readonly summary: string; readonly notes: readonly string[] };

const n = (value: number | undefined): number => value ?? 0;
const fmt = (value: number): string => value.toLocaleString("en-US");
const plural = (count: number, one: string, many: string = `${one}s`): string =>
  `${fmt(count)} ${count === 1 ? one : many}`;

export function ready<T>(section: Section<T>): T | null {
  return section.status === "ready" ? section.data : null;
}

// knowledgeSummary is the one-sentence "what Eshu knows" line under the greeting.
export function knowledgeSummary(data: KnowledgeHomeData): string | null {
  const overview = ready(data.overview);
  if (!overview) return null;
  const parts = [
    plural(n(overview.repo_count), "repository", "repositories"),
    plural(n(overview.workload_count), "service"),
    plural(n(overview.instance_count), "running instance"),
  ];
  return `Eshu is connected to ${parts[0]}, ${parts[1]} and ${parts[2]} across your organization.`;
}

// health turns index-status into a reader-facing freshness verdict.
export function health(data: KnowledgeHomeData): Health {
  const index = ready(data.index);
  if (!index) return { state: "unknown" };
  const queue = index.queue ?? {};
  const notes: string[] = [];
  if (n(queue.dead_letter) > 0)
    notes.push(`${plural(n(queue.dead_letter), "update")} could not be processed.`);
  for (const reason of index.reasons ?? []) {
    if (/generations are failed/.test(reason))
      notes.push("Some repositories failed their last refresh, so their answers are older.");
    else if (/findings are stale/.test(reason))
      notes.push("Vulnerability findings are behind; recent advisories may be missing.");
  }
  const unique = [...new Set(notes)];
  if (index.status === "healthy" && unique.length === 0) {
    return { state: "healthy", summary: "Knowledge is up to date." };
  }
  return { state: "degraded", summary: "Some answers may be out of date.", notes: unique };
}

const ROLE_ALL: readonly RoleId[] = [
  "everyone",
  "engineering",
  "security",
  "sre",
  "platform",
  "support",
  "leadership",
];

export function attentionItems(data: KnowledgeHomeData): AttentionItem[] {
  const items: AttentionItem[] = [];
  const vulns = ready(data.vulnerabilities);
  if (vulns) {
    const urgent = n(vulns.by_priority_bucket?.critical) + n(vulns.by_priority_bucket?.high);
    if (urgent > 0) {
      items.push({
        id: "vulns",
        tone: n(vulns.by_priority_bucket?.critical) > 0 ? "critical" : "warning",
        title: `${plural(urgent, "urgent vulnerability", "urgent vulnerabilities")} affect software we run`,
        detail: `${fmt(n(vulns.affected_exact))} confirmed and ${fmt(n(vulns.affected_derived))} likely affected; ${fmt(n(vulns.not_affected))} ruled out.`,
        href: "/vulnerabilities",
        action: "See what's affected",
        audiences: ["security", "engineering", "leadership", "everyone"],
      });
    }
  } else if (data.vulnerabilities.status === "unavailable") {
    items.push({
      id: "vulns-unavailable",
      tone: "info",
      title: "Vulnerability status couldn't be loaded",
      detail: unavailableSentence(data.vulnerabilities.reason),
      href: "/vulnerabilities",
      action: "Open vulnerabilities",
      audiences: ["security", "leadership"],
    });
  }
  const docs = ready(data.docs);
  const contradicted = n(docs?.by_status?.contradicted);
  if (contradicted > 0) {
    items.push({
      id: "docs",
      tone: "warning",
      title: `${plural(contradicted, "documentation claim")} no longer match the code`,
      detail:
        "Runbooks or READMEs describe something that has changed. Fixing them keeps answers trustworthy.",
      href: "/findings",
      action: "Review the docs",
      audiences: ["support", "engineering", "sre", "everyone"],
    });
  }
  const index = ready(data.index);
  const deadLetter = n(index?.queue?.dead_letter);
  if (deadLetter > 0) {
    items.push({
      id: "pipeline",
      tone: deadLetter > 1000 ? "critical" : "warning",
      title: `Eshu couldn't process ${plural(deadLetter, "update")}`,
      detail:
        "Those changes aren't reflected in answers yet. The platform team can replay them once the cause is fixed.",
      href: "/operations",
      action: "Open operations",
      audiences: ["platform", "sre"],
    });
  }
  const collectors = ready(data.collectors);
  const broken = (collectors?.readiness ?? []).filter(
    (row) => row.promotion_state === "failed" || row.promotion_state === "stale",
  );
  if (broken.length > 0) {
    items.push({
      id: "collectors",
      tone: "warning",
      title: `${plural(broken.length, "data source")} stopped updating`,
      detail: broken
        .map((row) => row.display_name ?? row.collector_kind)
        .slice(0, 4)
        .join(", "),
      href: "/collector-readiness",
      action: "See data sources",
      audiences: ["platform", "leadership"],
    });
  }
  return items;
}

export function unavailableSentence(reason: "forbidden" | "slow" | "not_ready" | "failed"): string {
  switch (reason) {
    case "forbidden":
      return "Your account doesn't have access to this area. Ask an Eshu admin if you need it.";
    case "slow":
      return "Eshu took too long to answer. It will try again next time you open Home.";
    case "not_ready":
      return "This part of Eshu isn't ready on this deployment yet.";
    default:
      return "Something went wrong loading this. Try again in a moment.";
  }
}

// orderForRole puts the items a role cares about first, most urgent first.
export function orderForRole(items: readonly AttentionItem[], role: RoleId): AttentionItem[] {
  const rank: Record<Tone, number> = { critical: 0, warning: 1, info: 2 };
  const score = (item: AttentionItem): number =>
    rank[item.tone] + (item.audiences.includes(role) || role === "everyone" ? 0 : 3);
  return [...items].sort((a, b) => score(a) - score(b));
}

export interface CoverageArea {
  readonly id: string;
  readonly name: string;
  readonly answers: string;
  readonly connected: number;
  readonly total: number;
  readonly state: "connected" | "partial" | "missing";
  readonly sources: readonly {
    readonly name: string;
    readonly state: string;
    readonly next: string;
  }[];
}

const AREAS: readonly { id: string; name: string; answers: string; kinds: readonly string[] }[] = [
  {
    id: "code",
    name: "Code & ownership",
    answers: "What a service does, who owns it, what calls what",
    kinds: ["git", "scanner_worker", "semantic_extraction"],
  },
  {
    id: "delivery",
    name: "Builds & deployments",
    answers: "How things ship and where they run",
    kinds: ["ci_cd_run", "kubernetes_live", "oci_registry", "webhook"],
  },
  {
    id: "cloud",
    name: "Cloud & infrastructure",
    answers: "Which cloud resources exist and who manages them",
    kinds: ["aws", "gcp", "azure", "terraform_state", "vault_live"],
  },
  {
    id: "security",
    name: "Security & dependencies",
    answers: "Which vulnerabilities actually affect us",
    kinds: ["vulnerability_intelligence", "sbom_attestation", "security_alert", "package_registry"],
  },
  {
    id: "operations",
    name: "Incidents & monitoring",
    answers: "What broke, what changed and who was paged",
    kinds: ["pagerduty", "grafana", "prometheus_mimir", "loki", "tempo"],
  },
  {
    id: "knowledge",
    name: "Docs & tickets",
    answers: "Which runbooks are current and what work is tracked",
    kinds: ["documentation", "confluence", "jira"],
  },
];

type ReadinessRow = NonNullable<StatusCollectorReadinessWire["readiness"]>[number];

export function coverageAreas(collectors: StatusCollectorReadinessWire): CoverageArea[] {
  const rows = collectors.readiness ?? [];
  const byKind = (kinds: readonly string[]): ReadinessRow[] =>
    rows.filter((row) => kinds.includes(row.collector_kind));
  return AREAS.map((area) => {
    const sources = byKind(area.kinds);
    const connected = sources.filter((row) => row.promotion_state === "implemented").length;
    const usable = sources.filter(
      (row) => row.promotion_state === "implemented" || row.promotion_state === "partial",
    ).length;
    const state: CoverageArea["state"] =
      sources.length > 0 && connected === sources.length
        ? "connected"
        : usable > 0
          ? "partial"
          : "missing";
    return {
      id: area.id,
      name: area.name,
      answers: area.answers,
      connected,
      total: sources.length,
      state,
      sources: sources.map((row) => ({
        name: row.display_name ?? row.collector_kind,
        state: row.promotion_state,
        next: row.recommended_next_action,
      })),
    };
  });
}

export const NO_OWNER = "No owner declared";

export interface TeamSummary {
  readonly name: string;
  readonly services: readonly string[];
  readonly critical: number;
}

// teams groups catalog services by their declared owning domain so anyone can
// browse "who owns what" without knowing a service name first.
export function teams(catalog: CatalogWire, limit = 8): TeamSummary[] {
  const grouped = new Map<string, { services: string[]; critical: number }>();
  for (const service of catalog.services ?? []) {
    const owner = service.domain?.trim() || NO_OWNER;
    const entry = grouped.get(owner) ?? { services: [], critical: 0 };
    if (service.name && !entry.services.includes(service.name)) entry.services.push(service.name);
    if (service.tier === "tier-1") entry.critical += 1;
    grouped.set(owner, entry);
  }
  return [...grouped.entries()]
    .map(([name, entry]) => ({ name, services: entry.services.sort(), critical: entry.critical }))
    .sort((a, b) => b.services.length - a.services.length || a.name.localeCompare(b.name))
    .slice(0, limit);
}

// exampleService picks a real service name to personalize suggested questions.
export function exampleService(catalog: CatalogWire | null): string | null {
  const services = (catalog?.services ?? []).filter((s) => s.name);
  const pick =
    services.find((s) => s.tier === "tier-1") ??
    services.find((s) => s.domain) ??
    services.find((s) => (s.instance_count ?? 0) > 0) ??
    services[0];
  return pick?.name ?? null;
}

export { ROLE_ALL };
