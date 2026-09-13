// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// runCLI writes base and head to temp files and invokes run(), returning
// its exit code and the captured stdout/stderr for assertions.
func runCLI(t *testing.T, base, head string) (code int, stdout, stderr string) {
	t.Helper()
	dir := t.TempDir()
	basePath := filepath.Join(dir, "base.go")
	headPath := filepath.Join(dir, "head.go")
	if err := os.WriteFile(basePath, []byte(base), 0o600); err != nil {
		t.Fatalf("write base: %v", err)
	}
	if err := os.WriteFile(headPath, []byte(head), 0o600); err != nil {
		t.Fatalf("write head: %v", err)
	}
	var outBuf, errBuf bytes.Buffer
	code = run([]string{"-base", basePath, "-head", headPath}, &outBuf, &errBuf)
	return code, outBuf.String(), errBuf.String()
}

// TestK_WhitespaceOnlyCodeChange proves case K: extra spaces between tokens
// on a code line do not change the token stream, so the edit is exempt
// (exit 0).
func TestK_WhitespaceOnlyCodeChange(t *testing.T) {
	base := "package query\n\n// comment one\nconst languageX = 1\n"
	head := "package query\n\n// comment one\nconst  languageX = 1\n"
	code, out, errOut := runCLI(t, base, head)
	if code != 0 {
		t.Fatalf("case K: got exit %d, want 0 (exempt); stdout=%q stderr=%q", code, out, errOut)
	}
}

// TestK2_NewlineMovesStatementBoundary proves case K2: moving a newline so
// automatic semicolon insertion lands in a different place must NOT be
// exempt, even though only whitespace/newlines moved. Base has the operator
// at the end of the first line (no semicolon inserted there); head has the
// operand at the end of the first line (a semicolon IS inserted there), so
// the two SEMICOLON-inclusive token streams differ in length.
func TestK2_NewlineMovesStatementBoundary(t *testing.T) {
	base := "package query\n\nfunc languageF(a, b int) int {\n\tx := a +\n\t\tb\n\treturn x\n}\n"
	head := "package query\n\nfunc languageF(a, b int) int {\n\tx := a\n\t+b\n\treturn x\n}\n"
	code, out, errOut := runCLI(t, base, head)
	if code == 0 {
		t.Fatalf("case K2: got exit 0 (exempt), want non-zero (real change); stdout=%q stderr=%q", out, errOut)
	}
}

// TestE_GoBuildDirectiveChange proves case E: a //go:build directive is
// kept in the compared stream (not treated as a droppable plain comment),
// so changing its value must NOT be exempt.
func TestE_GoBuildDirectiveChange(t *testing.T) {
	base := "//go:build linux\n\npackage query\n\nconst languageX = 1\n"
	head := "//go:build darwin\n\npackage query\n\nconst languageX = 1\n"
	code, out, errOut := runCLI(t, base, head)
	if code == 0 {
		t.Fatalf("case E: got exit 0 (exempt), want non-zero (directive changed); stdout=%q stderr=%q", out, errOut)
	}
}

// TestE4_LineDirectiveChange proves case E4: a //line directive is kept in
// the compared stream, so changing its value must NOT be exempt.
func TestE4_LineDirectiveChange(t *testing.T) {
	base := "package query\n\n//line a.go:1\nconst languageX = 1\n"
	head := "package query\n\n//line b.go:9\nconst languageX = 1\n"
	code, out, errOut := runCLI(t, base, head)
	if code == 0 {
		t.Fatalf("case E4: got exit 0 (exempt), want non-zero (directive changed); stdout=%q stderr=%q", out, errOut)
	}
}

// TestE5_PlusBuildDirectiveChange proves case E5: the legacy "// +build"
// constraint comment (the leading space after // is load-bearing) is kept
// in the compared stream, so changing its value must NOT be exempt.
func TestE5_PlusBuildDirectiveChange(t *testing.T) {
	base := "// +build linux\n\npackage query\n\nconst languageX = 1\n"
	head := "// +build darwin\n\npackage query\n\nconst languageX = 1\n"
	code, out, errOut := runCLI(t, base, head)
	if code == 0 {
		t.Fatalf("case E5: got exit 0 (exempt), want non-zero (directive changed); stdout=%q stderr=%q", out, errOut)
	}
}

// TestF_RawStringLineStartingSlashSlash proves case F: a line starting with
// `//` INSIDE a raw string literal is query text, not a Go comment, so a
// real go/scanner tokenizes the whole backtick string as one STRING token
// and the edit must NOT be exempt.
func TestF_RawStringLineStartingSlashSlash(t *testing.T) {
	base := "package query\n\nconst languageQ = `\nMATCH (n)\n// keep\nRETURN n\n`\n"
	head := "package query\n\nconst languageQ = `\nMATCH (n)\n// changed\nRETURN n\n`\n"
	code, out, errOut := runCLI(t, base, head)
	if code == 0 {
		t.Fatalf("case F: got exit 0 (exempt), want non-zero (raw string content changed); stdout=%q stderr=%q", out, errOut)
	}
}

// TestD_BlockCommentAdded proves case D: adding a new /* ... */ block
// comment is a real token added to the stream, not a droppable plain
// comment, so the edit must NOT be exempt.
func TestD_BlockCommentAdded(t *testing.T) {
	base := "package query\n\n// comment one\nconst languageX = 1\n"
	head := "package query\n\n// comment one\n/* block */\nconst languageX = 1\n"
	code, out, errOut := runCLI(t, base, head)
	if code == 0 {
		t.Fatalf("case D: got exit 0 (exempt), want non-zero (block comment added); stdout=%q stderr=%q", out, errOut)
	}
}

// TestA_CommentOnlyEdit proves the base case: changing only the text of a
// plain // comment is exempt.
func TestA_CommentOnlyEdit(t *testing.T) {
	base := "package query\n\n// comment one\nconst languageX = 1\n"
	head := "package query\n\n// comment two\nconst languageX = 1\n"
	code, out, errOut := runCLI(t, base, head)
	if code != 0 {
		t.Fatalf("case A: got exit %d, want 0 (exempt); stdout=%q stderr=%q", code, out, errOut)
	}
}

// TestCGO_NeverExempt proves the cgo carve-out: a file with import "C" is
// never exempt, even for an edit confined to its preamble comment (which
// compiles as C source, not a Go comment).
func TestCGO_NeverExempt(t *testing.T) {
	base := "package query\n\n// #include <stdio.h>\nimport \"C\"\n\nconst languageX = 1\n"
	head := "package query\n\n// #include <stdlib.h>\nimport \"C\"\n\nconst languageX = 1\n"
	code, out, errOut := runCLI(t, base, head)
	if code == 0 {
		t.Fatalf("case CGO: got exit 0 (exempt), want non-zero (cgo preamble); stdout=%q stderr=%q", out, errOut)
	}
}

// TestScanError_FailsClosed proves the fail-closed contract: a file
// go/scanner itself cannot lex (here, an unterminated raw string literal)
// must never report exit 0, even though its literal content is
// byte-identical to a case that would otherwise be exempt.
func TestScanError_FailsClosed(t *testing.T) {
	base := "package query\n\nconst languageX = 1\n"
	head := "package query\n\nconst languageQ = `unterminated\n" // no closing backtick
	code, _, errOut := runCLI(t, base, head)
	if code == 0 {
		t.Fatalf("scan error case: got exit 0 (exempt), want non-zero (fail closed); stderr=%q", errOut)
	}
}
