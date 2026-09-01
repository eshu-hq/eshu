// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package java_test

import (
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser"
	"github.com/eshu-hq/eshu/go/internal/parser/parsertest"
)

// TestDefaultEngineParsePathJavaComprehensiveRouteFixtures proves the Java
// parser extracts Spring, JAX-RS, and Micronaut route_entries from real
// on-disk annotated source under the shared java_comprehensive ecosystem
// fixture, not only from synthetic t.TempDir() sources (#5333). This is the
// same framework_semantics shape the HANDLES_ROUTE reducer and
// trace_route_callers query surface consume downstream.
func TestDefaultEngineParsePathJavaComprehensiveRouteFixtures(t *testing.T) {
	t.Parallel()

	repoRoot := javaFixturePath(t, "ecosystems", "java_comprehensive")
	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("parser.DefaultEngine() error = %v, want nil", err)
	}

	t.Run("spring", func(t *testing.T) {
		t.Parallel()
		sourcePath := filepath.Join(repoRoot, "routes", "CatalogController.java")
		got, err := engine.ParsePath(repoRoot, sourcePath, false, parser.Options{})
		if err != nil {
			t.Fatalf("ParsePath(%q) error = %v, want nil", sourcePath, err)
		}
		parsertest.AssertFrameworksEqual(t, got, "spring")
		parsertest.AssertNestedRouteEntriesEqual(t, got, "spring", []map[string]string{
			{"method": "GET", "path": "/api/catalog/items/{id}", "handler": "show"},
			{"method": "POST", "path": "/api/catalog/items", "handler": "create"},
		})
	})

	t.Run("jax_rs", func(t *testing.T) {
		t.Parallel()
		sourcePath := filepath.Join(repoRoot, "routes", "WidgetResource.java")
		got, err := engine.ParsePath(repoRoot, sourcePath, false, parser.Options{})
		if err != nil {
			t.Fatalf("ParsePath(%q) error = %v, want nil", sourcePath, err)
		}
		parsertest.AssertFrameworksEqual(t, got, "jax_rs")
		parsertest.AssertNestedRouteEntriesEqual(t, got, "jax_rs", []map[string]string{
			{"method": "GET", "path": "/widgets/{id}", "handler": "get"},
		})
	})

	t.Run("micronaut", func(t *testing.T) {
		t.Parallel()
		sourcePath := filepath.Join(repoRoot, "routes", "PingController.java")
		got, err := engine.ParsePath(repoRoot, sourcePath, false, parser.Options{})
		if err != nil {
			t.Fatalf("ParsePath(%q) error = %v, want nil", sourcePath, err)
		}
		parsertest.AssertFrameworksEqual(t, got, "micronaut")
		parsertest.AssertNestedRouteEntriesEqual(t, got, "micronaut", []map[string]string{
			{"method": "GET", "path": "/ping", "handler": "ping"},
		})
	})
}
