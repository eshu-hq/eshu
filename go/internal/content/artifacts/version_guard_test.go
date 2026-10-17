// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package artifacts

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// extractorPins is the pin history of ExtractorVersion, oldest first, one row
// per version. A bump appends a row, with a new corpus digest when the bump
// also adds the input that shows its change; a re-snapshot (README,
// "Extractor version and its guard") rewrites the digests of the last row in a
// commit that changes no extractor. Never delete a row or lower the version:
// the version is monotonic, so an extractor change is undone by a new bump.
//
// Version 1 was pinned on the extractors of origin/main c704fbc3d with
// go1.26.9 linux/arm64 (the go.mod toolchain): run TestExtractorOutputIsPinned
// with -v and copy the corpusDigest and outputDigest of its first log line.
// The digest input holds no map order, locale or path separator, and
// .gitattributes keeps the snapshot bytes exact (-text).
var extractorPins = []extractorPin{
	{
		Version:      1,
		CorpusDigest: "c8b4f29521e1a41f56111fb5d85aa989f797d08fccc6a400a523910820b70e06",
		OutputDigest: "7dde1bb7ab14ac9e08ca681ca85bfb70b3ce2eb99017911cc84afed14294ef01",
	},
}

// guardMinRowsPerKind is the fewest rows each evidence kind must hold in the
// guard's output, so the guard cannot silently stop covering an extractor.
const guardMinRowsPerKind = 3

// TestExtractorOutputIsPinned is the extractor-version guard (#8155, ADR
// 7126 "Extractor version"). It runs the derive's own call of LoadConfigPaths
// over the committed guard corpus, digests the rows, and fails unless the
// corpus digest, the output digest and ExtractorVersion match the last pin.
func TestExtractorOutputIsPinned(t *testing.T) {
	repos, corpusDigest := readGuardSnapshot(t)
	out := digestGuardOutput(t, repos)
	t.Logf("guard corpus: repositories=%d files=%d rows=%d withRows=%d corpusDigest=%s outputDigest=%s extractorVersion=%d",
		out.Repositories, out.Files, out.Rows, out.WithRows, corpusDigest, out.Digest, ExtractorVersion)

	kinds := make([]string, 0, len(out.KindRows))
	for kind := range out.KindRows {
		kinds = append(kinds, kind)
	}
	sort.Strings(kinds)
	for _, kind := range kinds {
		t.Logf("kind %-36s rows=%d", kind, out.KindRows[kind])
	}
	// Both checks report, so an extractor change that also thins a kind
	// still prints the bump instruction.
	if err := checkExtractorPins(corpusDigest, out.Digest, ExtractorVersion, extractorPins); err != nil {
		t.Error(err)
	}
	if err := checkGuardCoverage(out.KindRows, EvidenceKinds(), guardMinRowsPerKind); err != nil {
		t.Error(err)
	}
}

// checkGuardCoverage fails when the output holds a kind the extractors do not
// emit, or holds fewer than minRows rows of a kind they emit.
func checkGuardCoverage(kindRows map[string]int, known []string, minRows int) error {
	knownSet := make(map[string]bool, len(known))
	var thin []string
	for _, kind := range known {
		knownSet[kind] = true
		if kindRows[kind] < minRows {
			thin = append(thin, kind)
		}
	}
	var unknown []string
	for kind := range kindRows {
		if !knownSet[kind] {
			unknown = append(unknown, kind)
		}
	}
	sort.Strings(thin)
	sort.Strings(unknown)
	if len(thin) > 0 || len(unknown) > 0 {
		return &guardCoverageError{Thin: thin, Unknown: unknown, MinRows: minRows}
	}
	return nil
}

type guardCoverageError struct {
	Thin, Unknown []string
	MinRows       int
}

func (e *guardCoverageError) Error() string {
	return fmt.Sprintf("guard corpus coverage: kinds with fewer than %d rows %q, kinds no extractor emits %q; "+
		"add input for each thin kind to the generator's supplement and regenerate together with the bump when an extractor change thinned the kind, "+
		"otherwise in a commit with no extractor change (package README, \"Extractor version and its guard\")", e.MinRows, e.Thin, e.Unknown)
}

// guardForbiddenInputs are the corpus inputs only the generator may read.
// The guard reads the frozen snapshot, so an edit to these helpers or to
// tests/fixtures cannot move the pinned digest.
var guardForbiddenInputs = []string{
	"kitchenSinkFiles", "mutantCorpus", "randomTree", "nameFreeLoadCorpus", "fileRow",
	"newHydrationStore", "hydrationStore", "fixtureSnapshotInputs", "recordedSnapshotInputs",
	"generatedSnapshotInputs", "supplementSnapshotInputs", "freezeGuardRepositories",
}

// guardFiles are the sources the guard's path runs.
var guardFiles = []string{"version_guard_test.go", "version_digest_test.go", "version_test.go"}

// forbiddenGuardIdents returns the forbidden identifiers that src references.
func forbiddenGuardIdents(t *testing.T, name string, src []byte) []string {
	t.Helper()
	file, err := parser.ParseFile(token.NewFileSet(), name, src, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("parse %s: %v", name, err)
	}
	forbidden := map[string]bool{}
	for _, ident := range guardForbiddenInputs {
		forbidden[ident] = true
	}
	found := map[string]bool{}
	ast.Inspect(file, func(n ast.Node) bool {
		if ident, ok := n.(*ast.Ident); ok && forbidden[ident.Name] {
			found[ident.Name] = true
		}
		return true
	})
	out := make([]string, 0, len(found))
	for ident := range found {
		out = append(out, ident)
	}
	sort.Strings(out)
	return out
}

// TestGuardIsSelfContained proves the guard's sources reference no corpus
// input, with a seeded RED leg that plants a reference.
func TestGuardIsSelfContained(t *testing.T) {
	for _, name := range guardFiles {
		src, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		if got := forbiddenGuardIdents(t, name, src); len(got) != 0 {
			t.Errorf("%s references corpus inputs %v; the guard must read only testdata/guard", name, got)
		}
	}
	planted := []byte("package artifacts\n\nfunc plantedGuard() { _ = randomTree(nil); _ = newHydrationStore() }\n")
	if got, want := forbiddenGuardIdents(t, "planted.go", planted), []string{"newHydrationStore", "randomTree"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("planted references found %v, want %v", got, want)
	}
}

// TestCheckGuardCoverage is the seeded RED/GREEN pair of the coverage check.
func TestCheckGuardCoverage(t *testing.T) {
	known := []string{"a", "b"}
	if err := checkGuardCoverage(map[string]int{"a": 3, "b": 9}, known, 3); err != nil {
		t.Fatalf("green: %v", err)
	}
	err := checkGuardCoverage(map[string]int{"a": 2, "c": 4}, known, 3)
	if err == nil {
		t.Fatal("red: a thin kind, a missing kind and an unknown kind passed")
	}
	coverage, ok := err.(*guardCoverageError)
	if !ok || !reflect.DeepEqual(coverage.Thin, []string{"a", "b"}) || !reflect.DeepEqual(coverage.Unknown, []string{"c"}) {
		t.Fatalf("red: got %v", err)
	}
	const action = "regenerate together with the bump when an extractor change thinned the kind, otherwise in a commit with no extractor change"
	if !strings.Contains(err.Error(), action) {
		t.Fatalf("red: message %q does not say %q", err, action)
	}
}
