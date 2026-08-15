// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package assistantguidance

import (
	"strings"
	"testing"
)

func TestUpsertInsertsIntoEmptyFile(t *testing.T) {
	out := Upsert("", "hello")
	if !strings.Contains(out, BeginMarker) || !strings.Contains(out, EndMarker) {
		t.Fatalf("expected markers in output, got %q", out)
	}
	body, found := ExtractBody(out)
	if !found || body != "hello" {
		t.Fatalf("expected body 'hello', got %q found=%v", body, found)
	}
}

func TestUpsertAppendsPreservingExistingContent(t *testing.T) {
	existing := "# My Project\n\nSome rules here.\n"
	out := Upsert(existing, "eshu body")
	if !strings.HasPrefix(out, "# My Project\n\nSome rules here.") {
		t.Fatalf("existing content not preserved at head: %q", out)
	}
	body, found := ExtractBody(out)
	if !found || body != "eshu body" {
		t.Fatalf("expected appended body, got %q found=%v", body, found)
	}
}

func TestUpsertReplacesPreservingBeforeAndAfter(t *testing.T) {
	before := "# Heading\n\nIntro paragraph that must survive.\n\n"
	after := "\n\n## Trailing Section\n\nThis text also must survive.\n"
	existing := before + RenderBlock("OLD BODY") + after

	out := Upsert(existing, "NEW BODY")

	if !strings.Contains(out, "Intro paragraph that must survive.") {
		t.Fatalf("text before block was lost: %q", out)
	}
	if !strings.Contains(out, "## Trailing Section") || !strings.Contains(out, "This text also must survive.") {
		t.Fatalf("text after block was lost: %q", out)
	}
	if strings.Contains(out, "OLD BODY") {
		t.Fatalf("old body should have been replaced: %q", out)
	}
	body, _ := ExtractBody(out)
	if body != "NEW BODY" {
		t.Fatalf("expected NEW BODY, got %q", body)
	}
	if got := strings.Count(out, BeginMarker); got != 1 {
		t.Fatalf("expected exactly one begin marker, got %d", got)
	}
}

func TestUpsertIsIdempotent(t *testing.T) {
	existing := "# Project\n\nrules\n"
	once := Upsert(existing, "eshu body")
	twice := Upsert(once, "eshu body")
	if once != twice {
		t.Fatalf("reinstall not idempotent:\nonce=%q\ntwice=%q", once, twice)
	}
}

func TestRemovePreservesSurroundingText(t *testing.T) {
	before := "# Heading\n\nKeep me before.\n\n"
	after := "\n\n## After\n\nKeep me after.\n"
	existing := before + RenderBlock("body") + after

	out, removed := Remove(existing)
	if !removed {
		t.Fatal("expected removed=true")
	}
	if strings.Contains(out, BeginMarker) || strings.Contains(out, EndMarker) {
		t.Fatalf("markers should be gone: %q", out)
	}
	if !strings.Contains(out, "Keep me before.") || !strings.Contains(out, "Keep me after.") {
		t.Fatalf("surrounding text lost: %q", out)
	}
}

func TestRemoveOnlyBlockYieldsEmpty(t *testing.T) {
	existing := RenderBlock("body") + "\n"
	out, removed := Remove(existing)
	if !removed {
		t.Fatal("expected removed=true")
	}
	if strings.TrimSpace(out) != "" {
		t.Fatalf("expected empty output, got %q", out)
	}
}

func TestRemoveAbsentReturnsUnchanged(t *testing.T) {
	existing := "# Project\n\nno block here\n"
	out, removed := Remove(existing)
	if removed {
		t.Fatal("expected removed=false")
	}
	if out != existing {
		t.Fatalf("content should be unchanged: %q", out)
	}
}

func TestClassify(t *testing.T) {
	body := "desired body"
	if got := Classify("no block", body); got != BlockAbsent {
		t.Fatalf("expected BlockAbsent, got %v", got)
	}
	current := Upsert("# h\n", body)
	if got := Classify(current, body); got != BlockCurrent {
		t.Fatalf("expected BlockCurrent, got %v", got)
	}
	stale := Upsert("# h\n", "different")
	if got := Classify(stale, body); got != BlockStale {
		t.Fatalf("expected BlockStale, got %v", got)
	}
}

func TestFindBlockMalformedTreatedAsAbsent(t *testing.T) {
	// Begin marker present but no end marker: must be treated as absent so a
	// fresh block is appended rather than corrupting the file.
	malformed := BeginMarker + "\ndangling body without end\n"
	if _, _, found := FindBlock(malformed); found {
		t.Fatal("malformed block (no end marker) must report not found")
	}
	out := Upsert(malformed, "new")
	if strings.Count(out, BeginMarker) != 2 {
		t.Fatalf("expected a fresh block appended, got %q", out)
	}
}

func TestBlockSummary(t *testing.T) {
	cases := map[BlockStatus]string{
		BlockCurrent: "current",
		BlockStale:   "out-of-date",
		BlockAbsent:  "not installed",
	}
	for status, want := range cases {
		if got := BlockSummary(status); got != want {
			t.Fatalf("summary(%v) = %q, want %q", status, got, want)
		}
	}
}

// surroundingBytes splits content at the managed block and returns the bytes
// before and after it. It is the single helper every surrounding-content
// assertion below goes through, so "the surrounding bytes did not move" is
// checked one way rather than three.
func surroundingBytes(t *testing.T, content string) (before, after string) {
	t.Helper()
	start, end, found := FindBlock(content)
	if !found {
		t.Fatalf("expected a managed block in %q", content)
	}
	return content[:start], content[end:]
}

// TestRoundTripLeavesSurroundingBytesIdentical is the accuracy property this
// package exists for: insert, update, and remove must each rewrite ONLY the
// managed region. Every step asserts byte equality against the exact strings
// seeded above and below the block, not a substring match, so a stray newline
// or a dropped character fails the test.
func TestRoundTripLeavesSurroundingBytesIdentical(t *testing.T) {
	above := "# Team Rules\n\nAlways write tests first.\n\nSecond paragraph with  double  spaces.\n"
	below := "## Trailing Section\n\nKeep this trailing content.\n\tTabbed line.\n"
	original := above + "\n" + below

	// 1. Insert into a file with content above and below. Upsert appends, so
	//    seed the block by hand at the seam to model a real edited file.
	inserted := above + "\n" + RenderBlock("BODY ONE") + "\n\n" + below
	gotBefore, gotAfter := surroundingBytes(t, inserted)
	if gotBefore != above+"\n" {
		t.Fatalf("insert: bytes above the block = %q, want %q", gotBefore, above+"\n")
	}
	if gotAfter != "\n\n"+below {
		t.Fatalf("insert: bytes below the block = %q, want %q", gotAfter, "\n\n"+below)
	}

	// 2. Update the block in place. Everything outside the markers must be
	//    byte-identical to step 1.
	updated := Upsert(inserted, "BODY TWO — longer than the first body, with UTF-8 ✓")
	updBefore, updAfter := surroundingBytes(t, updated)
	if updBefore != gotBefore {
		t.Fatalf("update moved the bytes above the block:\n before=%q\n  after=%q", gotBefore, updBefore)
	}
	if updAfter != gotAfter {
		t.Fatalf("update moved the bytes below the block:\n before=%q\n  after=%q", gotAfter, updAfter)
	}
	if body, _ := ExtractBody(updated); body != "BODY TWO — longer than the first body, with UTF-8 ✓" {
		t.Fatalf("update did not replace the body, got %q", body)
	}

	// 3. Update again with the ORIGINAL body: the whole file must return to the
	//    exact bytes of step 1.
	restored := Upsert(updated, "BODY ONE")
	if restored != inserted {
		t.Fatalf("update round-trip is not byte-stable:\n want=%q\n  got=%q", inserted, restored)
	}

	// 4. Remove. The block and its seam whitespace go; the seeded text above and
	//    below survives byte-for-byte, joined by exactly one blank line.
	removedOut, removed := Remove(restored)
	if !removed {
		t.Fatal("remove: expected removed=true")
	}
	want := strings.TrimRight(above, "\n") + "\n\n" + below
	if removedOut != want {
		t.Fatalf("remove did not preserve surrounding bytes:\n want=%q\n  got=%q", want, removedOut)
	}
	if !strings.HasSuffix(removedOut, below) {
		t.Fatalf("remove lost the trailing bytes: %q", removedOut)
	}
	if strings.Contains(removedOut, BeginMarker) || strings.Contains(removedOut, EndMarker) {
		t.Fatalf("remove left a marker behind: %q", removedOut)
	}

	// The original seed is only used to show the fixture is what we think it
	// is; removal deliberately collapses the seam, so it is not byte-equal.
	if !strings.Contains(original, "Tabbed line.") {
		t.Fatal("fixture drift: original seed lost its tabbed line")
	}
}

// TestUpsertOnFileResemblingTheMarker pins how a near-miss marker is treated.
// A file that TALKS about the marker (the docs do) must not be mistaken for a
// managed block, but an EXACT marker pair anywhere in the file -- including
// inside a fenced code block -- is the managed region by definition, because
// the markers are the only identity the block has.
func TestUpsertOnFileResemblingTheMarker(t *testing.T) {
	t.Run("near miss is not a block", func(t *testing.T) {
		lookalikes := []string{
			"<!-- BEGIN ESHU GUIDANCE EXAMPLE -->",
			"<!--BEGIN ESHU GUIDANCE-->",
			"<!-- begin eshu guidance -->",
			"<!-- BEGIN ESHU  GUIDANCE -->",
		}
		for _, alike := range lookalikes {
			content := "# Docs\n\nWe wrap guidance in " + alike + " markers.\n"
			if _, _, found := FindBlock(content); found {
				t.Fatalf("lookalike %q was mistaken for a managed block", alike)
			}
			out := Upsert(content, "BODY")
			if !strings.HasPrefix(out, content) {
				t.Fatalf("lookalike %q: prose was rewritten:\n want prefix=%q\n got=%q", alike, content, out)
			}
			if got := strings.Count(out, BeginMarker); got != 1 {
				t.Fatalf("lookalike %q: expected exactly one real begin marker, got %d", alike, got)
			}
		}
	})

	t.Run("exact marker pair in prose is the block", func(t *testing.T) {
		content := "# Docs\n\n```\n" + BeginMarker + "\nsample\n" + EndMarker + "\n```\n"
		if _, _, found := FindBlock(content); !found {
			t.Fatal("an exact marker pair must be found; markers are the block's only identity")
		}
		out := Upsert(content, "BODY")
		if strings.Count(out, BeginMarker) != 1 {
			t.Fatalf("expected the documented pair to be replaced in place, got %q", out)
		}
		if !strings.HasPrefix(out, "# Docs\n\n```\n") || !strings.HasSuffix(out, "\n```\n") {
			t.Fatalf("bytes outside the marker pair moved: %q", out)
		}
	})
}

// TestUpsertPreservesCRLFBytes pins the CRLF behavior rather than claiming CRLF
// support: the markers this package writes are always LF, and a CRLF file keeps
// its carriage returns byte-for-byte in the surrounding content.
func TestUpsertPreservesCRLFBytes(t *testing.T) {
	existing := "# Windows Rules\r\n\r\nKeep the carriage returns.\r\n"
	out := Upsert(existing, "BODY")

	if !strings.HasPrefix(out, existing) {
		t.Fatalf("CRLF content was rewritten:\n want prefix=%q\n got=%q", existing, out)
	}
	if strings.Contains(RenderBlock("BODY"), "\r") {
		t.Fatal("the rendered block must use LF markers only")
	}
	if got := strings.Count(out, "\r\n"); got != 3 {
		t.Fatalf("expected the 3 seeded CRLF pairs to survive, got %d in %q", got, out)
	}

	removedOut, removed := Remove(out)
	if !removed {
		t.Fatal("expected the block to be removable from a CRLF file")
	}
	// TrimRight("\n") leaves the CR in place, which is why the restored file
	// keeps its final "\r" -- pinned so a future normalization is deliberate.
	want := "# Windows Rules\r\n\r\nKeep the carriage returns.\r\n"
	if removedOut != want {
		t.Fatalf("CRLF file not restored:\n want=%q\n  got=%q", want, removedOut)
	}
}
