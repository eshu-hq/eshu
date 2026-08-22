// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gitrepo

// The webpack bundler-prefix sniffer's own fixtures and tests. Split out of
// git_snapshot_native_discovery_test.go when #4782's cases took that file over
// the 500-line cap; the sniffer is a coherent enough subject to own a file.

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/discovery"
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
	const line = "var generatedBundleChunk = 1;\n"
	return header + strings.Repeat(line, bundleFixtureRepeats(len(header), len(line)))
}

// The over-admission guard: a hand-written file may legitimately talk ABOUT
// webpack. Naming it is not being it.
func largeHandWrittenWebpackMentionFixture() string {
	header := "// Our build uses webpack. See webpack.config.js for the webpackBootstrap\n" +
		"// runtime settings and how __webpack_modules__ is chunked.\n" +
		"export function configureBuild() { return 'hand written'; }\n"
	const line = "export const helper = 1;\n"
	return header + strings.Repeat(line, bundleFixtureRepeats(len(header), len(line)))
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

// A delta sync resolves its file set from explicit --file-targets rather than
// walking the tree, and that path never applied the generated-content filter.
// So the same webpack bundle was skipped when a full discovery found it and
// indexed when a delta sync touched it — one file, two answers, decided by
// which sync mode happened to run.
//
// The filter belongs on both paths for the same reason it exists on either:
// generated output is not source, and parsing it costs real time and mints
// phantom functions and calls into the graph (#4782).
func TestResolveNativeSnapshotFileSetForTargetsSkipsGeneratedBundles(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	writeCollectorTestFile(t, filepath.Join(repoRoot, ".git", "HEAD"), "ref: refs/heads/main\n")
	bundlePath := filepath.Join(repoRoot, "public", "js", "split-runtime.js")
	sourcePath := filepath.Join(repoRoot, "src", "app.js")
	writeCollectorTestFile(t, bundlePath, largeWebpackSplitRuntimeFixture())
	writeCollectorTestFile(t, sourcePath, "export function app() { return 'source'; }\n")

	resolvedRepoRoot, err := filepath.EvalSymlinks(repoRoot)
	if err != nil {
		resolvedRepoRoot = repoRoot
	}

	var stats discovery.DiscoveryStats
	fileSet, err := resolveNativeSnapshotFileSetForTargets(
		resolvedRepoRoot,
		[]string{bundlePath, sourcePath},
		parser.DefaultRegistry(),
		&stats,
	)
	if err != nil {
		t.Fatalf("resolveNativeSnapshotFileSetForTargets() error = %v", err)
	}

	if fileSetContainsSuffix(fileSet.Files, "public/js/split-runtime.js") {
		t.Error("a delta sync kept a generated webpack bundle; the full discovery path " +
			"skips the same file, so the graph depends on which sync mode ran")
	}
	if !fileSetContainsSuffix(fileSet.Files, "src/app.js") {
		t.Error("an explicitly targeted source file was dropped")
	}
	if stats.FilesSkippedByContent["generated-webpack"] != 1 {
		t.Errorf("FilesSkippedByContent[generated-webpack] = %d, want 1; the delta path "+
			"must report the skip through the same counter an operator already watches",
			stats.FilesSkippedByContent["generated-webpack"])
	}
}

// bundleFixtureRepeats sizes a fixture just past the sniffer's byte floor,
// derived from the constant rather than hardcoded.
//
// The sniffer reads only the first 8KiB to match a bundler prefix, but it never
// looks at all unless the file is at least generatedJavaScriptBundleMinBytes —
// so a fixture sized to the 8KiB read window would be skipped before the prefix
// mattered, and the test would prove nothing. Deriving the count means raising
// the floor cannot silently shrink these below it (#5968 review).
//
// The margin is two lines rather than one so integer division cannot land the
// total exactly on the floor.
func bundleFixtureRepeats(headerBytes, lineBytes int) int {
	return (generatedJavaScriptBundleMinBytes-headerBytes)/lineBytes + 2
}
