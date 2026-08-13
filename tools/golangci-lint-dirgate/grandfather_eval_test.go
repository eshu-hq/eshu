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
	"fmt"
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

// TestEvaluateDirectoryGrandfatheredShrinkRequiresRepin pins the fix for the
// #6054 P1 ratchet defect (codex review on PR #6081): the OLD behavior
// treated any live count below the pinned FileCount as an unconditional
// pass, with no digest check at all. That let a directory pinned at, say,
// 50 shrink to 45 and then silently regrow to 49 -- one file under the
// original pin -- without ever touching the ledger, defeating the ratchet
// the whole grandfather mechanism exists to enforce. The fix: a
// grandfathered directory only passes at EXACTLY its pinned count with a
// matching digest; a shrink (like a swap or a grow) now requires the row to
// be re-pinned, so regrowth is always measured from the best (lowest)
// state the directory ever reached, not the original landing snapshot.
func TestEvaluateDirectoryGrandfatheredShrinkRequiresRepin(t *testing.T) {
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
	if len(got) != 1 {
		t.Fatalf("findings = %v, want exactly 1 cap finding (shrink requires re-pinning the ledger row)", got)
	}
	for _, want := range []string{
		"shrunk", fmt.Sprintf("%d", maxDirFiles+10), fmt.Sprintf("%d", maxDirFiles+3),
		"re-pin", "dirgate-digest", "dirgate-grandfather.tsv", "generate-dirgate-grandfather-go.sh",
	} {
		if !strings.Contains(got[0].Message, want) {
			t.Fatalf("shrink finding message %q missing %q", got[0].Message, want)
		}
	}
}

// TestEvaluateDirectoryGrandfatheredShrinkBelowCapNeedsNoRepin proves the
// shrink-requires-repin rule above only applies while the directory is
// STILL over the 40-file cap. Once a grandfathered directory's real count
// drops to or below the cap, it is no longer a cap offender at all --
// scripts/verify-dirgate.sh --all already nudges that row toward removal
// (dirgate_report_removable_grandfathers); this must not additionally
// report a cap finding requiring a re-pin for a row that should simply be
// deleted.
func TestEvaluateDirectoryGrandfatheredShrinkBelowCapNeedsNoRepin(t *testing.T) {
	dir := t.TempDir()
	files := numberedFiles(maxDirFiles - 2)
	writeGoFiles(t, dir, files...)
	gf := map[string]grandfatherEntry{
		"test/shrunk-under-cap": {FileCount: maxDirFiles + 10, Digest: "does-not-matter"},
	}

	got, err := evaluateDirectory("test/shrunk-under-cap", dir, gf)
	if err != nil {
		t.Fatalf("evaluateDirectory: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("findings = %v, want none (directory is under the cap entirely; the row should be removed, not re-pinned)", got)
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

// TestEvaluateDirectoryGrandfatherGrowthDoesNotUnsuppressPinnedNaming pins
// down the fix for the #6054 follow-up defect: the OLD namingCovered gate
// (grandfathered && count <= entry.FileCount) suppressed the naming check
// for the WHOLE directory whenever the live count sat at or below the
// pinned cap, and un-suppressed the WHOLE directory -- including
// already-pinned legacy violations -- the moment it grew past the cap. A
// per-file naming exemption must stay independent of the cap check: growth
// still fails the cap, but a pinned exemption for an unchanged file must
// not resurface just because some OTHER file pushed the count over the pin.
func TestEvaluateDirectoryGrandfatherGrowthDoesNotUnsuppressPinnedNaming(t *testing.T) {
	dir := t.TempDir()
	mustMkdirGo(t, dir, "bar", "bar.go")
	pinnedFiles := append(numberedFiles(maxDirFiles+1), "bar_legacy.go")
	pinnedDigest := qualifyingDigest(pinnedFiles)
	gf := map[string]grandfatherEntry{
		"test/grown-naming": {
			FileCount:    len(pinnedFiles),
			Digest:       pinnedDigest,
			NamingExempt: []string{"bar_legacy.go"},
		},
	}

	writeGoFiles(t, dir, pinnedFiles...)
	writeGoFiles(t, dir, "one-more.go") // grows the directory past its pin

	got, err := evaluateDirectory("test/grown-naming", dir, gf)
	if err != nil {
		t.Fatalf("evaluateDirectory: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("findings = %v, want exactly 1 (cap only; bar_legacy.go stays pinned-exempt)", got)
	}
	// bar_legacy.go sorts alphabetically first, so it IS the representative
	// file the cap finding is reported against -- check the MESSAGE, not
	// the File, to distinguish "cap finding that happens to name this file"
	// from "bar_legacy.go's own naming violation resurfaced".
	if strings.Contains(got[0].Message, "should move into the sibling subpackage") {
		t.Fatalf("bar_legacy.go's pinned naming exemption resurfaced merely because the directory grew past its cap: %v", got)
	}
	if !strings.Contains(got[0].Message, "exceeding the") {
		t.Fatalf("findings = %v, want the sole finding to be the cap violation", got)
	}
}

// TestEvaluateDirectoryNewNamingViolationBelowPinnedCountIsNotSuppressed is
// the primary regression test for the #6054 follow-up defect: a BRAND-NEW
// naming violation in a grandfathered directory whose live count is still
// BELOW its pinned peak used to be silently swallowed by the old aggregate
// namingCovered gate (it stays swallowed for as long as the directory's
// move-issues shrink it further, which is exactly backwards). It must be
// reported regardless of the directory's aggregate file count.
func TestEvaluateDirectoryNewNamingViolationBelowPinnedCountIsNotSuppressed(t *testing.T) {
	dir := t.TempDir()
	mustMkdirGo(t, dir, "bar", "bar.go")
	gf := map[string]grandfatherEntry{
		// FileCount pinned well above the live count, as if other files in
		// this directory moved out elsewhere without touching this row --
		// exactly the shape the epic's move-issues (#6056-#6062) produce.
		"test/new-naming": {
			FileCount:    50,
			Digest:       "irrelevant-because-live-count-is-below-the-pin",
			NamingExempt: []string{"bar_legacy.go"},
		},
	}
	writeGoFiles(t, dir, "bar_legacy.go", "unrelated.go")
	// A brand-new file that also collides with the "bar" subpackage but was
	// never pinned in NamingExempt.
	writeGoFiles(t, dir, "bar_new.go")

	got, err := evaluateDirectory("test/new-naming", dir, gf)
	if err != nil {
		t.Fatalf("evaluateDirectory: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("findings = %v, want exactly 1 (bar_new.go; bar_legacy.go stays pinned-exempt)", got)
	}
	if got[0].File != "bar_new.go" {
		t.Fatalf("finding reported against %q, want bar_new.go", got[0].File)
	}
}

// TestEvaluateDirectoryPinnedNamingExemptionStaysGreen is the positive
// counterpart of the test above: an already-pinned legacy naming violation
// stays suppressed even while the directory sits well below its pinned cap.
func TestEvaluateDirectoryPinnedNamingExemptionStaysGreen(t *testing.T) {
	dir := t.TempDir()
	mustMkdirGo(t, dir, "bar", "bar.go")
	gf := map[string]grandfatherEntry{
		"test/pinned-naming": {
			FileCount:    50,
			Digest:       "irrelevant-because-live-count-is-below-the-pin",
			NamingExempt: []string{"bar_legacy.go"},
		},
	}
	writeGoFiles(t, dir, "bar_legacy.go", "unrelated.go")

	got, err := evaluateDirectory("test/pinned-naming", dir, gf)
	if err != nil {
		t.Fatalf("evaluateDirectory: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("findings = %v, want none (bar_legacy.go is pinned-exempt)", got)
	}
}

// TestEvaluateDirectoryStaleNamingExemptionDoesNotCoverADifferentFile
// proves exemption matching is exact-name-only: a stale ledger pin for a
// file that has since been renamed or removed (its real fix, not a ledger
// edit) must never be read as covering some OTHER, unrelated file that
// happens to also violate the naming rule against the same subpackage.
func TestEvaluateDirectoryStaleNamingExemptionDoesNotCoverADifferentFile(t *testing.T) {
	dir := t.TempDir()
	mustMkdirGo(t, dir, "bar", "bar.go")
	gf := map[string]grandfatherEntry{
		"test/stale-exempt": {
			FileCount:    50,
			Digest:       "irrelevant-because-live-count-is-below-the-pin",
			NamingExempt: []string{"bar_legacy.go"},
		},
	}
	// bar_legacy.go is gone (renamed/moved); a different, never-pinned file
	// now collides with the same subpackage.
	writeGoFiles(t, dir, "bar_replacement.go")

	got, err := evaluateDirectory("test/stale-exempt", dir, gf)
	if err != nil {
		t.Fatalf("evaluateDirectory: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("findings = %v, want exactly 1 (bar_replacement.go; the stale bar_legacy.go pin must not cover a different file)", got)
	}
	if got[0].File != "bar_replacement.go" {
		t.Fatalf("finding reported against %q, want bar_replacement.go", got[0].File)
	}
}

func TestEvaluateDirectoryMissingDirReturnsError(t *testing.T) {
	_, err := evaluateDirectory("test/missing", filepath.Join(t.TempDir(), "gone"), nil)
	if err == nil {
		t.Fatal("evaluateDirectory on a missing directory returned nil error")
	}
}

// TestEvaluateDirectoryGrandfatheredCapNolintIsRefused pins the rule that a cap
// nolint cannot buy off a grandfathered directory. The marker goes on the
// directory's representative file and would suppress the cap for every file in
// it, indefinitely — one marker on internal/query's doc.go un-gates 880 files.
// The "split it into a subpackage" alternative does not compile for query,
// reducer, projector or mcp until the acyclic boundary lands, so refusing the
// hatch here leaves the reviewed pin bump as the only exit, and a pin bump is a
// one-line ledger diff a reviewer can see.
func TestEvaluateDirectoryGrandfatheredCapNolintIsRefused(t *testing.T) {
	dir := t.TempDir()
	files := numberedFiles(maxDirFiles + 6)
	writeGoFiles(t, dir, files...)
	// Pin BELOW the live count, so the directory is over its pin.
	gf := map[string]grandfatherEntry{
		"test/pinned": {FileCount: maxDirFiles + 5, Digest: "stale-digest"},
	}
	// A fully justified marker on the representative file — the move that works
	// on a directory with no ledger row.
	rep := representativeFile(files)
	marker := "package fixture //nolint:dirgate // splitting is blocked on the acyclic boundary\n"
	if err := os.WriteFile(filepath.Join(dir, rep), []byte(marker), 0o600); err != nil {
		t.Fatalf("write nolint marker on %s: %v", rep, err)
	}

	got, err := evaluateDirectory("test/pinned", dir, gf)
	if err != nil {
		t.Fatalf("evaluateDirectory: %v", err)
	}
	if len(got) == 0 {
		t.Fatal("findings = none; a justified cap nolint must NOT suppress a grandfathered directory, or one marker silently un-gates the whole directory forever")
	}
}

// TestCapMessageGrandfatheredDoesNotOfferNolint pins the remediation text to
// what evaluateDirectory will actually honor. A grandfathered directory's
// //nolint escape is refused there, so offering it in the finding sends the
// author to add a marker the gate ignores -- and the bash mirror in
// scripts/lib/dirgate-core.sh already words these two cases differently, so
// getting it wrong here is a silent Go/bash divergence, the exact failure the
// mirror exists to prevent.
func TestCapMessageGrandfatheredDoesNotOfferNolint(t *testing.T) {
	t.Parallel()

	grandfathered := capMessage("internal/query", maxDirFiles+840, "", true)
	if strings.Contains(grandfathered, "// <reason>") {
		t.Fatalf("grandfathered cap message offers the //nolint escape the gate refuses: %q", grandfathered)
	}
	for _, want := range []string{
		"grandfathered", "will NOT suppress it", grandfatherLedger, "reviewed commit",
	} {
		if !strings.Contains(grandfathered, want) {
			t.Fatalf("grandfathered cap message %q missing %q", grandfathered, want)
		}
	}

	// The ungrandfathered case keeps the escape, which is honored there.
	plain := capMessage("internal/newpkg", maxDirFiles+1, "", false)
	for _, want := range []string{"split it into a subpackage", "//nolint:" + gateName, "// <reason>"} {
		if !strings.Contains(plain, want) {
			t.Fatalf("ungrandfathered cap message %q missing %q", plain, want)
		}
	}
	if strings.Contains(plain, "will NOT suppress") {
		t.Fatalf("ungrandfathered cap message wrongly claims the escape is refused: %q", plain)
	}
}
