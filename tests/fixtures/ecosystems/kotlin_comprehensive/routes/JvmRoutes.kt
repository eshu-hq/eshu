// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package example

// #5395 route query-proof matrix: cribs the JAX-RS/Micronaut/Ktor shape proven
// by internal/parser/java_kotlin_spring_route_semantics_test.go's
// TestDefaultEngineParsePathKotlinJVMRouteSemantics fixture, so the parser
// output this file produces is guaranteed to emit exact route_entries for all
// three frameworks in one combined walk.
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
