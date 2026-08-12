// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package main is a golangci-lint Go plugin (loaded as a .so via
// linters.settings.custom.path, same mechanism as
// ../golangci-lint-filelength) that enforces Eshu's per-directory sprawl
// limits: a 40-non-test-.go-file cap per package directory, and a naming
// rule that a file whose name is a sibling subdirectory's package name (or
// that package name plus an underscore) belongs inside that subdirectory
// instead.
//
// The plugin implements the AnalyzerPlugin contract documented at
// https://golangci-lint.run/docs/plugins/go-plugins and exports a `New`
// constructor that returns the analysis.Analyzer slice.
//
// `package main` is required because `go build -buildmode=plugin` requires
// exactly one main package in the plugin source.
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"golang.org/x/tools/go/analysis"
)

// maxDirFiles is the repository's enforced per-directory file cap. Test
// files are excluded from the count (see qualifyingFiles). Issue #6054.
const maxDirFiles = 40

// gateName is both the golangci-lint linter name and the //nolint:<name>
// escape-hatch token, mirroring the filelength plugin's convention.
const gateName = "dirgate"

// New is the constructor invoked by golangci-lint when it loads this
// plugin. The argument is the `settings` map from the linter config; it is
// currently unused but reserved for future per-linter knobs.
func New(_ any) ([]*analysis.Analyzer, error) {
	return []*analysis.Analyzer{Analyzer}, nil
}

// Analyzer is the single analysis.Analyzer exposed by this plugin. It runs
// in LoadModeSyntax so it does not require type information: the checks
// only need the package's file list and a directory read, not type facts.
var Analyzer = &analysis.Analyzer{
	Name: gateName,
	Doc: "reports package directories that exceed Eshu's 40-non-test-.go-file " +
		"cap, and files whose name belongs in a sibling subpackage instead " +
		"(issue #6054).",
	Run:      run,
	Requires: nil,
}

// run is invoked once per package. It resolves the package's directory
// from the loaded files, evaluates the directory against the cap, the
// naming rule, the nolint escape hatch, and the grandfather ledger, and
// reports each surviving finding at the position of the file it names.
func run(pass *analysis.Pass) (any, error) {
	dir, byBase := packageFilePositions(pass)
	if dir == "" {
		return nil, nil
	}
	if skipDir(dir) {
		return nil, nil
	}

	findings, err := evaluateDirectory(normalizeDir(dir), dir, grandfatheredDirectories)
	if err != nil {
		// A filesystem read failure here is an operator/system condition
		// (e.g. a permissions problem), not a code-quality issue; the
		// syntax load already required reading these files successfully.
		return nil, nil
	}

	for _, f := range findings {
		pos, ok := byBase[f.File]
		if !ok {
			// The finding names a file golangci-lint did not load as part
			// of this package pass (can happen for build-tag-excluded
			// files during a directory listing); report it against any
			// loaded file instead of dropping it silently.
			pos = anyPos(byBase)
		}
		pass.Report(analysis.Diagnostic{Pos: pos, Message: f.Message})
	}
	return nil, nil
}

// packageFilePositions returns the package's directory (from the first
// file with a resolvable position) and a map from file basename to that
// file's token.Pos, used to report each finding against its own file.
func packageFilePositions(pass *analysis.Pass) (dir string, byBase map[string]token.Pos) {
	byBase = make(map[string]token.Pos, len(pass.Files))
	for _, f := range pass.Files {
		if f == nil || f.Pos() == token.NoPos {
			continue
		}
		p := pass.Fset.Position(f.Pos())
		if p.Filename == "" {
			continue
		}
		if dir == "" {
			dir = filepath.Dir(p.Filename)
		}
		byBase[filepath.Base(p.Filename)] = f.Pos()
	}
	return dir, byBase
}

func anyPos(byBase map[string]token.Pos) token.Pos {
	for _, p := range byBase {
		return p
	}
	return token.NoPos
}

// skipDir reports whether dir (or an ancestor path segment) is a tree the
// gate intentionally does not evaluate: vendor, testdata, and generated
// trees (mirroring the filelength plugin's skip()), plus hidden
// directories (.git and similar tooling state).
func skipDir(dir string) bool {
	for seg := range strings.SplitSeq(filepath.ToSlash(dir), "/") {
		switch seg {
		case "vendor", "testdata", "generated":
			return true
		}
		if strings.HasPrefix(seg, ".") && seg != "." {
			return true
		}
	}
	return false
}

// qualifyingFiles lists the non-test .go files directly inside dir
// (non-recursive), sorted. Test files (_test.go) are excluded from the
// directory-size cap by design (issue #6054): a package's test suite can
// legitimately be larger than its production surface without indicating
// sprawl.
func qualifyingFiles(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("dirgate: read %s: %w", dir, err)
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		out = append(out, name)
	}
	sort.Strings(out)
	return out, nil
}

// namingSubpackages lists the immediate subdirectories of dir that
// themselves contain at least one .go file (i.e. are plausibly a Go
// package), excluding vendor/testdata/generated/hidden trees. The
// directory's own basename stands in for its package name: this gate does
// not open and parse the subpackage's `package` clause, so a subdirectory
// whose package name differs from its directory name (unconventional, and
// rare in this repository) is not detected -- see the doc comment on
// detectNamingViolations for the exact rule this approximation feeds.
func namingSubpackages(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("dirgate: read %s: %w", dir, err)
	}
	var out []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		if name == "vendor" || name == "testdata" || name == "generated" || strings.HasPrefix(name, ".") {
			continue
		}
		hasGo, err := dirHasGoFile(filepath.Join(dir, name))
		if err != nil {
			return nil, err
		}
		if hasGo {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out, nil
}

func dirHasGoFile(dir string) (bool, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false, fmt.Errorf("dirgate: read %s: %w", dir, err)
	}
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".go") {
			return true, nil
		}
	}
	return false, nil
}

// representativeFile picks the single file a whole-directory cap finding
// is reported against, and where its //nolint:dirgate escape hatch must
// live. doc.go is preferred when present (it is the package's canonical
// godoc-contract file); otherwise the alphabetically-first qualifying file
// is used, so the choice is always deterministic given the same file set.
func representativeFile(files []string) string {
	if len(files) == 0 {
		return ""
	}
	sorted := append([]string(nil), files...)
	sort.Strings(sorted)
	for _, f := range sorted {
		if f == "doc.go" {
			return f
		}
	}
	return sorted[0]
}

// qualifyingDigest returns the sha256 hex digest of the given qualifying
// file basenames, sorted and newline-joined with a trailing newline. It is
// the pin the grandfather ledger stores per directory (see
// scripts/lib/dirgate-grandfather.tsv and grandfather.go): two calls with
// the same SET of names produce the same digest regardless of input
// order, and any add/remove/rename of a name changes it.
func qualifyingDigest(files []string) string {
	sorted := append([]string(nil), files...)
	sort.Strings(sorted)
	h := sha256.New()
	for _, f := range sorted {
		h.Write([]byte(f))
		h.Write([]byte("\n"))
	}
	return hex.EncodeToString(h.Sum(nil))
}

// normalizeDir converts a package DIRECTORY path (as packageFilePositions
// derives it from golangci-lint's or `go vet`'s reported file positions --
// absolute or relative depending on how the tool was invoked) into the
// canonical grandfather-ledger key: the directory's path relative to the
// go/ module root, e.g. "/Users/dev/repos/eshu/go/internal/query" and
// "internal/query" both normalize to "internal/query".
//
// normalizeDir's ONLY caller, run(), already resolved a file position down
// to its containing directory (packageFilePositions calls filepath.Dir on
// the file path) before calling this function -- so normalizeDir must NOT
// strip another path segment itself. An earlier version of this function
// called filepath.Dir a SECOND time here, silently dropping the package's
// own directory name (e.g. "go/internal/query" normalized to "internal"
// instead of "internal/query") and un-grandfathering every pinned
// directory, because the mis-derived key never matched any ledger row. See
// TestRunRecognizesGrandfatheredDirectoryThroughRealKeyDerivation in
// dirgate_test.go for the regression test that goes through run() itself,
// the exact call chain that exposed the bug (every other test in this
// plugin calls evaluateDirectory directly with an already-correct key and
// so enters below this seam).
//
// The go/ module root is located by finding the LAST "go" path segment;
// this works whether the path is absolute (CI checks out the repo at an
// arbitrary path) or already relative to go/ (the common case when
// golangci-lint is invoked with go/ as its working directory, as
// scripts/dev/precommit-go.sh and .github/workflows/test.yml both do). A
// dir that IS the go/ module root itself normalizes to ".". If no "go"
// segment is found, the path is assumed to already be relative to the
// module root.
func normalizeDir(dir string) string {
	d := filepath.ToSlash(filepath.Clean(dir))
	d = strings.TrimPrefix(d, "./")
	segs := strings.Split(d, "/")
	for i := len(segs) - 1; i >= 0; i-- {
		if segs[i] == "go" {
			rest := segs[i+1:]
			if len(rest) == 0 {
				return "."
			}
			return strings.Join(rest, "/")
		}
	}
	return d
}
