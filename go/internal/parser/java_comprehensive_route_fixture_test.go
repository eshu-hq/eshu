// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package parser

import (
	"path/filepath"
	"testing"
)

// TestDefaultEngineParsePathJavaComprehensiveRouteFixtures proves the Java
// parser extracts Spring, JAX-RS, and Micronaut route_entries from real
// on-disk annotated source under the shared java_comprehensive ecosystem
// fixture, not only from synthetic t.TempDir() sources (#5333). This is the
// same framework_semantics shape the HANDLES_ROUTE reducer and
// trace_route_callers query surface consume downstream.
func TestDefaultEngineParsePathJavaComprehensiveRouteFixtures(t *testing.T) {
	t.Parallel()

	repoRoot := repoFixturePath("ecosystems", "java_comprehensive")
	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	t.Run("spring", func(t *testing.T) {
		t.Parallel()
		sourcePath := filepath.Join(repoRoot, "routes", "CatalogController.java")
		got, err := engine.ParsePath(repoRoot, sourcePath, false, Options{})
		if err != nil {
			t.Fatalf("ParsePath(%q) error = %v, want nil", sourcePath, err)
		}
		assertFrameworksEqual(t, got, "spring")
		assertNestedRouteEntriesEqual(t, got, "spring", []map[string]string{
			{"method": "GET", "path": "/api/catalog/items/{id}", "handler": "show"},
			{"method": "POST", "path": "/api/catalog/items", "handler": "create"},
		})
	})

	t.Run("jax_rs", func(t *testing.T) {
		t.Parallel()
		sourcePath := filepath.Join(repoRoot, "routes", "WidgetResource.java")
		got, err := engine.ParsePath(repoRoot, sourcePath, false, Options{})
		if err != nil {
			t.Fatalf("ParsePath(%q) error = %v, want nil", sourcePath, err)
		}
		assertFrameworksEqual(t, got, "jax_rs")
		assertNestedRouteEntriesEqual(t, got, "jax_rs", []map[string]string{
			{"method": "GET", "path": "/widgets/{id}", "handler": "get"},
		})
	})

	t.Run("micronaut", func(t *testing.T) {
		t.Parallel()
		sourcePath := filepath.Join(repoRoot, "routes", "PingController.java")
		got, err := engine.ParsePath(repoRoot, sourcePath, false, Options{})
		if err != nil {
			t.Fatalf("ParsePath(%q) error = %v, want nil", sourcePath, err)
		}
		assertFrameworksEqual(t, got, "micronaut")
		assertNestedRouteEntriesEqual(t, got, "micronaut", []map[string]string{
			{"method": "GET", "path": "/ping", "handler": "ping"},
		})
	})
}
