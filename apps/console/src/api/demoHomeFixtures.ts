// api/demoHomeFixtures.ts
// Demo-mode responses for the reads the Home page renders. Every fixture is
// typed against the wire types generated from the served OpenAPI contract
// (api/contracts/wire), and homeFixtures.test.ts validates each one against the
// contract snapshot at runtime as well, so demo Home renders exactly the shapes
// a live deployment returns. Loaded via dynamic import from demoRouter.ts so it
// never lands in the main bundle.
import type { CatalogWire } from "./contracts/wire/CatalogWire";
import type { DocumentationFindingsCountWire } from "./contracts/wire/DocumentationFindingsCountWire";
import type { EcosystemOverviewWire } from "./contracts/wire/EcosystemOverviewWire";
import type { IndexStatusWire } from "./contracts/wire/IndexStatusWire";
import type { InvestigationWorkflowsWire } from "./contracts/wire/InvestigationWorkflowsWire";
import type { QueryPlaybooksWire } from "./contracts/wire/QueryPlaybooksWire";
import type { StatusCollectorReadinessWire } from "./contracts/wire/StatusCollectorReadinessWire";
import type { SupplyChainImpactFindingsCountWire } from "./contracts/wire/SupplyChainImpactFindingsCountWire";
import type { DemoResult } from "./demoRouter";

const NOW = "2026-11-26T15:00:00Z";

export const demoEcosystemOverview: EcosystemOverviewWire = {
  repo_count: 128,
  workload_count: 64,
  platform_count: 9,
  instance_count: 212,
};

export const demoIndexStatus: IndexStatusWire = {
  status: "healthy",
  version: "demo",
  reasons: [],
  repository_count: 128,
  repository_count_by_state: { indexed: 126, stub: 2 },
  queue: {
    dead_letter: 0,
    failed: 0,
    in_flight: 3,
    oldest_outstanding_age: 42,
    outstanding: 18,
    overdue_claims: 0,
    pending: 15,
    retrying: 0,
    succeeded: 48211,
    total: 48229,
  },
  queue_blockages: [],
  scope_activity: { active: 1420, changed: 37, unchanged: 1383 },
};

type Readiness = NonNullable<StatusCollectorReadinessWire["readiness"]>[number];

function collector(
  kind: string,
  name: string,
  state: Readiness["promotion_state"],
  next: string,
  blockers: readonly string[] = [],
): Readiness {
  return {
    collector_kind: kind,
    instance_id: `${kind}-demo`,
    display_name: name,
    promotion_state: state,
    claim_driven: kind !== "git",
    reducer_readback: state === "implemented" ? "available" : "pending",
    recommended_next_action: next,
    blockers,
    last_proof_at: state === "implemented" ? NOW : null,
    updated_at: NOW,
  };
}

export const demoCollectorReadiness: StatusCollectorReadinessWire = {
  version: "demo",
  generated_at: NOW,
  count: 12,
  readiness: [
    collector("git", "Git repositories", "implemented", "No action needed."),
    collector("terraform_state", "Terraform state", "implemented", "No action needed."),
    collector("aws", "AWS cloud", "implemented", "No action needed."),
    collector(
      "kubernetes_live",
      "Kubernetes (live)",
      "partial",
      "Grant read access to the prod cluster.",
      ["no fact evidence observed yet"],
    ),
    collector("ci_cd_run", "CI/CD runs", "implemented", "No action needed."),
    collector(
      "vulnerability_intelligence",
      "Vulnerability intelligence",
      "implemented",
      "No action needed.",
    ),
    collector("sbom_attestation", "SBOMs & attestations", "implemented", "No action needed."),
    collector("oci_registry", "Container registry", "implemented", "No action needed."),
    collector("pagerduty", "PagerDuty", "partial", "Confirm reducer readback for incident facts.", [
      "reducer readback not yet available",
    ]),
    collector(
      "grafana",
      "Grafana",
      "disabled",
      "Enable the collector instance to resume collection.",
    ),
    collector("documentation", "Docs & runbooks", "implemented", "No action needed."),
    collector("jira", "Jira", "disabled", "Enable the collector instance to resume collection."),
  ],
};

export const demoImpactCount: SupplyChainImpactFindingsCountWire = {
  total_findings: 41,
  affected_findings: 9,
  affected_exact: 3,
  affected_derived: 6,
  possibly_affected: 5,
  not_affected: 27,
  by_priority_bucket: { critical: 2, high: 4, medium: 3, low: 0, informational: 0 },
  by_severity: { critical: 2, high: 5, medium: 2 },
  detection_profile: "precise",
  freshness: { state: "fresh", latest_observed_at: NOW },
  scope: {},
};

export const demoDocumentationCount: DocumentationFindingsCountWire = {
  total_findings: 23,
  by_status: { valid: 17, contradicted: 4, missing_evidence: 2 },
  by_truth_level: { exact: 19, derived: 4 },
  by_freshness_state: { fresh: 23 },
  scope: {},
};

export const demoPlaybooks: QueryPlaybooksWire = {
  schema_version: "query-playbooks.v1",
  count: 3,
  total: 3,
  limit: 50,
  offset: 0,
  truncated: false,
  next_offset: null,
  versions: [
    { id: "service_story_citation", version: "1.0.0" },
    { id: "supply_chain_impact_explanation", version: "1.0.0" },
    { id: "incident_context_evidence_path", version: "1.0.0" },
  ],
  playbooks: [
    {
      id: "service_story_citation",
      name: "Service story with citation packet",
      version: "1.0.0",
      prompt_family: "service.story",
      description: "Explain what a service is, who owns it, how it ships and what it depends on.",
    },
    {
      id: "supply_chain_impact_explanation",
      name: "Supply-chain impact explanation",
      version: "1.0.0",
      prompt_family: "supply-chain.impact",
      description: "Explain whether a vulnerable package actually reaches something you run.",
    },
    {
      id: "incident_context_evidence_path",
      name: "Incident context with evidence path",
      version: "1.0.0",
      prompt_family: "incident.context",
      description: "Show what changed around an incident and what evidence is missing.",
    },
  ],
};

export const demoWorkflows: InvestigationWorkflowsWire = {
  schema_version: "investigation-workflows.v1",
  count: 3,
  total: 3,
  limit: 50,
  offset: 0,
  truncated: false,
  next_offset: null,
  versions: [
    { id: "guided_vulnerable_dependency", version: "1.0.0" },
    { id: "guided_deployable_drift", version: "1.0.0" },
    { id: "guided_incident_context", version: "1.0.0" },
  ],
  workflows: [
    {
      id: "guided_vulnerable_dependency",
      name: "Guided vulnerable-dependency investigation",
      version: "1.0.0",
      domain: "supply_chain",
      description:
        "Start from a CVE or package and follow it to the services and owners it affects.",
      starter_prompts: ["Which services are affected by CVE-2025-13465?"],
    },
    {
      id: "guided_deployable_drift",
      name: "Guided deployable-drift investigation",
      version: "1.0.0",
      domain: "deployable_drift",
      description: "Explain why something running differs from what the code and IaC declare.",
      starter_prompts: ["Why is checkout-service running a different image than main?"],
    },
    {
      id: "guided_incident_context",
      name: "Guided incident-context investigation",
      version: "1.0.0",
      domain: "incident_context",
      description: "Gather what changed before an incident and who owns the affected services.",
      starter_prompts: ["What changed before the payments-api incident this morning?"],
    },
  ],
};

type Workload = NonNullable<CatalogWire["workloads"]>[number];

function workload(
  name: string,
  repo: string,
  domain: string,
  tier: string,
  envs: readonly string[],
): Workload {
  return {
    id: `workload:${name}`,
    name,
    kind: "service",
    repo_id: `repository:${repo}`,
    repo_name: repo,
    environments: envs,
    instance_count: envs.length,
    same_name_siblings: 0,
    tier,
    category: "service",
    domain,
  };
}

const demoWorkloads: readonly Workload[] = [
  workload("checkout-service", "checkout-service", "team-commerce", "tier-1", ["prod", "staging"]),
  workload("payments-api", "payments-api", "team-payments", "tier-1", ["prod", "staging"]),
  workload("ledger-service", "ledger-service", "team-payments", "tier-1", ["prod"]),
  workload("search-api", "search-api", "team-discovery", "tier-1", ["prod", "staging"]),
  workload("listing-indexer", "listing-indexer", "team-discovery", "tier-2", ["prod"]),
  workload("notifications", "notifications", "team-platform", "tier-2", ["prod", "staging"]),
  workload("auth-gateway", "auth-gateway", "team-platform", "tier-1", ["prod", "staging"]),
  workload("reporting-jobs", "reporting-jobs", "team-data", "tier-3", ["prod"]),
];

export const demoCatalog: CatalogWire = {
  count: demoWorkloads.length,
  counts: { repositories: 0, services: demoWorkloads.length, workloads: demoWorkloads.length },
  limit: 200,
  truncated: false,
  workloads_truncated: false,
  repositories: [],
  services: demoWorkloads,
  workloads: demoWorkloads,
};

const HOME_FIXTURES: ReadonlyMap<string, DemoResult> = new Map([
  [
    "/api/v0/ecosystem/overview",
    { capability: "platform_impact.context_overview", data: demoEcosystemOverview },
  ],
  ["/api/v0/index-status", { capability: "operations.index_status", data: demoIndexStatus }],
  [
    "/api/v0/status/collector-readiness",
    { capability: "status.collector_readiness", data: demoCollectorReadiness },
  ],
  [
    "/api/v0/supply-chain/impact/findings/count",
    { capability: "supply_chain.impact_findings.aggregate", data: demoImpactCount },
  ],
  [
    "/api/v0/documentation/findings/count",
    { capability: "documentation_findings.aggregate", data: demoDocumentationCount },
  ],
  ["/api/v0/query-playbooks", { capability: "query_playbooks.list", data: demoPlaybooks }],
  [
    "/api/v0/investigation-workflows",
    { capability: "investigation_workflows.list", data: demoWorkflows },
  ],
  ["/api/v0/catalog", { capability: "catalog.list", data: demoCatalog }],
]);

export const demoHomeRoutes: ReadonlySet<string> = new Set(HOME_FIXTURES.keys());

export function demoHomeResponse(method: string, path: string): DemoResult | null {
  if (method !== "GET") return null;
  return HOME_FIXTURES.get(path) ?? null;
}
