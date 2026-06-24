// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package status

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/scope"
)

// CollectorCatalogEntry declares the readiness expectations for one collector
// family. The catalog is the deterministic spine of the promotion proof report:
// every entry produces at least one proof row even when no instance is
// configured, so a reviewer always sees the full fleet and unconfigured lanes
// are explicit rather than silently absent.
type CollectorCatalogEntry struct {
	// CollectorKind is the durable scope.CollectorKind string for the family.
	CollectorKind string
	// DisplayName is the operator-facing label for the family.
	DisplayName string
	// ClaimDriven reports whether the family runs under workflow claims. A
	// claim-driven family with claims disabled is gated; a non-claim-driven
	// family in direct mode is operating normally.
	ClaimDriven bool
	// SourceScope is the static scope kind the family collects against. It
	// carries no instance identity and is safe to share.
	SourceScope string
	// TelemetryHandles lists the stable metric and span names an operator can
	// use to diagnose the family at runtime. Empty means the shared collector
	// handles apply.
	TelemetryHandles []string
}

// sharedCollectorTelemetryHandles are emitted by every collector and are safe to
// surface in a shareable report. Provider-specific handles are documented per
// package; the promotion proof reports the shared contract that always applies.
func sharedCollectorTelemetryHandles() []string {
	return []string{
		"collector.observe",
		"collector.stream",
		"fact.emit",
		"eshu_dp_facts_emitted_total",
		"eshu_dp_facts_committed_total",
	}
}

// collectorCatalogMetadata carries the static, drift-resistant metadata for a
// known collector kind. Kinds absent from this map still appear in the default
// catalog with synthesized defaults so a newly added collector is never hidden.
type collectorCatalogMetadata struct {
	displayName string
	claimDriven bool
	sourceScope string
}

func defaultCatalogMetadata() map[string]collectorCatalogMetadata {
	return map[string]collectorCatalogMetadata{
		string(scope.CollectorGit):                       {"Git Repository", false, "repository"},
		string(scope.CollectorAWS):                       {"AWS Cloud", true, "account"},
		string(scope.CollectorAzure):                     {"Azure Cloud", true, "account"},
		string(scope.CollectorGCP):                       {"GCP Cloud", true, "account"},
		string(scope.CollectorTerraformState):            {"Terraform State", true, "state_snapshot"},
		string(scope.CollectorWebhook):                   {"Webhook Trigger", false, "webhook"},
		string(scope.CollectorDocumentation):             {"Documentation", false, "documentation_source"},
		string(scope.CollectorOCIRegistry):               {"OCI Registry", true, "container_registry_repository"},
		string(scope.CollectorPackageRegistry):           {"Package Registry", true, "package_registry"},
		string(scope.CollectorVulnerabilityIntelligence): {"Vulnerability Intelligence", true, "vulnerability_intelligence"},
		string(scope.CollectorSBOMAttestation):           {"SBOM Attestation", true, "sbom_attestation"},
		string(scope.CollectorSecurityAlert):             {"Security Alert", true, "security_alert"},
		string(scope.CollectorCICDRun):                   {"CI/CD Run", true, "ci_cd_run"},
		string(scope.CollectorPagerDuty):                 {"PagerDuty", true, "pagerduty_account"},
		string(scope.CollectorJira):                      {"Jira", true, "jira_site"},
		string(scope.CollectorScannerWorker):             {"Scanner Worker", true, "scanner_worker"},
		string(scope.CollectorSemanticExtraction):        {"Semantic Extraction", true, "semantic_extraction"},
		string(scope.CollectorKubernetesLive):            {"Kubernetes Live", true, "cluster"},
		string(scope.CollectorVaultLive):                 {"Vault Live", true, "vault_cluster"},
		string(scope.CollectorPrometheusMimir):           {"Prometheus / Mimir", true, "metric_source"},
		string(scope.CollectorTempo):                     {"Tempo", true, "trace_source"},
		string(scope.CollectorGrafana):                   {"Grafana", true, "grafana_instance"},
		string(scope.CollectorLoki):                      {"Loki", true, "log_source"},
	}
}

// KnownCollectorKinds returns the canonical collector kind strings the readiness
// report enumerates. It mirrors scope.AllCollectorKinds so the catalog and any
// readiness consumer never drift from the platform's real collector fleet.
func KnownCollectorKinds() []string {
	kinds := scope.AllCollectorKinds()
	out := make([]string, 0, len(kinds))
	for _, kind := range kinds {
		out = append(out, string(kind))
	}
	return out
}

// DefaultCollectorCatalog returns the readiness catalog for the full collector
// fleet, in scope.AllCollectorKinds order. Kinds without declared metadata get a
// synthesized display name and claim-driven default so adding a collector to
// scope.AllCollectorKinds automatically surfaces a readiness lane.
func DefaultCollectorCatalog() []CollectorCatalogEntry {
	metadata := defaultCatalogMetadata()
	kinds := scope.AllCollectorKinds()
	entries := make([]CollectorCatalogEntry, 0, len(kinds))
	for _, kind := range kinds {
		kindStr := string(kind)
		meta, ok := metadata[kindStr]
		if !ok {
			meta = collectorCatalogMetadata{
				displayName: synthesizeDisplayName(kindStr),
				claimDriven: true,
				sourceScope: kindStr,
			}
		}
		entries = append(entries, CollectorCatalogEntry{
			CollectorKind:    kindStr,
			DisplayName:      meta.displayName,
			ClaimDriven:      meta.claimDriven,
			SourceScope:      meta.sourceScope,
			TelemetryHandles: sharedCollectorTelemetryHandles(),
		})
	}
	return entries
}

// presentCollectorCatalog returns the catalog restricted to collector kinds that
// have runtime evidence or a registered instance in the report. The global
// status surface uses this focused catalog so it reports only collectors that
// are actually present; the full-fleet enumeration (including no-instance and
// unsupported lanes) belongs to the dedicated collector-readiness read model.
func presentCollectorCatalog(report Report) []CollectorCatalogEntry {
	present := map[string]bool{}
	for _, row := range CollectorRuntimeStatuses(report) {
		present[row.CollectorKind] = true
	}
	// Return a non-nil empty catalog when nothing is present so callers get an
	// empty proof set instead of the full default fleet.
	entries := make([]CollectorCatalogEntry, 0, len(present))
	for _, entry := range DefaultCollectorCatalog() {
		if present[entry.CollectorKind] {
			entries = append(entries, entry)
		}
	}
	return entries
}

// synthesizeDisplayName turns a snake_case collector kind into a readable label
// for collectors that lack explicit catalog metadata.
func synthesizeDisplayName(kind string) string {
	parts := strings.Split(kind, "_")
	for i, part := range parts {
		if part == "" {
			continue
		}
		parts[i] = strings.ToUpper(part[:1]) + part[1:]
	}
	return strings.Join(parts, " ")
}
