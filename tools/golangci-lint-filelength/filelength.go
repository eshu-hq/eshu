// Package main is a golangci-lint Go plugin (loaded as a .so via
// linters.settings.custom.path) that enforces Eshu's repository-wide
// 500-line file cap on non-test, non-generated Go source files.
//
// The plugin implements the AnalyzerPlugin contract documented at
// https://golangci-lint.run/docs/plugins/go-plugins and exports a
// `New` constructor that returns the analysis.Analyzer slice.
//
// `package main` is required because `go build -buildmode=plugin`
// requires exactly one main package in the plugin source.
package main

import (
	"bufio"
	"fmt"
	"go/token"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/tools/go/analysis"
)

// maxFileLines is the repository's enforced 500-line cap (see
// AGENTS.md and CLAUDE.md "MUST keep files under 500 lines").
const maxFileLines = 500

// New is the constructor invoked by golangci-lint when it loads this
// plugin. The argument is the `settings` map from the linter config;
// it is currently unused but reserved for future per-linter knobs.
func New(_ any) ([]*analysis.Analyzer, error) {
	return []*analysis.Analyzer{Analyzer}, nil
}

// Analyzer is the single analysis.Analyzer exposed by this plugin.
// It runs in LoadModeSyntax so it does not require type information;
// the check only needs to count newline characters in the file body.
var Analyzer = &analysis.Analyzer{
	Name: "filelength",
	Doc: "reports Go source files that exceed Eshu's " +
		"repository-wide 500-line cap (AGENTS.md / CLAUDE.md " +
		"\"MUST keep files under 500 lines\").",
	Run:      run,
	Requires: nil,
}

// run is invoked once per package. It iterates over every Go file
// in the package's syntax tree and counts the lines of each file
// via a streamed bufio.Scanner read from disk (the AST node does
// not preserve physical line count for the whole file).
func run(pass *analysis.Pass) (any, error) {
	seen := make(map[string]struct{}, len(pass.Files))
	for _, f := range pass.Files {
		if f == nil || f.Pos() == token.NoPos {
			continue
		}
		// pass.Fset.Position maps AST node positions to file paths.
		pos := pass.Fset.Position(f.Pos())
		path := pos.Filename
		if path == "" {
			continue
		}
		if _, ok := seen[path]; ok {
			continue
		}
		seen[path] = struct{}{}

		if skip(path) {
			continue
		}

		count, err := countLines(path)
		if err != nil {
			// We deliberately do not report file-open errors as
			// diagnostics; the syntax load already required reading
			// the file, so a failure here is an operator/system
			// condition rather than a code-quality issue.
			continue
		}
		if count <= maxFileLines {
			continue
		}

		pass.Report(analysis.Diagnostic{
			Pos:     f.Pos(),
			Message: fmt.Sprintf("file exceeds %d-line cap (%d lines)", maxFileLines, count),
		})
	}
	return nil, nil
}

// skip returns true for files the cap intentionally does not apply to.
func skip(path string) bool {
	base := filepath.Base(path)
	if strings.HasSuffix(base, "_test.go") {
		return true
	}
	// Generated files live under generated/ markers and are produced
	// by tools; we leave their size to the generator author.
	if strings.Contains(path, string(filepath.Separator)+"generated"+string(filepath.Separator)) {
		return true
	}
	// Vendor and testdata are not part of the production source tree.
	if strings.Contains(path, string(filepath.Separator)+"vendor"+string(filepath.Separator)) {
		return true
	}
	if strings.Contains(path, string(filepath.Separator)+"testdata"+string(filepath.Separator)) {
		return true
	}
	return false
}

// countLines returns the number of physical newlines in path. The
// check is intentionally stream-based so multi-megabyte files do not
// blow up analyzer memory.
func countLines(path string) (int, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	const maxLineBytes = 16 << 20 // 16 MiB; far above any realistic Go source.
	scanner.Buffer(make([]byte, 64*1024), maxLineBytes)

	count := 0
	for scanner.Scan() {
		count++
	}
	if err := scanner.Err(); err != nil {
		return 0, err
	}
	return count, nil
}
