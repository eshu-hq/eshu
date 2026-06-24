// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"strings"
	"testing"
)

func TestUpsertManagedBlockInsertsIntoEmptyFile(t *testing.T) {
	out := upsertManagedBlock("", "hello")
	if !strings.Contains(out, guidanceBeginMarker) || !strings.Contains(out, guidanceEndMarker) {
		t.Fatalf("expected markers in output, got %q", out)
	}
	body, found := extractManagedBody(out)
	if !found || body != "hello" {
		t.Fatalf("expected body 'hello', got %q found=%v", body, found)
	}
}

func TestUpsertManagedBlockAppendsPreservingExistingContent(t *testing.T) {
	existing := "# My Project\n\nSome rules here.\n"
	out := upsertManagedBlock(existing, "eshu body")
	if !strings.HasPrefix(out, "# My Project\n\nSome rules here.") {
		t.Fatalf("existing content not preserved at head: %q", out)
	}
	body, found := extractManagedBody(out)
	if !found || body != "eshu body" {
		t.Fatalf("expected appended body, got %q found=%v", body, found)
	}
}

func TestUpsertManagedBlockReplacesPreservingBeforeAndAfter(t *testing.T) {
	before := "# Heading\n\nIntro paragraph that must survive.\n\n"
	after := "\n\n## Trailing Section\n\nThis text also must survive.\n"
	existing := before + renderManagedBlock("OLD BODY") + after

	out := upsertManagedBlock(existing, "NEW BODY")

	if !strings.Contains(out, "Intro paragraph that must survive.") {
		t.Fatalf("text before block was lost: %q", out)
	}
	if !strings.Contains(out, "## Trailing Section") || !strings.Contains(out, "This text also must survive.") {
		t.Fatalf("text after block was lost: %q", out)
	}
	if strings.Contains(out, "OLD BODY") {
		t.Fatalf("old body should have been replaced: %q", out)
	}
	body, _ := extractManagedBody(out)
	if body != "NEW BODY" {
		t.Fatalf("expected NEW BODY, got %q", body)
	}
	// Exactly one managed block.
	if got := strings.Count(out, guidanceBeginMarker); got != 1 {
		t.Fatalf("expected exactly one begin marker, got %d", got)
	}
}

func TestUpsertManagedBlockIsIdempotent(t *testing.T) {
	existing := "# Project\n\nrules\n"
	once := upsertManagedBlock(existing, "eshu body")
	twice := upsertManagedBlock(once, "eshu body")
	if once != twice {
		t.Fatalf("reinstall not idempotent:\nonce=%q\ntwice=%q", once, twice)
	}
}

func TestRemoveManagedBlockPreservesSurroundingText(t *testing.T) {
	before := "# Heading\n\nKeep me before.\n\n"
	after := "\n\n## After\n\nKeep me after.\n"
	existing := before + renderManagedBlock("body") + after

	out, removed := removeManagedBlock(existing)
	if !removed {
		t.Fatal("expected removed=true")
	}
	if strings.Contains(out, guidanceBeginMarker) || strings.Contains(out, guidanceEndMarker) {
		t.Fatalf("markers should be gone: %q", out)
	}
	if !strings.Contains(out, "Keep me before.") || !strings.Contains(out, "Keep me after.") {
		t.Fatalf("surrounding text lost: %q", out)
	}
}

func TestRemoveManagedBlockOnlyBlockYieldsEmpty(t *testing.T) {
	existing := renderManagedBlock("body") + "\n"
	out, removed := removeManagedBlock(existing)
	if !removed {
		t.Fatal("expected removed=true")
	}
	if strings.TrimSpace(out) != "" {
		t.Fatalf("expected empty output, got %q", out)
	}
}

func TestRemoveManagedBlockAbsentReturnsUnchanged(t *testing.T) {
	existing := "# Project\n\nno block here\n"
	out, removed := removeManagedBlock(existing)
	if removed {
		t.Fatal("expected removed=false")
	}
	if out != existing {
		t.Fatalf("content should be unchanged: %q", out)
	}
}

func TestClassifyBlock(t *testing.T) {
	body := "desired body"
	if got := classifyBlock("no block", body); got != blockAbsent {
		t.Fatalf("expected blockAbsent, got %v", got)
	}
	current := upsertManagedBlock("# h\n", body)
	if got := classifyBlock(current, body); got != blockCurrent {
		t.Fatalf("expected blockCurrent, got %v", got)
	}
	stale := upsertManagedBlock("# h\n", "different")
	if got := classifyBlock(stale, body); got != blockStale {
		t.Fatalf("expected blockStale, got %v", got)
	}
}

func TestFindManagedBlockMalformedTreatedAsAbsent(t *testing.T) {
	// Begin marker present but no end marker: must be treated as absent so a
	// fresh block is appended rather than corrupting the file.
	malformed := guidanceBeginMarker + "\ndangling body without end\n"
	if _, _, found := findManagedBlock(malformed); found {
		t.Fatal("malformed block (no end marker) must report not found")
	}
	out := upsertManagedBlock(malformed, "new")
	if strings.Count(out, guidanceBeginMarker) != 2 {
		t.Fatalf("expected a fresh block appended, got %q", out)
	}
}

func TestManagedBlockSummary(t *testing.T) {
	cases := map[blockStatus]string{
		blockCurrent: "current",
		blockStale:   "out-of-date",
		blockAbsent:  "not installed",
	}
	for status, want := range cases {
		if got := managedBlockSummary(status); got != want {
			t.Fatalf("summary(%v) = %q, want %q", status, got, want)
		}
	}
}
