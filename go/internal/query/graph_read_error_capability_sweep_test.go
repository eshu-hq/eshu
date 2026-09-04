// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
)

// referencesIdentifierByName reports whether contents (Go source) contains a
// syntactic reference to name as an identifier -- a call, a value, a type use,
// and so on -- as opposed to the name merely appearing inside a comment or a
// string literal. Parsing as Go source and walking for *ast.Ident nodes
// (#5761 F4, rather than strings.Contains) is what makes this
// false-positive-safe: an *ast.Ident node only exists for text the compiler
// would resolve as a Go identifier, and comments and string literals are
// never represented as Ident nodes in the AST.
//
// A parse error is returned rather than silently treated as "no reference",
// so a real call site can never be masked by an unexpected parse failure on
// a file the caller expected to be valid Go source.
func referencesIdentifierByName(fset *token.FileSet, filename string, contents []byte, name string) (bool, error) {
	file, err := parser.ParseFile(fset, filename, contents, parser.SkipObjectResolution)
	if err != nil {
		return false, err
	}
	found := false
	ast.Inspect(file, func(n ast.Node) bool {
		if found {
			return false
		}
		if ident, ok := n.(*ast.Ident); ok && ident.Name == name {
			found = true
			return false
		}
		return true
	})
	return found, nil
}

// TestWriteGraphReadErrorCapabilitiesExistInMatrix is the #5761 F5 gate:
// nothing previously bound the capability id an error envelope reports to the
// capabilityMatrix that BuildTruthEnvelope panics against, so a call site could
// pass a capability that exists nowhere in the catalog and no test would catch
// it -- the string only surfaces inside an error envelope, which no other test
// validates against the matrix.
//
// This AST-parses every non-test .go file in this package, finds every
// WriteGraphReadError(w, r, err, <capability>) call site, statically resolves
// <capability> to its set of possible string-literal values, and asserts each
// one is a real capabilityMatrix key. Resolution handles the three shapes this
// package actually uses:
//   - a string literal directly (e.g. "code_quality.complexity"),
//   - a package-level const identifier (e.g. catalogCapability), and
//   - a local variable, resolved to every literal it is assigned within its
//     enclosing function (covers code.go's fuzzy/exact/variable capability
//     switch and code_relationships.go's capability := relationshipCapability(...)
//     pattern, recursing one level into a called package-level function's own
//     return literals).
//
// An expression this sweep cannot resolve is a hard failure rather than a
// silent skip: a new dynamic-capability pattern must extend the sweep, not
// quietly go unchecked.
//
// minWriteGraphReadErrorCallSites is a pinned floor on how many
// capability-taking call sites this sweep must resolve. findCallSites now
// matches every callee in sweptCapabilityCallees, qualified or bare, at that
// callee's own capability-argument index -- WriteGraphReadError at 3 and
// GraphReadErrorEnvelope at 1 -- rather than one name at a fixed 4-argument
// arity. The floor still counts what it always counted, because the forwarders
// are excluded and the envelope variant has no call sites in this package yet;
// it will need raising the first time a family adds one. Counted by AST-walking
// every non-test .go file in this package on 2026-08-02
// (`go run` over the same collectCallSites/findCallSites matching logic): 66
// non-test files contain the call, at 124 total call sites. The floor exists
// so a change to the arity guard, the callee-name match, or the glob that
// silently stops the sweep from matching real call sites reads as "0 (or a
// sharp drop in) resolved call sites" -- a hard failure -- rather than as a
// clean pass that in fact examined nothing. A legitimate future addition of
// more WriteGraphReadError call sites only raises the matched count above
// this floor and never fails; only a shrinkage below 124 (accidental or a
// sweep regression) trips it, so removing call sites in a real refactor
// requires deliberately lowering this constant with a new justification.
const minWriteGraphReadErrorCallSites = 124

func TestWriteGraphReadErrorCapabilitiesExistInMatrix(t *testing.T) {
	dir := queryPackageDir(t)
	fileSet := token.NewFileSet()
	// Recursive, not a depth-1 glob (#6060). A handler family that moves into
	// go/internal/query/<family>/ takes its WriteGraphReadError call sites with
	// it, and a glob that never descends would report "swept clean" over files
	// it never opened. testdata is excluded because recursion newly reaches it.
	files, err := sweepGoFiles(dir)
	if err != nil {
		t.Fatalf("walk %s: %v", dir, err)
	}

	sweep := newCapabilitySweep(fileSet)
	var parsed []*ast.File
	for _, name := range files {
		if filepath.Ext(name) != ".go" || hasTestSuffix(name) {
			continue
		}
		file, err := parser.ParseFile(fileSet, name, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		parsed = append(parsed, file)
		// Declarations are collected from every directory the recursive walk
		// reaches, not just the root package. The sweep's lookup maps are keyed
		// by (declaring directory, bare identifier) -- see capabilitySweep in
		// graph_read_error_capability_sweep_resolve_test.go -- rather than by
		// bare identifier alone, because 23 names are declared in both root and
		// a leaf today (BoolVal, IntVal and BuildTruthEnvelope among them, since
		// root keeps a forwarder for each symbol a leaf owns). Directory scoping
		// is what makes it safe to feed every package into the same maps: a
		// lookup is always resolved against the identifier's own declaring
		// directory, so a root forwarder and a leaf's real declaration of the
		// same name can never shadow each other.
		sweep.collectDecls(file)
	}
	for _, file := range parsed {
		sweep.collectCallSites(file)
	}

	var findings []string
	totalMatched := 0
	for _, file := range parsed {
		fileFindings, matched := sweep.findCallSites(file)
		findings = append(findings, fileFindings...)
		totalMatched += matched
	}
	sort.Strings(findings)
	for _, finding := range findings {
		t.Error(finding)
	}

	// A sweep that matched zero WriteGraphReadError call sites is
	// indistinguishable from a sweep that examined nothing at all: both
	// report zero findings and pass. Fail loudly instead of silently.
	if totalMatched == 0 {
		t.Fatal("resolved zero WriteGraphReadError call sites; the sweep's " +
			"callee-name match, arity guard, or file glob is broken -- a " +
			"passing test here must mean call sites were examined and found " +
			"clean, not that none were found")
	}
	if totalMatched < minWriteGraphReadErrorCallSites {
		t.Errorf("resolved %d WriteGraphReadError call sites, below the pinned "+
			"floor of %d (see minWriteGraphReadErrorCallSites doc comment); "+
			"if call sites were legitimately removed, lower the floor with a "+
			"new justification -- otherwise the sweep is silently matching "+
			"fewer call sites than it used to",
			totalMatched, minWriteGraphReadErrorCallSites)
	}
}

func hasTestSuffix(name string) bool {
	const suffix = "_test.go"
	if len(name) < len(suffix) {
		return false
	}
	return name[len(name)-len(suffix):] == suffix
}

// queryPackageDir resolves this test file's own directory (go/internal/query).
func queryPackageDir(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller(0) failed")
	}
	return filepath.Dir(file)
}

// TestWriteGraphReadErrorHasNoCallersOutsideThisPackage is the #5761 F3 gate.
// TestWriteGraphReadErrorCapabilitiesExistInMatrix above only globs this
// package's own *.go files (queryPackageDir), but WriteGraphReadError is an
// exported function -- nothing previously proved every real call site lives
// in this package, so a call site added to another package could pass an
// unregistered capability and this sweep would never see it. This walks the
// whole Go module tree from go.mod outward and fails if any *.go file outside
// go/internal/query references the exported symbol, so the sweep's implicit
// package-scope assumption stays a tested invariant instead of a documentation
// claim that can silently go stale.
//
// #5761 F4: this used to test with a raw strings.Contains scan over the file
// bytes, which false-positives on the substring appearing inside a comment or
// a string literal (for example, a doc comment cross-referencing this very
// function by name) even though no actual reference exists. It is meant to
// enforce identifier references -- a call, a value, anything the compiler
// would resolve as the symbol -- so it now parses each file and inspects the
// AST via referencesIdentifierByName instead.
func TestWriteGraphReadErrorHasNoCallersOutsideThisPackage(t *testing.T) {
	t.Parallel()

	packageDir := queryPackageDir(t)
	moduleRoot := findModuleRoot(t, packageDir)
	fset := token.NewFileSet()

	walkErr := filepath.WalkDir(moduleRoot, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if path == packageDir {
				return filepath.SkipDir
			}
			if path != moduleRoot && strings.HasPrefix(entry.Name(), ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Ext(path) != ".go" {
			return nil
		}
		contents, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		referenced, parseErr := referencesIdentifierByName(fset, path, contents, "WriteGraphReadError")
		if parseErr != nil {
			t.Errorf("parse %s: %v", path, parseErr)
			return nil
		}
		if referenced {
			t.Errorf("%s references WriteGraphReadError outside go/internal/query; "+
				"TestWriteGraphReadErrorCapabilitiesExistInMatrix's sweep only covers "+
				"this package's call sites, so a call site here would go unchecked", path)
		}
		return nil
	})
	if walkErr != nil {
		t.Fatalf("walk %s: %v", moduleRoot, walkErr)
	}
}

// TestReferencesIdentifierByNameDistinguishesCodeFromCommentsAndStrings is the
// #5761 F4 regression for referencesIdentifierByName itself: it must report a
// real call-site reference as found, and must NOT report a false positive
// when the identifier's name only appears inside a comment or a string
// literal -- the exact gap the prior strings.Contains-based
// TestWriteGraphReadErrorHasNoCallersOutsideThisPackage had.
func TestReferencesIdentifierByNameDistinguishesCodeFromCommentsAndStrings(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		src  string
		want bool
	}{
		{
			name: "real_call_site",
			src:  "package p\nfunc f() { WriteGraphReadError(nil, nil, nil, \"\") }\n",
			want: true,
		},
		{
			name: "comment_only",
			src:  "package p\n\n// see WriteGraphReadError for the shared mapping this package uses.\nfunc f() {}\n",
			want: false,
		},
		{
			name: "string_literal_only",
			src:  "package p\n\nvar msg = \"references WriteGraphReadError in prose, not code\"\n",
			want: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			fset := token.NewFileSet()
			got, err := referencesIdentifierByName(fset, tc.name+".go", []byte(tc.src), "WriteGraphReadError")
			if err != nil {
				t.Fatalf("referencesIdentifierByName() error = %v, want nil", err)
			}
			if got != tc.want {
				t.Fatalf("referencesIdentifierByName() = %v, want %v (src=%q)", got, tc.want, tc.src)
			}
		})
	}
}

// findModuleRoot walks up from start looking for go.mod.
func findModuleRoot(t *testing.T, start string) string {
	t.Helper()
	dir := start
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("go.mod not found walking up from %s", start)
		}
		dir = parent
	}
}

// capabilitySweepDocumentedExceptions lists capability strings that are
// deliberately absent from capabilityMatrix, with the reason, so the sweep
// does not misreport a documented design choice as a gap. Adding an entry here
// requires the same justification a reviewer would want in the source: why the
// route bypasses the matrix's BuildTruthEnvelope panic-guard. It lives here,
// next to its size-guard test, rather than with the AST machinery in
// graph_read_error_capability_sweep_resolve_test.go.
var capabilitySweepDocumentedExceptions = map[string]string{
	"repository_freshness.status": "repository_freshness.go's repositoryFreshnessTruth builds its " +
		"TruthEnvelope directly from Postgres runtime state rather than through " +
		"capabilityMatrix/BuildTruthEnvelope, and says so in its doc comment; not a gap.",
}

// maxCapabilitySweepDocumentedExceptions is the #5761 F6 gate, pinning
// capabilitySweepDocumentedExceptions (defined above) the same way
// minWriteGraphReadErrorCallSites above pins the sweep's call-site floor:
// without a ceiling, a future change could silence any real sweep finding
// by adding an undiscussed entry. Raising this constant is only legitimate
// alongside a new, justified exception in the same change.
const maxCapabilitySweepDocumentedExceptions = 1

// TestCapabilitySweepDocumentedExceptionsStayPinnedAndNarrowlyScoped proves
// two things about capabilitySweepDocumentedExceptions: its size never grows
// past maxCapabilitySweepDocumentedExceptions without a deliberate bump, and
// every documented capability string is referenced (as a quoted string
// literal) in exactly one non-test .go file in this package -- proving each
// entry's "not a gap, just this one isolated call site" claim stays true
// instead of silently starting to cover a second, unrelated call site.
func TestCapabilitySweepDocumentedExceptionsStayPinnedAndNarrowlyScoped(t *testing.T) {
	t.Parallel()

	if got := len(capabilitySweepDocumentedExceptions); got > maxCapabilitySweepDocumentedExceptions {
		t.Fatalf("len(capabilitySweepDocumentedExceptions) = %d, want at most %d "+
			"(see maxCapabilitySweepDocumentedExceptions doc comment); a new exception "+
			"must bump this constant deliberately, with justification", got, maxCapabilitySweepDocumentedExceptions)
	}

	dir := queryPackageDir(t)
	// Recursive, not a depth-1 glob (#6060). A handler family that moves into
	// go/internal/query/<family>/ takes its WriteGraphReadError call sites with
	// it, and a glob that never descends would report "swept clean" over files
	// it never opened. testdata is excluded because recursion newly reaches it.
	files, err := sweepGoFiles(dir)
	if err != nil {
		t.Fatalf("walk %s: %v", dir, err)
	}

	for capability := range capabilitySweepDocumentedExceptions {
		matchingFiles := 0
		needle := `"` + capability + `"`
		for _, name := range files {
			if hasTestSuffix(name) {
				continue
			}
			contents, readErr := os.ReadFile(name)
			if readErr != nil {
				t.Fatalf("read %s: %v", name, readErr)
			}
			if strings.Contains(string(contents), needle) {
				matchingFiles++
			}
		}
		if matchingFiles != 1 {
			t.Errorf("capability %q referenced as a quoted string literal in %d non-test "+
				"files, want exactly 1 -- capabilitySweepDocumentedExceptions claims a single "+
				"isolated call site", capability, matchingFiles)
		}
	}
}

// sweepGoFiles returns every non-test .go file at or beneath dir, skipping
// testdata and hidden or underscore-prefixed directories.
func sweepGoFiles(dir string) ([]string, error) {
	var files []string
	err := filepath.WalkDir(dir, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			name := entry.Name()
			if path != dir && (name == "testdata" || strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_")) {
				return fs.SkipDir
			}
			return nil
		}
		if filepath.Ext(path) == ".go" && !hasTestSuffix(path) {
			files = append(files, path)
		}
		return nil
	})
	return files, err
}
