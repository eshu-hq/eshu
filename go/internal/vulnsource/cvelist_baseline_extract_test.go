// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package vulnsource

import (
	"archive/zip"
	"os"
	"path/filepath"
	"testing"
)

// The upstream midnight asset is a wrapper: exactly one member, cves.zip,
// which itself holds the records (verified live 2026-11-07: outer
// 654,413,635 bytes, inner 723,679,992 bytes DEFLATEd, so per-shard reads
// need the extracted inner archive, not a section view).
func TestCVEListBaselineExtractsInnerZip(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	inner, err := os.Create(filepath.Join(dir, "inner.zip"))
	if err != nil {
		t.Fatal(err)
	}
	iw := zip.NewWriter(inner)
	fw, err := iw.Create("cves/2026/83xxx/CVE-2026-83844.json")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fw.Write([]byte(`{}`)); err != nil {
		t.Fatal(err)
	}
	if err := iw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := inner.Close(); err != nil {
		t.Fatal(err)
	}

	innerRaw, err := os.ReadFile(filepath.Join(dir, "inner.zip"))
	if err != nil {
		t.Fatal(err)
	}
	outerPath := filepath.Join(dir, "midnight.zip.zip")
	outer, err := os.Create(outerPath)
	if err != nil {
		t.Fatal(err)
	}
	ow := zip.NewWriter(outer)
	fw, err = ow.Create("cves.zip")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fw.Write(innerRaw); err != nil {
		t.Fatal(err)
	}
	if err := ow.Close(); err != nil {
		t.Fatal(err)
	}
	if err := outer.Close(); err != nil {
		t.Fatal(err)
	}

	dest := filepath.Join(dir, "cves.zip")
	if err := ExtractCVEListInnerZip(outerPath, dest); err != nil {
		t.Fatalf("ExtractCVEListInnerZip() err = %v", err)
	}
	count, err := CVEListBaselineMemberCount(dest)
	if err != nil {
		t.Fatalf("MemberCount(extracted) err = %v", err)
	}
	if count != 1 {
		t.Fatalf("MemberCount(extracted) = %d, want 1", count)
	}
}

func TestCVEListBaselineExtractRejectsUnexpectedLayout(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	outerPath := filepath.Join(dir, "odd.zip")
	outer, err := os.Create(outerPath)
	if err != nil {
		t.Fatal(err)
	}
	ow := zip.NewWriter(outer)
	for _, name := range []string{"a.zip", "b.zip"} {
		fw, err := ow.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := fw.Write([]byte(`{}`)); err != nil {
			t.Fatal(err)
		}
	}
	if err := ow.Close(); err != nil {
		t.Fatal(err)
	}
	if err := outer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := ExtractCVEListInnerZip(outerPath, filepath.Join(dir, "out.zip")); err == nil {
		t.Fatal("ExtractCVEListInnerZip() err = nil for a two-member wrapper, want layout failure")
	}
}
