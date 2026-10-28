// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package component

import (
	"testing"
)

// TestPageBoundMirrorsSDKAccounting pins the page-bound arithmetic against
// the host stdout limit, mirroring collector.PageBoundForStdout in
// sdk/go/collector: the 4 MiB limit minus 64 KiB envelope headroom.
// Reconcile with the SDK helper when the eshu-sdk/collector pin carries
// pagination.
func TestPageBoundMirrorsSDKAccounting(t *testing.T) {
	t.Parallel()

	bound, err := pageBoundForStdout(DefaultStdoutLimitBytes)
	if err != nil {
		t.Fatalf("pageBoundForStdout(default) error = %v", err)
	}
	if want := DefaultStdoutLimitBytes - 64*1024; bound != want {
		t.Fatalf("bound = %d, want %d", bound, want)
	}
	if _, err := pageBoundForStdout(0); err == nil {
		t.Fatal("pageBoundForStdout(0) error = nil, want rejection")
	}
	if _, err := pageBoundForStdout(1024); err == nil {
		t.Fatal("pageBoundForStdout(headroom-only) error = nil, want rejection")
	}
}

// TestPageBoundConfigNarrowsOnly proves the operator page bound is honored
// when set and the default applies when absent.
func TestPageBoundConfigNarrowsOnly(t *testing.T) {
	t.Parallel()

	def, err := pageBound(0)
	if err != nil {
		t.Fatalf("pageBound(0) error = %v", err)
	}
	want, err := pageBoundForStdout(DefaultStdoutLimitBytes)
	if err != nil {
		t.Fatalf("pageBoundForStdout() error = %v", err)
	}
	if def != want {
		t.Fatalf("pageBound(0) = %d, want default %d", def, want)
	}
	if got, err := pageBound(8192); err != nil || got != 8192 {
		t.Fatalf("pageBound(8192) = %d, %v, want 8192 nil", got, err)
	}
	if _, err := pageBound(-1); err == nil {
		t.Fatal("pageBound(-1) error = nil, want rejection")
	}
}

// TestCursorRoundTrip proves minted cursors parse back to their page and
// forgeries fail closed.
func TestCursorRoundTrip(t *testing.T) {
	t.Parallel()

	for _, index := range []int{0, 1, 63} {
		cursor := formatCursor(index)
		got, err := parseCursorIndex(cursor)
		if err != nil {
			t.Fatalf("parseCursorIndex(%q) error = %v", cursor, err)
		}
		if got != index {
			t.Fatalf("parseCursorIndex(%q) = %d, want %d", cursor, got, index)
		}
	}
	for _, forged := range []string{"", "page-", "page-x", "page--1", "sheet-1", "page-1;rm -rf /"} {
		if _, err := parseCursorIndex(forged); err == nil {
			t.Fatalf("parseCursorIndex(%q) error = nil, want rejection", forged)
		}
	}
	if err := checkCursorRange("page-2", 2, 3); err != nil {
		t.Fatalf("checkCursorRange(in-range) error = %v", err)
	}
	if err := checkCursorRange("page-3", 3, 3); err == nil {
		t.Fatal("checkCursorRange(out-of-range) error = nil, want rejection")
	}
}
