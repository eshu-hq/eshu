import type {
  CapabilityTab,
  MetricStat,
  PipelineStep,
  PrincipleItem,
  SurfaceItem,
} from "./siteContentTypes";

/** Machine-verified headline platform counts from surface-inventory.generated.json. */
export const metrics: readonly MetricStat[] = [
  {
    value: "173",
    label: "Read-only MCP tools",
    detail: "Plus full, router & core_router modes",
  },
  {
    value: "281",
    label: "HTTP API routes",
    detail: "Versioned OpenAPI contract",
  },
  {
    value: "88",
    label: "Correlation pipelines",
    detail: "Run inside one reducer worker (not 88 pods)",
  },
  {
    value: "42",
    label: "Console UI pages",
    detail: "With OIDC, SAML, RBAC & TOTP MFA",
  },
  {
    value: "22",
    label: "Source languages",
    detail: "Plus 9+ IaC & GitOps formats",
  },
  {
    value: "23",
    label: "Read-only collectors",
    detail: "Off by default; enable only what you use",
  },
];

/** Six interactive capability pillars covering Eshu's full technical breadth. */
export const capabilityTabs: readonly CapabilityTab[] = [
  {
    id: "code",
    label: "Code & Flow",
    headline: "Cross-repo call graphs, data flow, and copy-paste drift detection.",
    summary:
      "Works on Day 1 from Git alone. Native Go and tree-sitter parsers index 22 languages with symbol resolution, control/data flow, dead-code detection, and parallel-implementation tracking.",
    highlights: [
      {
        title: "22 languages + cross-repo symbol graphs",
        tag: "Git only · Day 1",
        description:
          "Go, Python, TypeScript, TSX, JavaScript, Java, Kotlin, Scala, C, C++, C#, Rust, Ruby, PHP, Swift, Dart, Elixir, Haskell, Perl, Groovy, SQL, and JSON config — with per-edge resolution provenance.",
      },
      {
        title: "Trace data & control flow across functions",
        tag: "CFG · PDG · Taint",
        description:
          "Follow how user input and variables move through code using control-flow graphs, program dependence graphs, reaching definitions, and taint paths over /api/v0/code/flow/*.",
      },
      {
        title: "Spot copy-pasted or drifted code",
        tag: "Local profile · 5 families",
        description:
          "Detect parallel or diverging implementations inside a repository across five families (exact, renamed, drifted, wrapper_bypass, and convention_outlier) using structural function fingerprints and MinHash similarity sketches.",
      },
      {
        title: "Dead code, CODEOWNERS & submodule pins",
        tag: "Ownership",
        description:
          "Surface unreachable intra-repo and cross-repo code paths, cyclomatic complexity hotspots, CODEOWNERS ownership edges, and pinned Git submodules.",
      },
    ],
  },
  {
    id: "supply-chain",
    label: "Supply Chain",
    headline: "From CVE advisory to deployed workload — plus CI-ready SARIF & VEX scans.",
    summary:
      "Reconcile advisories, package registries, OCI images, SBOMs, and multi-language reachability using the same truth engine locally, in CI, or in a hosted cluster.",
    highlights: [
      {
        title: "End-to-end CVE & SBOM traceability",
        tag: "Opt-in collectors",
        description:
          "Join CISA KEV, FIRST EPSS, OSV, NVD, and CVE List v5 across 13+ package ecosystems, 7 container registries, and CycloneDX/SPDX/in-toto/SLSA documents bound by subject digest.",
      },
      {
        title: "Multi-language vulnerability reachability",
        tag: "Go GA · Value-flow opt-in",
        description:
          "Go reachability via govulncheck (GA), value-flow taint reachability for Python, TypeScript, JavaScript, Java, and C# (opt-in via ESHU_EMIT_DATAFLOW), and bounded JVM reachability (preview).",
      },
      {
        title: "CI-ready standalone scanner (SARIF & VEX)",
        tag: "eshu vuln-scan repo",
        description:
          "Run scoped local or CI scans exporting SARIF v2.1.0 and VEX statements with strict non-silent exit codes (0 clean, 3 findings, 4 missing evidence, 5 unsupported) — so missing data never passes as zero findings.",
      },
      {
        title: "Alert reconciliation & air-gapped EOL checks",
        tag: "Zero outbound lookup",
        description:
          "Reconcile GitHub Dependabot alerts against owned evidence and flag end-of-life language runtimes (supported, support-ending, eol, unknown) offline against vendored endoflife.date snapshots.",
      },
    ],
  },
  {
    id: "exposure",
    label: "Code-to-Cloud",
    headline: "Trace untrusted input straight into correlated cloud sinks.",
    summary:
      "Walk from HTTP, RPC, and Lambda handlers across call chains into IAM privileges, secret access paths, internet-exposed endpoints, SQL tables, and shell execution.",
    highlights: [
      {
        title: "Cloud-sink exposure path tracing",
        tag: "trace_exposure_path",
        description:
          "Connect entry points to correlated cloud facts (CAN_PERFORM, CAN_ESCALATE_TO, CAN_ASSUME, secret reads, 0.0.0.0/0 endpoints, SQL tables, shell exec) ranked honestly as internet_exposed, network_reachable, or internal.",
      },
      {
        title: "Secrets & IAM posture intelligence",
        tag: "GA",
        description:
          "Surface hardcoded secrets in code, workload-to-secret access paths, IAM trust and privilege-escalation chains, and explicit posture gaps with source citations.",
      },
      {
        title: "AWS cloud posture materialization",
        tag: "130+ scanners",
        description:
          "Materialize S3 internet exposure and external principal grants, RDS posture, EC2 internet exposure and EBS/KMS posture, security-group CIDR reachability, and SSM host inventory.",
      },
      {
        title: "HashiCorp Vault posture (metadata-only)",
        tag: "Opt-in / Gated",
        description:
          "Inspect Vault mounts, auth methods, and policy bindings to map secret-access paths without ever reading or storing secret values.",
      },
    ],
  },
  {
    id: "iac",
    label: "IaC & Drift",
    headline: "Compare declared IaC, applied Terraform state, and live cloud reality.",
    summary:
      "Parse 9+ IaC and GitOps formats on Day 1, catch unmanaged resources and dead modules, diff environments, and generate read-only migration packets.",
    highlights: [
      {
        title: "9+ IaC & GitOps formats",
        tag: "Git only · Day 1",
        description:
          "Terraform, Terragrunt, Helm, Kubernetes, Kustomize, ArgoCD, Flux, CloudFormation, Crossplane (SATISFIED_BY), Docker Compose, GitLab CI, and Atlantis.",
      },
      {
        title: "Unmanaged resources & Terraform import plans",
        tag: "Read-only",
        description:
          "Find cloud resources absent from Terraform, rank owner candidates from tags/repos/modules, and emit read-only Terraform import blocks with explicit safety refusals.",
      },
      {
        title: "Declared vs. applied vs. live drift",
        tag: "Multi-layer",
        description:
          "Detect Terraform config-vs-state drift, AWS/multi-cloud runtime drift, dead IaC modules no live workload depends on, and staging-vs-production environment differences.",
      },
      {
        title: "Wave-ordered multi-cloud re-platforming",
        tag: "compose_replatforming_plan",
        description:
          "Compose bounded migration packets (e.g., AWS to Azure) ordered into waves with per-item source state, safety gates, and owner candidates. Eshu never mutates cloud state.",
      },
    ],
  },
  {
    id: "ops",
    label: "Incidents & Ops",
    headline: "Bring incident, CI/CD, catalog, and observability context to the same graph.",
    summary:
      "Connect PagerDuty incidents, GitHub Actions/GitLab CI runs, Jira work items, service catalogs, data lineage, and metadata-only observability signals.",
    highlights: [
      {
        title: "Incident context & routing verification",
        tag: "get_incident_context",
        description:
          "Correlate PagerDuty incidents, timelines, and declared/applied/live routing with recent CI/CD builds, merged PRs, container digests, and Jira issues — flagging every missing slot.",
      },
      {
        title: "Metadata-only observability collectors",
        tag: "Zero raw logs/spans",
        description:
          "Correlate Grafana dashboards/rules, Prometheus/Mimir scrape targets, Loki label topology/rules, and Tempo trace services without ever ingesting log lines, metric samples, or spans.",
      },
      {
        title: "Service catalogs & scorecards",
        tag: "Git only · Day 1",
        description:
          "Ingest committed Backstage (catalog-info.yaml), OpsLevel (opslevel.yml), and Cortex (cortex.yaml) descriptors, API links, ownership, and scorecards.",
      },
      {
        title: "SQL & cloud data lineage",
        tag: "Parser-proven",
        description:
          "Trace lineage across SQL files, dbt models, AWS Glue Data Catalog, Athena queries, and Redshift clusters with explicit unresolved references.",
      },
    ],
  },
  {
    id: "ai-docs",
    label: "Doc Truth & AI",
    headline: "Keep organizational docs honest and give coding agents bounded context.",
    summary:
      "Verify Markdown and Confluence claims against live repo truth, save agent context tokens with MCP routing, and export portable proof packets.",
    highlights: [
      {
        title: "Documentation truth verification",
        tag: "eshu docs verify",
        description:
          "Check Markdown and Confluence claims (CLI commands, HTTP routes, ESHU_* env vars, repo paths, container images, Terraform addresses, service deployments) as valid, contradicted, or missing_evidence.",
      },
      {
        title: "Context-saving MCP tool router",
        tag: "full · router · core_router",
        description:
          "Serve all 173 tools or collapse discovery into find_tool + call_tool (with structured single-copy payloads) so Claude Code, Cursor, VS Code, and Codex stay fast and lean.",
      },
      {
        title: "Pre-change impact & developer plans",
        tag: "eshu change plan",
        description:
          "Run blast-radius analysis on a local git diff and generate a read-only change plan with ordered actions, patch guidance, recommended tests, and bounded next calls.",
      },
      {
        title: "Ask Eshu, evidence packets & semantic search",
        tag: "Opt-in & portable",
        description:
          "Provider-portable Ask Eshu streaming Q&A (default-off), deterministic portable evidence packets (JSON/Markdown/HTML), and optional local-embedder semantic retrieval.",
      },
    ],
  },
];

/** Five-stage facts-first architecture pipeline. */
export const pipeline: readonly PipelineStep[] = [
  {
    step: "01",
    label: "Sources & 23 collectors",
    detail:
      "Read-only intake across repos, registries, cloud APIs, CI/CD, incidents, and observability metadata.",
  },
  {
    step: "02",
    label: "Durable facts in Postgres",
    detail:
      "Versioned, generation-stamped facts, content store, claim queues, and full graph-rebuild recovery state.",
  },
  {
    step: "03",
    label: "88 correlation pipelines",
    detail:
      "Idempotent reducer workers admit evidence, link cross-source entities, and gate edges on readiness.",
  },
  {
    step: "04",
    label: "Neo4j graph & read models",
    detail:
      "Canonical entity graph and bounded Postgres read models with explicit provenance on every hop.",
  },
  {
    step: "05",
    label: "MCP, CLI, API & Console",
    detail:
      "One query contract with truth envelopes, freshness labels, and refusal when evidence is missing.",
  },
];

/** All five public interfaces into Eshu. */
export const surfaces: readonly SurfaceItem[] = [
  {
    title: "MCP Server",
    metric: "173 read-only tools",
    description:
      "Give AI coding assistants verified organizational context over stdio or HTTP/SSE with built-in context-budget controls.",
    bullets: [
      "Tool surface modes: full, router (find_tool/call_tool), and core_router",
      "Single-copy structured result shape for Claude Code and Codex",
      "Works with Claude Code, Cursor, VS Code, Codex, and any MCP client",
    ],
  },
  {
    title: "Self-Hosted Console",
    metric: "42 product & admin pages",
    description:
      "Read-only web UI for visual graph exploration, security triage, operations, and built-in enterprise governance — all MIT.",
    bullets: [
      "Explorer, Code Graph, Exposure Path, Service Atlas, Reachable Vulns, Drift & Replatforming",
      "Operations Live Board, Freshness Causality, Collector Readiness & Capability Matrix",
      "Built-in OIDC, SAML, GitHub SSO, TOTP MFA, RBAC roles, scoped tokens & audit logs",
    ],
  },
  {
    title: "CLI",
    metric: "41 command families",
    description:
      "Run zero-credential demos, local repo scans, SARIF/VEX vulnerability checks, pre-change plans, and doc verification.",
    bullets: [
      "eshu demo up, eshu first-run, eshu trace service, eshu map",
      "eshu vuln-scan repo (--export sarif|vex), eshu change plan, eshu docs verify",
      "Portable JSON/Markdown/HTML evidence packets, operator digests & redacted bundles",
    ],
  },
  {
    title: "HTTP API",
    metric: "281 OpenAPI routes",
    description:
      "Same bounded query model and authorization passthrough as MCP, versioned under /api/v0 and /api/v1.",
    bullets: [
      "Canonical { data, truth, error } envelopes with explicit freshness and omissions",
      "Code-flow endpoints (CFG, PDG, reaching-def, taint-path) and Ask Eshu streaming",
      "OpenAPI specification served directly at /api/v0/openapi.json",
    ],
  },
  {
    title: "Go Collector SDK & Host",
    metric: "31-repo modular org",
    description:
      "Build and run out-of-tree read-only collectors without importing Eshu core, managed with fail-closed producer grants.",
    bullets: [
      "eshu-sdk Go modules with byte-checked fact contracts",
      "eshu component CLI (init, verify, install, grant, diagnostics)",
      "eshu-extension-host & signed webhook intake (eshu-webhook)",
    ],
  },
];

/** Calm, evidence-first principles with zero vendor comparisons. */
export const principles: readonly PrincipleItem[] = [
  {
    title: "Evidence on every hop",
    description:
      "Every graph edge and query response keeps the source file, digest, lockfile, or collector fact behind it — so engineers, auditors, and assistants can verify why two records are linked.",
  },
  {
    title: "Refusal over guessing",
    description:
      "Every answer carries a truth label, freshness state, and explicit missing-evidence slots. When a required bridge or collector is absent, Eshu says so instead of fabricating a clean result.",
  },
  {
    title: "Strictly scoped, read-only & privacy-first",
    description:
      "Collectors stay off until you opt in, never mutate your systems, redact Terraform state secrets, and collect metadata only from Vault, Loki, Tempo, and Prometheus — while query endpoints reject unscoped whole-graph dumps.",
  },
  {
    title: "MIT-licensed & self-hosted",
    description:
      "Run locally on a laptop or deploy to Kubernetes via eshu-deploy with zero outbound vendor telemetry. Enterprise SSO, RBAC, audit logs, and all 162 verified capabilities ship in open source.",
  },
];
