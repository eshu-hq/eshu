// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cicdtools

import (
	"reflect"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/mcp/routecontract"
)

func TestRouteOwnsExactlyTheCICDFamily(t *testing.T) {
	t.Parallel()

	owned := []string{
		"list_ci_cd_run_correlations",
		"count_ci_cd_run_correlations",
		"get_ci_cd_run_correlation_inventory",
	}
	for _, toolName := range owned {
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
		"list_service_catalog_correlations",
		"list_codeowners_ownership",
		"list_container_image_identities",
		"count_container_image_identities",
		"list_advisory_evidence",
		"get_repository_stats",
		"list_ci_cd_run_correlation",
		"list_ci_cd_run_correlations_extra",
		"count_ci_cd_run_correlation",
		"get_ci_cd_run_correlation_inventories",
		"get_ci_cd_run_inventory",
		"list_ci_cd_runs",
		"LIST_CI_CD_RUN_CORRELATIONS",
		"",
		"not_a_tool",
	} {
		if request, handled := Route(toolName, routecontract.Arguments{}); handled {
			t.Errorf("Route(%s) handled = true (%#v), want false", toolName, request)
		}
	}
}

func TestRoutePreservesCICDRequestContracts(t *testing.T) {
	t.Parallel()

	args := routecontract.Arguments{
		"after_correlation_id": "correlation-1",
		"artifact_digest":      "sha256:abc",
		"commit_sha":           "0f1e2d3c",
		"environment":          "prod",
		"group_by":             "provider",
		"image_ref":            "registry.example/team-api:1.2.3",
		"limit":                float64(25),
		"offset":               float64(5),
		"outcome":              "correlated",
		"provider":             "github_actions",
		"provider_run_id":      "run-9",
		"repository_id":        "repo-web",
		"run_id":               "run-1",
		"scope_id":             "scope-a",
	}

	for _, tt := range []struct {
		toolName string
		want     routecontract.Request
	}{
		{
			toolName: "list_ci_cd_run_correlations",
			want: routecontract.Request{Method: "GET", Path: "/api/v0/ci-cd/run-correlations", Query: map[string]string{
				"after_correlation_id": "correlation-1",
				"artifact_digest":      "sha256:abc",
				"commit_sha":           "0f1e2d3c",
				"environment":          "prod",
				"image_ref":            "registry.example/team-api:1.2.3",
				"limit":                "25",
				"outcome":              "correlated",
				"provider":             "github_actions",
				"provider_run_id":      "run-9",
				"repository_id":        "repo-web",
				"run_id":               "run-1",
				"scope_id":             "scope-a",
			}},
		},
		{
			toolName: "count_ci_cd_run_correlations",
			want: routecontract.Request{Method: "GET", Path: "/api/v0/ci-cd/run-correlations/count", Query: map[string]string{
				"scope_id":        "scope-a",
				"repository_id":   "repo-web",
				"commit_sha":      "0f1e2d3c",
				"provider":        "github_actions",
				"artifact_digest": "sha256:abc",
				"image_ref":       "registry.example/team-api:1.2.3",
				"environment":     "prod",
				"outcome":         "correlated",
			}},
		},
		{
			toolName: "get_ci_cd_run_correlation_inventory",
			want: routecontract.Request{Method: "GET", Path: "/api/v0/ci-cd/run-correlations/inventory", Query: map[string]string{
				"group_by":        "provider",
				"scope_id":        "scope-a",
				"repository_id":   "repo-web",
				"commit_sha":      "0f1e2d3c",
				"provider":        "github_actions",
				"artifact_digest": "sha256:abc",
				"image_ref":       "registry.example/team-api:1.2.3",
				"environment":     "prod",
				"outcome":         "correlated",
				"limit":           "25",
				"offset":          "5",
			}},
		},
	} {
		request, handled := Route(tt.toolName, args)
		if !handled {
			t.Errorf("Route(%s) handled = false, want true", tt.toolName)
			continue
		}
		if !reflect.DeepEqual(request, tt.want) {
			t.Errorf("Route(%s) = %#v, want %#v", tt.toolName, request, tt.want)
		}
	}
}

func TestRouteAppliesCICDDefaultsAndCoercions(t *testing.T) {
	t.Parallel()

	// Absent limit/offset fall back to the dispatcher's documented defaults,
	// and an absent group_by falls back to "outcome".
	for _, tt := range []struct {
		toolName string
		key      string
		want     string
	}{
		{toolName: "list_ci_cd_run_correlations", key: "limit", want: "50"},
		{toolName: "get_ci_cd_run_correlation_inventory", key: "limit", want: "100"},
		{toolName: "get_ci_cd_run_correlation_inventory", key: "offset", want: "0"},
		{toolName: "get_ci_cd_run_correlation_inventory", key: "group_by", want: "outcome"},
	} {
		request, handled := Route(tt.toolName, routecontract.Arguments{})
		if !handled {
			t.Fatalf("Route(%s) handled = false, want true", tt.toolName)
		}
		if got := request.Query[tt.key]; got != tt.want {
			t.Errorf("Route(%s).Query[%s] = %q, want %q", tt.toolName, tt.key, got, tt.want)
		}
	}

	// The count route carries no limit, offset, or group_by key at all.
	countRequest, _ := Route("count_ci_cd_run_correlations", routecontract.Arguments{"limit": 25, "offset": 5, "group_by": "provider"})
	for _, key := range []string{"limit", "offset", "group_by"} {
		if _, present := countRequest.Query[key]; present {
			t.Errorf("count route query carries %q = %q, want the key absent", key, countRequest.Query[key])
		}
	}

	// An explicitly empty group_by still falls back; a non-empty one wins.
	for _, tt := range []struct {
		groupBy any
		want    string
	}{
		{groupBy: "", want: "outcome"},
		{groupBy: "provider", want: "provider"},
		{groupBy: 7, want: "outcome"}, // wrong type reads as absent
	} {
		request, _ := Route("get_ci_cd_run_correlation_inventory", routecontract.Arguments{"group_by": tt.groupBy})
		if got := request.Query["group_by"]; got != tt.want {
			t.Errorf("group_by=%#v -> %q, want %q", tt.groupBy, got, tt.want)
		}
	}

	// Numeric coercions match routecontract.Arguments.IntOr exactly, including
	// float truncation toward zero and the fallback for unsupported types.
	for _, tt := range []struct {
		limit any
		want  string
	}{
		{limit: 25, want: "25"},
		{limit: int64(26), want: "26"},
		{limit: 27.9, want: "27"},
		{limit: -3.9, want: "-3"},
		{limit: 0, want: "0"},
		{limit: "25", want: "50"},
		{limit: true, want: "50"},
		{limit: nil, want: "50"},
	} {
		request, _ := Route("list_ci_cd_run_correlations", routecontract.Arguments{"limit": tt.limit})
		if got := request.Query["limit"]; got != tt.want {
			t.Errorf("limit=%#v -> %q, want %q", tt.limit, got, tt.want)
		}
	}

	// The inventory route defaults independently of the listing route.
	for _, tt := range []struct {
		key   string
		value any
		want  string
	}{
		{key: "limit", value: 250, want: "250"},
		{key: "limit", value: "250", want: "100"},
		{key: "offset", value: int64(40), want: "40"},
		{key: "offset", value: 40.9, want: "40"},
		{key: "offset", value: "40", want: "0"},
	} {
		request, _ := Route("get_ci_cd_run_correlation_inventory", routecontract.Arguments{tt.key: tt.value})
		if got := request.Query[tt.key]; got != tt.want {
			t.Errorf("inventory %s=%#v -> %q, want %q", tt.key, tt.value, got, tt.want)
		}
	}

	// Wrong-typed and absent string arguments both read as empty, never as a
	// formatted Go value.
	request, _ := Route("list_ci_cd_run_correlations", routecontract.Arguments{"environment": 42, "outcome": nil})
	if got := request.Query["environment"]; got != "" {
		t.Errorf("environment=42 -> %q, want empty", got)
	}
	if got := request.Query["outcome"]; got != "" {
		t.Errorf("outcome=nil -> %q, want empty", got)
	}
}

// TestRouteFallsBackFromProviderRunIDToRunID pins the listing route's
// provider_run_id resolution: an explicit provider_run_id wins, an absent,
// empty, or wrong-typed one falls back to run_id, and run_id is always echoed
// under its own key regardless.
func TestRouteFallsBackFromProviderRunIDToRunID(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name          string
		args          routecontract.Arguments
		wantProvider  string
		wantRunIDEcho string
	}{
		{name: "both present", args: routecontract.Arguments{"provider_run_id": "run-9", "run_id": "run-1"}, wantProvider: "run-9", wantRunIDEcho: "run-1"},
		{name: "only provider_run_id", args: routecontract.Arguments{"provider_run_id": "run-9"}, wantProvider: "run-9", wantRunIDEcho: ""},
		{name: "only run_id", args: routecontract.Arguments{"run_id": "run-1"}, wantProvider: "run-1", wantRunIDEcho: "run-1"},
		{name: "both absent", args: routecontract.Arguments{}, wantProvider: "", wantRunIDEcho: ""},
		{name: "empty provider_run_id", args: routecontract.Arguments{"provider_run_id": "", "run_id": "run-1"}, wantProvider: "run-1", wantRunIDEcho: "run-1"},
		{name: "wrong-typed provider_run_id", args: routecontract.Arguments{"provider_run_id": 9, "run_id": "run-1"}, wantProvider: "run-1", wantRunIDEcho: "run-1"},
		{name: "wrong-typed run_id", args: routecontract.Arguments{"provider_run_id": "", "run_id": 1}, wantProvider: "", wantRunIDEcho: ""},
		{name: "both wrong-typed", args: routecontract.Arguments{"provider_run_id": 9, "run_id": 1}, wantProvider: "", wantRunIDEcho: ""},
	} {
		request, handled := Route("list_ci_cd_run_correlations", tt.args)
		if !handled {
			t.Fatalf("%s: Route handled = false, want true", tt.name)
		}
		if got := request.Query["provider_run_id"]; got != tt.wantProvider {
			t.Errorf("%s: provider_run_id = %q, want %q", tt.name, got, tt.wantProvider)
		}
		if got := request.Query["run_id"]; got != tt.wantRunIDEcho {
			t.Errorf("%s: run_id = %q, want %q", tt.name, got, tt.wantRunIDEcho)
		}
	}

	// The fallback is scoped to the listing route: neither aggregate route
	// carries a provider_run_id or run_id key.
	for _, toolName := range []string{"count_ci_cd_run_correlations", "get_ci_cd_run_correlation_inventory"} {
		request, _ := Route(toolName, routecontract.Arguments{"provider_run_id": "run-9", "run_id": "run-1"})
		for _, key := range []string{"provider_run_id", "run_id"} {
			if _, present := request.Query[key]; present {
				t.Errorf("Route(%s).Query carries %q, want the key absent", toolName, key)
			}
		}
	}
}

func TestRouteHandlesNilAndTypedNilCICDArguments(t *testing.T) {
	t.Parallel()

	var typedNil map[string]any
	for _, tt := range []struct {
		name string
		args routecontract.Arguments
	}{
		{name: "nil literal", args: nil},
		{name: "typed nil map", args: routecontract.Arguments(typedNil)},
		{name: "empty", args: routecontract.Arguments{}},
	} {
		request, handled := Route("list_ci_cd_run_correlations", tt.args)
		if !handled {
			t.Fatalf("%s: Route handled = false, want true", tt.name)
		}
		want := routecontract.Request{Method: "GET", Path: "/api/v0/ci-cd/run-correlations", Query: map[string]string{
			"after_correlation_id": "",
			"artifact_digest":      "",
			"commit_sha":           "",
			"environment":          "",
			"image_ref":            "",
			"limit":                "50",
			"outcome":              "",
			"provider":             "",
			"provider_run_id":      "",
			"repository_id":        "",
			"run_id":               "",
			"scope_id":             "",
		}}
		if !reflect.DeepEqual(request, want) {
			t.Fatalf("%s: Route = %#v, want %#v", tt.name, request, want)
		}

		inventory, handled := Route("get_ci_cd_run_correlation_inventory", tt.args)
		if !handled {
			t.Fatalf("%s: Route(inventory) handled = false, want true", tt.name)
		}
		wantInventory := routecontract.Request{Method: "GET", Path: "/api/v0/ci-cd/run-correlations/inventory", Query: map[string]string{
			"group_by":        "outcome",
			"scope_id":        "",
			"repository_id":   "",
			"commit_sha":      "",
			"provider":        "",
			"artifact_digest": "",
			"image_ref":       "",
			"environment":     "",
			"outcome":         "",
			"limit":           "100",
			"offset":          "0",
		}}
		if !reflect.DeepEqual(inventory, wantInventory) {
			t.Fatalf("%s: Route(inventory) = %#v, want %#v", tt.name, inventory, wantInventory)
		}

		count, handled := Route("count_ci_cd_run_correlations", tt.args)
		if !handled {
			t.Fatalf("%s: Route(count) handled = false, want true", tt.name)
		}
		wantCount := routecontract.Request{Method: "GET", Path: "/api/v0/ci-cd/run-correlations/count", Query: map[string]string{
			"scope_id":        "",
			"repository_id":   "",
			"commit_sha":      "",
			"provider":        "",
			"artifact_digest": "",
			"image_ref":       "",
			"environment":     "",
			"outcome":         "",
		}}
		if !reflect.DeepEqual(count, wantCount) {
			t.Fatalf("%s: Route(count) = %#v, want %#v", tt.name, count, wantCount)
		}
	}
}

func TestRouteDoesNotAliasCallerCICDArguments(t *testing.T) {
	t.Parallel()

	args := routecontract.Arguments{"environment": "prod", "limit": float64(25)}
	request, handled := Route("list_ci_cd_run_correlations", args)
	if !handled {
		t.Fatal("Route handled = false, want true")
	}
	request.Query["environment"] = "mutated"
	if got := args["environment"]; got != "prod" {
		t.Fatalf("caller arguments mutated through the returned query: environment = %#v", got)
	}
	if len(args) != 2 {
		t.Fatalf("caller arguments grew to %d keys, want 2", len(args))
	}

	// Two calls with the same arguments hand back independent query maps.
	first, _ := Route("get_ci_cd_run_correlation_inventory", args)
	second, _ := Route("get_ci_cd_run_correlation_inventory", args)
	first.Query["group_by"] = "mutated"
	if got := second.Query["group_by"]; got != "outcome" {
		t.Fatalf("a later request shares the earlier query map: group_by = %q", got)
	}
}
