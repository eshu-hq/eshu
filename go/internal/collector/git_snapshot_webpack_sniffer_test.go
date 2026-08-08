// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

// The webpack bundler-prefix sniffer's own fixtures and tests. Split out of
// git_snapshot_native_discovery_test.go when #4782's cases took that file over
// the 500-line cap; the sniffer is a coherent enough subject to own a file.

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser"
)

// A webpack chunk whose runtime lives in a SEPARATE chunk carries the module
// table but neither the module cache nor the require function. Webpack emits
// those two only when the chunk needs them — `if (useRequire || moduleCache)`
// and `if (useRequire)` in JavascriptModulesPlugin — so a build using
// `optimization.runtimeChunk` produces exactly this shape for every non-runtime
// chunk. It is a first-class configuration, not an oddity.
//
// The sniffer required the cache AND the require function, so it classified
// these as hand-written and sent them to a full tree-sitter parse.
//
// The fixture is sized deliberately. #4766's jsParseByteCap bounds any
// JavaScript file over 1 MiB before tree-sitter sees it, so the case that still
// costs anything is a bundle between this sniffer's 256 KiB floor and that cap
// — measured at 1.9s to parse a 0.58MB one, against 0.2ms to sniff it.
func largeWebpackSplitRuntimeFixture() string {
	header := "/******/ (() => { // webpackBootstrap\n" +
		"/******/ \tvar __webpack_modules__ = ({\n" +
		"/******/ \t\t\"./src/index.js\": ((module, exports, require) => { exports.x = 1; })\n" +
		"/******/ \t});\n"
	return header + strings.Repeat("var generatedBundleChunk = 1;\n", 12000)
}

// The over-admission guard: a hand-written file may legitimately talk ABOUT
// webpack. Naming it is not being it.
func largeHandWrittenWebpackMentionFixture() string {
	header := "// Our build uses webpack. See webpack.config.js for the webpackBootstrap\n" +
		"// runtime settings and how __webpack_modules__ is chunked.\n" +
		"export function configureBuild() { return 'hand written'; }\n"
	return header + strings.Repeat("export const helper = 1;\n", 12000)
}

func TestResolveNativeSnapshotFileSetSkipsWebpackChunksWithASplitRuntime(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	writeCollectorTestFile(t, filepath.Join(repoRoot, ".git", "HEAD"), "ref: refs/heads/main\n")
	writeCollectorTestFile(t, filepath.Join(repoRoot, "src", "app.js"), "export function app() { return 'source'; }\n")
	writeCollectorTestFile(t, filepath.Join(repoRoot, "public", "js", "split-runtime.js"), largeWebpackSplitRuntimeFixture())
	writeCollectorTestFile(t, filepath.Join(repoRoot, "src", "build_notes.js"), largeHandWrittenWebpackMentionFixture())

	resolvedRepoRoot, err := filepath.EvalSymlinks(repoRoot)
	if err != nil {
		resolvedRepoRoot = repoRoot
	}
	// Both fixtures must clear the size floor, or the sniffer never runs on
	// them. The positive case would fail loudly if its fixture shrank, but the
	// negative case would not: a file under the floor is kept no matter what
	// the sniffer decides, so "hand-written file was kept" would pass while
	// proving nothing about the over-admission guard.
	for _, fixture := range []struct {
		name string
		body string
	}{
		{"webpack split-runtime chunk", largeWebpackSplitRuntimeFixture()},
		{"hand-written webpack mention", largeHandWrittenWebpackMentionFixture()},
	} {
		if len(fixture.body) < generatedJavaScriptBundleMinBytes {
			t.Fatalf("%s fixture is %d bytes, below the %d-byte floor; the sniffer is "+
				"never consulted and the assertions below prove nothing",
				fixture.name, len(fixture.body), generatedJavaScriptBundleMinBytes)
		}
	}

	registry := parser.DefaultRegistry()
	fileSet, _, err := resolveNativeSnapshotFileSet(resolvedRepoRoot, registry, NativeRepositorySnapshotter{}.discoveryOptions())
	if err != nil {
		t.Fatalf("resolveNativeSnapshotFileSet() error = %v", err)
	}

	if fileSetContainsSuffix(fileSet.Files, "public/js/split-runtime.js") {
		t.Error("a webpack chunk with a split runtime was kept for parsing; it is generated output")
	}
	// The negative case matters as much: widening the sniffer must not start
	// swallowing source files that merely mention the bundler.
	if !fileSetContainsSuffix(fileSet.Files, "src/build_notes.js") {
		t.Error("a hand-written file that mentions webpack was skipped; naming a bundler is not being one")
	}
	if !fileSetContainsSuffix(fileSet.Files, "src/app.js") {
		t.Error("ordinary source was skipped")
	}
}
