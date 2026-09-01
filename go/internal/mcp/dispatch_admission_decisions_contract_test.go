// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	"reflect"
	"testing"

	admissiondecisionstools "github.com/eshu-hq/eshu/go/internal/mcp/admissiondecisions"
	"github.com/eshu-hq/eshu/go/internal/mcp/routecontract"
)

// admissionDecisionsRouteTools lists every tool the child package owns.
var admissionDecisionsRouteTools = []string{"list_admission_decisions"}

// admissionDecisionsQueryKeys is the eight-key query the listing must still
// send through dispatch.
var admissionDecisionsQueryKeys = []string{
	"anchor_id",
	"anchor_kind",
	"domain",
	"generation_id",
	"include_evidence",
	"limit",
	"scope_id",
	"state",
}

func TestResolveRouteUsesExactAdmissionDecisionsChildRequest(t *testing.T) {
	t.Parallel()

	argumentCases := []struct {
		name string
		args map[string]any
	}{
		{name: "nil", args: nil},
		{name: "empty", args: map[string]any{}},
		{name: "populated", args: map[string]any{
			"anchor_id":        "repo://team/api",
			"anchor_kind":      "repository",
			"domain":           "deployable_unit",
			"generation_id":    "generation-1",
			"include_evidence": true,
			"limit":            float64(25),
			"scope_id":         "git-repository-scope:team/api",
			"state":            "missing_evidence",
		}},
		{name: "required only", args: map[string]any{
			"domain":        "deployable_unit",
			"scope_id":      "scope-a",
			"generation_id": "generation-1",
		}},
		{name: "malformed", args: map[string]any{
			"anchor_kind":      42,
			"domain":           nil,
			"include_evidence": "true",
			"limit":            "25",
			"scope_id":         struct{}{},
			"state":            []string{"admitted"},
		}},
	}

	for _, tool := range admissionDecisionsRouteTools {
		for _, tt := range argumentCases {
			got, err := resolveRoute(tool, tt.args)
			if err != nil {
				t.Fatalf("resolveRoute(%s, %s) error = %v, want nil", tool, tt.name, err)
			}
			request, handled := admissiondecisionstools.Route(tool, routecontract.Arguments(tt.args))
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

// TestAdmissionDecisionsDispatchKeepsEveryQueryKey proves the eight filters
// survive the adapter boundary, where the handler actually reads them. The
// literal expectations here are deliberately independent of the child
// selector: the parity test above builds both of its sides from that selector,
// so it cannot notice a key the child itself dropped or misspelled.
func TestAdmissionDecisionsDispatchKeepsEveryQueryKey(t *testing.T) {
	t.Parallel()

	args := map[string]any{
		"anchor_id":        "repo://team/api",
		"anchor_kind":      "repository",
		"domain":           "deployable_unit",
		"generation_id":    "generation-1",
		"include_evidence": true,
		"limit":            float64(25),
		"scope_id":         "git-repository-scope:team/api",
		"state":            "missing_evidence",
	}
	want := map[string]string{
		"anchor_id":        "repo://team/api",
		"anchor_kind":      "repository",
		"domain":           "deployable_unit",
		"generation_id":    "generation-1",
		"include_evidence": "true",
		"limit":            "25",
		"scope_id":         "git-repository-scope:team/api",
		"state":            "missing_evidence",
	}

	got, err := resolveRoute("list_admission_decisions", args)
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	if got.method != "GET" {
		t.Errorf("method = %q, want GET", got.method)
	}
	if got.path != "/api/v0/evidence/admission-decisions" {
		t.Errorf("path = %q, want the admission-decisions path", got.path)
	}
	if got.body != nil {
		t.Errorf("body = %#v, want nil", got.body)
	}
	if n, wantN := len(got.query), len(admissionDecisionsQueryKeys); n != wantN {
		t.Fatalf("query carries %d keys (%#v), want %d", n, got.query, wantN)
	}
	for _, key := range admissionDecisionsQueryKeys {
		value, present := got.query[key]
		if !present {
			t.Errorf("dispatch dropped %q entirely", key)
			continue
		}
		if value != want[key] {
			t.Errorf("query[%s] = %q, want %q", key, value, want[key])
		}
	}
	for _, key := range []string{"offset", "group_by", "cursor", "repository_id"} {
		if value, present := got.query[key]; present {
			t.Errorf("query carries %q = %q, want the key absent", key, value)
		}
	}

	// The limit default and the explicit include_evidence=false reach the
	// handler unchanged when the caller omits both.
	bare, err := resolveRoute("list_admission_decisions", map[string]any{
		"domain":        "deployable_unit",
		"scope_id":      "scope-a",
		"generation_id": "generation-1",
	})
	if err != nil {
		t.Fatalf("resolveRoute(required only) error = %v, want nil", err)
	}
	if value := bare.query["limit"]; value != "50" {
		t.Errorf("absent limit -> %q, want the default 50", value)
	}
	if value := bare.query["include_evidence"]; value != "false" {
		t.Errorf("absent include_evidence -> %q, want an explicit false", value)
	}
}

// TestRepositoryRouteStillOwnsItsArmsAfterAdmissionDecisions proves the
// ninth delegation added in front of the repository switch claims only this
// family and leaves every neighbouring arm — including the ones sharing the
// "count_", "get_", and "list_" prefixes — answered as before.
func TestRepositoryRouteStillOwnsItsArmsAfterAdmissionDecisions(t *testing.T) {
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
		"list_kubernetes_correlations",
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
	} {
		if _, handled := admissionDecisionsRoute(tool, map[string]any{}); handled {
			t.Errorf("admissionDecisionsRoute(%s) handled = true, want false", tool)
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

// TestAdmissionDecisionsRouteRejectsNonFamilyTools mutation-proves the child
// selector through the adapter: the owned name is claimed, and near-miss names
// are not.
func TestAdmissionDecisionsRouteRejectsNonFamilyTools(t *testing.T) {
	t.Parallel()

	for _, tool := range admissionDecisionsRouteTools {
		if _, handled := admissionDecisionsRoute(tool, map[string]any{}); !handled {
			t.Errorf("admissionDecisionsRoute(%s) handled = false, want true", tool)
		}
	}
	for _, tool := range []string{
		"", "list_admission",
		"list_admission_decision",
		"list_admission_decisions_extra",
		"count_admission_decisions",
		"get_admission_decisions",
		"get_admission_decision_inventory",
		"admission_decisions",
		"LIST_ADMISSION_DECISIONS",
	} {
		if _, handled := admissionDecisionsRoute(tool, map[string]any{}); handled {
			t.Errorf("admissionDecisionsRoute(%q) handled = true, want false", tool)
		}
	}
}
