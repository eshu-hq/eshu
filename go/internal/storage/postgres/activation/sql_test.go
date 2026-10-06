// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package activation

import (
	"regexp"
	"strconv"
	"testing"
)

var wakeQueryLimits = regexp.MustCompile(`(?m)^\s*LIMIT (\d+)\s*$`)

// TestWakeQueryLimitEqualsWakeBatchLimit pins the literal batch cap in
// wakeQuery to the exported WakeBatchLimit contract, so the two cannot drift
// apart without failing the blocking hermetic lane.
func TestWakeQueryLimitEqualsWakeBatchLimit(t *testing.T) {
	t.Parallel()
	limits := wakeQueryLimits.FindAllStringSubmatch(wakeQuery, -1)
	if len(limits) != 1 {
		t.Fatalf("wakeQuery must carry exactly one LIMIT clause, found %d", len(limits))
	}
	got, err := strconv.Atoi(limits[0][1])
	if err != nil {
		t.Fatalf("parse wakeQuery LIMIT %q: %v", limits[0][1], err)
	}
	if got != WakeBatchLimit {
		t.Fatalf("wakeQuery LIMIT %d, WakeBatchLimit %d", got, WakeBatchLimit)
	}
}
