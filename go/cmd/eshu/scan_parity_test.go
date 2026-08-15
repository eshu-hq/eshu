// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

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
	"slices"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/cli/scan"
)

// scanParityOverrides mirrors the override map Options.BootstrapEnv builds
// for the options scanParityOptions returns, so the mergeEnvironment side of
// the table receives the same second argument the scan copy does. If
// BootstrapEnv's overrides change, update this together with it; the parity
// table will fail until the two agree again.
func scanParityOverrides() (scan.Options, map[string]string) {
	opts := scan.Options{
		Target:   scan.Target{Root: "/scan/parity/root"},
		ReposDir: "/scan/parity/repos",
	}
	overrides := map[string]string{
		"ESHU_REPO_SOURCE_MODE":  "filesystem",
		"ESHU_FILESYSTEM_ROOT":   opts.Target.Root,
		"ESHU_FILESYSTEM_DIRECT": "true",
		"ESHU_REPOS_DIR":         opts.ReposDir,
	}
	return opts, overrides
}

// TestScanMergeEnvMatchesMergeEnvironment folds one table of KEY=VALUE bases
// through both merge implementations -- mergeEnvironment directly, and the
// scan copy through Options.BootstrapEnv -- and requires identical merged
// sets. The bases cover the shapes most likely to drift: an entry with no
// '=', an empty key, a duplicate key, a value containing '=', an empty
// value, an entry an override shadows, and empty and nil bases. Both sides
// emit map-iteration order, so the comparison sorts first.
func TestScanMergeEnvMatchesMergeEnvironment(t *testing.T) {
	opts, overrides := scanParityOverrides()
	cases := []struct {
		name string
		base []string
	}{
		{"nil base", nil},
		{"empty base", []string{}},
		{"entry without equals is dropped", []string{"MALFORMED", "KEPT=yes"}},
		{"empty key is kept", []string{"=headless-value"}},
		{"duplicate key, later wins", []string{"DUP=first", "DUP=second"}},
		{"value containing equals", []string{"CHAIN=a=b=c"}},
		{"empty value", []string{"EMPTYVAL="}},
		{"base entry shadowed by an override", []string{"ESHU_REPOS_DIR=stale-repos-dir"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			want := mergeEnvironment(tc.base, overrides)
			got := opts.BootstrapEnv(tc.base)
			slices.Sort(want)
			slices.Sort(got)
			if !slices.Equal(want, got) {
				t.Fatalf(
					"merge implementations diverged (or BootstrapEnv's overrides changed; update scanParityOverrides with them)\nmergeEnvironment: %q\nBootstrapEnv:     %q",
					want, got,
				)
			}
		})
	}

	merged := opts.BootstrapEnv([]string{"MALFORMED", "DUP=first", "DUP=second"})
	if slices.Contains(merged, "DUP=first") || !slices.Contains(merged, "DUP=second") {
		t.Fatalf("BootstrapEnv duplicate handling changed: %q, want the later DUP entry only", merged)
	}
	for _, entry := range merged {
		if strings.HasPrefix(entry, "MALFORMED") {
			t.Fatalf("BootstrapEnv kept an entry without '=': %q", merged)
		}
	}
}

// TestScanPathExistsMatchesScanCommandProbe probes both pathExists copies
// with the same filesystem states. The original is called directly; the scan
// copy is observed through TargetKind, which reports "repository" exactly
// when the copy sees <root>/.git. The broken-symlink row is the one that
// catches an os.Stat / os.Lstat swap: Stat follows the link and reports the
// missing target, Lstat would report the link itself.
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

// TestScanEnvAndPathCopiesAreTokenIdentical pins both scan copies to their
// originals at the source level: each copy's function body must be
// token-identical to its original. The copies were created byte-identical on
// purpose; a change to one alone is a bug, and a legitimate change made to
// both keeps this green. This also covers what the behavioral tables cannot
// reach through the exported surface, such as mergeEnv's handling of an
// arbitrary override map.
func TestScanEnvAndPathCopiesAreTokenIdentical(t *testing.T) {
	const scanCopyFile = "../../internal/cli/scan/scan.go"
	pairs := []struct {
		originalFile string
		originalName string
		copyName     string
	}{
		{"local_host_config.go", "mergeEnvironment", "mergeEnv"},
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
