// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Parity guard for the one helper the #6059 extraction still duplicates.
//
// The extraction originally copied TWO helpers into internal/cli/scan, because
// go/cmd/eshu is package main and nothing can import it. #6129 then extracted
// internal/cli/procexec, which gave mergeEnvironment a real importable home --
// so internal/cli/scan now calls procexec.MergeEnvironment and that twin is
// gone rather than merely tested. Deleting a duplicate beats pinning it.
//
// pathExists has no such home yet: go/cmd/eshu/scan.go:148 keeps its own for
// the first-run runtime probe, and internal/cli/scan carries a copy. Until one
// of them moves somewhere both can reach, this is what stops them drifting.
//
// Two shapes, because behaviour alone does not cover everything:
//   - a behavioural table through both copies, reachable via TargetKind's
//     root/.git probe
//   - a token-identity check, for inputs the exported surface cannot vary
//
// The broken-symlink row is the one that matters most: it is exactly where
// os.Stat and os.Lstat disagree, and swapping one for the other is the drift a
// reviewer flagged as likely.

package main

// Parity tests tying internal/cli/scan's deliberate copies to their
// go/cmd/eshu originals.
//
// The #6059 extraction copied mergeEnvironment (local_host_config.go) and
// pathExists (scan.go) into internal/cli/scan as mergeEnv and pathExists,
// because this package is `package main` and cannot be imported, and the
// originals keep callers outside the scan family. The copies are
// byte-identical today, but nothing tied them together, so they could drift
// silently. These tests live here, not in the scan package, because the
// import only works in this direction.
//
// Two enforcement shapes:
//
//   - Behavioral tables run shared inputs through both sides where the scan
//     package's exported surface reaches its copy: Options.BootstrapEnv for
//     mergeEnv, TargetKind for pathExists.
//   - Both copies are additionally pinned token-identical to their originals
//     at the source level, which also covers the inputs the exported surface
//     cannot vary (the override map for mergeEnv).

import (
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/cli/scan"
)

func TestScanPathExistsMatchesScanCommandProbe(t *testing.T) {
	cases := []struct {
		name string
		make func(t *testing.T, gitPath string)
		want bool
	}{
		{
			"directory", func(t *testing.T, gitPath string) {
				t.Helper()
				if err := os.Mkdir(gitPath, 0o755); err != nil {
					t.Fatalf("mkdir: %v", err)
				}
			}, true,
		},
		{
			"regular file", func(t *testing.T, gitPath string) {
				t.Helper()
				if err := os.WriteFile(gitPath, []byte("gitdir: elsewhere\n"), 0o644); err != nil {
					t.Fatalf("write: %v", err)
				}
			}, true,
		},
		{
			"symlink to an existing directory", func(t *testing.T, gitPath string) {
				t.Helper()
				target := filepath.Join(filepath.Dir(gitPath), "real-git")
				if err := os.Mkdir(target, 0o755); err != nil {
					t.Fatalf("mkdir target: %v", err)
				}
				if err := os.Symlink(target, gitPath); err != nil {
					t.Skipf("symlinks unavailable: %v", err)
				}
			}, true,
		},
		{
			"broken symlink", func(t *testing.T, gitPath string) {
				t.Helper()
				target := filepath.Join(filepath.Dir(gitPath), "missing-target")
				if err := os.Symlink(target, gitPath); err != nil {
					t.Skipf("symlinks unavailable: %v", err)
				}
			}, false,
		},
		{
			"missing path", func(t *testing.T, gitPath string) {
				t.Helper()
			}, false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			gitPath := filepath.Join(root, ".git")
			tc.make(t, gitPath)
			original := pathExists(gitPath)
			viaCopy := scan.TargetKind(root, false) == "repository"
			if original != viaCopy {
				t.Fatalf("pathExists copies diverged: cmd/eshu original = %v, scan copy via TargetKind = %v", original, viaCopy)
			}
			if original != tc.want {
				t.Fatalf("both pathExists copies reported %v, want %v", original, tc.want)
			}
		})
	}
}

// scanParityFuncBody parses file and returns name's function body rendered
// through go/format, so gofmt-equivalent bodies compare equal while any token
// change does not. Comments attach to the file, not the body node, so
// comment-only edits do not fire.
func scanParityFuncBody(t *testing.T, file, name string) string {
	t.Helper()
	fset := token.NewFileSet()
	parsed, err := parser.ParseFile(fset, file, nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", file, err)
	}
	for _, decl := range parsed.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Recv != nil || fn.Name.Name != name {
			continue
		}
		var rendered strings.Builder
		if err := format.Node(&rendered, fset, fn.Body); err != nil {
			t.Fatalf("render %s from %s: %v", name, file, err)
		}
		return rendered.String()
	}
	t.Fatalf("function %s not found in %s; if it was renamed or moved, update this parity test with it", name, file)
	return ""
}

// TestScanPathExistsCopyIsTokenIdentical pins the surviving copy to its
// original at the source level: the copy's function body must be
// token-identical. The copy was created byte-identical on purpose; a change to
// one alone is a bug, and a legitimate change made to both keeps this green.
// It covers what the behavioural table cannot reach through the exported
// surface, and ignores comment-only edits.
//
// The merge pair used to be pinned here too. It no longer exists: #6129's
// procexec extraction gave mergeEnvironment an importable home, so
// internal/cli/scan calls procexec.MergeEnvironment and there is nothing left
// to compare.
func TestScanPathExistsCopyIsTokenIdentical(t *testing.T) {
	const scanCopyFile = "../../internal/cli/scan/scan.go"
	pairs := []struct {
		originalFile string
		originalName string
		copyName     string
	}{
		{"scan.go", "pathExists", "pathExists"},
	}
	for _, pair := range pairs {
		t.Run(pair.copyName, func(t *testing.T) {
			original := scanParityFuncBody(t, pair.originalFile, pair.originalName)
			copied := scanParityFuncBody(t, scanCopyFile, pair.copyName)
			if original != copied {
				t.Fatalf(
					"%s (internal/cli/scan/scan.go) drifted from %s (cmd/eshu/%s); change both or neither\noriginal:\n%s\ncopy:\n%s",
					pair.copyName, pair.originalName, pair.originalFile, original, copied,
				)
			}
		})
	}
}
