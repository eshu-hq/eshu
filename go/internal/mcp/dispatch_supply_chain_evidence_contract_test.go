// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	"reflect"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/mcp/routecontract"
	supplychainevidencetools "github.com/eshu-hq/eshu/go/internal/mcp/supplychainevidence"
)

// supplyChainEvidenceRouteTools lists every tool the child package owns, in
// the order the root repository switch used to answer them.
var supplyChainEvidenceRouteTools = []string{
	"list_advisory_evidence",
	"get_vulnerability_scanner_read_contract",
	"list_sbom_attestation_attachments",
	"count_sbom_attestation_attachments",
	"get_sbom_attestation_attachment_inventory",
}

func TestResolveRouteUsesExactSupplyChainEvidenceChildRequest(t *testing.T) {
	t.Parallel()

	argumentCases := []struct {
		name string
		args map[string]any
	}{
		{name: "nil", args: nil},
		{name: "empty", args: map[string]any{}},
		{name: "populated", args: map[string]any{
			"advisory_id":         "GHSA-aaaa-bbbb-cccc",
			"after_advisory_key":  "CVE-2026-0001",
			"after_attachment_id": "attachment-cursor",
			"artifact_kind":       "sbom",
			"attachment_status":   "verified",
			"cve_id":              "CVE-2026-0002",
			"digest":              "sha256:digest-1",
			"document_digest":     "sha256:doc-digest-1",
			"document_id":         "doc-1",
			"group_by":            "artifact_kind",
			"limit":               float64(25),
			"offset":              float64(5),
			"package_id":          "pkg:npm/example",
			"repository_id":       "repo://example/api",
			"route":               "impact_findings",
			"service_id":          "service:payments-api",
			"source":              "osv",
			"subject_digest":      "sha256:subject-1",
			"workload_id":         "workload:payments-api",
		}},
		{name: "malformed", args: map[string]any{
			"advisory_id": 42,
			"limit":       "25",
			"group_by":    struct{}{},
			"source":      []string{"osv"},
			"route":       nil,
		}},
	}

	for _, tool := range supplyChainEvidenceRouteTools {
		for _, tt := range argumentCases {
			got, err := resolveRoute(tool, tt.args)
			if err != nil {
				t.Fatalf("resolveRoute(%s, %s) error = %v, want nil", tool, tt.name, err)
			}
			request, handled := supplychainevidencetools.Route(tool, routecontract.Arguments(tt.args))
			if !handled {
				t.Fatalf("child Route(%s) handled = false, want true", tool)
			}
			want := &route{
				method: request.Method,
				path:   request.Path,
				body:   request.Body,
				query:  request.Query,
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("resolveRoute(%s, %s) = %#v, want child request %#v", tool, tt.name, got, want)
			}
		}
	}
}

// TestRepositoryRouteStillOwnsItsArmsAfterSupplyChainEvidence proves the
// delegation added in front of the repository switch claims only the
// supply-chain evidence family and leaves every neighbouring arm answered as
// before, including the ones sharing the "count_", "get_", and "list_"
// prefixes and the sibling supplychainimpact family.
func TestRepositoryRouteStillOwnsItsArmsAfterSupplyChainEvidence(t *testing.T) {
	t.Parallel()

	for _, tool := range []string{
		"list_indexed_repositories",
		"count_repositories_by_language",
		"list_repositories_by_language",
		"get_repository_language_inventory",
		"get_repository_stats",
		"get_repo_context",
		"get_relationship_evidence",
		"list_service_catalog_correlations",
		"get_repo_story",
		"get_repo_summary",
		"get_repository_coverage",
		"get_repository_freshness",
		"list_admission_decisions",
		"list_package_registry_packages",
		"count_package_registry_packages",
		"list_ci_cd_run_correlations",
		"list_codeowners_ownership",
		"list_kubernetes_correlations",
		"list_observability_coverage_correlations",
		"list_container_image_identities",
		"list_secrets_iam_identity_trust_chains",
		"count_secrets_iam_posture",
		"list_supply_chain_impact_findings",
		"count_supply_chain_impact_findings",
		"get_supply_chain_impact_inventory",
		"explain_supply_chain_impact",
		"list_security_alert_reconciliations",
	} {
		if _, handled := supplyChainEvidenceRoute(tool, map[string]any{}); handled {
			t.Errorf("supplyChainEvidenceRoute(%s) handled = true, want false", tool)
		}
		got, ok, err := repositoryRoute(tool, map[string]any{"repo_id": "r"})
		if err != nil {
			t.Errorf("repositoryRoute(%s) error = %v, want nil", tool, err)
			continue
		}
		if !ok || got == nil {
			t.Errorf("repositoryRoute(%s) ok = %v, route = %v, want a route", tool, ok, got)
		}
	}

	// An unknown tool still falls through the repository switch untouched.
	if got, ok, err := repositoryRoute("not_a_tool", map[string]any{}); ok || got != nil || err != nil {
		t.Fatalf("repositoryRoute(not_a_tool) = (%v, %v, %v), want (nil, false, nil)", got, ok, err)
	}
	// resolveRoute still reports an unknown tool as an error, not a nil route.
	if _, err := resolveRoute("not_a_tool", map[string]any{}); err == nil {
		t.Fatal("resolveRoute(not_a_tool) error = nil, want an unknown-tool error")
	}
}

// TestSupplyChainEvidenceRouteRejectsNonFamilyTools mutation-proves the child
// selector through the adapter: every owned name is claimed, and near-miss
// names are not.
func TestSupplyChainEvidenceRouteRejectsNonFamilyTools(t *testing.T) {
	t.Parallel()

	for _, tool := range supplyChainEvidenceRouteTools {
		if _, handled := supplyChainEvidenceRoute(tool, map[string]any{}); !handled {
			t.Errorf("supplyChainEvidenceRoute(%s) handled = false, want true", tool)
		}
	}
	for _, tool := range []string{
		"", "list_advisory_evidences", "list_advisory_evidenc",
		"get_vulnerability_scanner_read_contracts",
		"list_sbom_attestation_attachment",
		"count_sbom_attestation_attachment",
		"get_sbom_attestation_attachment_inventories",
		"LIST_ADVISORY_EVIDENCE", "list_supply_chain_impact_findings",
		"list_service_catalog_correlations",
	} {
		if _, handled := supplyChainEvidenceRoute(tool, map[string]any{}); handled {
			t.Errorf("supplyChainEvidenceRoute(%q) handled = true, want false", tool)
		}
	}
}

// TestSupplyChainEvidenceAttachmentCountStaysUnpagedThroughDispatch carries
// the family's count/listing asymmetry across the adapter boundary, where the
// handler actually sees it: count_sbom_attestation_attachments must never
// carry limit, offset, or group_by, even when every sibling key is offered.
func TestSupplyChainEvidenceAttachmentCountStaysUnpagedThroughDispatch(t *testing.T) {
	t.Parallel()

	got, err := resolveRoute("count_sbom_attestation_attachments", map[string]any{
		"subject_digest": "sha256:subject-1", "limit": float64(25), "offset": float64(5),
		"group_by": "artifact_kind", "repository_id": "repo://example/api",
	})
	if err != nil {
		t.Fatalf("resolveRoute error = %v, want nil", err)
	}
	if got.path != "/api/v0/supply-chain/sbom-attestations/attachments/count" {
		t.Errorf("path = %q, want the attachments count path", got.path)
	}
	for _, key := range []string{"limit", "offset", "group_by"} {
		if value, present := got.query[key]; present {
			t.Errorf("query carries %q = %q, want the key absent", key, value)
		}
	}
}
