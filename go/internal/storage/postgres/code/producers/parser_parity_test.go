// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package producerstore_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser"
	producerstore "github.com/eshu-hq/eshu/go/internal/storage/postgres/code/producers"
)

// TestGoModuleNameMatchesTheImportPathTheParserStamps pins the contract the Go
// symbol anchor depends on. The definition loader reads a stored go.mod with
// GoModuleName; the parser stamped every definition's import path from its
// own reading of the same file. If the two ever read a go.mod differently, the
// anchor scans the wrong repositories and a key silently stays unresolved
// (#7623). Each input is written to a temporary repository next to a one-file
// Go package, the parser's public pre-scan computes that package's import path,
// and the module path it implies must equal GoModuleName's answer. An empty
// module means the parser stamps no import path at all.
func TestGoModuleNameMatchesTheImportPathTheParserStamps(t *testing.T) {
	t.Parallel()

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	for _, tc := range []struct {
		name    string
		content string
	}{
		{"plain", "module github.com/acme/lib\n\ngo 1.24\n"},
		{"leading comments", "// header\n\nmodule github.com/acme/lib\n"},
		{"crlf", "module github.com/acme/lib\r\ngo 1.24\r\n"},
		{"classic mac line endings", "module github.com/acme/lib\rgo 1.24\r"},
		{"classic mac comment then module", "// header\rmodule github.com/acme/lib\r"},
		{"stray cr beside lf", "// c\rmodule a\nmodule b\n"},
		{"stray cr then module line", "x\rmodule a\n"},
		{"byte order mark", "\xef\xbb\xbfmodule github.com/acme/lib\n"},
		{"tabs", "\tmodule\tgithub.com/acme/lib\t\n"},
		{"trailing comment", "module github.com/acme/lib // old\n"},
		{"trailing comment then a plain module line", "module a // c\nmodule b\n"},
		{"quoted path", "module \"github.com/acme/lib\"\n"},
		{"module block", "module (\n\tgithub.com/acme/lib\n)\n"},
		{"two module lines", "module github.com/acme/one\nmodule github.com/acme/two\n"},
		{"module with no path", "module\n"},
		{"no module directive", "go 1.24\nrequire github.com/x/y v1.0.0\n"},
		{"empty", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			repoRoot := t.TempDir()
			if err := os.WriteFile(filepath.Join(repoRoot, "go.mod"), []byte(tc.content), 0o600); err != nil {
				t.Fatalf("write go.mod: %v", err)
			}
			packageDir := filepath.Join(repoRoot, "pkg")
			if err := os.MkdirAll(packageDir, 0o750); err != nil {
				t.Fatalf("mkdir: %v", err)
			}
			source := filepath.Join(packageDir, "x.go")
			if err := os.WriteFile(source, []byte("package pkg\n\nfunc F() {}\n"), 0o600); err != nil {
				t.Fatalf("write source: %v", err)
			}

			roots, err := engine.PreScanGoPackageSemanticRoots(repoRoot, []string{source})
			if err != nil {
				t.Fatalf("PreScanGoPackageSemanticRoots() error = %v, want nil", err)
			}
			stamped := roots[packageDir].ImportPath
			wantModule := ""
			if stamped != "" {
				var ok bool
				wantModule, ok = strings.CutSuffix(stamped, "/pkg")
				if !ok {
					t.Fatalf("parser stamped import path %q, want a module path plus /pkg", stamped)
				}
			}
			if got := producerstore.GoModuleName(tc.content); got != wantModule {
				t.Fatalf("GoModuleName(%q) = %q, but the parser stamps import path %q (module %q)", tc.content, got, stamped, wantModule)
			}
		})
	}
}
