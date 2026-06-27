import type { CleanupMode, CommandDemo, DemoNode } from "./siteContentTypes";

/** Short command labels used by the hero and launch contract tests. */
export const terminalCommands = [
  "eshu scan",
  "POST /api/v0/ask",
  "eshu trace service checkout",
  "mcp: ask",
  "mcp: list_supply_chain_impact_findings",
  "mcp: compose_replatforming_plan",
] as const;

/** Nodes shown in the source-to-runtime graph illustration. */
export const demoTrace = {
  service: "checkout-service",
  nodes: [
    { id: "code", label: "Code", detail: "services/checkout" },
    { id: "supply-chain", label: "Supply chain", detail: "npm, ECR, SBOM" },
    { id: "iac", label: "IaC", detail: "Terraform + K8s" },
    { id: "cloud", label: "Cloud", detail: "AWS prod us-east-1" },
    { id: "runtime", label: "Runtime", detail: "checkout-api pods" },
    { id: "incidents", label: "Incidents", detail: "PagerDuty, Jira" },
  ] satisfies readonly DemoNode[],
};

/** Interactive command examples rendered in the "Run the graph" section. */
export const commandDemos = [
  {
    command: "eshu scan --json",
    summary: "Graph ready for organization-wide questions.",
    activeNodeId: "code",
    output: [
      '"status": "ready",',
      '"succeeded": 8347,',
      '"queue_zero_ms": 853600,',
      '"freshness": "current"',
    ],
  },
  {
    command: "POST /api/v0/ask",
    summary: "Ask Eshu answers over HTTP with evidence handles.",
    activeNodeId: "supply-chain",
    output: [
      "POST /api/v0/ask",
      "Question: which services are affected by CVE-2024-3094?",
      "Answer: partial",
      "Affected workload: checkout-service",
      "Evidence packet: investigation_evidence_packet.v2",
      "Truth: derived from bounded graph and supply-chain reads",
      "Missing evidence: none for published finding",
    ],
  },
  {
    command: "eshu trace service checkout",
    summary: "Trace checkout from source to the runtime that serves it.",
    activeNodeId: "runtime",
    output: [
      "Service: checkout-service",
      "Repository: repo-checkout (checkout-service)",
      "Truth freshness: fresh",
      "Code to runtime:",
      "Trace status: partial",
      "- source: exact (2 evidence)",
      "- deployment: derived (3 evidence)",
      "Missing evidence: runtime",
    ],
  },
  {
    command: "mcp: ask",
    summary: "Agentic Q&A through MCP with scoped, read-only tools.",
    activeNodeId: "supply-chain",
    output: [
      "Tool: ask",
      "Streaming: SSE",
      "Sandbox: read-only Cypher + SQL",
      "Truth: derived with evidence handles",
      "Provider: configured agent_reasoning profile",
      "Default-off: unavailable until ESHU_ASK_ENABLED=true",
    ],
  },
  {
    command: "mcp: list_supply_chain_impact_findings",
    summary: "Published impact findings with evidence (refusal-on-insufficient-evidence).",
    activeNodeId: "supply-chain",
    output: [
      "Findings: 7",
      "Affected: npm:lodash@4.17.11",
      "Ecosystem: npm",
      "Confidence: exact (affected_exact)",
      "- CVE-2019-10744 prototype pollution",
      "- CVE-2020-8203 prototype pollution",
      "- CVE-2021-23337 command injection",
      "- CVE-2026-4800 command injection",
      "Promotion: vulnerability_intelligence -> implemented",
    ],
  },
  {
    command: "mcp: compose_replatforming_plan",
    summary: "Bounded AWS -> Azure migration packet with per-item source state and safety gate.",
    activeNodeId: "iac",
    output: [
      "Scope: aws/account=123456789012",
      "Findings: 12 unmanaged, 3 drifted, 1 ambiguous",
      "Plan: 4 items, ordered into migration waves",
      "Wave 1 (early-safe): 2 ready import candidates",
      "- aws_s3_bucket.payment-logs -> azurerm_storage_account",
      "  Import block: ready",
      "Wave 2 (review): 1 ambiguous owner candidate",
      "Wave 3 (blocked): 1 refused (safety gate)",
      "Read-only: never runs Terraform or mutates state",
    ],
  },
] satisfies readonly CommandDemo[];

/** Cleanup modes rendered in the cleanup investigation section. */
export const cleanupModes = [
  {
    label: "Dead code",
    summary: "Find code that is no longer reachable from live services.",
    findings: [
      "services/checkout/internal/legacy_coupon.go",
      "handlers/payment_retry_v1.ts",
      "jobs/reconcile_old_gateway.py",
    ],
  },
  {
    label: "Dead IaC",
    summary: "Apply the same reachability model to stale infrastructure.",
    findings: [
      "terraform/modules/legacy-cache",
      "helm/values/checkout-canary.yaml",
      "kustomize/overlays/old-payments",
    ],
  },
  {
    label: "Unmanaged resources",
    summary: "AWS resources that exist but aren't in Terraform.",
    findings: [
      "aws_s3_bucket.legacy-payment-logs",
      "aws_iam_role.orphan-ci-runner",
      "aws_lambda_function.old-reporter",
    ],
  },
] satisfies readonly CleanupMode[];
