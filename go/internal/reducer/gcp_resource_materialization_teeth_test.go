//go:build ifadeterminismteeth

// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"strconv"
	"testing"
	"time"
)

// TestIfaTeethStampCloudResourceRowStampsWallClockNanos proves
// ifaTeethStampCloudResourceRow assigns row a parseable, strictly positive
// wall-clock nanosecond value under the ifadeterminismteeth build tag — the
// guaranteed-red-floor property scripts/verify-ifa-determinism.sh --teeth
// relies on to make independent runs of the determinism matrix diverge even
// if the sequence counter below were ever inert.
func TestIfaTeethStampCloudResourceRowStampsWallClockNanos(t *testing.T) {
	before := time.Now().UnixNano()
	row := map[string]any{}
	ifaTeethStampCloudResourceRow(row)
	after := time.Now().UnixNano()

	raw, ok := row[ifaTeethWriteOrderKey].(string)
	if !ok {
		t.Fatalf("expected a string ifa_teeth_write_order value, got %v (%T)", row[ifaTeethWriteOrderKey], row[ifaTeethWriteOrderKey])
	}
	stamped, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		t.Fatalf("ifa_teeth_write_order %q did not parse as int64: %v", raw, err)
	}
	if stamped < before || stamped > after {
		t.Fatalf("stamped value %d is not within [%d, %d] (before/after time.Now().UnixNano())", stamped, before, after)
	}
}

// TestIfaTeethStampCloudResourceRowDiffersAcrossIndependentCalls proves two
// stamps taken with a real time gap between them differ — the exact
// property the multi-cell matrix depends on: each of N=1/N=2/N=4 is an
// independent process, so even a tiny gap between two calls is enough to
// prove the stamped value is not a fixed constant.
func TestIfaTeethStampCloudResourceRowDiffersAcrossIndependentCalls(t *testing.T) {
	row1 := map[string]any{}
	ifaTeethStampCloudResourceRow(row1)
	time.Sleep(time.Microsecond)
	row2 := map[string]any{}
	ifaTeethStampCloudResourceRow(row2)

	if row1[ifaTeethWriteOrderKey] == row2[ifaTeethWriteOrderKey] {
		t.Fatalf("expected distinct wall-clock stamps, got %q for both rows", row1[ifaTeethWriteOrderKey])
	}
}

// TestIfaTeethStampCloudResourceRowStampsMonotonicSequence proves
// ifaTeethStampCloudResourceRow also assigns row a parseable ifa_teeth_seq
// value under the ifadeterminismteeth build tag, and that the
// process-global counter strictly increases across successive calls — the
// per-process monotonic ordering signal issue #4396 slice 6b reintroduces
// now that a multi-scope fixture makes it interleaving-sensitive again
// rather than inert.
func TestIfaTeethStampCloudResourceRowStampsMonotonicSequence(t *testing.T) {
	row1 := map[string]any{}
	ifaTeethStampCloudResourceRow(row1)
	row2 := map[string]any{}
	ifaTeethStampCloudResourceRow(row2)

	seq1Raw, ok := row1[ifaTeethSequenceKey].(string)
	if !ok {
		t.Fatalf("expected a string ifa_teeth_seq value, got %v (%T)", row1[ifaTeethSequenceKey], row1[ifaTeethSequenceKey])
	}
	seq2Raw, ok := row2[ifaTeethSequenceKey].(string)
	if !ok {
		t.Fatalf("expected a string ifa_teeth_seq value, got %v (%T)", row2[ifaTeethSequenceKey], row2[ifaTeethSequenceKey])
	}
	seq1, err := strconv.ParseInt(seq1Raw, 10, 64)
	if err != nil {
		t.Fatalf("ifa_teeth_seq %q did not parse as int64: %v", seq1Raw, err)
	}
	seq2, err := strconv.ParseInt(seq2Raw, 10, 64)
	if err != nil {
		t.Fatalf("ifa_teeth_seq %q did not parse as int64: %v", seq2Raw, err)
	}
	if seq2 <= seq1 {
		t.Fatalf("expected strictly increasing ifa_teeth_seq across calls, got %d then %d", seq1, seq2)
	}
}
