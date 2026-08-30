// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	"reflect"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/mcp/routecontract"
	secretsiamtools "github.com/eshu-hq/eshu/go/internal/mcp/secretsiam"
)

// secretsIAMRouteTools lists every tool the child package owns, in the order
// the root repository switch used to answer them.
var secretsIAMRouteTools = []string{
	"list_secrets_iam_identity_trust_chains",
	"list_secrets_iam_privilege_posture_observations",
	"list_secrets_iam_secret_access_paths",
	"list_secrets_iam_posture_gaps",
	"count_secrets_iam_posture",
}

func TestResolveRouteUsesExactSecretsIAMChildRequest(t *testing.T) {
	t.Parallel()

	argumentCases := []struct {
		name string
		args map[string]any
	}{
		{name: "nil", args: nil},
		{name: "empty", args: map[string]any{}},
		{name: "populated", args: map[string]any{
			"after_chain_id":           "chain-cursor",
			"after_gap_id":             "gap-cursor",
			"after_observation_id":     "observation-cursor",
			"after_path_id":            "path-cursor",
			"chain_id":                 "chain-7",
			"gap_id":                   "gap-7",
			"gap_type":                 "missing_rotation",
			"iam_role_fingerprint":     "arn:aws:iam::1:role/api",
			"limit":                    float64(25),
			"observation_id":           "observation-7",
			"path_id":                  "path-7",
			"risk_type":                "wildcard_action",
			"scope_id":                 "scope-a",
			"service_account_join_key": "k8s/prod/api-sa",
			"severity":                 "high",
			"state":                    "active",
			"vault_mount_join_key":     "vault/prod/kv",
			"workload_object_id":       "workload-7",
		}},
		{name: "scope only", args: map[string]any{"scope_id": "scope-a"}},
		{name: "malformed", args: map[string]any{
			"chain_id": 42,
			"limit":    "25",
			"scope_id": struct{}{},
			"severity": []string{"high"},
			"state":    nil,
		}},
	}

	for _, tool := range secretsIAMRouteTools {
		for _, tt := range argumentCases {
			got, err := resolveRoute(tool, tt.args)
			if err != nil {
				t.Fatalf("resolveRoute(%s, %s) error = %v, want nil", tool, tt.name, err)
			}
			request, handled := secretsiamtools.Route(tool, routecontract.Arguments(tt.args))
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

// TestRepositoryRouteStillOwnsItsArmsAfterSecretsIAM proves the fourth
// delegation added in front of the repository switch claims only the
// secrets/IAM family and leaves every neighbouring arm — including the ones
// sharing the "count_", "get_", and "list_" prefixes — answered as before.
func TestRepositoryRouteStillOwnsItsArmsAfterSecretsIAM(t *testing.T) {
	t.Parallel()

	for _, tool := range []string{
		"list_indexed_repositories",
		"list_admission_decisions",
		"list_package_registry_packages",
		"count_package_registry_packages",
		"get_package_registry_package_inventory",
		"list_ci_cd_run_correlations",
		"count_ci_cd_run_correlations",
		"get_ci_cd_run_correlation_inventory",
		"list_codeowners_ownership",
		"list_service_catalog_correlations",
		"list_kubernetes_correlations",
		"list_observability_coverage_correlations",
		"list_container_image_identities",
		"count_container_image_identities",
		"get_container_image_identity_inventory",
		"list_advisory_evidence",
		"get_repository_stats",
	} {
		if _, handled := secretsIAMRoute(tool, map[string]any{}); handled {
			t.Errorf("secretsIAMRoute(%s) handled = true, want false", tool)
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

// TestSecretsIAMRouteRejectsNonFamilyTools mutation-proves the child selector
// through the adapter: every owned name is claimed, and near-miss names are not.
func TestSecretsIAMRouteRejectsNonFamilyTools(t *testing.T) {
	t.Parallel()

	for _, tool := range secretsIAMRouteTools {
		if _, handled := secretsIAMRoute(tool, map[string]any{}); !handled {
			t.Errorf("secretsIAMRoute(%s) handled = false, want true", tool)
		}
	}
	for _, tool := range []string{
		"", "list_secrets_iam", "list_secrets_iam_posture",
		"list_secrets_iam_identity_trust_chain",
		"list_secrets_iam_identity_trust_chains_extra",
		"list_secrets_iam_posture_gap", "count_secrets_iam_posture_gaps",
		"count_secrets_iam_postures", "get_secrets_iam_posture",
		"get_secrets_iam_posture_inventory", "COUNT_SECRETS_IAM_POSTURE",
	} {
		if _, handled := secretsIAMRoute(tool, map[string]any{}); handled {
			t.Errorf("secretsIAMRoute(%q) handled = true, want false", tool)
		}
	}
}

// TestSecretsIAMPostureSummaryStaysScopeOnlyThroughDispatch carries the
// family's one asymmetry across the adapter boundary, where the handler
// actually sees it. The four listings page and take a limit; the posture
// summary aggregates a whole scope and takes scope_id alone. A limit reaching
// the summary handler would not cap the total -- it reads only scope_id -- but
// it would advertise a bound the endpoint does not honor.
func TestSecretsIAMPostureSummaryStaysScopeOnlyThroughDispatch(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name string
		args map[string]any
	}{
		{name: "scope only", args: map[string]any{"scope_id": "scope-a"}},
		{name: "limit offered", args: map[string]any{"scope_id": "scope-a", "limit": float64(25)}},
		{name: "every sibling key offered", args: map[string]any{
			"after_gap_id": "gap-cursor", "gap_type": "missing_rotation",
			"limit": 25, "offset": 5, "scope_id": "scope-a",
			"severity": "high", "state": "active",
		}},
		{name: "nil arguments", args: nil},
	} {
		got, err := resolveRoute("count_secrets_iam_posture", tt.args)
		if err != nil {
			t.Fatalf("%s: resolveRoute error = %v, want nil", tt.name, err)
		}
		if got.path != "/api/v0/secrets-iam/posture-summary" {
			t.Errorf("%s: path = %q, want the posture-summary path", tt.name, got.path)
		}
		if len(got.query) != 1 {
			t.Errorf("%s: query = %#v, want scope_id alone", tt.name, got.query)
		}
		if _, present := got.query["scope_id"]; !present {
			t.Errorf("%s: query dropped scope_id entirely", tt.name)
		}
		for _, key := range []string{"limit", "offset", "state", "severity", "group_by"} {
			if value, present := got.query[key]; present {
				t.Errorf("%s: query carries %q = %q, want the key absent", tt.name, key, value)
			}
		}
	}

	// The four listings do still page, so the summary's austerity is a
	// property of that one route, not of the family.
	for _, tool := range secretsIAMRouteTools[:4] {
		got, err := resolveRoute(tool, map[string]any{"scope_id": "scope-a"})
		if err != nil {
			t.Fatalf("resolveRoute(%s) error = %v, want nil", tool, err)
		}
		if value := got.query["limit"]; value != "50" {
			t.Errorf("resolveRoute(%s) limit = %q, want the default 50", tool, value)
		}
	}
}
