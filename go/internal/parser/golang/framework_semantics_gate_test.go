// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package golang

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
	tree_sitter_go "github.com/tree-sitter/tree-sitter-go/bindings/go"
)

// goNoFrameworkImportFixture imports neither net/http nor any of the
// goRouteFrameworkConstructors framework packages. Profiling (issue #5219,
// kubernetes corpus: 17,490 files) showed goHTTPFrameworkSemantics can only
// ever produce output when one of those imports is present, yet it
// unconditionally builds a parent-lookup and walks the whole tree for every
// Go file. 94.7% of files in that corpus import none of them.
const goNoFrameworkImportFixture = `package test

import (
	"fmt"
	"strings"
)

func Greet(name string) string {
	return fmt.Sprintf("hello %s", strings.ToUpper(name))
}
`

// goNetHTTPFrameworkImportFixture registers a net/http ServeMux route, the
// minimal shape goHTTPFrameworkSemantics recognizes as framework evidence.
const goNetHTTPFrameworkImportFixture = `package test

import "net/http"

func Register() {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /users", listUsers)
}

func listUsers(w http.ResponseWriter, r *http.Request) {}
`

func goParseFixture(t *testing.T, source string) map[string]any {
	t.Helper()

	dir := t.TempDir()
	path := filepath.Join(dir, "test.go")
	if err := os.WriteFile(path, []byte(source), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v, want nil", err)
	}

	parser := tree_sitter.NewParser()
	if err := parser.SetLanguage(tree_sitter.NewLanguage(tree_sitter_go.Language())); err != nil {
		t.Fatalf("SetLanguage(go) error = %v, want nil", err)
	}

	payload, err := Parse(parser, path, false, shared.Options{})
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}
	return payload
}

// TestGoHTTPFrameworkSemanticsGateSkipsFilesWithoutFrameworkImports pins issue
// #5219: goHTTPFrameworkSemantics must not run at all for a Go file that
// imports none of net/http or a goRouteFrameworkConstructors framework
// package, because it provably cannot produce output for such a file
// (dead_code_registrations.go / framework_routes.go gate on those imports
// before emitting any entry). Counting invocations directly, rather than only
// asserting on payload shape, proves the walk itself is skipped -- an
// output-only assertion would pass even if the gate were removed, since the
// ungated call already returns (nil, false) for this fixture.
//
// Not parallel: goHTTPFrameworkSemanticsInvocationCount is a package-level
// counter (documented process-global, test-only).
func TestGoHTTPFrameworkSemanticsGateSkipsFilesWithoutFrameworkImports(t *testing.T) {
	ResetGoHTTPFrameworkSemanticsInvocationCountForTest()

	payload := goParseFixture(t, goNoFrameworkImportFixture)

	if got := GoHTTPFrameworkSemanticsInvocationCountForTest(); got != 0 {
		t.Fatalf("GoHTTPFrameworkSemanticsInvocationCountForTest() = %d, want 0 (import gate should have skipped the walk)", got)
	}
	if _, ok := payload["framework_semantics"]; ok {
		t.Fatalf("payload[%q] present, want absent for a file with no framework import", "framework_semantics")
	}
}

// TestGoHTTPFrameworkSemanticsGateRunsForNetHTTPImport is the paired positive
// case: a file that does import net/http and register a route must still
// invoke goHTTPFrameworkSemantics exactly once and produce an unchanged
// framework_semantics payload, proving the gate does not suppress real work.
func TestGoHTTPFrameworkSemanticsGateRunsForNetHTTPImport(t *testing.T) {
	ResetGoHTTPFrameworkSemanticsInvocationCountForTest()

	payload := goParseFixture(t, goNetHTTPFrameworkImportFixture)

	if got := GoHTTPFrameworkSemanticsInvocationCountForTest(); got != 1 {
		t.Fatalf("GoHTTPFrameworkSemanticsInvocationCountForTest() = %d, want 1 for a file with a net/http route", got)
	}
	semantics, ok := payload["framework_semantics"].(map[string]any)
	if !ok {
		t.Fatalf("payload[%q] missing or wrong type, want map[string]any", "framework_semantics")
	}
	frameworks, ok := semantics["frameworks"].([]string)
	if !ok || len(frameworks) != 1 || frameworks[0] != "net_http" {
		t.Fatalf("semantics[%q] = %#v, want []string{\"net_http\"}", "frameworks", semantics["frameworks"])
	}
}

// TestGoFileImportsRouteFrameworkCoversEveryGatedImport pins the gate
// predicate directly: the set it opens on must be exactly net/http plus every
// goRouteFrameworkConstructors import path, and nothing else. This guards the
// 0/0 invariant at the source of truth — the end-to-end tests above only
// exercise net/http, so without this a dropped framework in the predicate loop
// would silently make Parse skip goHTTPFrameworkSemantics for that framework's
// files. The gin/echo/chi/fiber paths are read from goRouteFrameworkConstructors
// so the case list cannot fall behind a newly added constructor.
func TestGoFileImportsRouteFrameworkCoversEveryGatedImport(t *testing.T) {
	t.Parallel()

	gated := []string{"net/http"}
	for _, spec := range goRouteFrameworkConstructors {
		gated = append(gated, spec.importPath)
	}

	for _, importPath := range gated {
		aliases := map[string][]string{importPath: {"x"}}
		if !goFileImportsRouteFramework(aliases) {
			t.Errorf("goFileImportsRouteFramework(%q) = false, want true (gated import must open the gate)", importPath)
		}
	}

	nonFramework := map[string][]string{
		"fmt":                            {"fmt"},
		"strings":                        {"strings"},
		"github.com/spf13/cobra":         {"cobra"},
		"sigs.k8s.io/controller-runtime": {"ctrl"},
	}
	if goFileImportsRouteFramework(nonFramework) {
		t.Errorf("goFileImportsRouteFramework(non-framework imports) = true, want false")
	}
	if goFileImportsRouteFramework(map[string][]string{}) {
		t.Errorf("goFileImportsRouteFramework(empty) = true, want false")
	}
}
