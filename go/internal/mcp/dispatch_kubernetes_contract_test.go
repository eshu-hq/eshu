// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	"reflect"
	"testing"

	kubernetestools "github.com/eshu-hq/eshu/go/internal/mcp/kubernetes"
	"github.com/eshu-hq/eshu/go/internal/mcp/routecontract"
)

// kubernetesRouteTools lists every tool the child package owns.
var kubernetesRouteTools = []string{"list_kubernetes_correlations"}

// kubernetesQueryKeys is the ten-key query the listing must still send
// through dispatch.
var kubernetesQueryKeys = []string{
	"after_correlation_id",
	"cluster_id",
	"drift_kind",
	"image_ref",
	"limit",
	"namespace",
	"outcome",
	"scope_id",
	"source_digest",
	"workload_object_id",
}

func TestResolveRouteUsesExactKubernetesChildRequest(t *testing.T) {
	t.Parallel()

	argumentCases := []struct {
		name string
		args map[string]any
	}{
		{name: "nil", args: nil},
		{name: "empty", args: map[string]any{}},
		{name: "populated", args: map[string]any{
			"after_correlation_id": "kubernetes-correlation-1",
			"cluster_id":           "cluster-prod",
			"drift_kind":           "in_sync",
			"image_ref":            "registry.example.com/checkout@sha256:abc",
			"limit":                float64(25),
			"namespace":            "payments",
			"outcome":              "exact",
			"scope_id":             "kubernetes-live://cluster-prod",
			"source_digest":        "sha256:abc",
			"workload_object_id":   "deployment/payments/checkout",
		}},
		{name: "single anchor", args: map[string]any{
			"namespace": "payments",
		}},
		{name: "malformed", args: map[string]any{
			"cluster_id":         42,
			"namespace":          nil,
			"limit":              "25",
			"scope_id":           struct{}{},
			"outcome":            []string{"exact"},
			"workload_object_id": true,
		}},
	}

	for _, tool := range kubernetesRouteTools {
		for _, tt := range argumentCases {
			got, err := resolveRoute(tool, tt.args)
			if err != nil {
				t.Fatalf("resolveRoute(%s, %s) error = %v, want nil", tool, tt.name, err)
			}
			request, handled := kubernetestools.Route(tool, routecontract.Arguments(tt.args))
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

// TestKubernetesDispatchKeepsEveryQueryKey proves the ten filters survive the
// adapter boundary, where the handler actually reads them. The literal
// expectations here are deliberately independent of the child selector: the
// parity test above builds both of its sides from that selector, so it cannot
// notice a key the child itself dropped or misspelled.
func TestKubernetesDispatchKeepsEveryQueryKey(t *testing.T) {
	t.Parallel()

	args := map[string]any{
		"after_correlation_id": "kubernetes-correlation-1",
		"cluster_id":           "cluster-prod",
		"drift_kind":           "in_sync",
		"image_ref":            "registry.example.com/checkout@sha256:abc",
		"limit":                float64(25),
		"namespace":            "payments",
		"outcome":              "exact",
		"scope_id":             "kubernetes-live://cluster-prod",
		"source_digest":        "sha256:abc",
		"workload_object_id":   "deployment/payments/checkout",
	}
	want := map[string]string{
		"after_correlation_id": "kubernetes-correlation-1",
		"cluster_id":           "cluster-prod",
		"drift_kind":           "in_sync",
		"image_ref":            "registry.example.com/checkout@sha256:abc",
		"limit":                "25",
		"namespace":            "payments",
		"outcome":              "exact",
		"scope_id":             "kubernetes-live://cluster-prod",
		"source_digest":        "sha256:abc",
		"workload_object_id":   "deployment/payments/checkout",
	}

	got, err := resolveRoute("list_kubernetes_correlations", args)
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	if got.method != "GET" {
		t.Errorf("method = %q, want GET", got.method)
	}
	if got.path != "/api/v0/kubernetes/correlations" {
		t.Errorf("path = %q, want the kubernetes correlations path", got.path)
	}
	if got.body != nil {
		t.Errorf("body = %#v, want nil", got.body)
	}
	if n, wantN := len(got.query), len(kubernetesQueryKeys); n != wantN {
		t.Fatalf("query carries %d keys (%#v), want %d", n, got.query, wantN)
	}
	for _, key := range kubernetesQueryKeys {
		value, present := got.query[key]
		if !present {
			t.Errorf("dispatch dropped %q entirely", key)
			continue
		}
		if value != want[key] {
			t.Errorf("query[%s] = %q, want %q", key, value, want[key])
		}
	}
	for _, key := range []string{"offset", "group_by", "cursor", "repository_id", "workload_id"} {
		if value, present := got.query[key]; present {
			t.Errorf("query carries %q = %q, want the key absent", key, value)
		}
	}

	// The limit default reaches the handler unchanged when the caller omits
	// it, and every unset filter is still sent as an explicit empty string.
	bare, err := resolveRoute("list_kubernetes_correlations", map[string]any{
		"cluster_id": "cluster-prod",
	})
	if err != nil {
		t.Fatalf("resolveRoute(single anchor) error = %v, want nil", err)
	}
	if value := bare.query["limit"]; value != "50" {
		t.Errorf("absent limit -> %q, want the default 50", value)
	}
	for _, key := range kubernetesQueryKeys {
		if key == "limit" || key == "cluster_id" {
			continue
		}
		if value, present := bare.query[key]; !present || value != "" {
			t.Errorf("absent %s -> (%q, %v), want an explicit empty string", key, value, present)
		}
	}
}

// TestRepositoryRouteStillOwnsItsArmsAfterKubernetes proves the tenth
// delegation added in front of the repository switch claims only this family
// and leaves every neighbouring arm — including the sibling correlation
// listings that share the "list_" prefix and "_correlations" suffix — answered
// as before.
func TestRepositoryRouteStillOwnsItsArmsAfterKubernetes(t *testing.T) {
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
		"list_advisory_evidence",
		"get_vulnerability_scanner_read_contract",
		"list_sbom_attestation_attachments",
		"count_sbom_attestation_attachments",
		"get_sbom_attestation_attachment_inventory",
		"get_repo_story",
		"get_repository_coverage",
		"get_repository_freshness",
		"list_package_registry_packages",
		"list_ci_cd_run_correlations",
		"list_codeowners_ownership",
		"list_secrets_iam_posture_gaps",
		"list_observability_coverage_correlations",
		"list_container_image_identities",
		"list_supply_chain_impact_findings",
		"list_security_alert_reconciliations",
		"list_admission_decisions",
	} {
		if _, handled := kubernetesCorrelationsRoute(tool, map[string]any{}); handled {
			t.Errorf("kubernetesCorrelationsRoute(%s) handled = true, want false", tool)
		}
		got, ok, err := repositoryRoute(tool, map[string]any{})
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

// TestKubernetesRouteRejectsNonFamilyTools mutation-proves the child selector
// through the adapter: the owned name is claimed, and near-miss names are not.
func TestKubernetesRouteRejectsNonFamilyTools(t *testing.T) {
	t.Parallel()

	for _, tool := range kubernetesRouteTools {
		if _, handled := kubernetesCorrelationsRoute(tool, map[string]any{}); !handled {
			t.Errorf("kubernetesCorrelationsRoute(%s) handled = false, want true", tool)
		}
	}
	for _, tool := range []string{
		"", "list_kubernetes",
		"list_kubernetes_correlation",
		"list_kubernetes_correlations_extra",
		"count_kubernetes_correlations",
		"get_kubernetes_correlations",
		"get_kubernetes_correlation_inventory",
		"kubernetes_correlations",
		"LIST_KUBERNETES_CORRELATIONS",
	} {
		if _, handled := kubernetesCorrelationsRoute(tool, map[string]any{}); handled {
			t.Errorf("kubernetesCorrelationsRoute(%q) handled = true, want false", tool)
		}
	}
}
