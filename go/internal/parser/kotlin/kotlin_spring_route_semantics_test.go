// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package kotlin_test

import (
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser"
	"github.com/eshu-hq/eshu/go/internal/parser/parsertest"
)

func TestDefaultEngineParsePathKotlinSpringRouteSemantics(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "src/main/kotlin/example/Routes.kt")
	writeKotlinTestFile(t, filePath, `package example

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
	parsertest.AssertNestedStringSliceEqual(t, got, "spring", "route_paths", []string{"/api/health/{id}", "/api/jobs"})
	parsertest.AssertNestedRouteEntriesEqual(t, got, "spring", []map[string]string{
		{"method": "GET", "path": "/api/health/{id}", "handler": "health"},
		{"method": "POST", "path": "/api/jobs", "handler": "create"},
	})
}

func TestDefaultEngineParsePathKotlinJVMRouteSemantics(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	filePath := filepath.Join(repoRoot, "src/main/kotlin/example/JvmRoutes.kt")
	writeKotlinTestFile(t, filePath, `package example

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

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("parser.DefaultEngine() error = %v, want nil", err)
	}

	got, err := engine.ParsePath(repoRoot, filePath, false, parser.Options{})
	if err != nil {
		t.Fatalf("ParsePath() error = %v, want nil", err)
	}

	parsertest.AssertFrameworksEqual(t, got, "jax_rs", "micronaut", "ktor")
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
	parsertest.AssertNestedStringSliceEqual(t, got, "ktor", "route_methods", []string{"GET"})
	parsertest.AssertNestedStringSliceEqual(t, got, "ktor", "route_paths", []string{"/ktor/ping"})
	parsertest.AssertNestedRouteEntriesEqual(t, got, "ktor", []map[string]string{
		{"method": "GET", "path": "/ktor/ping", "handler": "ping"},
	})
}
