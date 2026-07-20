// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package example

// #5361 route query-proof matrix: cribs the Spring MVC controller shape
// proven by internal/parser/java_kotlin_spring_route_semantics_test.go.
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
}
