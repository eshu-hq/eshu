import type { Capability, PipelineStep, Surface, UseCase } from "./siteContentTypes";

/** Product capability rows shown in the "What Eshu does" section. */
export const capabilities = [
  {
    title: "Agentic Q&A",
    description:
      "Ask Eshu answers natural-language questions over the evidence graph with provider-portable adapters, per-token streaming, and a read-only Cypher + SQL sandbox. Every claim carries evidence handles, truth class, and limitations."
  },
  {
    title: "Supply chain traceability",
    description:
      "Trace a vulnerable dependency from advisory through package, lockfile, registry, container image, SBOM, deployment, and workload. Refuses findings without owned evidence; no false positives from KEV + EPSS alone."
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
      "When a page fires, return the deployment chain, declared/applied/live routing, fallback change candidates, and explicit missing slots for build, deploy, commit, PR, and Jira evidence."
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
      "147 MCP tools spanning the machine-verified capability catalog. Truth envelopes on every read. Refusal over silent downgrade. Works with Claude Code, Codex, Cursor, VS Code, or any MCP client."
  },
  {
    title: "Institutional knowledge",
    description:
      "Every role in your engineering org asks the same kind of question and gets the same answer with the same truth envelope. The graph is the institutional memory."
  }
] satisfies readonly Capability[];

/** Source-to-runtime evidence pipeline summarized on the launch page. */
export const pipeline = [
  { label: "Source code", detail: "22+ languages" },
  { label: "Manifest + lockfile", detail: "13+ package ecosystems" },
  { label: "Container registry", detail: "ECR, GHCR, Docker Hub, Harbor, GAR, ACR, JFrog" },
  { label: "SBOM + attestation", detail: "CycloneDX, SPDX, in-toto, OCI referrers" },
  { label: "Vulnerability feeds", detail: "CISA KEV, EPSS, OSV, NVD, Dependabot" },
  { label: "IaC + cloud posture", detail: "Terraform state, AWS, Azure, GCP, K8s" },
  { label: "Observability + incidents", detail: "PagerDuty, Jira, Grafana, Loki, Tempo" },
  { label: "Eshu evidence graph", detail: "reducer-owned truth, provenance, refusal" }
] satisfies readonly PipelineStep[];

/** Public surfaces where Eshu exposes the same graph-backed truth. */
export const surfaces = [
  {
    title: "MCP",
    description:
      "Give AI assistants graph-backed context. 147 tools across the capability catalog, including `ask`. Works with Claude Code, Codex, Cursor, VS Code, or any MCP client."
  },
  {
    title: "HTTP API",
    description:
      "Same query model as MCP, versioned under /api/v0. OpenAPI spec at /api/v0/openapi.json. Canonical envelope format with truth envelopes and freshness states."
  },
  {
    title: "CLI",
    description:
      "Run local scans, trace a service, ask a natural-language question, map a resource, or start the workspace MCP server for any harness."
  },
  {
    title: "Console",
    description:
      "Private read-only product UI for graph data: Ask Eshu, evidence packets, code graph, dependencies, findings, IaC, impact, incidents, operations, repositories, service intelligence, SBOM, topology, and vulnerabilities."
  },
  {
    title: "SDK",
    description:
      "Open-source Go SDK (sdk/go/collector) for out-of-tree collector authors. Wire protocol collector-sdk/v1alpha1. Fail-closed host validation."
  }
] satisfies readonly Surface[];

/** Breadth statement for ingestion and evidence coverage. */
export const coverage =
  "Eshu indexes code and infrastructure together: 22+ source languages, 8+ IaC formats (Terraform, Terragrunt, Helm, Kubernetes, Kustomize, ArgoCD, CloudFormation, Crossplane), 13+ package ecosystems (npm, PyPI, Go modules, Maven, NuGet, Composer, RubyGems, Cargo, Swift, Pub, Hex, plus OS apk/dpkg/rpm), 7 container registries (ECR, GHCR, Docker Hub, Harbor, GAR, ACR, JFrog), 4 vulnerability sources (CISA KEV, FIRST EPSS, OSV, NVD) plus govulncheck reachability for Go, and 134 AWS service scanners. Capability truth is profile-aware and gated by the machine-verified capability catalog at specs/capability-matrix.v1.yaml.";

/** Questions Eshu can answer with bounded evidence. */
export const useCases = [
  {
    question: "Which workloads are affected by CVE-X?",
    answer:
      "Trace from advisory through package, lockfile, registry, image, SBOM, deployment, workload. Refuses findings without owned evidence."
  },
  {
    question: "Can I ask this directly?",
    answer:
      "Ask Eshu routes the natural-language question through bounded retrieval, read-only query tools, evidence handles, and explicit limitations."
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
      "get_service_story, list_cloud_resource_inventory, and get_changed_since return the customer's ecosystem scoped to their account."
  },
  {
    question: "What owns this cloud resource?",
    answer:
      "find_unmanaged_resource_owners returns owner candidates from tags, repos, modules, and services, with confidence, freshness, and ambiguity reasoning."
  }
] satisfies readonly UseCase[];
