// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import "testing"

func TestResolveRouteMapsKubernetesCorrelationsToBoundedQuery(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("list_kubernetes_correlations", map[string]any{
		"after_correlation_id": "kubernetes-correlation-1",
		"scope_id":             "kubernetes-live://cluster-prod",
		"cluster_id":           "cluster-prod",
		"workload_object_id":   "deployment/payments/checkout",
		"namespace":            "payments",
		"image_ref":            "registry.example.com/checkout@sha256:abc",
		"source_digest":        "sha256:abc",
		"outcome":              "exact",
		"drift_kind":           "in_sync",
		"limit":                float64(25),
	})
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	if got, want := route.method, "GET"; got != want {
		t.Fatalf("route.method = %q, want %q", got, want)
	}
	if got, want := route.path, "/api/v0/kubernetes/correlations"; got != want {
		t.Fatalf("route.path = %q, want %q", got, want)
	}
	for key, want := range map[string]string{
		"after_correlation_id": "kubernetes-correlation-1",
		"scope_id":             "kubernetes-live://cluster-prod",
		"cluster_id":           "cluster-prod",
		"workload_object_id":   "deployment/payments/checkout",
		"namespace":            "payments",
		"image_ref":            "registry.example.com/checkout@sha256:abc",
		"source_digest":        "sha256:abc",
		"outcome":              "exact",
		"drift_kind":           "in_sync",
		"limit":                "25",
	} {
		if got := route.query[key]; got != want {
			t.Fatalf("route.query[%s] = %#v, want %#v", key, got, want)
		}
	}
}
