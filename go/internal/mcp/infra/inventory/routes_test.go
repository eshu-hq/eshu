// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package infrainventorytools

import (
	"reflect"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/mcp/routecontract"
)

// familyRoutes pins the four owned tool names to their method and internal
// path, literally, so the ownership test cannot drift with the selector's
// own table.
var familyRoutes = map[string]struct {
	method string
	path   string
}{
	"count_infra_resources":        {method: "GET", path: "/api/v0/infra/resources/count"},
	"get_infra_resource_inventory": {method: "GET", path: "/api/v0/infra/resources/inventory"},
	"investigate_resource":         {method: "POST", path: "/api/v0/impact/resource-investigation"},
	"analyze_infra_relationships":  {method: "POST", path: "/api/v0/infra/relationships"},
}

func TestRouteOwnsExactlyTheInfraInventoryFamily(t *testing.T) {
	t.Parallel()

	for tool, want := range familyRoutes {
		request, handled := Route(tool, routecontract.Arguments{})
		if !handled {
			t.Fatalf("Route(%s) handled = false, want true", tool)
		}
		if request.Method != want.method {
			t.Errorf("Route(%s) method = %q, want %q", tool, request.Method, want.method)
		}
		if request.Path != want.path {
			t.Errorf("Route(%s) path = %q, want %q", tool, request.Path, want.path)
		}
	}

	for _, tool := range []string{
		"",
		"find_infra_resources",
		"find_dead_iac",
		"investigate_service",
		"trace_resource_to_code",
		"COUNT_INFRA_RESOURCES",
		"count_infra_resources_extra",
	} {
		if _, handled := Route(tool, routecontract.Arguments{}); handled {
			t.Errorf("Route(%q) handled = true, want false", tool)
		}
	}
}

func TestRouteCarriesEveryCountInfraResourcesQueryKey(t *testing.T) {
	t.Parallel()

	request, handled := Route("count_infra_resources", routecontract.Arguments{
		"category": "k8s", "kind": "Deployment", "resource_type": "aws_lambda_function",
		"provider": "aws", "environment": "production", "resource_service": "aws.ec2",
		"resource_category": "compute",
	})
	if !handled {
		t.Fatal("Route(count_infra_resources) handled = false, want true")
	}
	if request.Body != nil {
		t.Errorf("Route(count_infra_resources) body = %#v, want nil", request.Body)
	}
	want := map[string]string{
		"category": "k8s", "kind": "Deployment", "resource_type": "aws_lambda_function",
		"provider": "aws", "environment": "production", "resource_service": "aws.ec2",
		"resource_category": "compute",
	}
	if !reflect.DeepEqual(request.Query, want) {
		t.Errorf("Route(count_infra_resources) query = %#v, want %#v", request.Query, want)
	}
}

func TestRouteCarriesEveryInfraResourceInventoryQueryKey(t *testing.T) {
	t.Parallel()

	request, handled := Route("get_infra_resource_inventory", routecontract.Arguments{
		"group_by": "resource_category", "category": "terraform", "kind": "aws_lambda_function",
		"resource_type": "aws_lambda_function", "provider": "aws", "environment": "staging",
		"resource_service": "aws.lambda", "resource_category": "compute",
		"limit": float64(25), "offset": float64(50),
	})
	if !handled {
		t.Fatal("Route(get_infra_resource_inventory) handled = false, want true")
	}
	want := map[string]string{
		"group_by": "resource_category", "category": "terraform", "kind": "aws_lambda_function",
		"resource_type": "aws_lambda_function", "provider": "aws", "environment": "staging",
		"resource_service": "aws.lambda", "resource_category": "compute",
		"limit": "25", "offset": "50",
	}
	if !reflect.DeepEqual(request.Query, want) {
		t.Errorf("Route(get_infra_resource_inventory) query = %#v, want %#v", request.Query, want)
	}
}

func TestRouteCarriesEveryInvestigateResourceBodyKey(t *testing.T) {
	t.Parallel()

	request, handled := Route("investigate_resource", routecontract.Arguments{
		"query": "orders-db", "resource_id": "cloud:rds:orders-db", "resource_type": "database",
		"environment": "prod", "max_depth": float64(3), "limit": float64(25),
	})
	if !handled {
		t.Fatal("Route(investigate_resource) handled = false, want true")
	}
	body, ok := request.Body.(map[string]any)
	if !ok {
		t.Fatalf("Route(investigate_resource) body type = %T, want map[string]any", request.Body)
	}
	want := map[string]any{
		"query": "orders-db", "resource_id": "cloud:rds:orders-db", "resource_type": "database",
		"environment": "prod", "max_depth": 3, "limit": 25,
	}
	if !reflect.DeepEqual(body, want) {
		t.Errorf("Route(investigate_resource) body = %#v, want %#v", body, want)
	}
}

func TestRouteCarriesEveryAnalyzeInfraRelationshipsBodyKey(t *testing.T) {
	t.Parallel()

	request, handled := Route("analyze_infra_relationships", routecontract.Arguments{
		"query_type": "what_runs_lambda_image",
		"target":     "arn:aws:lambda:us-east-1:000000000000:function:image-consumer",
	})
	if !handled {
		t.Fatal("Route(analyze_infra_relationships) handled = false, want true")
	}
	body, ok := request.Body.(map[string]any)
	if !ok {
		t.Fatalf("Route(analyze_infra_relationships) body type = %T, want map[string]any", request.Body)
	}
	want := map[string]any{
		"entity_id":         "arn:aws:lambda:us-east-1:000000000000:function:image-consumer",
		"relationship_type": "what_runs_lambda_image",
	}
	if !reflect.DeepEqual(body, want) {
		t.Errorf("Route(analyze_infra_relationships) body = %#v, want %#v", body, want)
	}
}

func TestRouteAppliesInfraInventoryDefaultsForAbsentArguments(t *testing.T) {
	t.Parallel()

	count, handled := Route("count_infra_resources", nil)
	if !handled {
		t.Fatal("Route(count_infra_resources, nil) handled = false, want true")
	}
	if got, want := count.Query["category"], ""; got != want {
		t.Errorf("count_infra_resources absent category -> %q, want an explicit empty string", got)
	}

	inventory, handled := Route("get_infra_resource_inventory", nil)
	if !handled {
		t.Fatal("Route(get_infra_resource_inventory, nil) handled = false, want true")
	}
	if got, want := inventory.Query["group_by"], "provider"; got != want {
		t.Errorf("get_infra_resource_inventory absent group_by -> %q, want %q", got, want)
	}
	if got, want := inventory.Query["limit"], "100"; got != want {
		t.Errorf("get_infra_resource_inventory absent limit -> %q, want %q", got, want)
	}
	if got, want := inventory.Query["offset"], "0"; got != want {
		t.Errorf("get_infra_resource_inventory absent offset -> %q, want %q", got, want)
	}

	investigate, handled := Route("investigate_resource", nil)
	if !handled {
		t.Fatal("Route(investigate_resource, nil) handled = false, want true")
	}
	investigateBody := investigate.Body.(map[string]any)
	if got, want := investigateBody["max_depth"], 4; got != want {
		t.Errorf("investigate_resource absent max_depth -> %#v, want %#v", got, want)
	}
	if got, want := investigateBody["limit"], 25; got != want {
		t.Errorf("investigate_resource absent limit -> %#v, want %#v", got, want)
	}

	relationships, handled := Route("analyze_infra_relationships", nil)
	if !handled {
		t.Fatal("Route(analyze_infra_relationships, nil) handled = false, want true")
	}
	relationshipsBody := relationships.Body.(map[string]any)
	if got, want := relationshipsBody["entity_id"], ""; got != want {
		t.Errorf("analyze_infra_relationships absent target -> %#v, want an explicit empty string", got)
	}
}

func TestRouteCoercesIntegerArguments(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		value any
		want  string
	}{
		{name: "int", value: int(9), want: "9"},
		{name: "int64", value: int64(11), want: "11"},
		{name: "float64", value: float64(13), want: "13"},
		{name: "string falls back", value: "17", want: "100"},
		{name: "bool falls back", value: true, want: "100"},
		{name: "nil falls back", value: nil, want: "100"},
	}

	for _, tt := range cases {
		request, _ := Route("get_infra_resource_inventory", routecontract.Arguments{"limit": tt.value})
		if got := request.Query["limit"]; got != tt.want {
			t.Errorf("limit %s (%#v) -> %q, want %q", tt.name, tt.value, got, tt.want)
		}
	}
}

// TestRouteBuildsFreshValues proves the selected body and query maps are not
// the caller's argument map, and that writing through them does not leak
// back into a later call, so one dispatch's mutation cannot bleed into
// another.
func TestRouteBuildsFreshValues(t *testing.T) {
	t.Parallel()

	args := routecontract.Arguments{"target": "checkout", "query_type": "what_deploys"}
	request, _ := Route("analyze_infra_relationships", args)
	body := request.Body.(map[string]any)
	body["probe"] = "written-through-body"
	if _, leaked := args["probe"]; leaked {
		t.Error("Route(analyze_infra_relationships) body aliases the caller's argument map")
	}

	queryArgs := routecontract.Arguments{"category": "cloud"}
	queryRequest, _ := Route("count_infra_resources", queryArgs)
	queryRequest.Query["probe"] = "written-through-query"
	if _, leaked := queryArgs["probe"]; leaked {
		t.Error("Route(count_infra_resources) query aliases the caller's argument map")
	}
}
