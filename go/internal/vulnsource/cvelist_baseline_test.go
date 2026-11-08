// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package vulnsource

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 5.2 baseline store for eshu-hq/eshu#10354: the CVE List midnight zip is
// fetched to disk (streamed, sha256-verified), retained latest-plus-previous,
// and read per shard without loading whole (mirror ADR §3.1-3.3). The mirror
// cursor (fetchTime + baseline date/digest) persists in a sidecar next to
// the store; the Postgres GenerationStateWriter binding rides the
// deploy-cadence step and is recorded as follow-up in the evidence note.
func TestCVEListBaselineVerifyStream(t *testing.T) {
	t.Parallel()

	payload := []byte(`{"midnight": true}`)
	sum := sha256.Sum256(payload)
	digest := hex.EncodeToString(sum[:])

	dir := t.TempDir()
	zipPath := filepath.Join(dir, "2026-11-07_all_CVEs_at_midnight.zip")
	if err := os.WriteFile(zipPath, payload, 0o600); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := VerifyCVEListBaselineStream(f, digest); err != nil {
		t.Fatalf("VerifyCVEListBaselineStream() err = %v, want nil for matching sha256", err)
	}

	if err := VerifyCVEListBaselineStream(strings.NewReader("tampered"), digest); err == nil {
		t.Fatal("VerifyCVEListBaselineStream() err = nil, want mismatch failure")
	}
}

func TestCVEListBaselineRetentionKeepsLatestPrevious(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	for _, day := range []string{"2026-11-05", "2026-11-06", "2026-11-07", "not-a-baseline"} {
		if err := os.Mkdir(filepath.Join(dir, "baseline-"+day), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	// A file that merely looks like a baseline dir must never be pruned.
	if err := os.WriteFile(filepath.Join(dir, "baseline-2026-11-04"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	removed, err := PruneCVEListBaselines(dir)
	if err != nil {
		t.Fatalf("PruneCVEListBaselines() err = %v", err)
	}
	if len(removed) != 1 || removed[0] != "baseline-2026-11-05" {
		t.Fatalf("PruneCVEListBaselines() removed = %v, want [baseline-2026-11-05]", removed)
	}
	for _, keep := range []string{"baseline-2026-11-06", "baseline-2026-11-07", "baseline-not-a-baseline", "baseline-2026-11-04"} {
		if _, err := os.Stat(filepath.Join(dir, keep)); err != nil {
			t.Fatalf("PruneCVEListBaselines() removed %q, want it kept", keep)
		}
	}
}

func TestCVEListBaselineShardReadStreamsMembers(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	zipPath := filepath.Join(dir, "midnight.zip")
	w, err := os.Create(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(w)
	for _, name := range []string{
		"cves/2026/83xxx/CVE-2026-83844.json",
		"cves/2026/83xxx/CVE-2026-83845.json",
		"cves/2026/33xxx/CVE-2026-33818.json",
	} {
		fw, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := fw.Write([]byte(`{"cveMetadata":{"cveId":"x"}}`)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}

	members, err := CVEListBaselineShardMembers(zipPath, "cves/2026/83xxx")
	if err != nil {
		t.Fatalf("CVEListBaselineShardMembers() err = %v", err)
	}
	if len(members) != 2 {
		t.Fatalf("CVEListBaselineShardMembers() = %v, want 2 members", members)
	}
	count, err := CVEListBaselineMemberCount(zipPath)
	if err != nil {
		t.Fatalf("CVEListBaselineMemberCount() err = %v", err)
	}
	if count != 3 {
		t.Fatalf("CVEListBaselineMemberCount() = %d, want 3 (mirror count source)", count)
	}
}

func TestCVEListMirrorCursorRoundTrip(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	want := CVEListMirrorCursor{
		FetchTime:      "2026-11-07T23:29:09.281Z",
		BaselineDate:   "2026-11-07",
		BaselineSHA256: "bb5c9d5a",
	}
	if err := WriteCVEListMirrorCursor(dir, want); err != nil {
		t.Fatalf("WriteCVEListMirrorCursor() err = %v", err)
	}
	got, found, err := ReadCVEListMirrorCursor(dir)
	if err != nil {
		t.Fatalf("ReadCVEListMirrorCursor() err = %v", err)
	}
	if !found {
		t.Fatal("ReadCVEListMirrorCursor() found = false, want true after write")
	}
	if got != want {
		t.Fatalf("ReadCVEListMirrorCursor() = %+v, want %+v", got, want)
	}
	_, found, err = ReadCVEListMirrorCursor(t.TempDir())
	if err != nil || found {
		t.Fatalf("ReadCVEListMirrorCursor() on empty dir = (%v, %v), want (zero, false, nil)", found, err)
	}
}

func TestCVEListMirrorCursorAdvanceAndGap(t *testing.T) {
	t.Parallel()

	cur := CVEListMirrorCursor{FetchTime: "2026-11-07T23:23:16.258Z", BaselineDate: "2026-11-07"}
	advanced := cur.Advance("2026-11-07T23:29:09.281Z")
	if advanced.FetchTime != "2026-11-07T23:29:09.281Z" {
		t.Fatalf("Advance() fetchTime = %q, want the newest processed batch", advanced.FetchTime)
	}
	if cur.FetchTime != "2026-11-07T23:23:16.258Z" {
		t.Fatal("Advance() mutated the receiver; cursors are values")
	}
	// A tail read that never reached the cursor (log rolled past it) must
	// refuse to advance: the caller re-seeds from a fresh baseline.
	if !cur.NeedsReseed(false) {
		t.Fatal("NeedsReseed(false) = false, want true (fail closed on gap)")
	}
	if cur.NeedsReseed(true) {
		t.Fatal("NeedsReseed(true) = true, want false (reached window advances)")
	}
}
