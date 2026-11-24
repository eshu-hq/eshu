import type { OpsFact, RoleScenario, RolloutPhase } from "./siteContentTypes";

/** Real-world prompts & evidence answers across six organizational roles. */
export const roleScenarios: readonly RoleScenario[] = [
  {
    id: "support-admin",
    role: "Support & Sysadmin",
    context: "Customer escalation or ownership handoff",
    interfaceLabel: "Console · Service Atlas / CLI",
    prompt:
      "What does checkout-api run in prod, who owns it, what changed recently, and which runbook applies?",
    answerSummary:
      "Returns the service dossier, owning team from CODEOWNERS + service catalog, recent deploys, linked infrastructure, and verified runbook docs — without hunting through five wikis.",
    evidencePoints: [
      "Owner: team-payments (CODEOWNERS + catalog-info.yaml)",
      "Deployment: argocd/prod-checkout -> image sha256:9f4a…c21d",
      "Runbook: docs/runbooks/checkout.md (claims checked via eshu docs verify)",
      "Freshness: fresh · missing_evidence: none",
    ],
  },
  {
    id: "engineer",
    role: "Software Engineer",
    context: "Refactoring a shared package inside Claude Code or Cursor",
    interfaceLabel: "Claude Code / Cursor / VS Code (MCP)",
    prompt:
      "Use Eshu: who calls ChargeCustomer across all our repos, and what breaks if I change its signature?",
    answerSummary:
      "Your coding assistant calls Eshu over MCP instead of grepping one repo. It returns cross-repo callers with exact file/line citations, downstream services, and recommended test targets.",
    evidencePoints: [
      "Callers: repo:checkout-api/billing.go:84, repo:invoice-worker/charge.go:41",
      "Cross-repo impact: 2 services + 1 Helm chart affected",
      "Pre-change tests: go test ./internal/billing/... ./internal/checkout/...",
      "Context budget: ESHU_MCP_TOOL_SURFACE=router keeps tool overhead minimal",
    ],
  },
  {
    id: "secops",
    role: "SecOps & AppSec",
    context: "Zero-day CVE triage & exposure review",
    interfaceLabel: "Console · Reachable Vulns / CI SARIF",
    prompt:
      "Which deployed workloads actually reach CVE-2025-13465, and can untrusted HTTP input hit a privileged cloud sink?",
    answerSummary:
      "Joins KEV/EPSS/OSV/NVD advisories to owned lockfiles, SBOM digests, and reachability — separating reachable exposure from noise and refusing clean verdicts when evidence is missing.",
    evidencePoints: [
      "Impact: affected_exact via services/checkout/package-lock.json",
      "Exposure path: POST /v1/export -> FetchSecret -> IAM secret read",
      "CI gate: eshu vuln-scan repo --export sarif exits 4 on missing evidence",
      "Offline safety: vendored EOL snapshots & zero outbound telemetry",
    ],
  },
  {
    id: "sre",
    role: "SRE & On-Call",
    context: "2 AM production incident triage",
    interfaceLabel: "MCP / Console · Incident Context",
    prompt:
      "PagerDuty just paged on checkout-api. What changed before the alert, and is it safe to roll back?",
    answerSummary:
      "Correlates declared, applied, and live PagerDuty routing with recent CI/CD runs, image digests, and metadata-only Grafana/Tempo signals — labeling any missing Jira or PR link explicitly.",
    evidencePoints: [
      "Routing: declared=applied=live (PagerDuty checkout-oncall)",
      "Recent build: GitHub Actions #4821 -> image sha256:3b8e…710a",
      "Observability: Grafana dashboard + Tempo service tags linked (zero raw spans)",
      "Missing slot: jira_work_item (explicitly flagged, never guessed)",
    ],
  },
  {
    id: "platform",
    role: "Platform Engineer",
    context: "Terraform governance & infrastructure change risk",
    interfaceLabel: "CLI / Console · IaC & Cloud Drift",
    prompt:
      "Which AWS resources aren't managed in Terraform, and what depends on this shared RDS module?",
    answerSummary:
      "Surfaces unmanaged cloud resources with ranked owner candidates and read-only Terraform import blocks, plus full downstream blast radius before a module edit lands.",
    evidencePoints: [
      "Unmanaged: aws_s3_bucket.legacy-payment-logs (owner candidate: team-payments)",
      "Import block: ready (read-only HCL — Eshu never mutates cloud state)",
      "Blast radius: 3 production workloads share rds:payments-prod",
      "Multi-cloud: wave-ordered AWS -> Azure migration packets on demand",
    ],
  },
  {
    id: "cto",
    role: "CTO & VP Engineering",
    context: "Scaling institutional memory across teams",
    interfaceLabel: "Console · Executive Rollups / Ask Eshu",
    prompt:
      "How do we keep institutional knowledge accurate as teams grow, and what is our real security and migration posture?",
    answerSummary:
      "Replaces tribal knowledge and stale spreadsheets with one self-updating graph built from actual code, infra, and runtime artifacts — shared by engineers, security, support, and leadership.",
    evidencePoints: [
      "New hires & team transfers ramp from live service dossiers, not folklore",
      "Docs stay honest: eshu docs verify catches drifted runbooks automatically",
      "Governance built in: OIDC, SAML, RBAC, TOTP MFA & audit logs under MIT",
      "Incremental rollout: useful on Day 1 with Git repos alone",
    ],
  },
];

/** Incremental 3-stage adoption path so teams start small and expand safely. */
export const rolloutPhases: readonly RolloutPhase[] = [
  {
    phase: "Day 1",
    timeframe: "15 minutes · Zero cloud credentials",
    title: "Index your Git repositories",
    requirements: "Core stack (Docker Compose or local CLI) + read-only Git access",
    unlocks: [
      "Cross-repo code graph across 22 languages + dead code & code divergence",
      "IaC & GitOps map (Terraform, Helm, K8s, ArgoCD, Flux, Crossplane, Compose)",
      "Backstage/OpsLevel/Cortex service catalogs, CODEOWNERS & doc truth checks",
      "173 read-only MCP tools in Claude Code, Cursor, VS Code & Codex",
    ],
  },
  {
    phase: "Week 1",
    timeframe: "Supply chain & team access",
    title: "Add security & SSO governance",
    requirements: "Enable Console SSO/RBAC + opt into supply-chain collectors",
    unlocks: [
      "CVE-to-workload traceability (KEV, EPSS, OSV, NVD, OCI registries, SBOMs)",
      "Go govulncheck + opt-in multi-language reachability & offline EOL checks",
      "Standalone CI vulnerability scans (eshu vuln-scan repo --export sarif|vex)",
      "OIDC, SAML, GitHub SSO, TOTP MFA, scoped tokens & audit logs (all MIT)",
    ],
  },
  {
    phase: "Month 1",
    timeframe: "Full organizational memory",
    title: "Layer in cloud, incidents & observability",
    requirements: "Enable only the read-only collectors your organization uses",
    unlocks: [
      "Redacted Terraform state & read-only AWS/cloud posture and drift",
      "PagerDuty incident context, Jira work items, GitHub Actions & GitLab CI",
      "Metadata-only Grafana, Prometheus/Mimir, Loki & Tempo coverage",
      "Custom internal collectors via Go SDK (eshu-sdk) & webhook wake-ups",
    ],
  },
];

/** Day-2 deployment and operations facts written directly for sysadmins. */
export const opsFacts: readonly OpsFact[] = [
  {
    title: "One core stack — not 31 microservices",
    detail:
      "Deploy the core stack from eshu-deploy via Docker Compose or Helm (API, MCP, ingester, reducer worker, Console UI, Postgres, Neo4j). All 88 correlation pipelines run inside the reducer; optional collectors are enabled one by one.",
  },
  {
    title: "Rebuildable graph from durable Postgres facts",
    detail:
      "Postgres is the durable source of truth for versioned facts, queues, and content. If Neo4j ever needs maintenance or recovery, Eshu can rebuild the graph deterministically from stored facts.",
  },
  {
    title: "Built for 3 AM operations",
    detail:
      "Exposes /healthz, /readyz, indexing-completeness status, Prometheus /metrics, OpenTelemetry traces, Freshness Causality diagnostics, and safe dead-letter replay inside the Console Operations Live Board.",
  },
  {
    title: "Air-gapped & privacy-first friendly",
    detail:
      "Zero outbound vendor telemetry, vendored offline EOL snapshots, redacted Terraform state, and metadata-only observability & Vault collectors so raw logs, spans, metrics, and secrets never leave their source systems.",
  },
];
