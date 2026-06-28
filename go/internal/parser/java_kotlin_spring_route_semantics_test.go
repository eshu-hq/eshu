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

func TestDefaultEngineParsePathJavaJAXRSAndMicronautRouteSemantics(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "src/main/java/example/JvmRoutes.java")
	writeTestFile(t, filePath, `package example;

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

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	assertFrameworksEqual(t, got, "jax_rs", "micronaut")
	assertNestedStringSliceEqual(t, got, "jax_rs", "route_methods", []string{"GET", "POST"})
	assertNestedStringSliceEqual(t, got, "jax_rs", "route_paths", []string{"/jax/items/{id}", "/jax/items"})
	assertNestedRouteEntriesEqual(t, got, "jax_rs", []map[string]string{
		{"method": "GET", "path": "/jax/items/{id}", "handler": "show"},
		{"method": "POST", "path": "/jax/items", "handler": "create"},
	})
	assertNestedStringSliceEqual(t, got, "micronaut", "route_methods", []string{"GET", "POST"})
	assertNestedStringSliceEqual(t, got, "micronaut", "route_paths", []string{"/mn/health", "/mn/jobs"})
	assertNestedRouteEntriesEqual(t, got, "micronaut", []map[string]string{
		{"method": "GET", "path": "/mn/health", "handler": "health"},
		{"method": "POST", "path": "/mn/jobs", "handler": "createJob"},
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

func TestDefaultEngineParsePathKotlinJVMRouteSemantics(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "src/main/kotlin/example/JvmRoutes.kt")
	writeTestFile(t, filePath, `package example

import io.ktor.server.application.Application
import io.ktor.server.routing.get
import io.ktor.server.routing.post
import io.ktor.server.routing.routing
import io.micronaut.http.annotation.Controller
import io.micronaut.http.annotation.Get
import io.micronaut.http.annotation.Post
import jakarta.ws.rs.DELETE
import jakarta.ws.rs.GET
import jakarta.ws.rs.POST
import jakarta.ws.rs.Path

@Path("/jax")
class JaxRsRoutes {
    @GET
    @Path("/items/{id}")
    fun show(): String = "ok"

    @POST
    @Path("/items")
    fun create(): String = "ok"

    @DELETE
    @Path(dynamicPath)
    fun skippedJaxRs(): String = "skip"
}

@Controller("/mn")
class MicronautRoutes {
    @Get("/health")
    fun health(): String = "ok"

    @Post(uri = "/jobs")
    fun createJob(): String = "ok"

    @Get(dynamicPath)
    fun skippedMicronaut(): String = "skip"
}

@Path(dynamicBase)
class DynamicJaxRsBase {
    @GET
    @Path("/leak")
    fun skippedDynamicBase(): String = "skip"
}

fun Application.module() {
    routing {
        get("/ktor/ping") {
            ping()
        }
        post(dynamicPath) {
            skippedKtor()
        }
        get("/ktor/inline") {
            call.respondText("ok")
        }
    }
}

fun ping(): String = "ok"
fun skippedKtor(): String = "skip"
`)

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	assertFrameworksEqual(t, got, "jax_rs", "micronaut", "ktor")
	assertNestedStringSliceEqual(t, got, "jax_rs", "route_methods", []string{"GET", "POST"})
	assertNestedStringSliceEqual(t, got, "jax_rs", "route_paths", []string{"/jax/items/{id}", "/jax/items"})
	assertNestedRouteEntriesEqual(t, got, "jax_rs", []map[string]string{
		{"method": "GET", "path": "/jax/items/{id}", "handler": "show"},
		{"method": "POST", "path": "/jax/items", "handler": "create"},
	})
	assertNestedStringSliceEqual(t, got, "micronaut", "route_methods", []string{"GET", "POST"})
	assertNestedStringSliceEqual(t, got, "micronaut", "route_paths", []string{"/mn/health", "/mn/jobs"})
	assertNestedRouteEntriesEqual(t, got, "micronaut", []map[string]string{
		{"method": "GET", "path": "/mn/health", "handler": "health"},
		{"method": "POST", "path": "/mn/jobs", "handler": "createJob"},
	})
	assertNestedStringSliceEqual(t, got, "ktor", "route_methods", []string{"GET"})
	assertNestedStringSliceEqual(t, got, "ktor", "route_paths", []string{"/ktor/ping"})
	assertNestedRouteEntriesEqual(t, got, "ktor", []map[string]string{
		{"method": "GET", "path": "/ktor/ping", "handler": "ping"},
	})
}
