// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package git

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser"
)

// BenchmarkSymlinkTargetAdmitted measures the per-file cost the #8809 target
// check adds to discovery: one Lstat for a regular file, and EvalSymlinks,
// the location rules, and a Stat for an admitted symlink. The git/AGENTS.md
// "Symlink admission evidence" figures come from this benchmark.
func BenchmarkSymlinkTargetAdmitted(b *testing.B) {
	root := b.TempDir()
	if resolved, err := filepath.EvalSymlinks(root); err == nil {
		root = resolved
	}
	if err := os.MkdirAll(filepath.Join(root, "real"), 0o755); err != nil {
		b.Fatal(err)
	}
	for path, body := range map[string]string{"real/.nvmrc": "20.11.1\n", ".python-version": "3.12.2\n"} {
		if err := os.WriteFile(filepath.Join(root, path), []byte(body), 0o644); err != nil {
			b.Fatal(err)
		}
	}
	link := filepath.Join(root, ".nvmrc")
	if err := os.Symlink("real/.nvmrc", link); err != nil {
		b.Skipf("symlinks unavailable: %v", err)
	}
	admission := newSymlinkAdmission(root, parser.DefaultRegistry(), NativeRepositorySnapshotter{}.discoveryOptions())

	for _, bench := range []struct {
		name string
		path string
	}{
		{name: "regular_file", path: filepath.Join(root, ".python-version")},
		{name: "symlink", path: link},
	} {
		category := admission.category(bench.path)
		b.Run(bench.name, func(b *testing.B) {
			for b.Loop() {
				if admitted, _ := admission.symlinkTargetAdmitted(bench.path, category); !admitted {
					b.Fatalf("%s was not admitted", bench.path)
				}
			}
		})
	}
}
