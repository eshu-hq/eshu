// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package exposure

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// provenanceRootDir is the go/internal directory, relative to this package
// directory, that sink Provenance file paths are written against. A Provenance
// path such as "reducer/iamcan/iam_can_perform_materialization.go" resolves to
// go/internal/reducer/iamcan/iam_can_perform_materialization.go.
const provenanceRootDir = ".."

// TestSinkCatalogProvenancePathsExist proves every Go file a sink spec cites in
// its Provenance still exists. Provenance is the catalog's audit trail back to
// the materializer that authors each qualifying edge, and it is hashed into
// SinkCatalogVersion, so a reducer package move that strands a path leaves the
// catalog unauditable until someone pays a deliberate version bump (#6547).
// Every graph-backed spec must cite at least one Go file, which keeps this
// check from passing vacuously on a Provenance that names no path at all.
func TestSinkCatalogProvenancePathsExist(t *testing.T) {
	t.Parallel()

	checked := 0
	for _, spec := range SinkCatalog() {
		paths := provenanceGoPaths(spec.Provenance)
		if spec.GraphBacked && len(paths) == 0 {
			t.Errorf("graph-backed sink %q (relationship %q) cites no Go file in Provenance %q", spec.Kind, spec.Relationship, spec.Provenance)
		}
		for _, path := range paths {
			checked++
			resolved := filepath.Join(provenanceRootDir, filepath.FromSlash(path))
			if _, err := os.Stat(resolved); err != nil {
				t.Errorf("sink %q (relationship %q) Provenance cites %q, which does not resolve to go/internal/%s: %v", spec.Kind, spec.Relationship, path, path, err)
			}
		}
	}
	if checked == 0 {
		t.Fatal("no Provenance Go paths were checked; the extractor or the catalog changed shape")
	}
}

// TestProvenanceGoPathsExtractsEveryCitedFile pins the extractor the existence
// check depends on, so a Provenance written as "a.go and b.go (edge)" yields
// both files and a prose-only Provenance yields none.
func TestProvenanceGoPathsExtractsEveryCitedFile(t *testing.T) {
	t.Parallel()

	got := provenanceGoPaths("reducer/x/a.go and storage/cypher/b.go (Function-[:R]->T)")
	want := []string{"reducer/x/a.go", "storage/cypher/b.go"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("provenanceGoPaths = %q, want %q", got, want)
	}
	if got := provenanceGoPaths("#3191 non-GraphBacked fixture: keep unresolved"); len(got) != 0 {
		t.Fatalf("provenanceGoPaths on prose = %q, want none", got)
	}

	// Punctuated citations must not be skipped. A path swallowed here would
	// never reach the existence check, so a stale one could ride along inside
	// a multi-file citation written as a list or an aside.
	punctuated := provenanceGoPaths("reducer/x/a.go, (storage/cypher/b.go) and `reducer/y/c.go`.")
	wantPunctuated := []string{"reducer/x/a.go", "storage/cypher/b.go", "reducer/y/c.go"}
	if strings.Join(punctuated, ",") != strings.Join(wantPunctuated, ",") {
		t.Fatalf("provenanceGoPaths on punctuated citation = %q, want %q", punctuated, wantPunctuated)
	}
}

// provenanceGoPaths returns the slash-separated Go file paths cited in a
// Provenance string, in order of appearance.
func provenanceGoPaths(provenance string) []string {
	var out []string
	for _, field := range strings.Fields(provenance) {
		// Citations are prose, so a path can arrive wrapped in punctuation:
		// "a/b.go," in a list, "(a/b.go)" in an aside, or backticked. Trimming
		// first keeps those from being skipped silently, which would let a
		// stale path ride along inside a punctuated multi-file citation.
		field = strings.Trim(field, "`,;:.()[]'\"")
		if strings.HasSuffix(field, ".go") && strings.Contains(field, "/") {
			out = append(out, field)
		}
	}
	return out
}
