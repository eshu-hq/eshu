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
// property scripts/verify-ifa-determinism.sh --teeth relies on to make
// independent runs of the determinism matrix diverge. Only
// `go test -tags ifadeterminismteeth ./...` compiles this file.
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
