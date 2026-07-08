// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package kotlin

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter_kotlin "github.com/tree-sitter-grammars/tree-sitter-kotlin/bindings/go"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// TestParseFullTreeWalkCount pins the number of shared.WalkNamed calls
// kotlinFrameworkSemantics performs on a single file, following the pattern
// established by internal/parser/php/walk_count_test.go (#4515 P2b). Before
// the walk-collapse fix (issue #4841, epic #4831), kotlinFrameworkSemantics
// ran four independent full-tree shared.WalkNamed passes: kotlinSpringRoutes,
// kotlinJAXRSRoutes, kotlinMicronautRoutes, and kotlinKtorRoutes. Each pass
// only reads its own node kind ("function_declaration" for the first three,
// "call_expression" for Ktor) and writes to its own route slice, with no
// shared mutable state and no dependency on another pass's output, so they
// collapse into one combined full-tree pass.
//
// The test targets kotlinFrameworkSemantics directly (not the full Parse)
// because walkFile's main w.walkNode traversal and ast_functions.go's
// modifier-annotation scan also call shared.WalkNamed on small bounded
// subtrees unrelated to this merge, which would otherwise dilute the count.
//
// The Kotlin grammar models a trailing-lambda call such as
// `get("/items") { ... }` as an outer call_expression wrapping an inner
// call_expression, so kotlinKtorRouteFromCall's shared.WalkNamed-based lambda
// search (kotlinFirstDescendantByKind, called from kotlinKtorLambdaHandler)
// runs once for the outer node (finds the lambda, 1 call) plus its explicit
// handler-name scan (1 call), and once more for the inner node (no lambda
// sibling to find, 1 call, no match). Those three nested calls are bounded
// per-match Ktor evidence, not full-tree passes, and are unaffected by the
// framework-walk merge, so the fixture's one Ktor route keeps them constant
// across the before/after count (before: 4 full-tree walks + 3 nested = 7;
// after: 1 merged full-tree walk + 3 nested = 4).
//
// This test counts shared.WalkNamed itself (via
// shared.SetWalkNamedHookForTest), not a manually annotated call site, so it
// also fails if a future change reintroduces a per-framework walk.
//
// Not parallel: SetWalkNamedHookForTest installs a process-global hook.
func TestParseFullTreeWalkCount(t *testing.T) {
	rawSource := `package com.example

import org.springframework.web.bind.annotation.GetMapping
import org.springframework.web.bind.annotation.RequestMapping
import org.springframework.web.bind.annotation.RestController
import javax.ws.rs.GET
import javax.ws.rs.Path
import io.micronaut.http.annotation.Controller
import io.micronaut.http.annotation.Get
import io.ktor.server.routing.Routing
import io.ktor.server.routing.get

@RestController
@RequestMapping("/spring")
class SpringController {
    @GetMapping("/items")
    fun items(): List<String> = listOf("a")
}

@Path("/jaxrs")
class JAXRSResource {
    @GET
    @Path("/items")
    fun items(): String = "a"
}

@Controller("/micronaut")
class MicronautController {
    @Get("/items")
    fun items(): String = "a"
}

fun Routing.ktorRoutes() {
    get("/items") {
        handleItems()
    }
}

fun handleItems() {}
`

	dir := t.TempDir()
	path := filepath.Join(dir, "Controllers.kt")
	if err := os.WriteFile(path, []byte(rawSource), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v, want nil", err)
	}

	parser := tree_sitter.NewParser()
	if err := parser.SetLanguage(tree_sitter.NewLanguage(tree_sitter_kotlin.Language())); err != nil {
		t.Fatalf("SetLanguage(Kotlin) error = %v, want nil", err)
	}

	source, err := shared.ReadSource(path)
	if err != nil {
		t.Fatalf("ReadSource() error = %v, want nil", err)
	}
	tree := parser.Parse(source, nil)
	if tree == nil {
		t.Fatal("parser.Parse() returned nil tree")
	}
	defer tree.Close()

	var walkNamedCalls int
	restore := shared.SetWalkNamedHookForTest(func() { walkNamedCalls++ })
	defer restore()

	semantics := kotlinFrameworkSemantics(tree.RootNode(), source)
	if semantics == nil {
		t.Fatal("kotlinFrameworkSemantics() = nil, want non-nil (fixture has Spring/JAX-RS/Micronaut/Ktor routes)")
	}

	const wantWalkNamedCalls = 4
	if walkNamedCalls != wantWalkNamedCalls {
		t.Fatalf("shared.WalkNamed call count = %d, want %d (1 merged framework-route walk + 3 nested Ktor lambda-handler walks)", walkNamedCalls, wantWalkNamedCalls)
	}
}
