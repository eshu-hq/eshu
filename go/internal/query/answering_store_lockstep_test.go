// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/capabilitycatalog"
)

// answeringStoreSpecsDir resolves the repository specs/ directory from this
// test file's location so the lockstep test reads the real capability matrix.
func answeringStoreSpecsDir(t *testing.T) string {
	t.Helper()
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(filename), "..", "..", "..", "specs"))
}

// TestCapabilityAnsweringStoresDeclaredForAllCapabilities is the AC1 gate:
// every capability in the matrix must declare the store (or stores) that
// answers its reads, and every declared store must be in the closed
// vocabulary. An empty declaration fails here; -mode verify fails the same
// capability through FindingMissingAnsweringStore.
func TestCapabilityAnsweringStoresDeclaredForAllCapabilities(t *testing.T) {
	matrix, err := capabilitycatalog.LoadMatrix(answeringStoreSpecsDir(t))
	if err != nil {
		t.Fatalf("LoadMatrix: %v", err)
	}
	if len(matrix.Capabilities) == 0 {
		t.Fatal("matrix has no capabilities")
	}
	for _, capability := range matrix.Capabilities {
		if len(capability.AnsweringStores) == 0 {
			t.Errorf("capability %q declares no answering store", capability.Capability)
		}
		seen := map[string]struct{}{}
		for _, store := range capability.AnsweringStores {
			if _, dup := seen[store]; dup {
				t.Errorf("capability %q declares store %q twice", capability.Capability, store)
			}
			seen[store] = struct{}{}
			if !capabilitycatalog.ValidAnsweringStore(store) {
				t.Errorf("capability %q declares unknown answering store %q", capability.Capability, store)
			}
		}
	}
}

// readinessReadTables are the tables the impact readiness store reads for the
// readiness block of the list and explain responses; the store audit's readiness
// probes prove each one (go/internal/query/store/audit).
var readinessReadTables = []string{
	"read_table:collector_instances", "read_table:container_image_identity_supports", "read_table:package_manifest_consumption_key_backfill_markers", "read_table:package_manifest_consumption_key_dirty_scopes", "read_table:package_manifest_consumption_keys", "read_table:package_registry_identity_keys", "read_table:vulnerability_source_state_packages", "read_table:vulnerability_source_states", "read_table:workflow_runs",
}

// answeringStoreProbe pins one capability's declared stores against the store
// its handler actually reads. The evidence column names the handler file that
// proves the read; mutate a probe's declared stores in the matrix and this
// test fails.
var answeringStoreProbes = []struct {
	capability string
	want       []string
	evidence   string
}{
	// Reducer-result families read fact_records through reducer-owned kinds.
	// Impact findings list is mixed since #10880 slice 2: the winners
	// read serves payloads from the read table, while the legacy gate-off
	// path and the runtime workload filter still read reducer facts.
	{
		capability: "supply_chain.impact_findings.list",
		want:       []string{"read_table:supply_chain_impact_canonical_winners", "fact_records_reducer"},
		evidence:   "go/internal/query/supply/chain/impact/findings_queries.go: winners payload read (#10880) + legacy FindingFactKind path + runtime_filter_sql.go reducer_workload_identity",
	},
	{
		capability: "supply_chain.impact_findings.aggregate",
		want:       []string{"read_table:supply_chain_impact_canonical_winners", "fact_records_reducer"},
		evidence:   "go/internal/query/supply/chain/impact/aggregates_winners_queries.go: winners count/bucket/inventory read (#10880 slice 2b) + legacy aggregates_queries.go canonical-facts CTE + runtime_filter_sql.go reducer_workload_identity",
	},
	{
		capability: "supply_chain.security_alert_reconciliations.list",
		want:       []string{"fact_records_reducer"},
		evidence:   "go/internal/query/supply/chain/alerts/store.go: factKind=reducer_security_alert_reconciliation",
	},
	{
		capability: "supply_chain.sbom_attestation_attachments.list",
		want:       []string{"fact_records_reducer"},
		evidence:   "go/internal/query/sbom_attestation_attachments.go fact_kind=reducer_sbom_attestation_attachment",
	},
	{
		capability: "ci_cd.run_correlations.list",
		want:       []string{"fact_records_reducer"},
		evidence:   "go/internal/query/cicd/run_correlations.go: cicdRunCorrelationFactKind=reducer_ci_cd_run_correlation",
	},
	{
		capability: "kubernetes.correlations.list",
		want:       []string{"fact_records_reducer"},
		evidence:   "go/internal/query/kubernetes/correlations.go: kubernetesCorrelationFactKind=reducer_kubernetes_correlation",
	},
	{
		capability: "secrets_iam.posture_gaps.list",
		want:       []string{"fact_records_reducer"},
		evidence:   "go/internal/query/secrets/posture_stores.go: reducer_secrets_iam_* fact kinds",
	},
	{
		capability: "cloud_runtime_drift.readback.list",
		want:       []string{"fact_records_reducer"},
		evidence:   "go/internal/query/cloud_runtime_drift_store.go: reducer_multi_cloud/aws_cloud_runtime_drift_finding rows",
	},
	{
		capability: "iac_management.get_status",
		want:       []string{"fact_records_reducer"},
		evidence:   "go/internal/query/iac/management.go: ManagementStore reads reducer-materialized cloud management findings",
	},
	// Collector-fact families read fact_records through collector-owned kinds.
	{
		capability: "supply_chain.advisory_catalog.list",
		want:       []string{"fact_records_collector"},
		evidence:   "go/internal/query/supply/chain/advisory/catalog_sql.go: fact_kind=vulnerability.cve/affected_package/known_exploited",
	},
	{
		capability: "code_flow.reaching_def",
		want:       []string{"fact_records_collector"},
		evidence:   "go/internal/query/codequery/flow.go: bounded exact_parser_fact read over parser dataflow facts",
	},
	{
		capability: "incident.context.read",
		want:       []string{"fact_records_collector"},
		evidence:   "go/internal/query/incident/sql/context.go: incident.record/lifecycle_event and change.record facts",
	},
	// Read-model tables moved off fact_records are named explicitly.
	{
		capability: "supply_chain.container_image_identities.list",
		want:       []string{"read_table:container_image_identity_supports"},
		evidence:   "go/internal/query/container_image_identity_v3_sql.go: FROM container_image_identity_current_supports (092 support store view)",
	},
	{
		capability: "code_quality.dead_code",
		want:       []string{"read_table:code_reachability_rows", "content_search"},
		evidence:   "go/internal/query/codequery/deadcode/cross_repo_filter.go: FROM code_reachability_rows; content_reader_dead_code_* content fallback",
	},
	{
		capability: "security.hardcoded_secrets",
		want:       []string{"read_table:content_file_secret_lines", "content_search"},
		evidence:   "go/internal/query/content_reader_security_secrets.go: FROM content_file_secret_lines with legacy content_files scan",
	},
	{
		capability: "iac_quality.dead_iac",
		want:       []string{"read_table:iac_reachability_rows", "content_search"},
		evidence:   "go/internal/storage/postgres/iac/reachability.go: FROM iac_reachability_rows; iac/handler.go dead-IaC repository file reads",
	},
	// Graph families traverse relationships with depth unknown at write time.
	{
		capability: "call_graph.transitive_callers",
		want:       []string{"graph"},
		evidence:   "go/internal/query/codequery/chain Cypher call-chain traversal",
	},
	{
		capability: "codeowners.ownership.list",
		want:       []string{"graph"},
		evidence:   "go/internal/query/codeowners/ownership_cypher.go: MATCH DECLARES_CODEOWNER",
	},
	{
		capability: "platform_impact.blast_radius",
		want:       []string{"graph"},
		evidence:   "go/internal/query/impact/blast_radius.go: anchored DEPENDS_ON traversals",
	},
	{
		capability: "relationship_evidence.drilldown",
		want:       []string{"graph"},
		evidence:   "go/internal/query/relationships_catalog.go: per-family MATCH builders",
	},
	// Content families read the content store or search index.
	{
		capability: "code_search.exact_symbol",
		want:       []string{"content_search"},
		evidence:   "go/internal/query/content_reader_entity_search.go: FROM content_entities",
	},
	{
		capability: "evidence_citation.packet",
		want:       []string{"content_search"},
		evidence:   "go/internal/query/evidence/citation.go: postgres_content_store handles, no graph traversal",
	},
	// Mixed families name every store that answers the read.
	{
		capability: "supply_chain.advisory_evidence.list",
		want:       []string{"fact_records_collector", "fact_records_reducer"},
		evidence:   "go/internal/query/supply/chain/advisory/evidence_sql.go: vulnerability.* collector facts + repo_probe/impact_candidates over reducer_supply_chain_impact_finding",
	},
	{
		capability: "supply_chain.impact_explanation.read",
		want:       append([]string{"fact_records_reducer", "fact_records_collector"}, readinessReadTables...),
		evidence:   "go/internal/query/supply/chain/impact/explain_postgres.go FindingFactKind + vulnerability/collector evidence reads; readiness_postgres.go readiness block",
	},
	{
		capability: "supply_chain.impact_findings.list",
		want: append([]string{
			"read_table:supply_chain_impact_canonical_winners", "fact_records_reducer", "fact_records_collector",
		}, readinessReadTables...),
		evidence: "go/internal/query/supply/chain/impact/findings_queries.go winners read + readiness_postgres.go readiness block (collector facts and the readiness tables)",
	},
	{
		capability: "service_catalog.correlations.list",
		want:       []string{"fact_records_reducer", "fact_records_collector"},
		evidence:   "go/internal/query/service/catalog_correlations.go: reducer_service_catalog_correlation + service_catalog.* collector evidence",
	},
	{
		capability: "symbol_graph.inheritance",
		want:       []string{"graph", "content_search"},
		evidence:   "go/internal/query/language/handler.go: graph_first_content_backed + content_backed entity-type paths",
	},
	// Status families answer from snapshots, static policy, or caller data.
	{
		capability: "operations.status",
		want:       []string{"status_fixed"},
		evidence:   "go/internal/query/status_operations.go: persisted status snapshot report",
	},
	{
		capability: "audit.events.list",
		want:       []string{"read_table:governance_audit_events"},
		evidence:   "go/internal/storage/postgres/migrations/006b_governance_audit_events.sql: governance_audit_events table",
	},
	{
		capability: "operator.work_items.list",
		want:       []string{"read_table:fact_work_items"},
		evidence:   "go/internal/query/admin/store/queries.go: bounded select over fact_work_items",
	},
}

// TestCapabilityAnsweringStoreLockstep asserts every probe's declared stores
// cover the stores its handler evidence requires: drop a required store from
// the matrix and this test fails.
func TestCapabilityAnsweringStoreLockstep(t *testing.T) {
	matrix, err := capabilitycatalog.LoadMatrix(answeringStoreSpecsDir(t))
	if err != nil {
		t.Fatalf("LoadMatrix: %v", err)
	}
	byID := make(map[string][]string, len(matrix.Capabilities))
	for _, capability := range matrix.Capabilities {
		byID[capability.Capability] = capability.AnsweringStores
	}
	for _, probe := range answeringStoreProbes {
		declared, ok := byID[probe.capability]
		if !ok {
			t.Errorf("probe capability %q missing from matrix (evidence: %s)", probe.capability, probe.evidence)
			continue
		}
		for _, want := range probe.want {
			if !slices.Contains(declared, want) {
				t.Errorf("capability %q declares %v, want store %q (evidence: %s)",
					probe.capability, declared, want, probe.evidence)
			}
		}
	}
}

// TestBuildFlagsMissingAnsweringStore is the seeded-violation pair for the
// verify gate: a capability with no declared store must produce
// FindingMissingAnsweringStore, and a fully declared matrix must not.
func TestBuildFlagsMissingAnsweringStore(t *testing.T) {
	specsDir := answeringStoreSpecsDir(t)
	matrix, err := capabilitycatalog.LoadMatrix(specsDir)
	if err != nil {
		t.Fatalf("LoadMatrix: %v", err)
	}
	overlay, err := capabilitycatalog.LoadOverlay(specsDir + "/" + capabilitycatalog.OverlayFileName)
	if err != nil {
		t.Fatalf("LoadOverlay: %v", err)
	}
	signals := capabilitycatalog.Signals{MCPTools: map[string]bool{}}
	_, findings := capabilitycatalog.Build(matrix, overlay, signals)
	for _, finding := range findings {
		if finding.Kind == capabilitycatalog.FindingMissingAnsweringStore {
			t.Errorf("fully declared matrix still reports %v for %q: %s",
				finding.Kind, finding.Subject, finding.Detail)
		}
	}

	stripped := matrix
	stripped.Capabilities = slices.Clone(matrix.Capabilities)
	stripped.Capabilities[0].AnsweringStores = nil
	_, strippedFindings := capabilitycatalog.Build(stripped, overlay, signals)
	found := false
	for _, finding := range strippedFindings {
		if finding.Kind == capabilitycatalog.FindingMissingAnsweringStore &&
			finding.Subject == stripped.Capabilities[0].Capability {
			found = true
		}
	}
	if !found {
		t.Errorf("stripping answering stores from %q produced no FindingMissingAnsweringStore (want seeded violation to fail)",
			stripped.Capabilities[0].Capability)
	}
}

// TestLoadMatrixRejectsUnknownAnsweringStore seeds a matrix row with a store
// outside the closed vocabulary and requires a hard load error.
func TestLoadMatrixRejectsUnknownAnsweringStore(t *testing.T) {
	dir := t.TempDir()
	main := `version: v1
profiles: [local_lightweight]
capabilities:
  - capability: probe.seeded_violation
    tools: [probe_tool]
    answering_stores: [graph, not_a_store]
    profiles:
      local_lightweight: {status: supported, max_truth_level: exact, required_runtime: local_host, verification: [{go_test: ./internal/query}]}
`
	if err := os.WriteFile(filepath.Join(dir, capabilitycatalog.MatrixFileName), []byte(main), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := capabilitycatalog.LoadMatrix(dir); err == nil {
		t.Error("LoadMatrix accepted unknown answering store not_a_store, want hard error")
	}
}
