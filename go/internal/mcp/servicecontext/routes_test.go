// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package servicecontexttools

import (
	"reflect"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/mcp/routecontract"
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
		Path:   "/api/v0/services/sample-service-api/context",
		Query:  map[string]string{"environment": "prod"},
	}
	if !reflect.DeepEqual(request, want) {
		t.Fatalf("Route() = %#v, want %#v", request, want)
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
		Path:   "/api/v0/services/sample-service-api/story",
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

func TestRouteGetServiceStoryRequiresWorkloadIDOrServiceName(t *testing.T) {
	t.Parallel()

	request, handled, err := Route("get_service_story", routecontract.Arguments{})
	if !handled {
		t.Fatal("Route() handled = false, want true")
	}
	const wantError = "get_service_story requires workload_id or service_name"
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
		Path:   "/api/v0/services/sample-service-api/intelligence-report",
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
	const wantError = "get_service_intelligence_report requires workload_id or service_name"
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

func TestRouteInvestigateServiceDoesNotRequireSelector(t *testing.T) {
	t.Parallel()

	request, handled, err := Route("investigate_service", routecontract.Arguments{})
	if !handled || err != nil {
		t.Fatalf("Route() = (_, %v, %v), want handled without error even with no service_name", handled, err)
	}
	if got, want := request.Path, "/api/v0/investigations/services/"; got != want {
		t.Fatalf("Route() path = %q, want %q", got, want)
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
	if got, want := request.Path, "/api/v0/investigations/services/payments"; got != want {
		t.Fatalf("Route() path = %q, want %q", got, want)
	}
	if got, want := request.Query["service_id"], "workload:payments"; got != want {
		t.Fatalf("Route() query[service_id] = %q, want %q", got, want)
	}
}
