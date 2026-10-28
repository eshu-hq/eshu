// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package component

import (
	"testing"
	"time"
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

// TestPageBoundConfigNeverWidens proves a widening operator page bound is
// clamped to the stdout-derived default the narrows-only contract promises:
// pages must fit the host stdout limit, so a bound above it can only buy a
// truncation plus retry loop, never more facts per call.
func TestPageBoundConfigNeverWidens(t *testing.T) {
	t.Parallel()

	def, err := pageBound(0)
	if err != nil {
		t.Fatalf("pageBound(0) error = %v", err)
	}
	for _, configured := range []int64{def, def + 1, 2 * def, 1 << 30} {
		got, err := pageBound(configured)
		if err != nil {
			t.Fatalf("pageBound(%d) error = %v, want clamp to default", configured, err)
		}
		if got != def {
			t.Fatalf("pageBound(%d) = %d, want default %d", configured, got, def)
		}
	}
}

// TestCursorRoundTrip proves minted cursors parse back to their page and
// claim observation time, and forgeries fail closed. The observation time
// travels in the token so every page of one claim stamps the same
// Generation.ObservedAt; a cursor minted before that (bare page-N) is a
// forgery now, since echoing the call clock would reintroduce drift.
func TestCursorRoundTrip(t *testing.T) {
	t.Parallel()

	stamp := time.Date(2026, time.October, 28, 12, 0, 0, 123456789, time.UTC)
	for _, index := range []int{0, 1, 63} {
		cursor := formatCursor(index, stamp)
		got, gotStamp, err := parseCursor(cursor)
		if err != nil {
			t.Fatalf("parseCursor(%q) error = %v", cursor, err)
		}
		if got != index {
			t.Fatalf("parseCursor(%q) = %d, want %d", cursor, got, index)
		}
		if !gotStamp.Equal(stamp) {
			t.Fatalf("parseCursor(%q) observed_at = %s, want %s", cursor, gotStamp.Format(time.RFC3339Nano), stamp.Format(time.RFC3339Nano))
		}
	}
	for _, forged := range []string{"", "page-", "page-x", "page--1", "sheet-1", "page-1;rm -rf /", "page-1", "page-1.", "page-1.x", "page-1.-5", "page-x.123", "page--1.123"} {
		if _, _, err := parseCursor(forged); err == nil {
			t.Fatalf("parseCursor(%q) error = nil, want rejection", forged)
		}
	}
	if err := checkCursorRange("page-2.123", 2, 3); err != nil {
		t.Fatalf("checkCursorRange(in-range) error = %v", err)
	}
	if err := checkCursorRange("page-3.123", 3, 3); err == nil {
		t.Fatal("checkCursorRange(out-of-range) error = nil, want rejection")
	}
}
