import type { CommandDemo, DemoNode, HeroTrace, QuickstartTab } from "./siteContentTypes";

/** Six-node layered topology rendered in the interactive 2D hero evidence graph. */
export const demoTrace = {
  service: "checkout-api",
  nodes: [
    { id: "code", label: "checkout-api", detail: "22 langs · call graph" },
    { id: "supply-chain", label: "go.mod + SBOM", detail: "13+ pkg · 7 registries" },
    { id: "iac", label: "payments-rds.tf", detail: "9+ IaC & GitOps" },
    { id: "cloud", label: "IAM & AWS Sinks", detail: "Secrets · 0.0.0.0/0" },
    { id: "runtime", label: "prod/checkout", detail: "Workloads & Obs" },
    { id: "incidents", label: "PagerDuty & Docs", detail: "Routing · Runbooks" },
  ] satisfies readonly DemoNode[],
};

/** Five real workflows users can click on a single-row bar inside the hero graph card. */
export const heroTraces: readonly HeroTrace[] = [
  {
    id: "blast",
    label: "Pre-change",
    sourceScope: "Git & IaC only (Day 1)",
    question: "What breaks if I modify this shared database module before merging?",
    command: "eshu change plan --repo-id repo:infra --base origin/main",
    truthLabel: "exact · 3 consumers linked",
    freshness: "fresh",
    activeNodeIds: ["code", "iac", "cloud"],
    evidence: [
      "target: terraform/modules/payments-rds/main.tf",
      "consumers: checkout-api, billing-worker, refund-cli",
      "tests: go test ./internal/billing/... ./internal/checkout/...",
      "missing: none (proven from indexed Git & IaC)",
    ],
  },
  {
    id: "cve",
    label: "CVE trace",
    sourceScope: "Git + opt-in supply-chain collectors",
    question: "Which deployed workloads ship this vulnerable package, and is it reachable?",
    command: "mcp: list_supply_chain_impact_findings",
    truthLabel: "derived · 4 hops verified",
    freshness: "fresh",
    activeNodeIds: ["code", "supply-chain", "iac", "runtime"],
    evidence: [
      "manifest: services/checkout/go.mod + go.sum (Git · exact)",
      "reachability: govulncheck -> reachable (handleCheckout)",
      "sbom_digest: sha256:9f4a…c21d (CycloneDX subject match)",
      "deployment: prod/checkout-api (GitOps ArgoCD -> K8s manifest)",
    ],
  },
  {
    id: "exposure",
    label: "Exposure",
    sourceScope: "Git + opt-in AWS collector",
    question: "Can untrusted HTTP input reach a privileged cloud sink?",
    command: "mcp: trace_exposure_path",
    truthLabel: "derived · internet_exposed",
    freshness: "fresh",
    activeNodeIds: ["code", "iac", "cloud", "runtime"],
    evidence: [
      "source: http_handler POST /v1/export (0.0.0.0/0 verified)",
      "call_chain: ExportHandler -> RunBatch -> FetchSecret",
      "sink: SECRETS_IAM_GRANTS_SECRET_READ -> prod/billing-key",
      "missing: none",
    ],
  },
  {
    id: "incident",
    label: "Incident",
    sourceScope: "Git + opt-in PagerDuty/CI/Obs collectors",
    question: "What changed before this PagerDuty alert fired?",
    command: "mcp: get_incident_context",
    truthLabel: "partial · missing slot labeled",
    freshness: "fresh",
    activeNodeIds: ["code", "supply-chain", "runtime", "incidents"],
    evidence: [
      "routing: declared=applied=live (PagerDuty checkout-oncall)",
      "deploy: gh-actions run #4821 -> image sha256:3b8e…710a",
      "observability: Grafana + Tempo service tags correlated",
      "missing: jira_work_item (no linked issue on PR #914)",
    ],
  },
  {
    id: "drift",
    label: "Cloud drift",
    sourceScope: "Git + opt-in AWS & Terraform state",
    question: "Which AWS resources exist outside Terraform, and who owns them?",
    command: "mcp: find_unmanaged_resources",
    truthLabel: "derived · read-only import blocks",
    freshness: "fresh",
    activeNodeIds: ["iac", "cloud"],
    evidence: [
      "unmanaged: aws_s3_bucket.legacy-payment-logs (public block off)",
      "owner_candidate: team-payments (tags + repo module match)",
      "import_plan: ready (read-only HCL import block generated)",
      "mutation: never — Eshu never runs Terraform or edits cloud state",
    ],
  },
];

/** Interactive command workbench examples covering CLI, MCP, and HTTP API. */
export const commandDemos: readonly CommandDemo[] = [
  {
    id: "demo",
    command: "eshu demo up",
    surface: "CLI",
    summary:
      "Boot a zero-credential Eshu stack seeded with the golden-corpus synthetic organization and wait until indexing settles.",
    output: [
      "Stack: eshu-demo (isolated Compose project, ephemeral auth key)",
      "Readiness: healthy + indexed + queue drained to 0",
      "First answer:",
      "  Q: Which workload does api-svc run in, and which functions handle its routes?",
      "  A: Workload api-svc (service) defined in repo api-svc; 2 HTTP handlers linked",
      "  Truth: basis=hybrid freshness=fresh level=derived profile=production",
    ],
  },
  {
    id: "vuln-scan",
    command: "eshu vuln-scan repo . --export sarif",
    surface: "CLI",
    summary:
      "Run the vulnerability scanner against one repository and export SARIF v2.1.0 (or --export vex) with non-silent readiness exit codes.",
    output: [
      "Readiness: ready_with_findings (exit code 3; missing evidence returns exit code 4)",
      "Schema: SARIF v2.1.0 (with readiness, freshness, and missing_evidence properties)",
      "Finding: CVE-2025-13465 · pkg:npm/example-lib@2.3.4 (affected_exact)",
      "Remediation: first_patched_version=2.3.5 (source=ghsa, confidence=exact)",
      "Evidence: fact-package-lock-4412, fact-advisory-osv-8891",
    ],
  },
  {
    id: "trace",
    command: "eshu trace service checkout-api --env prod",
    surface: "CLI",
    summary:
      "Walk a service from source code through IaC and deployment manifests to runtime, labeling every hop and any missing collector.",
    output: [
      "Service: checkout-api (env: prod)",
      "Truth freshness: fresh · trace_status: partial",
      "  source:     exact   (2 evidence handles -> repo:checkout-service)",
      "  deployment: derived (3 evidence handles -> argocd/prod + helm/checkout)",
      "  runtime:    missing (enable kubernetes_live collector for live pod proof)",
    ],
  },
  {
    id: "change-plan",
    command: "eshu change plan --repo-id repo:payments --base origin/main",
    surface: "CLI",
    summary:
      "Inspect a local git diff against the graph to produce a bounded, read-only developer change plan.",
    output: [
      "Artifact: developer_change_plan.v1 (read-only)",
      "Changed files: 2 modified, 1 renamed (blast radius: 4 downstream callers)",
      "Recommended tests: go test ./internal/billing/... ./internal/checkout/...",
      "Cross-repo impacts: repo:invoice-worker (calls ChargeCustomer)",
      "Freshness: fresh · missing_evidence: none",
    ],
  },
  {
    id: "docs-verify",
    command: "eshu docs verify docs/runbook.md",
    surface: "CLI",
    summary:
      "Check Markdown claims (CLI commands, HTTP routes, env vars, paths, image refs, Terraform blocks) against repo and graph truth.",
    output: [
      "Checked claims: 14 (12 valid, 1 contradicted, 1 missing_evidence)",
      "CONTRADICTED: `GET /api/v0/legacy/status` (not in OpenAPI route catalog)",
      "MISSING_EVIDENCE: container image `ghcr.io/org/api:v1.9` (oci_registry not enabled)",
      "Packet: documentation_evidence_packet emitted with excerpt hash provenance",
    ],
  },
  {
    id: "ask",
    command: "POST /api/v0/ask",
    surface: "HTTP API",
    summary:
      "Opt-in, self-hosted, provider-portable agentic Q&A with SSE streaming, read-only Cypher + SQL sandboxing, and evidence packets.",
    output: [
      "Question: Which services share the prod payments RDS cluster?",
      "Streaming: SSE · sandbox: read-only Cypher + SQL",
      "Answer: 3 services linked (checkout-api, billing-worker, refund-cli)",
      "Evidence packet: investigation_evidence_packet.v2",
      "Default-off guard: disabled until ESHU_ASK_ENABLED=true + provider profile",
    ],
  },
];

/** Three accurate, verified quickstart paths with explicit prerequisites. */
export const quickstartTabs: readonly QuickstartTab[] = [
  {
    id: "demo",
    label: "1. Zero-credential demo",
    badge: "Fastest evaluation",
    prerequisites: "Requires Docker + Go 1.26+ (builds local CLI from source)",
    description:
      "Spin up an isolated synthetic organization seeded from Eshu's golden corpus. No cloud keys, Git tokens, or external credentials required.",
    commands: [
      "git clone https://github.com/eshu-hq/eshu && cd eshu",
      "./scripts/install-local-binaries.sh",
      "eshu demo up",
      "# Inspect answers, then clean up containers and volumes:",
      "eshu demo down",
    ],
    note: "Waits until health, indexing, and queue drain all finish before printing your first truth-labeled answer.",
  },
  {
    id: "deploy",
    label: "2. Full stack (eshu-deploy)",
    badge: "Docker Compose & Helm",
    prerequisites:
      "Requires Docker Compose or Kubernetes + Helm (pulls prebuilt GHCR images — no Go toolchain needed)",
    description:
      "Run the full API, MCP server, ingester, reducer, 42-page Console UI, Postgres, and Neo4j stack locally or as a shared team service.",
    commands: [
      "git clone https://github.com/eshu-hq/eshu-deploy && cd eshu-deploy",
      'export ESHU_FILESYSTEM_HOST_ROOT="$HOME/src"',
      'export ESHU_REPOSITORY_RULES_JSON=\'{"exact":["my-service"]}\'',
      "docker compose up -d",
      "docker compose exec eshu eshu workspace status",
    ],
    note: "Start with Git repositories alone; all external collectors stay off until you opt in via ESHU_COLLECTOR_INSTANCES_JSON.",
  },
  {
    id: "mcp",
    label: "3. Local workspace + MCP",
    badge: "Claude · Cursor · VS Code · Codex",
    prerequisites:
      "Requires Go 1.26+ — no Docker required (auto-indexes your current repo with embedded local storage)",
    description:
      "Generate a ready-to-paste client config for a workspace-local stdio MCP server (or pass --hosted to connect to a shared team deployment).",
    commands: [
      "./scripts/install-local-binaries.sh",
      "eshu mcp setup --platform claude    # or cursor, vscode, codex",
      "# Optional: collapse 173 tools into find_tool + call_tool:",
      "export ESHU_MCP_TOOL_SURFACE=router",
      '# Open your repo, restart your IDE, and ask: "Use Eshu to trace this service"',
    ],
    note: "eshu mcp start attaches to your workspace's local owner and indexes the open repo automatically. Hosted deployments detect OIDC/SSO or token auth.",
  },
];
