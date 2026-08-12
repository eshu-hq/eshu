// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Tests for evaluateDirectory, the fs-based (no analysis.Pass required)
// decision function that combines the size cap, the naming rule, the
// nolint escape hatch, and the digest-pinned grandfather ledger into the
// list of findings run() should report for one package directory. Keeping
// this logic AST-free makes it directly testable against real temp
// directories without constructing a fake analysis.Pass.
package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeGoFiles creates one trivial .go file per name inside dir.
func writeGoFiles(t *testing.T, dir string, names ...string) {
	t.Helper()
	for _, n := range names {
		if err := os.WriteFile(filepath.Join(dir, n), []byte("package fixture\n"), 0o600); err != nil {
			t.Fatalf("write %s: %v", n, err)
		}
	}
}

// numberedFiles returns n distinct qualifying basenames, "file0000.go" .. .
func numberedFiles(n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = filenameFor(i)
	}
	return out
}

func filenameFor(i int) string {
	const digits = "0123456789"
	b := []byte{digits[i/1000%10], digits[i/100%10], digits[i/10%10], digits[i%10]}
	return "file" + string(b) + ".go"
}

func TestEvaluateDirectoryUnderCapNoViolation(t *testing.T) {
	dir := t.TempDir()
	writeGoFiles(t, dir, "a.go", "b.go")

	got, err := evaluateDirectory("test/under", dir, nil)
	if err != nil {
		t.Fatalf("evaluateDirectory: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("findings = %v, want none", got)
	}
}

func TestEvaluateDirectoryOverCapNotGrandfathered(t *testing.T) {
	dir := t.TempDir()
	writeGoFiles(t, dir, numberedFiles(maxDirFiles+1)...)

	got, err := evaluateDirectory("test/overcap", dir, nil)
	if err != nil {
		t.Fatalf("evaluateDirectory: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("findings = %v, want exactly 1 cap finding", got)
	}
	f := got[0]
	if f.File != "file0000.go" {
		t.Fatalf("cap finding reported against %q, want the representative file file0000.go", f.File)
	}
	for _, want := range []string{"test/overcap", "41", "40", "split it into a subpackage", "nolint:dirgate"} {
		if !strings.Contains(f.Message, want) {
			t.Fatalf("cap finding message %q missing %q", f.Message, want)
		}
	}
}

func TestEvaluateDirectoryNamingViolationUnderCap(t *testing.T) {
	dir := t.TempDir()
	writeGoFiles(t, dir, "bar_baz.go", "unrelated.go")
	mustMkdirGo(t, dir, "bar", "bar.go")

	got, err := evaluateDirectory("test/naming", dir, nil)
	if err != nil {
		t.Fatalf("evaluateDirectory: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("findings = %v, want exactly 1 naming finding", got)
	}
	f := got[0]
	if f.File != "bar_baz.go" {
		t.Fatalf("naming finding reported against %q, want bar_baz.go", f.File)
	}
	for _, want := range []string{"bar_baz.go", "bar", "nolint:dirgate"} {
		if !strings.Contains(f.Message, want) {
			t.Fatalf("naming finding message %q missing %q", f.Message, want)
		}
	}
}

func TestEvaluateDirectoryNolintSuppressesCapFinding(t *testing.T) {
	dir := t.TempDir()
	writeGoFiles(t, dir, numberedFiles(maxDirFiles+1)...)
	// representativeFile() picks the alphabetically-first file when there is
	// no doc.go, i.e. file0000.go here -- put the justified marker there.
	if err := os.WriteFile(filepath.Join(dir, "file0000.go"),
		[]byte("package fixture //nolint:dirgate // intentionally oversized, tracked in #9999\n"), 0o600); err != nil {
		t.Fatalf("write nolint fixture: %v", err)
	}

	got, err := evaluateDirectory("test/suppressed", dir, nil)
	if err != nil {
		t.Fatalf("evaluateDirectory: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("findings = %v, want none (nolint-suppressed)", got)
	}
}

func TestEvaluateDirectoryNolintSuppressesOnlyItsOwnNamingFinding(t *testing.T) {
	dir := t.TempDir()
	writeGoFiles(t, dir, "unrelated.go")
	mustMkdirGo(t, dir, "bar", "bar.go")
	mustMkdirGo(t, dir, "baz", "baz.go")
	if err := os.WriteFile(filepath.Join(dir, "bar_one.go"),
		[]byte("package fixture //nolint:dirgate // legacy shim kept here on purpose, see #1\n"), 0o600); err != nil {
		t.Fatalf("write nolint fixture: %v", err)
	}
	writeGoFiles(t, dir, "baz_two.go")

	got, err := evaluateDirectory("test/partial-suppress", dir, nil)
	if err != nil {
		t.Fatalf("evaluateDirectory: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("findings = %v, want exactly 1 (baz_two.go only; bar_one.go is nolint-suppressed)", got)
	}
	if got[0].File != "baz_two.go" {
		t.Fatalf("surviving finding = %q, want baz_two.go", got[0].File)
	}
}

func TestEvaluateDirectoryGrandfatheredExactMatch(t *testing.T) {
	dir := t.TempDir()
	files := numberedFiles(maxDirFiles + 5)
	writeGoFiles(t, dir, files...)
	digest := qualifyingDigest(files)
	gf := map[string]grandfatherEntry{
		"test/pinned": {FileCount: maxDirFiles + 5, Digest: digest},
	}

	got, err := evaluateDirectory("test/pinned", dir, gf)
	if err != nil {
		t.Fatalf("evaluateDirectory: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("findings = %v, want none (exact grandfathered snapshot)", got)
	}
}

func TestEvaluateDirectoryGrandfatheredShrinkIsFine(t *testing.T) {
	dir := t.TempDir()
	files := numberedFiles(maxDirFiles + 3)
	writeGoFiles(t, dir, files...)
	// Pin a HIGHER count than what's on disk now, as if files were removed
	// since landing without touching the ledger.
	gf := map[string]grandfatherEntry{
		"test/shrunk": {FileCount: maxDirFiles + 10, Digest: "does-not-matter-for-a-shrink"},
	}

	got, err := evaluateDirectory("test/shrunk", dir, gf)
	if err != nil {
		t.Fatalf("evaluateDirectory: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("findings = %v, want none (shrinking below the pinned count is fine)", got)
	}
}

func TestEvaluateDirectoryGrandfatheredSwapAtSameCountFails(t *testing.T) {
	dir := t.TempDir()
	pinnedFiles := numberedFiles(maxDirFiles + 2)
	pinnedDigest := qualifyingDigest(pinnedFiles)
	gf := map[string]grandfatherEntry{
		"test/swapped": {FileCount: maxDirFiles + 2, Digest: pinnedDigest},
	}

	// Same COUNT as pinned, but a different file set: swap the last pinned
	// file for a new one the ledger never saw.
	liveFiles := append([]string{}, pinnedFiles[:len(pinnedFiles)-1]...)
	liveFiles = append(liveFiles, "swapped-in.go")
	writeGoFiles(t, dir, liveFiles...)

	got, err := evaluateDirectory("test/swapped", dir, gf)
	if err != nil {
		t.Fatalf("evaluateDirectory: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("findings = %v, want exactly 1 cap finding (same-count swap)", got)
	}
	if !strings.Contains(got[0].Message, "swap") {
		t.Fatalf("swap finding message %q does not explain the swap", got[0].Message)
	}
}

func TestEvaluateDirectoryGrandfatheredGrowthFails(t *testing.T) {
	dir := t.TempDir()
	pinnedFiles := numberedFiles(maxDirFiles + 2)
	pinnedDigest := qualifyingDigest(pinnedFiles)
	gf := map[string]grandfatherEntry{
		"test/grown": {FileCount: maxDirFiles + 2, Digest: pinnedDigest},
	}

	writeGoFiles(t, dir, pinnedFiles...)
	writeGoFiles(t, dir, "brand-new-file.go")

	got, err := evaluateDirectory("test/grown", dir, gf)
	if err != nil {
		t.Fatalf("evaluateDirectory: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("findings = %v, want exactly 1 cap finding (growth)", got)
	}
	for _, want := range []string{"grew", "42", "43"} {
		if !strings.Contains(got[0].Message, want) {
			t.Fatalf("growth finding message %q missing %q", got[0].Message, want)
		}
	}
}

func TestEvaluateDirectoryGrandfatherGrowthAlsoUnsuppressesNaming(t *testing.T) {
	dir := t.TempDir()
	mustMkdirGo(t, dir, "bar", "bar.go")
	pinnedFiles := append(numberedFiles(maxDirFiles+1), "bar_legacy.go")
	pinnedDigest := qualifyingDigest(pinnedFiles)
	gf := map[string]grandfatherEntry{
		"test/grown-naming": {FileCount: len(pinnedFiles), Digest: pinnedDigest},
	}

	writeGoFiles(t, dir, pinnedFiles...)
	writeGoFiles(t, dir, "one-more.go") // grows the directory past its pin

	got, err := evaluateDirectory("test/grown-naming", dir, gf)
	if err != nil {
		t.Fatalf("evaluateDirectory: %v", err)
	}
	// Growth un-grandfathers the whole directory: expect the cap finding
	// AND the pre-existing naming violation (bar_legacy.go) to both surface.
	if len(got) != 2 {
		t.Fatalf("findings = %v, want 2 (cap + the now-unsuppressed naming violation)", got)
	}
}

func TestEvaluateDirectoryMissingDirReturnsError(t *testing.T) {
	_, err := evaluateDirectory("test/missing", filepath.Join(t.TempDir(), "gone"), nil)
	if err == nil {
		t.Fatal("evaluateDirectory on a missing directory returned nil error")
	}
}
