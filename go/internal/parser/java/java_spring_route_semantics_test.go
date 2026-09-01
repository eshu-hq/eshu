// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package java_test

import (
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser"
	"github.com/eshu-hq/eshu/go/internal/parser/parsertest"
)

func TestDefaultEngineParsePathJavaSpringRouteSemantics(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "src/main/java/example/CatalogController.java")
	writeJavaTestFile(t, filePath, `package example;

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

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("parser.DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, parser.Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	parsertest.AssertFrameworksEqual(t, got, "spring")
	parsertest.AssertNestedStringSliceEqual(t, got, "spring", "route_methods", []string{"GET", "POST"})
	parsertest.AssertNestedStringSliceEqual(t, got, "spring", "route_paths", []string{"/api/items/{id}", "/api/items"})
	parsertest.AssertNestedRouteEntriesEqual(t, got, "spring", []map[string]string{
		{"method": "GET", "path": "/api/items/{id}", "handler": "show"},
		{"method": "POST", "path": "/api/items", "handler": "create"},
	})
}

func TestDefaultEngineParsePathJavaJAXRSAndMicronautRouteSemantics(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "src/main/java/example/JvmRoutes.java")
	writeJavaTestFile(t, filePath, `package example;

import io.micronaut.http.annotation.Controller;
import io.micronaut.http.annotation.Get;
import io.micronaut.http.annotation.Post;
import jakarta.ws.rs.DELETE;
import jakarta.ws.rs.GET;
import jakarta.ws.rs.POST;
import jakarta.ws.rs.Path;

@Path("/jax")
public class JvmRoutes {
    @GET
    @Path("/items/{id}")
    public Item show(String id) {
        return new Item(id);
    }

    @POST
    @Path("/items")
    public Item create(Item item) {
        return item;
    }

    @DELETE
    @Path(dynamicPath)
    public Item skippedJaxRs() {
        return null;
    }
}

@Controller("/mn")
class MicronautRoutes {
    @Get("/health")
    public String health() {
        return "ok";
    }

    @Post(uri = "/jobs")
    public String createJob() {
        return "ok";
    }

    @Get(dynamicPath)
    public String skippedMicronaut() {
        return "skip";
    }
}

@Path(dynamicBase)
class DynamicJaxRsBase {
    @GET
    @Path("/leak")
    public String skippedDynamicBase() {
        return "skip";
    }
}
`)

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("parser.DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, parser.Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	parsertest.AssertFrameworksEqual(t, got, "jax_rs", "micronaut")
	parsertest.AssertNestedStringSliceEqual(t, got, "jax_rs", "route_methods", []string{"GET", "POST"})
	parsertest.AssertNestedStringSliceEqual(t, got, "jax_rs", "route_paths", []string{"/jax/items/{id}", "/jax/items"})
	parsertest.AssertNestedRouteEntriesEqual(t, got, "jax_rs", []map[string]string{
		{"method": "GET", "path": "/jax/items/{id}", "handler": "show"},
		{"method": "POST", "path": "/jax/items", "handler": "create"},
	})
	parsertest.AssertNestedStringSliceEqual(t, got, "micronaut", "route_methods", []string{"GET", "POST"})
	parsertest.AssertNestedStringSliceEqual(t, got, "micronaut", "route_paths", []string{"/mn/health", "/mn/jobs"})
	parsertest.AssertNestedRouteEntriesEqual(t, got, "micronaut", []map[string]string{
		{"method": "GET", "path": "/mn/health", "handler": "health"},
		{"method": "POST", "path": "/mn/jobs", "handler": "createJob"},
	})
}
