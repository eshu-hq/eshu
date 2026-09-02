// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	"reflect"
	"testing"

	infrasearchtools "github.com/eshu-hq/eshu/go/internal/mcp/infrasearch"
	"github.com/eshu-hq/eshu/go/internal/mcp/routecontract"
)

// infraSearchRouteTools lists every tool the child package owns.
var infraSearchRouteTools = []string{"find_infra_resources"}

// infraSearchBodyKeys is the eight-key body the search must still send
// through dispatch.
var infraSearchBodyKeys = []string{
	"query",
	"category",
	"kind",
	"provider",
	"environment",
	"resource_service",
	"resource_category",
	"limit",
}

func TestResolveRouteUsesExactInfraSearchChildRequest(t *testing.T) {
	t.Parallel()

	argumentCases := []struct {
		name string
		args map[string]any
	}{
		{name: "nil", args: nil},
		{name: "empty", args: map[string]any{}},
		{name: "populated", args: map[string]any{
			"query":             "checkout-bucket",
			"category":          "cloud",
			"kind":              "aws_s3_bucket",
			"provider":          "aws",
			"environment":       "prod",
			"resource_service":  "s3",
			"resource_category": "storage",
			"limit":             float64(25),
		}},
		{name: "structured only", args: map[string]any{
			"kind": "aws_instance",
		}},
		{name: "malformed", args: map[string]any{
			"query":       42,
			"category":    nil,
			"limit":       "25",
			"provider":    struct{}{},
			"environment": []string{"prod"},
			"kind":        true,
		}},
	}

	for _, tool := range infraSearchRouteTools {
		for _, tt := range argumentCases {
			got, err := resolveRoute(tool, tt.args)
			if err != nil {
				t.Fatalf("resolveRoute(%s, %s) error = %v, want nil", tool, tt.name, err)
			}
			request, handled := infrasearchtools.Route(tool, routecontract.Arguments(tt.args))
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

// TestInfraSearchDispatchKeepsEveryBodyKey proves the eight fields survive
// the adapter boundary, where the handler actually decodes them. The literal
// expectations here are deliberately independent of the child selector: the
// parity test above builds both of its sides from that selector, so it cannot
// notice a key the child itself dropped or misspelled.
func TestInfraSearchDispatchKeepsEveryBodyKey(t *testing.T) {
	t.Parallel()

	args := map[string]any{
		"query":             "checkout-bucket",
		"category":          "cloud",
		"kind":              "aws_s3_bucket",
		"provider":          "aws",
		"environment":       "prod",
		"resource_service":  "s3",
		"resource_category": "storage",
		"limit":             float64(25),
	}
	want := map[string]any{
		"query":             "checkout-bucket",
		"category":          "cloud",
		"kind":              "aws_s3_bucket",
		"provider":          "aws",
		"environment":       "prod",
		"resource_service":  "s3",
		"resource_category": "storage",
		"limit":             25,
	}

	got, err := resolveRoute("find_infra_resources", args)
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	if got.method != "POST" {
		t.Errorf("method = %q, want POST", got.method)
	}
	if got.path != "/api/v0/infra/resources/search" {
		t.Errorf("path = %q, want the infra resource search path", got.path)
	}
	if got.query != nil {
		t.Errorf("query = %#v, want nil", got.query)
	}
	body, ok := got.body.(map[string]any)
	if !ok {
		t.Fatalf("body type = %T, want map[string]any", got.body)
	}
	if n, wantN := len(body), len(infraSearchBodyKeys); n != wantN {
		t.Fatalf("body carries %d keys (%#v), want %d", n, body, wantN)
	}
	for _, key := range infraSearchBodyKeys {
		value, present := body[key]
		if !present {
			t.Errorf("dispatch dropped %q entirely", key)
			continue
		}
		if value != want[key] {
			t.Errorf("body[%s] = %#v, want %#v", key, value, want[key])
		}
	}
	for _, key := range []string{"offset", "cursor", "repo_id", "resource_id", "max_depth"} {
		if value, present := body[key]; present {
			t.Errorf("body carries %q = %#v, want the key absent", key, value)
		}
	}

	// The limit default reaches the handler unchanged when the caller omits
	// it, and every unset filter is still sent as an explicit empty string.
	bare, err := resolveRoute("find_infra_resources", map[string]any{
		"kind": "aws_instance",
	})
	if err != nil {
		t.Fatalf("resolveRoute(structured only) error = %v, want nil", err)
	}
	bareBody := bare.body.(map[string]any)
	if value := bareBody["limit"]; value != 50 {
		t.Errorf("absent limit -> %#v, want the default 50", value)
	}
	for _, key := range infraSearchBodyKeys {
		if key == "limit" || key == "kind" {
			continue
		}
		if value, present := bareBody[key]; !present || value != "" {
			t.Errorf("absent %s -> (%#v, %v), want an explicit empty string", key, value, present)
		}
	}
}

// TestResolveRouteStillOwnsItsSwitchArmsAfterInfraSearch proves the
// delegation added in front of the main switch claims only this family and
// leaves every neighbouring infra and content arm -- including the sibling
// aggregates that share the "infra_resource" stem -- answered as before.
func TestResolveRouteStillOwnsItsSwitchArmsAfterInfraSearch(t *testing.T) {
	t.Parallel()

	for _, tool := range []string{
		"count_infra_resources",
		"get_infra_resource_inventory",
		"investigate_resource",
		"analyze_infra_relationships",
		"find_code",
		"find_symbol",
		"get_file_content",
		"search_file_content",
		"search_entity_content",
		"build_evidence_citation_packet",
		"get_incident_context",
		"list_work_item_evidence",
	} {
		if _, handled := infraResourceSearchRoute(tool, map[string]any{}); handled {
			t.Errorf("infraResourceSearchRoute(%s) handled = true, want false", tool)
		}
		got, err := resolveRoute(tool, map[string]any{})
		if err != nil {
			t.Errorf("resolveRoute(%s) error = %v, want nil", tool, err)
			continue
		}
		if got == nil {
			t.Errorf("resolveRoute(%s) = nil, want a route", tool)
		}
	}

	// resolveRoute still reports an unknown tool as an error, not a nil route.
	if _, err := resolveRoute("not_a_tool", map[string]any{}); err == nil {
		t.Fatal("resolveRoute(not_a_tool) error = nil, want an unknown-tool error")
	}
}

// TestInfraSearchRouteRejectsNonFamilyTools mutation-proves the child
// selector through the adapter: the owned name is claimed, and near-miss
// names are not.
func TestInfraSearchRouteRejectsNonFamilyTools(t *testing.T) {
	t.Parallel()

	for _, tool := range infraSearchRouteTools {
		if _, handled := infraResourceSearchRoute(tool, map[string]any{}); !handled {
			t.Errorf("infraResourceSearchRoute(%s) handled = false, want true", tool)
		}
	}
	for _, tool := range []string{
		"", "find_infra",
		"find_infra_resource",
		"find_infra_resources_extra",
		"search_infra_resources",
		"list_infra_resources",
		"count_infra_resources",
		"get_infra_resource_inventory",
		"infra_resources",
		"FIND_INFRA_RESOURCES",
	} {
		if _, handled := infraResourceSearchRoute(tool, map[string]any{}); handled {
			t.Errorf("infraResourceSearchRoute(%q) handled = true, want false", tool)
		}
	}
}
