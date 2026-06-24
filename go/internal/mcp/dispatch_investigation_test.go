// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import "testing"

func TestResolveRouteMapsInvestigateService(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("investigate_service", map[string]any{
		"service_name": "payments-api",
		"repo":         "payments-repo",
		"environment":  "prod",
		"intent":       "runbook",
		"question":     "explain owners",
	})
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	if got, want := route.method, "GET"; got != want {
		t.Fatalf("method = %q, want %q", got, want)
	}
	if got, want := route.path, "/api/v0/investigations/services/payments-api"; got != want {
		t.Fatalf("path = %q, want %q", got, want)
	}
	if got, want := route.query["environment"], "prod"; got != want {
		t.Fatalf("environment query = %q, want %q", got, want)
	}
	if got, want := route.query["repo"], "payments-repo"; got != want {
		t.Fatalf("repo query = %q, want %q", got, want)
	}
	if got, want := route.query["intent"], "runbook"; got != want {
		t.Fatalf("intent query = %q, want %q", got, want)
	}
	if got, want := route.query["question"], "explain owners"; got != want {
		t.Fatalf("question query = %q, want %q", got, want)
	}
}

func TestResolveRouteMapsQualifiedInvestigateServiceSelector(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("investigate_service", map[string]any{
		"service_name": "workload:payments-api",
	})
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	if got, want := route.path, "/api/v0/investigations/services/payments-api"; got != want {
		t.Fatalf("path = %q, want %q", got, want)
	}
	if got, want := route.query["service_id"], "workload:payments-api"; got != want {
		t.Fatalf("service_id query = %q, want %q", got, want)
	}
}
