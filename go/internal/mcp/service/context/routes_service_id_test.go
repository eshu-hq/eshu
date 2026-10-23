// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package servicecontexttools

import (
	"reflect"
	"testing"

	routecontract "github.com/eshu-hq/eshu/go/internal/mcp/contract/route"
)

// TestRouteGetServiceStoryAcceptsServiceIDAlone covers the follow-up call an
// ambiguous answer asks for: the exact candidate id passed as service_id with
// no other selector.
func TestRouteGetServiceStoryAcceptsServiceIDAlone(t *testing.T) {
	t.Parallel()

	const fullID = "workload:repository:r_sample:checkout-service"
	cases := []struct {
		name string
		args routecontract.Arguments
		want routecontract.Request
	}{
		{
			name: "full scoped id",
			args: routecontract.Arguments{"service_id": fullID},
			want: routecontract.Request{
				Method: "GET",
				Path:   "/api/v0/services/" + fullID + "/story",
				Query:  map[string]string{"service_id": fullID},
			},
		},
		{
			name: "handle",
			args: routecontract.Arguments{"service_id": "workload:checkout-service"},
			want: routecontract.Request{
				Method: "GET",
				Path:   "/api/v0/services/workload:checkout-service/story",
				Query:  map[string]string{"service_id": "workload:checkout-service"},
			},
		},
		{
			name: "surrounding space is trimmed",
			args: routecontract.Arguments{"service_id": "  " + fullID + "  "},
			want: routecontract.Request{
				Method: "GET",
				Path:   "/api/v0/services/" + fullID + "/story",
				Query:  map[string]string{"service_id": fullID},
			},
		},
		{
			name: "environment and repo ride along",
			args: routecontract.Arguments{"service_id": fullID, "environment": "staging", "repo": "repository:r_sample"},
			want: routecontract.Request{
				Method: "GET",
				Path:   "/api/v0/services/" + fullID + "/story",
				Query:  map[string]string{"service_id": fullID, "environment": "staging", "repo": "repository:r_sample"},
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			request, handled, err := Route("get_service_story", tc.args)
			if !handled || err != nil {
				t.Fatalf("Route() = (_, %v, %v), want handled without error", handled, err)
			}
			if !reflect.DeepEqual(request, tc.want) {
				t.Fatalf("Route() = %#v, want %#v", request, tc.want)
			}
		})
	}
}

// TestRouteGetServiceStoryServiceIDWinsOverOtherSelectors pins the precedence
// service_id, then workload_id, then service_name. The exact id is the most
// specific selector, so adding it to an ambiguous name call resolves the call.
func TestRouteGetServiceStoryServiceIDWinsOverOtherSelectors(t *testing.T) {
	t.Parallel()

	const fullID = "workload:repository:r_sample:checkout-service"
	request, handled, err := Route("get_service_story", routecontract.Arguments{
		"service_id":   fullID,
		"workload_id":  "checkout-service",
		"service_name": "checkout-service",
	})
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

// TestRouteGetServiceStoryBlankServiceIDFallsBack keeps a blank service_id from
// shadowing the other selectors.
func TestRouteGetServiceStoryBlankServiceIDFallsBack(t *testing.T) {
	t.Parallel()

	request, handled, err := Route("get_service_story", routecontract.Arguments{
		"service_id":   "   ",
		"service_name": "checkout-service",
	})
	if !handled || err != nil {
		t.Fatalf("Route() = (_, %v, %v), want handled without error", handled, err)
	}
	if got, want := request.Path, "/api/v0/services/checkout-service/story"; got != want {
		t.Fatalf("Route() path = %q, want %q", got, want)
	}
	if _, ok := request.Query["service_id"]; ok {
		t.Fatalf("Route() query = %#v, want no service_id for a blank value", request.Query)
	}
}

// TestRouteGetServiceStoryServiceIDWithoutWorkloadPrefix documents that a
// service_id that is not a workload handle or scoped id is only a name: it is
// the path segment and is not forwarded as the service_id query.
func TestRouteGetServiceStoryServiceIDWithoutWorkloadPrefix(t *testing.T) {
	t.Parallel()

	request, handled, err := Route("get_service_story", routecontract.Arguments{"service_id": "checkout-service"})
	if !handled || err != nil {
		t.Fatalf("Route() = (_, %v, %v), want handled without error", handled, err)
	}
	if got, want := request.Path, "/api/v0/services/checkout-service/story"; got != want {
		t.Fatalf("Route() path = %q, want %q", got, want)
	}
	if _, ok := request.Query["service_id"]; ok {
		t.Fatalf("Route() query = %#v, want no service_id for a bare name", request.Query)
	}
}

// TestRouteReportAndInvestigationAcceptServiceID covers the other two tools
// that share the ambiguous answer: service_id alone selects the service, and it
// is forwarded as the service_id query so the handler matches the id exactly.
func TestRouteReportAndInvestigationAcceptServiceID(t *testing.T) {
	t.Parallel()

	const fullID = "workload:repository:r_sample:checkout-service"
	cases := []struct {
		tool       string
		args       routecontract.Arguments
		wantPath   string
		wantQuery  map[string]string
		wantAbsent []string
	}{
		{
			tool:      "get_service_intelligence_report",
			args:      routecontract.Arguments{"service_id": fullID, "environment": "staging"},
			wantPath:  "/api/v0/services/" + fullID + "/intelligence-report",
			wantQuery: map[string]string{"service_id": fullID, "environment": "staging"},
		},
		{
			tool:      "investigate_service",
			args:      routecontract.Arguments{"service_id": fullID, "environment": "staging"},
			wantPath:  "/api/v0/investigations/services/" + fullID,
			wantQuery: map[string]string{"service_id": fullID, "environment": "staging"},
		},
		{
			tool:      "get_service_intelligence_report",
			args:      routecontract.Arguments{"service_id": fullID, "workload_id": "checkout-service", "service_name": "checkout-service"},
			wantPath:  "/api/v0/services/" + fullID + "/intelligence-report",
			wantQuery: map[string]string{"service_id": fullID},
		},
		{
			tool:      "investigate_service",
			args:      routecontract.Arguments{"service_id": fullID, "service_name": "checkout-service"},
			wantPath:  "/api/v0/investigations/services/" + fullID,
			wantQuery: map[string]string{"service_id": fullID},
		},
		{
			tool:       "investigate_service",
			args:       routecontract.Arguments{"service_id": "  ", "service_name": "checkout-service"},
			wantPath:   "/api/v0/investigations/services/checkout-service",
			wantAbsent: []string{"service_id"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.tool, func(t *testing.T) {
			t.Parallel()
			request, handled, err := Route(tc.tool, tc.args)
			if !handled || err != nil {
				t.Fatalf("Route() = (_, %v, %v), want handled without error", handled, err)
			}
			if request.Path != tc.wantPath {
				t.Fatalf("Route() path = %q, want %q", request.Path, tc.wantPath)
			}
			for key, want := range tc.wantQuery {
				if got := request.Query[key]; got != want {
					t.Errorf("Route() query[%s] = %q, want %q", key, got, want)
				}
			}
			for _, key := range tc.wantAbsent {
				if _, ok := request.Query[key]; ok {
					t.Errorf("Route() query has %q, want it absent", key)
				}
			}
		})
	}
}

// TestRouteInvestigateServiceRequiresASelector replaces the old "does not
// require a selector" behavior: the schema no longer marks service_name
// required (service_id may stand alone), so the route names the accepted
// selectors itself.
func TestRouteInvestigateServiceRequiresASelector(t *testing.T) {
	t.Parallel()

	request, handled, err := Route("investigate_service", routecontract.Arguments{})
	if !handled {
		t.Fatal("Route() handled = false, want true")
	}
	const wantError = "investigate_service requires service_id or service_name"
	if err == nil || err.Error() != wantError {
		t.Fatalf("Route() error = %v, want %q", err, wantError)
	}
	if !reflect.DeepEqual(request, routecontract.Request{}) {
		t.Fatalf("Route() request = %#v, want zero request", request)
	}
}
