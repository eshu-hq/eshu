// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// notExercisedProviderUnconfigured names the one case where a 5xx is not a
// HardFailed breach: an optional sign-in provider the core was not configured
// with. The core answers 503 "<provider> login is unavailable" for those
// routes by design (the handler is built with no service unless
// ESHU_AUTH_GITHUB_ENABLED or ESHU_AUTH_OIDC_ENABLED activates it), the gate
// does not enable providers, and the routes read no seeded data, so there is
// nothing to measure. It is a typed, printed, counted class, never a silent
// pass; see providerUnconfiguredRoutes for the exact scope.
const notExercisedProviderUnconfigured = "provider-unconfigured"

// providerUnconfiguredRoutes is the whole class: exactly four routes, each with
// the one detail message the product answers while its provider is not
// configured. TestProviderUnconfiguredMessagesMatchTheProduct builds the
// product handlers with no service and requires these exact messages, so a
// wording change in the product fails there instead of turning four routes
// into HardFailed breaches (or the table into a stale copy). Do not add a route
// or loosen the match to a prefix or a pattern: a 5xx on any other route, or a
// 503 with any other body, is a real failure.
var providerUnconfiguredRoutes = map[string]string{
	"GET /api/v0/auth/github/login":    "github login is unavailable",
	"GET /api/v0/auth/github/callback": "github login is unavailable",
	"GET /api/v0/auth/oidc/login":      "oidc login is unavailable",
	"GET /api/v0/auth/oidc/callback":   "oidc login is unavailable",
}

// isProviderUnconfiguredResponse reports whether one sample of route is the
// provider-unconfigured answer: the route is in the table, the status is 503,
// and the JSON body's detail is exactly the route's message. Any other
// status, body, or a body that is not JSON is not the class.
func isProviderUnconfiguredResponse(route string, status int, body string) bool {
	message, ok := providerUnconfiguredRoutes[route]
	if !ok || status != http.StatusServiceUnavailable {
		return false
	}
	var envelope struct {
		Detail string `json:"detail"`
	}
	if err := json.Unmarshal([]byte(body), &envelope); err != nil {
		return false
	}
	return envelope.Detail == message
}

// printProviderUnconfiguredSummary prints one line naming how many routes the
// run classified provider-unconfigured and which, so the gap is visible on
// every run. It prints nothing when no route was classified.
func printProviderUnconfiguredSummary(w io.Writer, results []RouteLatency) {
	var routes []string
	for _, r := range results {
		if r.NotExercisedClass == notExercisedProviderUnconfigured {
			routes = append(routes, r.Route)
		}
	}
	if len(routes) == 0 {
		return
	}
	_, _ = fmt.Fprintf(w, "read-api-latency-gate: %d route(s) not exercised, %s (503 by design: the provider is not configured on the core): %s\n",
		len(routes), notExercisedProviderUnconfigured, strings.Join(routes, ", "))
}
