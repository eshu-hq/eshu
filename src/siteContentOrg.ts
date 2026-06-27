import type { PersonaDemo, ProofPoint, RolePrompt } from "./siteContentTypes";

/** Role-specific examples for the persona tab interaction. */
export const personaDemos = [
  {
    role: "Ask Eshu user",
    context: "Natural-language investigation across the whole engineering graph",
    question: "Which services are affected by this CVE, and what evidence backs that?",
    answer:
      "Ask Eshu uses per-token streaming, then returns evidence handles, truth class, missing evidence, and limitations for every claim. It can use read-only Cypher and SQL through the sandbox, but it cannot mutate the graph.",
    primaryTool: "ask",
  },
  {
    role: "SRE / on-call",
    context: "Production incident in progress",
    question: "Is this safe to roll back? Show me the full chain.",
    answer:
      "Eshu returns the deployment chain (commit -> image -> registry -> workload), declared/applied/live routing for any PagerDuty service, fallback change candidates, and explicit missing slots for build, deploy, commit, PR, and Jira. Same evidence your on-call would see.",
    primaryTool: "get_incident_context",
  },
  {
    role: "Security analyst",
    context: "CVE published overnight",
    question: "Which of my workloads are affected by CVE-2025-13465? Show me the chain.",
    answer:
      "Eshu joins the advisory to your owned package manifest, lockfile, registry, container image, SBOM, deployment, and workload. Findings are published only when owned evidence backs them; KEV + EPSS alone do not trigger findings.",
    primaryTool: "list_supply_chain_impact_findings",
  },
  {
    role: "Platform engineer",
    context: "AWS account audit",
    question: "Which AWS resources aren't in Terraform? Generate the import plan.",
    answer:
      "Eshu returns unmanaged resources, owner candidates from tags/repos/modules/services, and read-only Terraform import blocks for safety-approved supported cloud-only findings. Refused items get explicit refusal reasons.",
    primaryTool: "find_unmanaged_resources",
  },
  {
    role: "Engineer switching teams",
    context: "First week on a new product",
    question: "What does this service do? How is it deployed? Who owns it?",
    answer:
      "Eshu returns the service dossier, deployment chain from code to cloud, owner candidates, and related code, infrastructure, and documentation. No three-month ramp needed.",
    primaryTool: "get_service_story",
  },
  {
    role: "CTO",
    context: "Board meeting in 30 minutes",
    question: "What's our security posture? What's the cost of migrating from AWS to Azure?",
    answer:
      "Eshu returns drift-finding rollups, published vulnerability impact by ecosystem, re-platforming rollups by account/env/service, and readiness by wave. Same source of truth your engineers use.",
    primaryTool: "get_replatforming_rollups",
  },
  {
    role: "Developer",
    context: "Refactoring a shared client",
    question: "Who calls this function across all repos? What breaks if I change it?",
    answer:
      "Eshu returns transitive callers up to graph depth, with resolution_method per edge (scip, declared, import_binding, type_inferred, etc.). Provenance on every claim.",
    primaryTool: "get_code_relationship_story",
  },
  {
    role: "Sales engineer",
    context: "Customer demo",
    question: "What does this customer have deployed? What changed since last quarter?",
    answer:
      "Eshu returns the customer's ecosystem overview, service stories, and changed-since inventory. Same MCP tools your engineers use, scoped to the customer's account.",
    primaryTool: "get_ecosystem_overview",
  },
  {
    role: "Data engineer",
    context: "Dashboard accuracy audit",
    question: "What's the lineage from this dashboard to the source table?",
    answer:
      "Eshu parses SQL, dbt models, Glue Data Catalog, Athena queries, and Redshift clusters. It returns the lineage chain with parser-proven edges and explicit unresolved references.",
    primaryTool: "investigate_resource",
  },
] satisfies readonly PersonaDemo[];

/** Organization-level proof cards shown above the surface list. */
export const proofPoints = [
  {
    value: "Ask Eshu",
    title: "Agentic Q&A over the knowledge graph",
    description:
      "self-hosted, provider-portable Ask Eshu answers natural-language questions with per-token streaming, read-only Cypher and SQL sandboxing, bounded retrieval, evidence handles, and explicit limitations.",
  },
  {
    value: "Supply chain",
    title: "Production-promoted end-to-end traceability",
    description:
      "vulnerability_intelligence collector at promotion_state: implemented. 7 published impact findings (CVE-2019-10744 through CVE-2026-4800) for npm lodash 4.17.11, full chain from advisory to workload. Refuses findings without owned evidence.",
  },
  {
    value: "Multi-cloud",
    title: "Re-platforming from AWS to Azure, runnable today",
    description:
      "compose_replatforming_plan returns bounded migration packets with per-item source state, safety gate, owner candidates, ready/refused Terraform import candidates, and migration waves. AWS -> Azure demo at docs/public/guides/aws-to-azure-replatforming-demo.md.",
  },
  {
    value: "Personas",
    title: "18 personas, one MCP server, one source of truth",
    description:
      "New engineer, switching engineer, SRE, security analyst, platform engineer, developer, support, migration architect, frontend, backend, data engineer, product manager, director, VP, CTO, CEO, sales/CSM, customer-facing. Every persona asks questions of the same evidence graph.",
  },
  {
    value: "Open source",
    title: "MIT-licensed, self-hosted, no vendor telemetry",
    description:
      "Every capability anchored to a public spec, a public fixture, and a public test. The capability catalog at specs/capability-matrix.v1.yaml is machine-verified: go run ./cmd/capability-inventory -mode verify fails CI on drift. No enterprise edition. No contact-sales tier.",
  },
] satisfies readonly ProofPoint[];

/** First-prompt examples shown near the end of the launch page. */
export const rolePrompts = [
  {
    role: "New engineer",
    prompt: "Use Eshu to ask what this service does end to end.",
  },
  {
    role: "SRE / on-call",
    prompt: "Use Eshu to show me the blast radius of this change.",
  },
  {
    role: "Security analyst",
    prompt: "Use Eshu to find every workload affected by CVE-2025-13465.",
  },
  {
    role: "Platform engineer",
    prompt: "Use Eshu to find AWS resources not in Terraform and generate the import plan.",
  },
  {
    role: "Migration architect",
    prompt: "Use Eshu to compose the AWS -> Azure re-platforming plan.",
  },
  {
    role: "VP Engineering",
    prompt:
      "Use Eshu to show our security posture and re-platforming readiness for the board update.",
  },
  {
    role: "CTO",
    prompt: "Use Eshu to find our single points of failure and our supply chain risk surface.",
  },
  {
    role: "Sales engineer",
    prompt:
      "Use Eshu to show me what this customer has deployed and what changed since last quarter.",
  },
] satisfies readonly RolePrompt[];
