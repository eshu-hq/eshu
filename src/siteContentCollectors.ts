import type { CollectorCategory, CollectorItem, ReadinessLegendItem } from "./siteContentTypes";

/** Plain-English legend explaining the four machine-verified collector status badges. */
export const readinessLegend: readonly ReadinessLegendItem[] = [
  {
    key: "implemented",
    label: "Implemented",
    meaning: "Built, tested end-to-end, and ready to enable.",
  },
  {
    key: "partial",
    label: "Partial",
    meaning:
      "Collects and correlates real evidence today; full cluster/chart promotion proof still in progress.",
  },
  {
    key: "gated",
    label: "Opt-in / Gated",
    meaning: "Behind an explicit operator gate while live-provider promotion completes.",
  },
  {
    key: "foundation_only",
    label: "Foundation",
    meaning: "Read-only core and schema landed; hosted claim-driven runtime still in progress.",
  },
];

/** Ordered filter tabs for the collector ecosystem grid. */
export const collectorCategories: readonly ("All" | CollectorCategory)[] = [
  "All",
  "Core & Intake",
  "Supply Chain & Security",
  "Cloud & IaC",
  "Delivery & Docs",
  "Observability & Semantic",
];

/**
 * All 23 collector lanes tracked in surface-inventory.generated.json.
 * Every collector is strictly read-only and stays off by default until configured.
 */
export const collectors: readonly CollectorItem[] = [
  {
    name: "Git & Repository Intake",
    lane: "git",
    category: "Core & Intake",
    readiness: "implemented",
    summary:
      "Indexes 22 source languages, 9+ IaC formats, lockfiles, CODEOWNERS, Backstage/OpsLevel/Cortex catalogs, and repo docs.",
  },
  {
    name: "Scanner Worker",
    lane: "scanner_worker",
    category: "Core & Intake",
    readiness: "implemented",
    summary:
      "Isolated ephemeral analyzers for CycloneDX SBOM generation, OCI layer/rootfs unpacking, and OS package extraction.",
  },
  {
    name: "Webhook Intake",
    lane: "webhook",
    category: "Core & Intake",
    readiness: "partial",
    summary:
      "Verifies signed deliveries from GitHub, GitLab, Bitbucket, AWS, GCP, PagerDuty, and Jira to wake scoped collector runs.",
  },
  {
    name: "Vulnerability Intelligence",
    lane: "vulnerability_intelligence",
    category: "Supply Chain & Security",
    readiness: "implemented",
    summary:
      "Reconciles CISA KEV, FIRST EPSS, OSV, NVD, and CVE List v5 (with optional GHSA) against owned dependency facts.",
  },
  {
    name: "Package Registry",
    lane: "package_registry",
    category: "Supply Chain & Security",
    readiness: "implemented",
    summary:
      "Collects package versions, publication metadata, and native dependency graphs across supported ecosystems.",
  },
  {
    name: "OCI Container Registry",
    lane: "oci_registry",
    category: "Supply Chain & Security",
    readiness: "implemented",
    summary:
      "Digest-first image manifests and tag history across ECR, GHCR, Docker Hub, Harbor, GAR, ACR, and JFrog.",
  },
  {
    name: "SBOM & Attestations",
    lane: "sbom_attestation",
    category: "Supply Chain & Security",
    readiness: "implemented",
    summary:
      "Parses CycloneDX, SPDX, in-toto, SLSA, and OCI referrer documents and binds them by explicit subject digest.",
  },
  {
    name: "Provider Security Alerts",
    lane: "security_alert",
    category: "Supply Chain & Security",
    readiness: "implemented",
    summary:
      "Reads GitHub Dependabot repository and org-wide alerts and reconciles them against Eshu-owned dependency evidence.",
  },
  {
    name: "AWS Cloud",
    lane: "aws",
    category: "Cloud & IaC",
    readiness: "partial",
    summary:
      "Read-only account inventory, IAM/S3/RDS/EC2/security-group posture, and runtime drift across 130+ AWS scanners.",
  },
  {
    name: "Terraform State",
    lane: "terraform_state",
    category: "Cloud & IaC",
    readiness: "partial",
    summary:
      "Reads local or S3 Terraform state with secret values redacted so Eshu can compare declared, applied, and live cloud state.",
    privacyNote: "Secrets redacted before emission",
  },
  {
    name: "GCP Cloud",
    lane: "gcp",
    category: "Cloud & IaC",
    readiness: "gated",
    summary:
      "Google Cloud Asset Inventory, tag bindings, IAM trust facts, and multi-cloud inventory admission.",
  },
  {
    name: "Azure Cloud",
    lane: "azure",
    category: "Cloud & IaC",
    readiness: "gated",
    summary:
      "Azure resource inventory, relationships, and multi-cloud drift evidence for governed cloud estates.",
  },
  {
    name: "Kubernetes Live",
    lane: "kubernetes_live",
    category: "Cloud & IaC",
    readiness: "foundation_only",
    summary:
      "Read-only cluster workloads, pods, namespaces, services, ingresses, RBAC, and RUNS_IMAGE correlation.",
  },
  {
    name: "HashiCorp Vault Live",
    lane: "vault_live",
    category: "Cloud & IaC",
    readiness: "gated",
    summary: "Reads Vault posture and mount/policy metadata to map secret-access paths.",
    privacyNote: "Metadata only — never reads secret values",
  },
  {
    name: "CI/CD Runs",
    lane: "ci_cd_run",
    category: "Delivery & Docs",
    readiness: "partial",
    summary:
      "Correlates GitHub Actions and GitLab CI workflow runs, jobs, artifacts, and deployments back to commits and images.",
  },
  {
    name: "PagerDuty",
    lane: "pagerduty",
    category: "Delivery & Docs",
    readiness: "partial",
    summary:
      "Collects incidents, timelines, change events, and live service/integration routing alongside Terraform routing.",
  },
  {
    name: "Jira Cloud",
    lane: "jira",
    category: "Delivery & Docs",
    readiness: "partial",
    summary:
      "Reads scoped work items, transitions, and remote links to enrich incident and change context.",
  },
  {
    name: "Confluence Docs",
    lane: "documentation",
    category: "Delivery & Docs",
    readiness: "partial",
    summary:
      "Collects spaces, page trees, sections, links, and claim candidates for documentation truth checks.",
  },
  {
    name: "Grafana Tempo",
    lane: "tempo",
    category: "Observability & Semantic",
    readiness: "implemented",
    summary: "Maps active trace services, tag metadata, and freshness across Tempo targets.",
    privacyNote: "Metadata only — span payloads stay in Tempo",
  },
  {
    name: "Grafana",
    lane: "grafana",
    category: "Observability & Semantic",
    readiness: "partial",
    summary:
      "Reads dashboard and alert-rule metadata as an embedded Go library to check service observability coverage.",
  },
  {
    name: "Prometheus & Mimir",
    lane: "prometheus_mimir",
    category: "Observability & Semantic",
    readiness: "partial",
    summary: "Inspects scrape targets and alerting rules to correlate monitored workloads.",
    privacyNote: "Metadata only — metric samples never leave",
  },
  {
    name: "Grafana Loki",
    lane: "loki",
    category: "Observability & Semantic",
    readiness: "partial",
    summary: "Reads label topology, series shape, and ruler rules to validate log coverage.",
    privacyNote: "Metadata only — log lines stay in Loki",
  },
  {
    name: "Semantic Extraction",
    lane: "semantic_extraction",
    category: "Observability & Semantic",
    readiness: "gated",
    summary:
      "Optional provider-portable semantic hints for code and documentation retrieval when explicitly enabled.",
  },
];
