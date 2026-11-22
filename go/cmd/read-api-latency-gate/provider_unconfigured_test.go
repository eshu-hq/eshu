// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query"
)

const (
	githubLoginRoute    = "GET /api/v0/auth/github/login"
	githubCallbackRoute = "GET /api/v0/auth/github/callback"
	oidcLoginRoute      = "GET /api/v0/auth/oidc/login"
	oidcCallbackRoute   = "GET /api/v0/auth/oidc/callback"
)

// productErrorBody is the JSON envelope the product writes for an error.
func productErrorBody(detail string) string {
	return fmt.Sprintf(`{"detail":%q,"error":"Service Unavailable"}`, detail)
}

// TestProviderUnconfiguredTableIsExactlyTheFourAuthRoutes pins the class to
// the four routes the arbiter named; a fifth route needs a ruling, not a
// quiet table edit.
func TestProviderUnconfiguredTableIsExactlyTheFourAuthRoutes(t *testing.T) {
	want := map[string]bool{
		githubLoginRoute: true, githubCallbackRoute: true,
		oidcLoginRoute: true, oidcCallbackRoute: true,
	}
	if len(providerUnconfiguredRoutes) != len(want) {
		t.Fatalf("providerUnconfiguredRoutes has %d routes, want exactly %d", len(providerUnconfiguredRoutes), len(want))
	}
	for route := range want {
		if providerUnconfiguredRoutes[route] == "" {
			t.Errorf("route %q has no expected body message", route)
		}
	}
}

// TestProviderUnconfiguredMessagesMatchTheProduct ties each table message to
// the product: it mounts the product handlers with no service (what the core
// builds when a provider is not enabled) and requires each route to answer 503
// with the table's message as its detail. A copied literal alone would keep
// passing after the product changed the wording.
func TestProviderUnconfiguredMessagesMatchTheProduct(t *testing.T) {
	mux := http.NewServeMux()
	(&query.GitHubLoginHandler{}).Mount(mux)
	(&query.OIDCLoginHandler{}).Mount(mux)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	for route, message := range providerUnconfiguredRoutes {
		_, path, err := SplitRoute(route)
		if err != nil {
			t.Fatalf("SplitRoute(%q): %v", route, err)
		}
		resp, err := http.Get(srv.URL + path)
		if err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		var body struct {
			Detail string `json:"detail"`
		}
		decodeErr := json.NewDecoder(resp.Body).Decode(&body)
		_ = resp.Body.Close()
		if decodeErr != nil {
			t.Fatalf("%s: decode product body: %v", route, decodeErr)
		}
		if resp.StatusCode != http.StatusServiceUnavailable {
			t.Errorf("%s: product status = %d, want 503", route, resp.StatusCode)
		}
		if body.Detail != message {
			t.Errorf("%s: product detail = %q, table message = %q", route, body.Detail, message)
		}
	}
}

// sweepOne sweeps one route against a handler and returns its result.
func sweepOne(t *testing.T, route string, handler http.HandlerFunc) RouteLatency {
	t.Helper()
	srv := httptest.NewServer(handler)
	defer srv.Close()
	results, err := SweepRoutes(SweepOptions{
		BaseURL:    srv.URL,
		APIKey:     "test-key",
		Routes:     []string{route},
		Iterations: 5,
		Timeout:    5 * time.Second,
	})
	if err != nil {
		t.Fatalf("SweepRoutes(%s): %v", route, err)
	}
	if len(results) != 1 {
		t.Fatalf("len(results) = %d, want 1", len(results))
	}
	return results[0]
}

func respondWith(status int, body string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}
}

func TestSweepRoutesClassifiesProviderUnconfigured(t *testing.T) {
	for route, message := range providerUnconfiguredRoutes {
		got := sweepOne(t, route, respondWith(http.StatusServiceUnavailable, productErrorBody(message)))
		if got.Exercised {
			t.Errorf("%s: Exercised = true, want false for the provider-unconfigured class", route)
		}
		if got.HardFailed {
			t.Errorf("%s: HardFailed = true, want false for the provider-unconfigured class", route)
		}
		if got.NotExercisedClass != notExercisedProviderUnconfigured {
			t.Errorf("%s: NotExercisedClass = %q, want %q", route, got.NotExercisedClass, notExercisedProviderUnconfigured)
		}
		if got.Status != http.StatusServiceUnavailable {
			t.Errorf("%s: Status = %d, want 503", route, got.Status)
		}
		if breaches := EvaluateBudgets([]RouteLatency{got}, RouteBudgets{}); len(breaches) != 0 {
			t.Errorf("%s: class produced breaches %+v, want none", route, breaches)
		}
	}
}

func TestSweepRoutesKeepsProviderRouteFailuresHardFailed(t *testing.T) {
	message := providerUnconfiguredRoutes[githubLoginRoute]
	cases := []struct {
		name    string
		route   string
		handler http.HandlerFunc
	}{
		{"table route answers 500", githubLoginRoute, respondWith(http.StatusInternalServerError, productErrorBody(message))},
		{"table route answers 503 with another body", githubLoginRoute, respondWith(http.StatusServiceUnavailable, productErrorBody("browser session store is unavailable"))},
		{"table route answers 503 with the OTHER provider's message", githubLoginRoute, respondWith(http.StatusServiceUnavailable, productErrorBody(providerUnconfiguredRoutes[oidcLoginRoute]))},
		{"table route answers 503 with a non-JSON body", githubLoginRoute, respondWith(http.StatusServiceUnavailable, message)},
		{"route outside the table answers the same body", "GET /api/v0/auth/profile", respondWith(http.StatusServiceUnavailable, productErrorBody(message))},
		{"route outside the table, sibling auth route", "GET /api/v0/auth/saml/login", respondWith(http.StatusServiceUnavailable, productErrorBody(message))},
		{"mixed samples: later 503 has another body", githubLoginRoute, mixedAfter(3, http.StatusServiceUnavailable, productErrorBody(message), http.StatusServiceUnavailable, productErrorBody("other"))},
		{"mixed samples: later sample is a 200", githubLoginRoute, mixedAfter(3, http.StatusServiceUnavailable, productErrorBody(message), http.StatusOK, `{}`)},
		{"mixed samples: later sample is a 500", githubLoginRoute, mixedAfter(3, http.StatusServiceUnavailable, productErrorBody(message), http.StatusInternalServerError, `{}`)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := sweepOne(t, tc.route, tc.handler)
			if !got.HardFailed {
				t.Fatalf("HardFailed = false, want true (Exercised=%v class=%q status=%d)", got.Exercised, got.NotExercisedClass, got.Status)
			}
			if got.NotExercisedClass != "" {
				t.Fatalf("NotExercisedClass = %q, want empty", got.NotExercisedClass)
			}
			if breaches := EvaluateBudgets([]RouteLatency{got}, RouteBudgets{}); len(breaches) == 0 {
				t.Fatalf("EvaluateBudgets produced no breach for a HardFailed route")
			}
		})
	}
}

// mixedAfter serves the first status/body for the first n requests (warmups
// included) and the second status/body for every request after, so the later
// counted samples differ from the first.
func mixedAfter(n int32, firstStatus int, firstBody string, laterStatus int, laterBody string) http.HandlerFunc {
	var calls int32
	return func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&calls, 1) <= n {
			respondWith(firstStatus, firstBody)(w, r)
			return
		}
		respondWith(laterStatus, laterBody)(w, r)
	}
}

func TestPrintReportNeverPrintsOKForProviderUnconfigured(t *testing.T) {
	results := []RouteLatency{{
		Route: githubLoginRoute, Status: http.StatusServiceUnavailable,
		NotExercisedClass: notExercisedProviderUnconfigured,
	}}
	var out bytes.Buffer
	printReport(&out, results, RouteBudgets{}, RouteWorkBudgets{})
	line := out.String()
	if !strings.Contains(line, "NOT_EXERCISED(503 provider-unconfigured)") {
		t.Fatalf("report %q lacks the distinct provider-unconfigured status", line)
	}
	for _, row := range strings.Split(line, "\n") {
		if strings.Contains(row, githubLoginRoute) && strings.Contains(row, " OK") {
			t.Fatalf("report row %q prints OK for the class", row)
		}
	}
}

func TestPrintProviderUnconfiguredSummaryNamesCountAndRoutes(t *testing.T) {
	results := []RouteLatency{
		{Route: githubLoginRoute, NotExercisedClass: notExercisedProviderUnconfigured},
		{Route: "GET /api/v0/status", Exercised: true},
		{Route: oidcLoginRoute, NotExercisedClass: notExercisedProviderUnconfigured},
	}
	var out bytes.Buffer
	printProviderUnconfiguredSummary(&out, results)
	line := strings.TrimSpace(out.String())
	if strings.Count(line, "\n") != 0 {
		t.Fatalf("summary %q is not exactly one line", line)
	}
	for _, want := range []string{"2 route(s)", githubLoginRoute, oidcLoginRoute, "provider-unconfigured"} {
		if !strings.Contains(line, want) {
			t.Errorf("summary %q lacks %q", line, want)
		}
	}
	if strings.Contains(line, "GET /api/v0/status") {
		t.Errorf("summary %q names an exercised route", line)
	}

	out.Reset()
	printProviderUnconfiguredSummary(&out, []RouteLatency{{Route: "GET /api/v0/status", Exercised: true}})
	if out.Len() != 0 {
		t.Fatalf("summary printed %q with no class routes, want nothing", out.String())
	}
}

func TestLatencyReportRecordsProviderUnconfiguredClassPerRoute(t *testing.T) {
	report := buildLatencyReportRoute(RouteLatency{
		Route: githubLoginRoute, Status: http.StatusServiceUnavailable,
		NotExercisedClass: notExercisedProviderUnconfigured,
	})
	raw, err := json.Marshal(report)
	if err != nil {
		t.Fatalf("marshal report route: %v", err)
	}
	if !strings.Contains(string(raw), `"not_exercised_class":"provider-unconfigured"`) {
		t.Fatalf("report route %s lacks not_exercised_class", raw)
	}
	exercised := buildLatencyReportRoute(RouteLatency{Route: "GET /x", Exercised: true})
	raw, _ = json.Marshal(exercised)
	if strings.Contains(string(raw), "not_exercised_class") {
		t.Fatalf("exercised route %s carries a class", raw)
	}
}
