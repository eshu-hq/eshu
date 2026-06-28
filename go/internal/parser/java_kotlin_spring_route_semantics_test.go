// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package parser

import (
	"path/filepath"
	"testing"
)

func TestDefaultEngineParsePathJavaSpringRouteSemantics(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "src/main/java/example/CatalogController.java")
	writeTestFile(t, filePath, `package example;

import org.springframework.web.bind.annotation.GetMapping;
import org.springframework.web.bind.annotation.PathVariable;
import org.springframework.web.bind.annotation.PostMapping;
import org.springframework.web.bind.annotation.RequestMapping;
import org.springframework.web.bind.annotation.RestController;

@RestController
@RequestMapping("/api")
public class CatalogController {
    @GetMapping("/items/{id}")
    public Item show(@PathVariable String id) {
        return new Item(id);
    }

    @PostMapping(path = "/items")
    public Item create(Item item) {
        return item;
    }

    @GetMapping(dynamicPath)
    public Item dynamicRoute() {
        return null;
    }

    public Item helper() {
        return null;
    }
}
`)

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	assertFrameworksEqual(t, got, "spring")
	assertNestedStringSliceEqual(t, got, "spring", "route_methods", []string{"GET", "POST"})
	assertNestedStringSliceEqual(t, got, "spring", "route_paths", []string{"/api/items/{id}", "/api/items"})
	assertNestedRouteEntriesEqual(t, got, "spring", []map[string]string{
		{"method": "GET", "path": "/api/items/{id}", "handler": "show"},
		{"method": "POST", "path": "/api/items", "handler": "create"},
	})
}

func TestDefaultEngineParsePathKotlinSpringRouteSemantics(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "src/main/kotlin/example/Routes.kt")
	writeTestFile(t, filePath, `package example

import org.springframework.web.bind.annotation.GetMapping
import org.springframework.web.bind.annotation.PostMapping
import org.springframework.web.bind.annotation.RequestMapping
import org.springframework.web.bind.annotation.RestController

@RestController
@RequestMapping("/api")
class Routes {
    @GetMapping("/health/{id}")
    fun health(): String = "ok"

    @PostMapping(path = ["/jobs"])
    fun create(): String = "ok"

    @GetMapping(dynamicPath)
    fun dynamicRoute(): String = "skip"

    fun helper(): String = "unused"
}
`)

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	assertFrameworksEqual(t, got, "spring")
	assertNestedStringSliceEqual(t, got, "spring", "route_methods", []string{"GET", "POST"})
	assertNestedStringSliceEqual(t, got, "spring", "route_paths", []string{"/api/health/{id}", "/api/jobs"})
	assertNestedRouteEntriesEqual(t, got, "spring", []map[string]string{
		{"method": "GET", "path": "/api/health/{id}", "handler": "health"},
		{"method": "POST", "path": "/api/jobs", "handler": "create"},
	})
}
