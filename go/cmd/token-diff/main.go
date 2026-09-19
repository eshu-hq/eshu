// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Command token-diff is documented in doc.go.
package main

import (
	"flag"
	"fmt"
	"go/parser"
	"go/scanner"
	"go/token"
	"io"
	"os"
	"strings"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// run implements the CLI. It is separated from main so tests can drive it
// with fake arguments and captured output instead of a real process.
//
// Exit 0 means the two files are token-identical after the comment-only
// exemption rules below, or, with -allow-internal-import-rename, differ only
// by one internal package move (see internalImportRenameOnly), so a caller
// may treat the edit as behavior-free.
// Exit 1 means they differ (a real change). Exit 2 means an error occurred
// (bad flags, unreadable file, or a Go parse/scan error); callers MUST treat
// exit 2 the same as exit 1 -- fail closed, never fail open on an error.
func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("token-diff", flag.ContinueOnError)
	fs.SetOutput(stderr)
	basePath := fs.String("base", "", "path to the base version of the Go source file")
	headPath := fs.String("head", "", "path to the head version of the Go source file")
	allowRename := fs.Bool("allow-internal-import-rename", false,
		"also exit 0 when the only change is one repository-internal import-path move plus its qualifier rename")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *basePath == "" || *headPath == "" {
		_, _ = fmt.Fprintln(stderr, "token-diff: -base and -head are both required")
		return 2
	}

	baseSrc, err := os.ReadFile(*basePath) // #nosec G304 -- both paths are caller-supplied CLI flags naming its own base/head comparison inputs, not external/untrusted input.
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "token-diff: reading base %s: %v\n", *basePath, err)
		return 2
	}
	headSrc, err := os.ReadFile(*headPath) // #nosec G304 -- see above.
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "token-diff: reading head %s: %v\n", *headPath, err)
		return 2
	}

	baseCgo, err := hasCgoImport(baseSrc)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "token-diff: parsing base imports: %v\n", err)
		return 2
	}
	headCgo, err := hasCgoImport(headSrc)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "token-diff: parsing head imports: %v\n", err)
		return 2
	}
	if baseCgo || headCgo {
		// A cgo preamble comment (the block of C code directly above
		// `import "C"`) compiles as C source, so a "comment-only" edit to it
		// changes real behavior. Never exempt either side of a diff touching
		// such a file, regardless of what the token comparison would say.
		_, _ = fmt.Fprintln(stdout, "token-diff: cgo preamble present (import \"C\"), never exempt")
		return 1
	}

	baseTokens, err := tokenize(baseSrc)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "token-diff: scanning base: %v\n", err)
		return 2
	}
	headTokens, err := tokenize(headSrc)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "token-diff: scanning head: %v\n", err)
		return 2
	}

	if tokensEqual(baseTokens, headTokens) {
		_, _ = fmt.Fprintln(stdout, "token-diff: token streams identical (comment-only or whitespace-only change)")
		return 0
	}
	if *allowRename {
		rename, ok, err := internalImportRenameOnly(baseSrc, headSrc)
		if err != nil {
			_, _ = fmt.Fprintf(stderr, "token-diff: checking import rename: %v\n", err)
			return 2
		}
		if ok {
			_, _ = fmt.Fprintf(stdout, "token-diff: internal import rename only: %s\n", rename)
			return 0
		}
	}
	_, _ = fmt.Fprintln(stdout, "token-diff: token streams differ")
	return 1
}

// hasCgoImport reports whether src declares `import "C"`, the cgo preamble
// marker. parser.ImportsOnly stops right after the import block, so this is
// cheap even on a large file.
func hasCgoImport(src []byte) (bool, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "src.go", src, parser.ImportsOnly)
	if err != nil {
		return false, err
	}
	for _, imp := range f.Imports {
		if imp.Path.Value == `"C"` {
			return true, nil
		}
	}
	return false, nil
}

// tok is one token in the compared stream: its kind and, for every kind
// except token.SEMICOLON, its exact literal text.
type tok struct {
	kind token.Token
	lit  string
}

// tokenize lexes src with go/scanner in ScanComments mode and returns the
// filtered token stream the comment-only exemption compares. Plain `//` line
// comments are dropped (owner ruling: they cannot change compiled
// behavior); block comments (`/* ... */`) and directive comments (`//go:...`,
// `//line ...`, `// +build ...`) are kept, so an edit to any of those still
// counts as a real change. Every other token is kept, including the
// automatically-inserted SEMICOLON tokens go/scanner emits at line ends --
// dropping those would make a newline that moves a statement boundary
// invisible to the comparison.
func tokenize(src []byte) ([]tok, error) {
	fset := token.NewFileSet()
	file := fset.AddFile("src.go", fset.Base(), len(src))
	var errs []string
	onErr := func(pos token.Position, msg string) {
		errs = append(errs, fmt.Sprintf("%s: %s", pos, msg))
	}
	var s scanner.Scanner
	s.Init(file, src, onErr, scanner.ScanComments)

	var out []tok
	for {
		_, kind, lit := s.Scan()
		if kind == token.EOF {
			break
		}
		if kind == token.COMMENT && isDroppableComment(lit) {
			continue
		}
		out = append(out, tok{kind: kind, lit: lit})
	}
	if len(errs) > 0 {
		return nil, fmt.Errorf("%s", strings.Join(errs, "; "))
	}
	return out, nil
}

// isDroppableComment reports whether lit -- a comment token's literal text,
// delimiters included -- is a plain `//` line comment with no directive
// meaning. Block comments and the three recognized directive-comment forms
// are never droppable.
func isDroppableComment(lit string) bool {
	if !strings.HasPrefix(lit, "//") {
		return false // a /* ... */ block comment.
	}
	body := lit[len("//"):]
	switch {
	case strings.HasPrefix(body, "go:"):
		return false // //go:build, //go:generate, //go:embed, //go:linkname, ...
	case body == "line" || strings.HasPrefix(body, "line "):
		return false // //line file:line[:col]
	case strings.HasPrefix(body, " +build"):
		return false // the legacy "// +build" constraint; the leading space is load-bearing.
	default:
		return true
	}
}

// tokensEqual compares two filtered token streams. token.SEMICOLON entries
// compare by kind only: go/scanner reports an explicit `;` and an
// automatically inserted end-of-line semicolon as the same token kind with
// different literals ("\n" for the automatic case), and a real statement
// boundary is a real statement boundary either way.
func tokensEqual(a, b []tok) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].kind != b[i].kind {
			return false
		}
		if a[i].kind == token.SEMICOLON {
			continue
		}
		if a[i].lit != b[i].lit {
			return false
		}
	}
	return true
}
