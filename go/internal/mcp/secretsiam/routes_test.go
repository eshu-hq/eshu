// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package secretsiamtools

import (
	"reflect"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/mcp/routecontract"
)

// listingTools are the four keyset listings. They share the limit default; the
// posture summary deliberately does not, so it is excluded here.
var listingTools = []string{
	"list_secrets_iam_identity_trust_chains",
	"list_secrets_iam_privilege_posture_observations",
	"list_secrets_iam_secret_access_paths",
	"list_secrets_iam_posture_gaps",
}

// familyTools are all five names this package owns, in the order the root
// repository switch used to answer them.
var familyTools = append(append([]string{}, listingTools...), "count_secrets_iam_posture")

// populatedArguments carries every key any of the five routes reads, each with
// a distinct value, so a key swapped between two routes fails the exact
// request comparisons below rather than passing on a shared value.
var populatedArguments = routecontract.Arguments{
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
	"unused_decoy":             "ignored",
	"vault_mount_join_key":     "vault/prod/kv",
	"workload_object_id":       "workload-7",
}

func TestRouteOwnsExactlyTheSecretsIAMFamily(t *testing.T) {
	t.Parallel()

	for _, toolName := range familyTools {
		request, handled := Route(toolName, routecontract.Arguments{})
		if !handled {
			t.Errorf("Route(%s) handled = false, want true", toolName)
			continue
		}
		if request.Method != "GET" {
			t.Errorf("Route(%s) method = %q, want GET", toolName, request.Method)
		}
		if request.Body != nil {
			t.Errorf("Route(%s) body = %#v, want nil", toolName, request.Body)
		}
	}

	// Neighbours in the root repository switch, plus near-miss names: this
	// package must claim none of them.
	for _, toolName := range []string{
		"list_indexed_repositories",
		"list_admission_decisions",
		"list_package_registry_packages",
		"count_package_registry_packages",
		"list_ci_cd_run_correlations",
		"count_ci_cd_run_correlations",
		"list_codeowners_ownership",
		"list_service_catalog_correlations",
		"list_kubernetes_correlations",
		"list_observability_coverage_correlations",
		"list_container_image_identities",
		"list_advisory_evidence",
		"get_repository_stats",
		"list_secrets_iam_identity_trust_chain",
		"list_secrets_iam_identity_trust_chains_extra",
		"list_secrets_iam_posture_gap",
		"list_secrets_iam_posture",
		"list_secrets_iam",
		"count_secrets_iam_posture_gaps",
		"count_secrets_iam_postures",
		"get_secrets_iam_posture",
		"get_secrets_iam_posture_inventory",
		"COUNT_SECRETS_IAM_POSTURE",
		"LIST_SECRETS_IAM_POSTURE_GAPS",
		"",
		"not_a_tool",
	} {
		if request, handled := Route(toolName, routecontract.Arguments{}); handled {
			t.Errorf("Route(%s) handled = true (%#v), want false", toolName, request)
		}
	}
}

func TestRoutePreservesSecretsIAMRequestContracts(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		toolName string
		want     routecontract.Request
	}{
		{
			toolName: "list_secrets_iam_identity_trust_chains",
			want: routecontract.Request{Method: "GET", Path: "/api/v0/secrets-iam/identity-trust-chains", Query: map[string]string{
				"after_chain_id":           "chain-cursor",
				"chain_id":                 "chain-7",
				"iam_role_fingerprint":     "arn:aws:iam::1:role/api",
				"limit":                    "25",
				"scope_id":                 "scope-a",
				"service_account_join_key": "k8s/prod/api-sa",
				"state":                    "active",
				"workload_object_id":       "workload-7",
			}},
		},
		{
			toolName: "list_secrets_iam_privilege_posture_observations",
			want: routecontract.Request{Method: "GET", Path: "/api/v0/secrets-iam/privilege-posture-observations", Query: map[string]string{
				"after_observation_id": "observation-cursor",
				"limit":                "25",
				"observation_id":       "observation-7",
				"risk_type":            "wildcard_action",
				"scope_id":             "scope-a",
				"severity":             "high",
				"state":                "active",
			}},
		},
		{
			toolName: "list_secrets_iam_secret_access_paths",
			want: routecontract.Request{Method: "GET", Path: "/api/v0/secrets-iam/secret-access-paths", Query: map[string]string{
				"after_path_id":        "path-cursor",
				"chain_id":             "chain-7",
				"limit":                "25",
				"path_id":              "path-7",
				"scope_id":             "scope-a",
				"state":                "active",
				"vault_mount_join_key": "vault/prod/kv",
			}},
		},
		{
			toolName: "list_secrets_iam_posture_gaps",
			want: routecontract.Request{Method: "GET", Path: "/api/v0/secrets-iam/posture-gaps", Query: map[string]string{
				"after_gap_id":             "gap-cursor",
				"gap_id":                   "gap-7",
				"gap_type":                 "missing_rotation",
				"limit":                    "25",
				"scope_id":                 "scope-a",
				"service_account_join_key": "k8s/prod/api-sa",
				"state":                    "active",
			}},
		},
		{
			toolName: "count_secrets_iam_posture",
			want: routecontract.Request{Method: "GET", Path: "/api/v0/secrets-iam/posture-summary", Query: map[string]string{
				"scope_id": "scope-a",
			}},
		},
	} {
		request, handled := Route(tt.toolName, populatedArguments)
		if !handled {
			t.Errorf("Route(%s) handled = false, want true", tt.toolName)
			continue
		}
		if !reflect.DeepEqual(request, tt.want) {
			t.Errorf("Route(%s) = %#v, want %#v", tt.toolName, request, tt.want)
		}
	}
}

// TestRouteKeepsThePostureSummaryScopeAnchoredOnly pins the asymmetry that
// makes this family interesting. The four listings page, so each takes a limit
// and its own cursor and filter keys. count_secrets_iam_posture is a
// scope-anchored aggregate over the whole posture: it carries scope_id and
// nothing else. Handing it a limit would not cap the aggregate -- the handler
// never reads one -- but it would advertise a bound the endpoint does not
// honor, so a mutant that adds one has to fail here.
func TestRouteKeepsThePostureSummaryScopeAnchoredOnly(t *testing.T) {
	t.Parallel()

	// Every key any sibling route reads is offered, and all but scope_id must
	// be refused.
	request, handled := Route("count_secrets_iam_posture", populatedArguments)
	if !handled {
		t.Fatal("Route(count_secrets_iam_posture) handled = false, want true")
	}
	if got, want := len(request.Query), 1; got != want {
		t.Fatalf("posture summary carries %d query keys (%#v), want %d", got, request.Query, want)
	}
	if got := request.Query["scope_id"]; got != "scope-a" {
		t.Errorf("posture summary scope_id = %q, want scope-a", got)
	}
	for _, key := range []string{
		"limit", "offset", "state", "severity", "risk_type", "gap_type",
		"group_by", "after_gap_id", "scope", "repository_id",
	} {
		if value, present := request.Query[key]; present {
			t.Errorf("posture summary carries %q = %q, want the key absent", key, value)
		}
	}

	// An explicit limit is still refused, including one the caller typed the
	// way the listings accept it.
	for _, limit := range []any{50, int64(50), float64(50), "50", nil} {
		request, _ := Route("count_secrets_iam_posture", routecontract.Arguments{"scope_id": "scope-a", "limit": limit})
		if _, present := request.Query["limit"]; present {
			t.Errorf("posture summary with limit=%#v carries a limit key, want it absent", limit)
		}
		if got, want := len(request.Query), 1; got != want {
			t.Errorf("posture summary with limit=%#v carries %d keys, want %d", limit, got, want)
		}
	}

	// scope_id itself stays present even when absent or wrongly typed, so the
	// handler sees an empty anchor rather than no anchor key at all.
	for _, tt := range []struct {
		name string
		args routecontract.Arguments
	}{
		{name: "absent", args: routecontract.Arguments{}},
		{name: "nil arguments", args: nil},
		{name: "wrong type", args: routecontract.Arguments{"scope_id": 42}},
	} {
		request, _ := Route("count_secrets_iam_posture", tt.args)
		value, present := request.Query["scope_id"]
		if !present {
			t.Errorf("%s: posture summary dropped the scope_id key entirely", tt.name)
		}
		if value != "" {
			t.Errorf("%s: posture summary scope_id = %q, want empty", tt.name, value)
		}
	}
}

func TestRouteAppliesSecretsIAMDefaultsAndCoercions(t *testing.T) {
	t.Parallel()

	// Each of the four listings falls back to the dispatcher's documented
	// limit default of 50.
	for _, toolName := range listingTools {
		request, handled := Route(toolName, routecontract.Arguments{})
		if !handled {
			t.Fatalf("Route(%s) handled = false, want true", toolName)
		}
		if got := request.Query["limit"]; got != "50" {
			t.Errorf("Route(%s) absent limit -> %q, want 50", toolName, got)
		}
	}

	// Numeric coercions match routecontract.Arguments.IntOr exactly on every
	// listing, including float truncation toward zero and the fallback for
	// unsupported types.
	for _, toolName := range listingTools {
		for _, tt := range []struct {
			limit any
			want  string
		}{
			{limit: 25, want: "25"},
			{limit: int64(26), want: "26"},
			{limit: 27.9, want: "27"},
			{limit: -3.9, want: "-3"},
			{limit: -7, want: "-7"},
			{limit: 0, want: "0"},
			{limit: "25", want: "50"},
			{limit: true, want: "50"},
			{limit: nil, want: "50"},
			{limit: float32(25), want: "50"},
		} {
			request, _ := Route(toolName, routecontract.Arguments{"limit": tt.limit})
			if got := request.Query["limit"]; got != tt.want {
				t.Errorf("Route(%s) limit=%#v -> %q, want %q", toolName, tt.limit, got, tt.want)
			}
		}
	}

	// Wrong-typed and absent string arguments both read as empty, never as a
	// formatted Go value.
	for _, tt := range []struct {
		toolName string
		key      string
		value    any
	}{
		{toolName: "list_secrets_iam_identity_trust_chains", key: "scope_id", value: 42},
		{toolName: "list_secrets_iam_identity_trust_chains", key: "chain_id", value: nil},
		{toolName: "list_secrets_iam_privilege_posture_observations", key: "severity", value: []string{"high"}},
		{toolName: "list_secrets_iam_secret_access_paths", key: "vault_mount_join_key", value: struct{}{}},
		{toolName: "list_secrets_iam_posture_gaps", key: "gap_type", value: true},
		{toolName: "count_secrets_iam_posture", key: "scope_id", value: []byte("scope-a")},
	} {
		request, handled := Route(tt.toolName, routecontract.Arguments{tt.key: tt.value})
		if !handled {
			t.Fatalf("Route(%s) handled = false, want true", tt.toolName)
		}
		if got := request.Query[tt.key]; got != "" {
			t.Errorf("Route(%s) %s=%#v -> %q, want empty", tt.toolName, tt.key, tt.value, got)
		}
	}

	// Each route carries exactly the keys its handler reads, and never a key
	// that belongs to a sibling route or to a paging style this family does
	// not use.
	for _, tt := range []struct {
		toolName string
		keys     int
		foreign  []string
	}{
		{toolName: "list_secrets_iam_identity_trust_chains", keys: 8, foreign: []string{"offset", "group_by", "gap_id", "observation_id", "path_id"}},
		{toolName: "list_secrets_iam_privilege_posture_observations", keys: 7, foreign: []string{"offset", "group_by", "chain_id", "gap_id", "path_id"}},
		{toolName: "list_secrets_iam_secret_access_paths", keys: 7, foreign: []string{"offset", "group_by", "gap_id", "observation_id", "severity"}},
		{toolName: "list_secrets_iam_posture_gaps", keys: 7, foreign: []string{"offset", "group_by", "chain_id", "observation_id", "path_id"}},
		{toolName: "count_secrets_iam_posture", keys: 1, foreign: []string{"offset", "group_by", "limit", "state"}},
	} {
		request, _ := Route(tt.toolName, populatedArguments)
		if got := len(request.Query); got != tt.keys {
			t.Errorf("Route(%s) carries %d query keys (%#v), want %d", tt.toolName, got, request.Query, tt.keys)
		}
		for _, key := range tt.foreign {
			if _, present := request.Query[key]; present {
				t.Errorf("Route(%s) carries %q, want the key absent", tt.toolName, key)
			}
		}
	}
}

func TestRouteHandlesNilAndTypedNilSecretsIAMArguments(t *testing.T) {
	t.Parallel()

	empty := map[string]routecontract.Request{
		"list_secrets_iam_identity_trust_chains": {Method: "GET", Path: "/api/v0/secrets-iam/identity-trust-chains", Query: map[string]string{
			"after_chain_id":           "",
			"chain_id":                 "",
			"iam_role_fingerprint":     "",
			"limit":                    "50",
			"scope_id":                 "",
			"service_account_join_key": "",
			"state":                    "",
			"workload_object_id":       "",
		}},
		"list_secrets_iam_privilege_posture_observations": {Method: "GET", Path: "/api/v0/secrets-iam/privilege-posture-observations", Query: map[string]string{
			"after_observation_id": "",
			"limit":                "50",
			"observation_id":       "",
			"risk_type":            "",
			"scope_id":             "",
			"severity":             "",
			"state":                "",
		}},
		"list_secrets_iam_secret_access_paths": {Method: "GET", Path: "/api/v0/secrets-iam/secret-access-paths", Query: map[string]string{
			"after_path_id":        "",
			"chain_id":             "",
			"limit":                "50",
			"path_id":              "",
			"scope_id":             "",
			"state":                "",
			"vault_mount_join_key": "",
		}},
		"list_secrets_iam_posture_gaps": {Method: "GET", Path: "/api/v0/secrets-iam/posture-gaps", Query: map[string]string{
			"after_gap_id":             "",
			"gap_id":                   "",
			"gap_type":                 "",
			"limit":                    "50",
			"scope_id":                 "",
			"service_account_join_key": "",
			"state":                    "",
		}},
		"count_secrets_iam_posture": {Method: "GET", Path: "/api/v0/secrets-iam/posture-summary", Query: map[string]string{
			"scope_id": "",
		}},
	}

	var typedNil map[string]any
	for _, tt := range []struct {
		name string
		args routecontract.Arguments
	}{
		{name: "nil literal", args: nil},
		{name: "typed nil map", args: routecontract.Arguments(typedNil)},
		{name: "empty", args: routecontract.Arguments{}},
	} {
		for _, toolName := range familyTools {
			request, handled := Route(toolName, tt.args)
			if !handled {
				t.Fatalf("%s: Route(%s) handled = false, want true", tt.name, toolName)
			}
			if !reflect.DeepEqual(request, empty[toolName]) {
				t.Fatalf("%s: Route(%s) = %#v, want %#v", tt.name, toolName, request, empty[toolName])
			}
		}
	}
}

func TestRouteDoesNotAliasCallerSecretsIAMArguments(t *testing.T) {
	t.Parallel()

	for _, toolName := range familyTools {
		args := routecontract.Arguments{"scope_id": "scope-a", "limit": float64(25)}
		request, handled := Route(toolName, args)
		if !handled {
			t.Fatalf("Route(%s) handled = false, want true", toolName)
		}
		request.Query["scope_id"] = "mutated"
		if got := args["scope_id"]; got != "scope-a" {
			t.Fatalf("Route(%s) mutated caller arguments through the returned query: scope_id = %#v", toolName, got)
		}
		if len(args) != 2 {
			t.Fatalf("Route(%s) grew caller arguments to %d keys, want 2", toolName, len(args))
		}

		// Two calls with the same arguments hand back independent query maps.
		first, _ := Route(toolName, args)
		second, _ := Route(toolName, args)
		first.Query["scope_id"] = "mutated"
		if got := second.Query["scope_id"]; got != "scope-a" {
			t.Fatalf("Route(%s) shares a query map between calls: scope_id = %q", toolName, got)
		}
	}
}
