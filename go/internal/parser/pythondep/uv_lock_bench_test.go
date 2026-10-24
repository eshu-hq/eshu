// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package pythondep

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// benchmarkLockfiles writes a uv.lock and a poetry.lock that declare the same
// n packages and returns their paths, so the two readers are compared on
// equal input.
func benchmarkLockfiles(b *testing.B, n int) (uvPath, poetryPath string) {
	b.Helper()
	var uv, poetry strings.Builder
	uv.WriteString("version = 1\nrevision = 2\nrequires-python = \">=3.12\"\n")
	for i := 0; i < n; i++ {
		fmt.Fprintf(&uv, "\n[[package]]\nname = \"pkg-%d\"\nversion = \"1.%d.0\"\nsource = { registry = \"https://pypi.org/simple\" }\ndependencies = [\n    { name = \"dep-a\" },\n    { name = \"dep-b\", marker = \"python_full_version < '3.11'\" },\n]\nwheels = [\n    { url = \"https://files.example/pkg-%d.whl\", hash = \"sha256:aa\", size = 1 },\n]\n", i, i, i)
		fmt.Fprintf(&poetry, "\n[[package]]\nname = \"pkg-%d\"\nversion = \"1.%d.0\"\ndescription = \"x\"\noptional = false\npython-versions = \">=3.8\"\n", i, i)
	}
	dir := b.TempDir()
	uvPath = filepath.Join(dir, "uv.lock")
	poetryPath = filepath.Join(dir, "poetry.lock")
	if err := os.WriteFile(uvPath, []byte(uv.String()), 0o600); err != nil {
		b.Fatal(err)
	}
	if err := os.WriteFile(poetryPath, []byte(poetry.String()), 0o600); err != nil {
		b.Fatal(err)
	}
	return uvPath, poetryPath
}

// BenchmarkParseUVLock measures the uv.lock reader on 2,000 packages.
func BenchmarkParseUVLock(b *testing.B) {
	uvPath, _ := benchmarkLockfiles(b, 2000)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := ParseUVLock(uvPath); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkParsePoetryLockBaseline measures the closest existing lockfile
// reader on the same 2,000 packages, as the comparison point.
func BenchmarkParsePoetryLockBaseline(b *testing.B) {
	_, poetryPath := benchmarkLockfiles(b, 2000)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := ParsePoetryLock(poetryPath); err != nil {
			b.Fatal(err)
		}
	}
}
