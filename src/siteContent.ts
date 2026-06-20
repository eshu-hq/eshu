export interface NavItem {
  readonly label: string;
  readonly href: string;
}

export interface Capability {
  readonly title: string;
  readonly description: string;
}

export interface PipelineStep {
  readonly label: string;
  readonly detail: string;
}

export interface UseCase {
  readonly question: string;
  readonly answer: string;
}

export interface Surface {
  readonly title: string;
  readonly description: string;
}

export interface RolePrompt {
  readonly role: string;
  readonly prompt: string;
}

export interface ProofPoint {
  readonly value: string;
  readonly title: string;
  readonly description: string;
}

export interface DemoNode {
  readonly id: string;
  readonly label: string;
  readonly detail: string;
}

export interface CommandDemo {
  readonly command: string;
  readonly summary: string;
  readonly output: readonly string[];
  readonly activeNodeId: string;
}

export interface PersonaDemo {
  readonly role: string;
  readonly context: string;
  readonly question: string;
  readonly answer: string;
  readonly primaryTool: string;
}

export interface CleanupMode {
  readonly label: string;
  readonly summary: string;
  readonly findings: readonly string[];
}

const githubHref = "https://github.com/eshu-hq/eshu";
const docsHref = "https://github.com/eshu-hq/eshu/tree/main/docs/public";
const personaMatrixHref =
  "https://github.com/eshu-hq/eshu/blob/main/docs/internal/persona-question-tool-matrix.md";
const supplyChainDemoHref =
  "https://github.com/eshu-hq/eshu/blob/main/docs/public/guides/supply-chain-demo.md";
const replatformingDemoHref =
  "https://github.com/eshu-hq/eshu/blob/main/docs/public/guides/aws-to-azure-replatforming-demo.md";
const lightweightAuditHref =
  "https://github.com/eshu-hq/eshu/blob/main/docs/public/reference/local-lightweight-capability-audit.md";

export const siteContent = {
  nav: [
    { label: "Product", href: "#product" },
    { label: "How it works", href: "#how-it-works" },
    { label: "Personas", href: "#personas" },
    { label: "Try it", href: "#try-it" },
    { label: "Use cases", href: "#use-cases" },
    { label: "Docs", href: docsHref }
  ] satisfies readonly NavItem[],
  hero: {
    coreLine: "One Graph. Every Layer. Every Role.",
    heading: "The institutional knowledge layer for engineering organizations.",
    description:
      "Eshu connects code, dependencies, supply chain, infrastructure, and runtime into one queryable, evidence-backed source of truth — and serves it to AI assistants through MCP. When an engineer asks a question, Eshu answers with the full chain of evidence. When evidence is missing, Eshu refuses.",
    primaryCta: { label: "Try it locally", href: "#try-it" },
    secondaryCta: { label: "Read the docs", href: docsHref }
  },
  capabilities: [
    {
      title: "Supply chain traceability",
      description:
        "Trace a vulnerable dependency from advisory through package, lockfile, registry, container image, SBOM, deployment, and workload. Refuses findings without owned evidence — no false positives from KEV + EPSS alone."
    },
    {
      title: "Code-to-cloud tracing",
      description:
        "Follow a service from source files through Terraform, Kubernetes, cloud resources, and the runtime that serves it. Same provenance on every edge, regardless of where the evidence lives."
    },
    {
      title: "Multi-cloud re-platforming",
      description:
        "Compose a bounded, provider-neutral migration packet from observed AWS state. Hand it to an LLM to generate the Terraform for the target cloud. Eshu never runs Terraform or mutates cloud state."
    },
    {
      title: "Incident response context",
      description:
        "When a page fires, return the deployment chain, the declared/applied/live routing, the fallback change candidates, and the explicit missing slots for build/deploy/commit/PR/Jira."
    },
    {
      title: "IaC governance and drift",
      description:
        "Find AWS resources not in Terraform. Generate read-only import plans. Surface drift findings with per-layer evidence flags. Hours, not weeks, to remediate."
    },
    {
      title: "Code intelligence",
      description:
        "22+ source languages with tree-sitter parsers. Call graphs with provenance. Dead-code candidates. Complexity metrics. Every answer bounded by scope, limit, timeout, and deterministic ordering."
    },
    {
      title: "AI assistant context",
      description:
        "147 MCP tools spanning the machine-verified capability catalog. Truth envelopes on every read. Refusal over silent downgrade. Works with Claude Code, Codex, Cursor, VS Code — any MCP client."
    },
    {
      title: "Institutional knowledge",
      description:
        "Every role in your engineering org asks the same kind of question and gets the same answer with the same truth envelope. The graph is the institutional memory."
    }
  ] satisfies readonly Capability[],
  pipeline: [
    { label: "Source code", detail: "22+ languages" },
    { label: "Manifest + lockfile", detail: "13+ package ecosystems" },
    { label: "Container registry", detail: "ECR, GHCR, Docker Hub, Harbor, GAR, ACR, JFrog" },
    { label: "SBOM + attestation", detail: "CycloneDX, SPDX, in-toto, OCI referrers" },
    { label: "Vulnerability feeds", detail: "CISA KEV, EPSS, OSV, NVD, Dependabot" },
    { label: "IaC + cloud posture", detail: "Terraform state, AWS, Azure, GCP, K8s" },
    { label: "Observability + incidents", detail: "PagerDuty, Jira, Grafana, Loki, Tempo" },
    { label: "Eshu evidence graph", detail: "reducer-owned truth, provenance, refusal" }
  ] satisfies readonly PipelineStep[],
  terminalCommands: [
    "eshu scan",
    "eshu trace service checkout",
    "mcp: list_supply_chain_impact_findings",
    "mcp: compose_replatforming_plan"
  ] as const,
  demoTrace: {
    service: "checkout-service",
    nodes: [
      { id: "code", label: "Code", detail: "services/checkout" },
      { id: "supply-chain", label: "Supply chain", detail: "npm, ECR, SBOM" },
      { id: "iac", label: "IaC", detail: "Terraform + K8s" },
      { id: "cloud", label: "Cloud", detail: "AWS prod us-east-1" },
      { id: "runtime", label: "Runtime", detail: "checkout-api pods" },
      { id: "incidents", label: "Incidents", detail: "PagerDuty, Jira" }
    ] satisfies readonly DemoNode[]
  },
  commandDemos: [
    {
      command: "eshu scan --json",
      summary: "Graph ready for organization-wide questions.",
      activeNodeId: "code",
      output: [
        "\"status\": \"ready\",",
        "\"succeeded\": 8347,",
        "\"queue_zero_ms\": 853600,",
        "\"freshness\": \"current\""
      ]
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
        "Missing evidence: runtime"
      ]
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
        "Promotion: vulnerability_intelligence -> implemented"
      ]
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
        "Read-only: never runs Terraform or mutates state"
      ]
    }
  ] satisfies readonly CommandDemo[],
  personaDemos: [
    {
      role: "SRE / on-call",
      context: "Production incident in progress",
      question: "Is this safe to roll back? Show me the full chain.",
      answer:
        "Eshu returns the deployment chain (commit -> image -> registry -> workload), the declared/applied/live routing for any PagerDuty service, the fallback change candidates, and the explicit missing slots for build/deploy/commit/PR/Jira. Same evidence your on-call would see.",
      primaryTool: "get_incident_context"
    },
    {
      role: "Security analyst",
      context: "CVE published overnight",
      question: "Which of my workloads are affected by CVE-2025-13465? Show me the chain.",
      answer:
        "Eshu joins the advisory to your owned package-manifest, lockfile, registry, container image, SBOM, deployment, and workload. Findings are published only when owned evidence backs them — KEV + EPSS alone don't trigger findings.",
      primaryTool: "list_supply_chain_impact_findings"
    },
    {
      role: "Platform engineer",
      context: "AWS account audit",
      question: "Which AWS resources aren't in Terraform? Generate the import plan.",
      answer:
        "Eshu returns the unmanaged resources, owner candidates from tags/repos/modules/services, and read-only Terraform import blocks for the safety-approved supported cloud-only findings. Refused items get explicit refusal reasons.",
      primaryTool: "find_unmanaged_resources"
    },
    {
      role: "Engineer switching teams",
      context: "First week on a new product",
      question: "What does this service do? How is it deployed? Who owns it?",
      answer:
        "Eshu returns the service dossier, the deployment chain from code to cloud, the owner candidates, and the related code, infrastructure, and documentation. No 3-month ramp needed.",
      primaryTool: "get_service_story"
    },
    {
      role: "CTO",
      context: "Board meeting in 30 minutes",
      question: "What's our security posture? What's the cost of migrating from AWS to Azure?",
      answer:
        "Eshu returns the drift findings rollups, the published vuln impact findings by ecosystem, the re-platforming rollups by account/env/service, and the readiness-by-wave view. Same source of truth your engineers use.",
      primaryTool: "get_replatforming_rollups"
    },
    {
      role: "Developer",
      context: "Refactoring a shared client",
      question: "Who calls this function across all repos? What breaks if I change it?",
      answer:
        "Eshu returns transitive callers up to graph depth, with resolution_method per edge (scip, declared, import_binding, type_inferred, etc.). Provenance on every claim.",
      primaryTool: "get_code_relationship_story"
    },
    {
      role: "Sales engineer",
      context: "Customer demo",
      question: "What does this customer have deployed? What changed since last quarter?",
      answer:
        "Eshu returns the customer's ecosystem overview, the service stories, the changed-since inventory. Same MCP tools your engineers use, scoped to the customer's account.",
      primaryTool: "get_ecosystem_overview"
    },
    {
      role: "Data engineer",
      context: "Dashboard accuracy audit",
      question: "What's the lineage from this dashboard to the source table?",
      answer:
        "Eshu parses SQL, dbt models, Glue Data Catalog, Athena queries, and Redshift clusters. Returns the lineage chain with parser-proven edges and explicit unresolved references.",
      primaryTool: "investigate_resource"
    }
  ] satisfies readonly PersonaDemo[],
  cleanupModes: [
    {
      label: "Dead code",
      summary: "Find code that is no longer reachable from live services.",
      findings: [
        "services/checkout/internal/legacy_coupon.go",
        "handlers/payment_retry_v1.ts",
        "jobs/reconcile_old_gateway.py"
      ]
    },
    {
      label: "Dead IaC",
      summary: "Apply the same reachability model to stale infrastructure.",
      findings: [
        "terraform/modules/legacy-cache",
        "helm/values/checkout-canary.yaml",
        "kustomize/overlays/old-payments"
      ]
    },
    {
      label: "Unmanaged resources",
      summary: "AWS resources that exist but aren't in Terraform.",
      findings: [
        "aws_s3_bucket.legacy-payment-logs",
        "aws_iam_role.orphan-ci-runner",
        "aws_lambda_function.old-reporter"
      ]
    }
  ] satisfies readonly CleanupMode[],
  surfaces: [
    {
      title: "MCP",
      description:
        "Give AI assistants graph-backed context. 147 tools across the capability catalog. Works with Claude Code, Codex, Cursor, VS Code, any MCP client."
    },
    {
      title: "HTTP API",
      description:
        "Same query model as MCP, versioned under /api/v0. OpenAPI spec at /api/v0/openapi.json. Canonical envelope format with truth envelopes and freshness states."
    },
    {
      title: "CLI",
      description:
        "Run local scans, trace a service, map a resource. eshu mcp start boots the workspace MCP server for any harness."
    },
    {
      title: "Console",
      description:
        "Private read-only product UI for Eshu graph data. 30+ pages covering code graph, dead code, dependencies, findings, IaC, images, impact, incident context, operations, replatforming, repositories, service intelligence, secrets/IAM, SBOM, topology, vulnerabilities."
    },
    {
      title: "SDK",
      description:
        "Open-source Go SDK (sdk/go/collector) for out-of-tree collector authors. Wire protocol collector-sdk/v1alpha1. Fail-closed host validation."
    }
  ] satisfies readonly Surface[],
  coverage:
    "Eshu indexes code and infrastructure together: 22+ source languages, 8+ IaC formats (Terraform, Terragrunt, Helm, Kubernetes, Kustomize, ArgoCD, CloudFormation, Crossplane), 13+ package ecosystems (npm, PyPI, Go modules, Maven, NuGet, Composer, RubyGems, Cargo, Swift, Pub, Hex, plus OS apk/dpkg/rpm), 7 container registries (ECR, GHCR, Docker Hub, Harbor, GAR, ACR, JFrog), 4 vulnerability sources (CISA KEV, FIRST EPSS, OSV, NVD) plus govulncheck reachability for Go, and 134 AWS service scanners. Capability truth is profile-aware and gated by the machine-verified capability catalog at specs/capability-matrix.v1.yaml.",
  proofPoints: [
    {
      value: "Supply chain",
      title: "Production-promoted end-to-end traceability",
      description:
        "vulnerability_intelligence collector at promotion_state: implemented. 7 published impact findings (CVE-2019-10744 through CVE-2026-4800) for npm lodash 4.17.11, full chain from advisory to workload. Refuses findings without owned evidence."
    },
    {
      value: "Multi-cloud",
      title: "Re-platforming from AWS to Azure, runnable today",
      description:
        "compose_replatforming_plan returns bounded migration packets with per-item source state, safety gate, owner candidates, ready/refused Terraform import candidates, migration waves. AWS -> Azure demo at docs/public/guides/aws-to-azure-replatforming-demo.md."
    },
    {
      value: "Personas",
      title: "18 personas, one MCP server, one source of truth",
      description:
        "New engineer, switching engineer, SRE, security analyst, platform engineer, developer, support, migration architect, frontend, backend, data engineer, product manager, director, VP, CTO, CEO, sales/CSM, customer-facing. Every persona asks questions of the same evidence graph."
    },
    {
      value: "Open source",
      title: "MIT-licensed, self-hosted, no vendor telemetry",
      description:
        "Every capability anchored to a public spec, a public fixture, and a public test. The capability catalog at specs/capability-matrix.v1.yaml is machine-verified: go run ./cmd/capability-inventory -mode verify fails CI on drift. No enterprise edition. No contact-sales tier."
    }
  ] satisfies readonly ProofPoint[],
  rolePrompts: [
    {
      role: "New engineer",
      prompt: "Use Eshu to explain this service end to end."
    },
    {
      role: "SRE / on-call",
      prompt: "Use Eshu to show me the blast radius of this change."
    },
    {
      role: "Security analyst",
      prompt: "Use Eshu to find every workload affected by CVE-2025-13465."
    },
    {
      role: "Platform engineer",
      prompt: "Use Eshu to find AWS resources not in Terraform and generate the import plan."
    },
    {
      role: "Migration architect",
      prompt: "Use Eshu to compose the AWS -> Azure re-platforming plan."
    },
    {
      role: "VP Engineering",
      prompt: "Use Eshu to show our security posture and re-platforming readiness for the board update."
    },
    {
      role: "CTO",
      prompt: "Use Eshu to find our single points of failure and our supply chain risk surface."
    },
    {
      role: "Sales engineer",
      prompt: "Use Eshu to show me what this customer has deployed and what changed since last quarter."
    }
  ] satisfies readonly RolePrompt[],
  useCases: [
    {
      question: "Which workloads are affected by CVE-X?",
      answer:
        "Trace from advisory through package, lockfile, registry, image, SBOM, deployment, workload. Refuses findings without owned evidence."
    },
    {
      question: "Which AWS resources aren't in Terraform?",
      answer:
        "find_unmanaged_resources returns the unmanaged set, owner candidates, and read-only Terraform import blocks for the safety-approved supported cloud-only findings."
    },
    {
      question: "How do I migrate from AWS to Azure?",
      answer:
        "compose_replatforming_plan returns a bounded migration packet with per-item source state, safety gate, owner candidates, and ready/refused import candidates. Hand to an LLM to generate the azurerm_* Terraform."
    },
    {
      question: "What's the blast radius of this change?",
      answer:
        "find_blast_radius returns bounded graph traversal from a target entity, with provenance on every relationship row."
    },
    {
      question: "Who calls this function across all repos?",
      answer:
        "get_code_relationship_story returns transitive callers, callees, importers, and implementers with resolution_method per edge."
    },
    {
      question: "What does this customer have deployed?",
      answer:
        "get_service_story, list_cloud_resource_inventory, get_changed_since return the customer's ecosystem scoped to their account."
    },
    {
      question: "What owns this cloud resource?",
      answer:
        "find_unmanaged_resource_owners returns owner candidates from tags/repos/modules/services, with confidence, freshness, and ambiguity reasoning."
    }
  ] satisfies readonly UseCase[],
  tryIt: {
    heading: "Try it in under a minute.",
    steps: [
      "git clone https://github.com/eshu-hq/eshu",
      "cd eshu",
      "docker compose up --build",
      "eshu mcp setup    # prints the client snippet for Claude Code, Codex, Cursor, or VS Code",
      "eshu mcp start   # boots the local MCP server"
    ],
    firstQuestion:
      "Then in your AI assistant of choice: \"Use Eshu to explain this service end to end.\"",
    ctaLabel: "View on GitHub",
    ctaHref: githubHref
  },
  difference: {
    heading: "What makes Eshu different.",
    points: [
      {
        target: "Snyk",
        claim: "Returns findings from KEV + EPSS alone. Eshu refuses — owned evidence required."
      },
      {
        target: "Wiz",
        claim: "Knows cloud security. Eshu knows cloud security AND the code AND the package chain that produced the deploy."
      },
      {
        target: "Sourcegraph",
        claim: "Finds code. Eshu finds code AND the deployment chain AND the blast radius AND the supply chain risk."
      },
      {
        target: "Firefly / Morpheus",
        claim: "Do cloud governance. Eshu does cloud governance AND the re-platforming plan AND the institutional knowledge layer."
      },
      {
        target: "Firehydrant / incident.io",
        claim: "Do incident response. Eshu does incident response AND the supply-chain context AND the deployment evidence."
      },
      {
        target: "The unification",
        claim: "No competitor ships all of this in one graph, behind one MCP server, with one set of truth envelopes, MIT-licensed, self-hosted, free."
      }
    ]
  },
  references: {
    fullPersonaMatrix: personaMatrixHref,
    supplyChainDemo: supplyChainDemoHref,
    replatformingDemo: replatformingDemoHref,
    lightweightAudit: lightweightAuditHref
  },
  closing: {
    heading: "Stop losing institutional knowledge to tenure.",
    description:
      "Eshu is the answer: a queryable, evidence-backed knowledge layer that builds itself from your actual artifacts. New hires ramp in days. Engineers switch teams without losing context. Customer-facing teams answer questions from the same source of truth as your engineers. Security, platform, and SRE work from the same data."
  }
} as const;
