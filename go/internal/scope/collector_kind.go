// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package scope

// CollectorKind identifies the collector family that owns the scope.
type CollectorKind string

const (
	// CollectorGit represents the Git repository collector.
	CollectorGit CollectorKind = "git"
	// CollectorAWS represents the cloud inventory collector.
	CollectorAWS CollectorKind = "aws"
	// CollectorAzure represents the read-only Azure cloud inventory collector.
	// It observes Azure control-plane metadata through Azure Resource Graph for
	// one bounded tenant, subscription, or management-group shard and emits
	// provider-specific source facts; it never mutates Azure state.
	CollectorAzure CollectorKind = "azure"
	// CollectorGCP represents the read-only Google Cloud Asset Inventory
	// collector. It observes control-plane resource metadata through Cloud Asset
	// Inventory and emits redacted source facts; it never reads data-plane content.
	CollectorGCP CollectorKind = "gcp"
	// CollectorTerraformState represents the Terraform state collector.
	CollectorTerraformState CollectorKind = "terraform_state"
	// CollectorWebhook represents the event/webhook collector.
	CollectorWebhook CollectorKind = "webhook"
	// CollectorDocumentation represents the documentation source collector.
	CollectorDocumentation CollectorKind = "documentation"
	// CollectorOCIRegistry represents the OCI registry collector.
	CollectorOCIRegistry CollectorKind = "oci_registry"
	// CollectorPackageRegistry represents the package registry collector.
	CollectorPackageRegistry CollectorKind = "package_registry"
	// CollectorVulnerabilityIntelligence represents the vulnerability source collector.
	CollectorVulnerabilityIntelligence CollectorKind = "vulnerability_intelligence"
	// CollectorSBOMAttestation represents the hosted SBOM and attestation collector.
	CollectorSBOMAttestation CollectorKind = "sbom_attestation"
	// CollectorSecurityAlert represents hosted provider security-alert collectors.
	CollectorSecurityAlert CollectorKind = "security_alert"
	// CollectorCICDRun represents hosted CI/CD workflow-run collectors.
	CollectorCICDRun CollectorKind = "ci_cd_run"
	// CollectorPagerDuty represents hosted PagerDuty incident evidence collectors.
	CollectorPagerDuty CollectorKind = "pagerduty"
	// CollectorJira represents Jira work-item evidence collectors.
	CollectorJira CollectorKind = "jira"
	// CollectorScannerWorker represents isolated security analyzer workers.
	CollectorScannerWorker CollectorKind = "scanner_worker"
	// CollectorSemanticExtraction represents optional semantic extraction jobs
	// that emit model-assisted observations and hints as provenance only.
	CollectorSemanticExtraction CollectorKind = "semantic_extraction"
	// CollectorKubernetesLive represents the read-only Kubernetes live cluster
	// collector. It observes a configured cluster's API server with read-only
	// credentials and emits typed source facts; it never mutates the cluster.
	CollectorKubernetesLive CollectorKind = "kubernetes_live"
	// CollectorVaultLive represents the read-only Vault metadata collector for
	// the secrets/IAM posture lane. It observes a configured Vault cluster's
	// metadata endpoints with a read-only token and emits redacted source facts;
	// it never reads a secret value.
	CollectorVaultLive CollectorKind = "vault_live"
	// CollectorPrometheusMimir represents the read-only Prometheus/Grafana Mimir
	// metric metadata collector. It polls configured metric API targets for
	// bounded target and rule metadata and emits redacted source facts.
	CollectorPrometheusMimir CollectorKind = "prometheus_mimir"
	// CollectorTempo represents the read-only Grafana Tempo observability
	// collector. It polls a configured Tempo query API for bounded trace-signal
	// metadata and emits redacted source facts; it never reads trace payloads.
	CollectorTempo CollectorKind = "tempo"
	// CollectorGrafana represents the read-only Grafana observability metadata
	// collector. It observes a configured Grafana API target with a read-only
	// token and emits bounded folder, dashboard, datasource, and alert-rule
	// source facts; it never mutates the Grafana instance.
	CollectorGrafana CollectorKind = "grafana"
	// CollectorLoki represents the read-only Grafana Loki observability
	// collector. It polls a configured Loki API target with read-only
	// credentials and emits bounded, redacted log-signal metadata facts; it
	// never reads log payloads.
	CollectorLoki CollectorKind = "loki"
)

// AllCollectorKinds returns every collector kind known to the platform in a
// stable, deterministic order. It is the single source of truth for tooling
// that must enumerate the full collector fleet (readiness reports, promotion
// proofs, fleet hygiene checks) so adding a collector means updating one list.
func AllCollectorKinds() []CollectorKind {
	return []CollectorKind{
		CollectorGit,
		CollectorAWS,
		CollectorAzure,
		CollectorGCP,
		CollectorTerraformState,
		CollectorWebhook,
		CollectorDocumentation,
		CollectorOCIRegistry,
		CollectorPackageRegistry,
		CollectorVulnerabilityIntelligence,
		CollectorSBOMAttestation,
		CollectorSecurityAlert,
		CollectorCICDRun,
		CollectorPagerDuty,
		CollectorJira,
		CollectorScannerWorker,
		CollectorSemanticExtraction,
		CollectorKubernetesLive,
		CollectorVaultLive,
		CollectorPrometheusMimir,
		CollectorTempo,
		CollectorGrafana,
		CollectorLoki,
	}
}
