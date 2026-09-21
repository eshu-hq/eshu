// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package javascript

import "testing"

func collectSurfaceReexportSet(
	reexports []javaScriptTypeScriptSurfaceReexport,
) map[javaScriptTypeScriptSurfaceReexport]struct{} {
	set := make(map[javaScriptTypeScriptSurfaceReexport]struct{}, len(reexports))
	for _, reexport := range reexports {
		set[reexport] = struct{}{}
	}
	return set
}

func assertContainsReexport(
	t *testing.T,
	reexports []javaScriptTypeScriptSurfaceReexport,
	want javaScriptTypeScriptSurfaceReexport,
) {
	t.Helper()
	if _, ok := collectSurfaceReexportSet(reexports)[want]; !ok {
		t.Fatalf("static re-exports = %#v, want edge %#v", reexports, want)
	}
}

// TestStaticReexportsTypeOnlyStarReExport pins that a type-only star re-export
// (export type * from "...") still emits a star re-export edge so the public
// surface keeps the re-exported types. The TypeScript tree-sitter grammar does
// not model the type modifier on a star export and emits an ERROR node for the
// "type" token, so the star form must be recognized structurally from the
// export_statement's source plus the absence of a named export clause, not from
// a leading "*" in the node text.
func TestStaticReexportsTypeOnlyStarReExport(t *testing.T) {
	t.Parallel()

	root, source, closeFn := parseTypeScriptRootForTest(t, `export type * from "./types";
`)
	defer closeFn()

	got := javaScriptTypeScriptStaticReexportsFromRoot(root, source)
	assertContainsReexport(t, got, javaScriptTypeScriptSurfaceReexport{
		exportedName: "*",
		originalName: "*",
		source:       "./types",
	})
}

// TestStaticReexportsTypeOnlyStarAsNamespaceReExport pins the namespaced
// type-only star form (export type * as NS from "...").
func TestStaticReexportsTypeOnlyStarAsNamespaceReExport(t *testing.T) {
	t.Parallel()

	root, source, closeFn := parseTypeScriptRootForTest(t, `export type * as Types from "./types";
`)
	defer closeFn()

	got := javaScriptTypeScriptStaticReexportsFromRoot(root, source)
	assertContainsReexport(t, got, javaScriptTypeScriptSurfaceReexport{
		exportedName: "*",
		originalName: "*",
		source:       "./types",
	})
}

// TestStaticReexportsValueStarReExportStaysGreen guards that the plain value
// star re-export form keeps emitting a star edge after the type-only fix.
func TestStaticReexportsValueStarReExportStaysGreen(t *testing.T) {
	t.Parallel()

	root, source, closeFn := parseTypeScriptRootForTest(t, `export * from "./values";
`)
	defer closeFn()

	got := javaScriptTypeScriptStaticReexportsFromRoot(root, source)
	assertContainsReexport(t, got, javaScriptTypeScriptSurfaceReexport{
		exportedName: "*",
		originalName: "*",
		source:       "./values",
	})
}

// TestStaticReexportsNamedTypeReExportStaysGreen guards that a named type
// re-export keeps emitting per-name edges (not a star edge) after the fix.
func TestStaticReexportsNamedTypeReExportStaysGreen(t *testing.T) {
	t.Parallel()

	root, source, closeFn := parseTypeScriptRootForTest(t, `export type { Alpha, Beta as Gamma } from "./types";
`)
	defer closeFn()

	got := javaScriptTypeScriptStaticReexportsFromRoot(root, source)
	set := collectSurfaceReexportSet(got)
	star := javaScriptTypeScriptSurfaceReexport{exportedName: "*", originalName: "*", source: "./types"}
	if _, ok := set[star]; ok {
		t.Fatalf("named type re-export emitted a star edge: %#v", got)
	}
	assertContainsReexport(t, got, javaScriptTypeScriptSurfaceReexport{
		exportedName: "Alpha",
		originalName: "Alpha",
		source:       "./types",
	})
	assertContainsReexport(t, got, javaScriptTypeScriptSurfaceReexport{
		exportedName: "Gamma",
		originalName: "Beta",
		source:       "./types",
	})
}
