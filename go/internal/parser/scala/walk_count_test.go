// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package scala

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
	tree_sitter_scala "github.com/tree-sitter/tree-sitter-scala/bindings/go"
)

// TestParseFullTreeWalkCount pins the number of shared.WalkNamed full-tree
// traversals Parse performs on a single file, following the pattern
// established by internal/parser/php/walk_count_test.go (#4515 P2b) and
// internal/parser/csharp/equivalence_dump_test.go (#4869). Before the
// walk-collapse fix (issue #4841, epic #4831), Parse ran a dedicated
// full-tree shared.WalkNamed pass in scalaHTTP4sRouteEntries, gated on the
// imports bucket the main walk had already populated. The HTTP4s
// call_expression check reads a node kind ("call_expression") the main walk
// already visits and does not depend on any state the main walk collects
// later, so it collapses into the main pass: buildScalaFrameworkSemantics
// applies the import gate to routes the main pass already gathered instead
// of re-walking the tree to collect them.
//
// scalaCollectTypeContracts (dead_code_roots.go) is a genuine pre-pass: it
// seeds traitMethods/typeTraits that appendFunctionWithContext reads during
// the main walk, so it must still run first and is not counted against the
// merge target here; both walks are always present.
//
// This test counts shared.WalkNamed itself (via
// shared.SetWalkNamedHookForTest), not a manually annotated call site, so it
// also fails if a future change reintroduces the route walk (or any other
// full pass) as a plain shared.WalkNamed call anywhere in the Scala package.
//
// Not parallel: SetWalkNamedHookForTest installs a process-global hook.
func TestParseFullTreeWalkCount(t *testing.T) {
	source := `package com.example

import org.http4s.HttpRoutes
import org.http4s.dsl.io._

trait Greeter {
  def greet(): String
}

class GreeterService extends Greeter {
  override def greet(): String = "hi"
}

object Routes {
  def greetHandler: String = "hi"
  val routes = HttpRoutes.of[IO] {
    case GET -> Root / "items" => greetHandler
  }
}
`

	dir := t.TempDir()
	path := filepath.Join(dir, "Routes.scala")
	if err := os.WriteFile(path, []byte(source), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v, want nil", err)
	}

	parser := tree_sitter.NewParser()
	if err := parser.SetLanguage(tree_sitter.NewLanguage(tree_sitter_scala.Language())); err != nil {
		t.Fatalf("SetLanguage(Scala) error = %v, want nil", err)
	}

	var walkNamedCalls int
	restore := shared.SetWalkNamedHookForTest(func() { walkNamedCalls++ })
	defer restore()

	payload, err := Parse(path, false, shared.Options{}, parser)
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}
	semantics, _ := payload["framework_semantics"].(map[string]any)
	if semantics == nil {
		t.Fatal("payload[\"framework_semantics\"] = nil, want non-nil (fixture imports HTTP4s and has one route)")
	}

	const wantWalkNamedCalls = 2
	if walkNamedCalls != wantWalkNamedCalls {
		t.Fatalf("shared.WalkNamed call count = %d, want %d (type-contracts pre-pass + merged main/HTTP4s walk)", walkNamedCalls, wantWalkNamedCalls)
	}
}
