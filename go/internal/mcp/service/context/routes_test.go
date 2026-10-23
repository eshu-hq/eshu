// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package servicecontexttools

import (
	"reflect"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/mcp/contract/route"
)

// familyTools pins the four owned tool names, literally, so the ownership
// test cannot drift with the selector's own switch.
var familyTools = []string{
	"get_service_context",
	"get_service_story",
	"get_service_intelligence_report",
	"investigate_service",
}

func TestRouteOwnsExactlyTheServiceContextFamily(t *testing.T) {
	t.Parallel()

	for _, tool := range familyTools {
		_, handled, err := Route(tool, routecontract.Arguments{"workload_id": "wl-1", "service_name": "svc-1"})
		if !handled {
			t.Fatalf("Route(%s) handled = false, want true", tool)
		}
		if err != nil {
			t.Fatalf("Route(%s) error = %v, want nil with a selector present", tool, err)
		}
	}

	for _, tool := range []string{
		"",
		"list_service_catalog_correlations",
		"get_workload_context",
		"get_workload_story",
		"get_entity_context",
		"GET_SERVICE_CONTEXT",
		"get_service_context_extra",
	} {
		if _, handled, err := Route(tool, routecontract.Arguments{}); handled || err != nil {
			t.Errorf("Route(%q) = (_, %v, %v), want unhandled and nil error", tool, handled, err)
		}
	}
}

func TestRouteMapsGetServiceContext(t *testing.T) {
	t.Parallel()

	request, handled, err := Route("get_service_context", routecontract.Arguments{
		"workload_id": "workload:sample-service-api",
		"environment": "prod",
	})
	if !handled || err != nil {
		t.Fatalf("Route() = (_, %v, %v), want handled without error", handled, err)
	}
	want := routecontract.Request{
		Method: "GET",
		Path:   "/api/v0/services/workload:sample-service-api/context",
		Query:  map[string]string{"environment": "prod", "evidence_detail": "handles"},
	}
	if !reflect.DeepEqual(request, want) {
		t.Fatalf("Route() = %#v, want %#v", request, want)
	}
}

// TestRouteGetServiceContextEvidenceDetail proves the MCP default is handles
// (#7129), an explicit value wins, and a bad value travels verbatim so the
// handler, not this adapter, rejects it with a 400.
func TestRouteGetServiceContextEvidenceDetail(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct{ name, arg, want string }{
		{"default", "", "handles"},
		{"explicit full", "full", "full"},
		{"explicit handles", "handles", "handles"},
		{"unknown value is forwarded", "compact", "compact"},
	} {
		args := routecontract.Arguments{"workload_id": "workload:sample-service-api"}
		if tc.arg != "" {
			args["evidence_detail"] = tc.arg
		}
		request, _, err := Route("get_service_context", args)
		if err != nil {
			t.Fatalf("%s: Route() error = %v", tc.name, err)
		}
		if got := request.Query["evidence_detail"]; got != tc.want {
			t.Fatalf("%s: evidence_detail = %q, want %q", tc.name, got, tc.want)
		}
	}
}

// TestRouteGetServiceStoryHasNoEvidenceDetail proves the story route, which
// builds its own bounded dossier, is not given the context-only parameter.
func TestRouteGetServiceStoryHasNoEvidenceDetail(t *testing.T) {
	t.Parallel()

	request, _, err := Route("get_service_story", routecontract.Arguments{"workload_id": "workload:sample-service-api"})
	if err != nil {
		t.Fatalf("Route() error = %v", err)
	}
	if _, has := request.Query["evidence_detail"]; has {
		t.Fatalf("story query = %#v, want no evidence_detail", request.Query)
	}
}

func TestRouteGetServiceContextRequiresWorkloadID(t *testing.T) {
	t.Parallel()

	request, handled, err := Route("get_service_context", routecontract.Arguments{"service_name": "sample-service-api"})
	if !handled {
		t.Fatal("Route() handled = false, want true")
	}
	const wantError = "get_service_context requires workload_id"
	if err == nil || err.Error() != wantError {
		t.Fatalf("Route() error = %v, want %q", err, wantError)
	}
	if !reflect.DeepEqual(request, routecontract.Request{}) {
		t.Fatalf("Route() request = %#v, want zero request", request)
	}
}

func TestRouteMapsGetServiceStoryQualifiedID(t *testing.T) {
	t.Parallel()

	request, handled, err := Route("get_service_story", routecontract.Arguments{
		"workload_id": "workload:sample-service-api",
		"environment": "prod",
	})
	if !handled || err != nil {
		t.Fatalf("Route() = (_, %v, %v), want handled without error", handled, err)
	}
	want := routecontract.Request{
		Method: "GET",
		Path:   "/api/v0/services/workload:sample-service-api/story",
		Query:  map[string]string{"environment": "prod", "service_id": "workload:sample-service-api"},
	}
	if !reflect.DeepEqual(request, want) {
		t.Fatalf("Route() = %#v, want %#v", request, want)
	}
}

func TestRouteMapsGetServiceStoryCatalogIDAsNameSelector(t *testing.T) {
	t.Parallel()

	request, handled, err := Route("get_service_story", routecontract.Arguments{
		"workload_id": "service:sample-service-api",
	})
	if !handled || err != nil {
		t.Fatalf("Route() = (_, %v, %v), want handled without error", handled, err)
	}
	body := request
	if got, want := body.Path, "/api/v0/services/sample-service-api/story"; got != want {
		t.Fatalf("Route() path = %q, want %q", got, want)
	}
	if got := body.Query["service_id"]; got != "" {
		t.Fatalf("Route() query[service_id] = %q, want empty for catalog service id", got)
	}
}

func TestRouteMapsGetServiceStoryRepositoryScopedServiceName(t *testing.T) {
	t.Parallel()

	request, handled, err := Route("get_service_story", routecontract.Arguments{
		"service_name":  "sample-service-api",
		"repository_id": "repository:r_sample",
		"environment":   "prod",
	})
	if !handled || err != nil {
		t.Fatalf("Route() = (_, %v, %v), want handled without error", handled, err)
	}
	want := routecontract.Request{
		Method: "GET",
		Path:   "/api/v0/services/sample-service-api/story",
		Query:  map[string]string{"environment": "prod", "repo": "repository:r_sample"},
	}
	if !reflect.DeepEqual(request, want) {
		t.Fatalf("Route() = %#v, want %#v", request, want)
	}
}

func TestRouteGetServiceStoryRequiresAnySelector(t *testing.T) {
	t.Parallel()

	request, handled, err := Route("get_service_story", routecontract.Arguments{})
	if !handled {
		t.Fatal("Route() handled = false, want true")
	}
	const wantError = "get_service_story requires service_id, workload_id, or service_name"
	if err == nil || err.Error() != wantError {
		t.Fatalf("Route() error = %v, want %q", err, wantError)
	}
	if !reflect.DeepEqual(request, routecontract.Request{}) {
		t.Fatalf("Route() request = %#v, want zero request", request)
	}
}

func TestRouteMapsGetServiceIntelligenceReport(t *testing.T) {
	t.Parallel()

	request, handled, err := Route("get_service_intelligence_report", routecontract.Arguments{
		"workload_id": "workload:sample-service-api",
		"environment": "prod",
	})
	if !handled || err != nil {
		t.Fatalf("Route() = (_, %v, %v), want handled without error", handled, err)
	}
	want := routecontract.Request{
		Method: "GET",
		Path:   "/api/v0/services/workload:sample-service-api/intelligence-report",
		Query:  map[string]string{"environment": "prod", "service_id": "workload:sample-service-api"},
	}
	if !reflect.DeepEqual(request, want) {
		t.Fatalf("Route() = %#v, want %#v", request, want)
	}
}

func TestRouteGetServiceIntelligenceReportRequiresSelector(t *testing.T) {
	t.Parallel()

	request, handled, err := Route("get_service_intelligence_report", routecontract.Arguments{})
	if !handled {
		t.Fatal("Route() handled = false, want true")
	}
	// Assert the message, not just that an error exists. This route builds its
	// own string rather than sharing the %s-formatted one the context and story
	// routes use, so a reword here is caught by nothing else.
	const wantError = "get_service_intelligence_report requires service_id, workload_id, or service_name"
	if err == nil || err.Error() != wantError {
		t.Fatalf("Route() error = %v, want %q", err, wantError)
	}
	if !reflect.DeepEqual(request, routecontract.Request{}) {
		t.Fatalf("Route() request = %#v, want zero request", request)
	}
}

func TestRouteMapsInvestigateService(t *testing.T) {
	t.Parallel()

	request, handled, err := Route("investigate_service", routecontract.Arguments{
		"service_name": "my-svc",
		"environment":  "prod",
		"intent":       "incident",
	})
	if !handled || err != nil {
		t.Fatalf("Route() = (_, %v, %v), want handled without error", handled, err)
	}
	want := routecontract.Request{
		Method: "GET",
		Path:   "/api/v0/investigations/services/my-svc",
		Query:  map[string]string{"environment": "prod", "intent": "incident", "question": ""},
	}
	if !reflect.DeepEqual(request, want) {
		t.Fatalf("Route() = %#v, want %#v", request, want)
	}
}

func TestRouteInvestigateServiceForwardsQualifiedWorkloadSelector(t *testing.T) {
	t.Parallel()

	request, handled, err := Route("investigate_service", routecontract.Arguments{
		"service_name": "workload:payments",
	})
	if !handled || err != nil {
		t.Fatalf("Route() = (_, %v, %v), want handled without error", handled, err)
	}
	if got, want := request.Path, "/api/v0/investigations/services/workload:payments"; got != want {
		t.Fatalf("Route() path = %q, want %q", got, want)
	}
	if got, want := request.Query["service_id"], "workload:payments"; got != want {
		t.Fatalf("Route() query[service_id] = %q, want %q", got, want)
	}
}

// TestRouteForwardsWorkloadHandleWhole proves a workload: handle reaches the
// API ladder intact: the MCP builder forwards it whole as the HTTP path
// segment instead of cutting at the first colon (#5385 lane 2).
func TestRouteForwardsWorkloadHandleWhole(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		tool     string
		args     routecontract.Arguments
		wantPath string
		wantID   string
	}{
		{
			tool:     "get_service_context",
			args:     routecontract.Arguments{"workload_id": "workload:checkout"},
			wantPath: "/api/v0/services/workload:checkout/context",
		},
		{
			tool:     "get_service_story",
			args:     routecontract.Arguments{"workload_id": "workload:checkout"},
			wantPath: "/api/v0/services/workload:checkout/story",
			wantID:   "workload:checkout",
		},
		{
			tool:     "get_service_intelligence_report",
			args:     routecontract.Arguments{"workload_id": "workload:checkout"},
			wantPath: "/api/v0/services/workload:checkout/intelligence-report",
			wantID:   "workload:checkout",
		},
		{
			tool:     "investigate_service",
			args:     routecontract.Arguments{"service_name": "workload:checkout"},
			wantPath: "/api/v0/investigations/services/workload:checkout",
			wantID:   "workload:checkout",
		},
	} {
		request, handled, err := Route(tc.tool, tc.args)
		if !handled || err != nil {
			t.Fatalf("%s: Route() = (_, %v, %v), want handled without error", tc.tool, handled, err)
		}
		if request.Path != tc.wantPath {
			t.Errorf("%s: path = %q, want %q", tc.tool, request.Path, tc.wantPath)
		}
		if got := request.Query["service_id"]; tc.wantID != "" && got != tc.wantID {
			t.Errorf("%s: query[service_id] = %q, want %q", tc.tool, got, tc.wantID)
		}
	}
}

// TestRouteForwardsFullScopedWorkloadIDWhole proves the full scoped node id
// (workload:<repo_id>:<name>) is also forwarded whole, with service_id set,
// so the ladder's exact-id rung matches first (#5385 lane 2).
func TestRouteForwardsFullScopedWorkloadIDWhole(t *testing.T) {
	t.Parallel()

	const fullID = "workload:repository:r_8477a002:checkout"
	request, handled, err := Route("get_service_story", routecontract.Arguments{"workload_id": fullID})
	if !handled || err != nil {
		t.Fatalf("Route() = (_, %v, %v), want handled without error", handled, err)
	}
	if got, want := request.Path, "/api/v0/services/"+fullID+"/story"; got != want {
		t.Fatalf("Route() path = %q, want %q", got, want)
	}
	if got := request.Query["service_id"]; got != fullID {
		t.Fatalf("Route() query[service_id] = %q, want %q", got, fullID)
	}
}

// TestRouteLeavesBareAndNonWorkloadSelectorsUnchanged proves the lane-2
// forwarding touches only workload: selectors: bare names pass through and
// other qualified types keep the old first-colon cut.
func TestRouteLeavesBareAndNonWorkloadSelectorsUnchanged(t *testing.T) {
	t.Parallel()

	request, handled, err := Route("get_service_context", routecontract.Arguments{"workload_id": "checkout"})
	if !handled || err != nil {
		t.Fatalf("Route() = (_, %v, %v), want handled without error", handled, err)
	}
	if got, want := request.Path, "/api/v0/services/checkout/context"; got != want {
		t.Errorf("bare: path = %q, want %q", got, want)
	}

	request, handled, err = Route("get_service_intelligence_report", routecontract.Arguments{"workload_id": "service:checkout"})
	if !handled || err != nil {
		t.Fatalf("Route() = (_, %v, %v), want handled without error", handled, err)
	}
	if got, want := request.Path, "/api/v0/services/checkout/intelligence-report"; got != want {
		t.Errorf("non-workload: path = %q, want %q", got, want)
	}
	if got := request.Query["service_id"]; got != "" {
		t.Errorf("non-workload: query[service_id] = %q, want empty", got)
	}
}
