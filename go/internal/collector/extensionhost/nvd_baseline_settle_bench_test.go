// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package extensionhost

import (
	"strconv"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// benchmarkSettlementItems returns n distinct work items, built once so the
// loops below measure the stash and not key formatting.
func benchmarkSettlementItems(n int) []workflow.WorkItem {
	items := make([]workflow.WorkItem, n)
	for i := range items {
		items[i] = workflow.WorkItem{WorkItemID: "work-item-" + strconv.Itoa(i)}
	}
	return items
}

// BenchmarkBaselineSettlementsStashBelowCapacity measures the steady-state
// stash the commit transaction runs once per baseline chunk: a worker holds one
// claim at a time, so the stash stays far below its bound and every stash is a
// map write under the shared lock. It does not depend on the eviction return
// values, so it measures the same call before and after eshu-hq/eshu#10746.
func BenchmarkBaselineSettlementsStashBelowCapacity(b *testing.B) {
	stash := newBaselineSettlements()
	items := benchmarkSettlementItems(maxBaselineSettlements / 2)
	settlement := baselineSettlement{runID: "baseline-2026-11", year: 2002, facts: 100, stashedAt: time.Unix(1_700_000_000, 0)}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		stash.stash(items[i%len(items)], settlement)
	}
}

// BenchmarkBaselineSettlementsStashAtCapacity measures a stash of a new key into
// a full stash, which scans every held row for the oldest and deletes it. The
// key space is far larger than the bound, so a key is always absent when it
// returns and every iteration evicts. The reporting that follows an eviction is
// outside the lock and measured separately.
func BenchmarkBaselineSettlementsStashAtCapacity(b *testing.B) {
	const keySpace = 4096
	stash := newBaselineSettlements()
	items := benchmarkSettlementItems(keySpace)
	start := time.Unix(1_700_000_000, 0)
	for i := 0; i < maxBaselineSettlements; i++ {
		stash.stash(items[i], baselineSettlement{runID: "baseline-2026-11", stashedAt: start.Add(time.Duration(i) * time.Second)})
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		stash.stash(items[(maxBaselineSettlements+i)%keySpace], baselineSettlement{
			runID:     "baseline-2026-11",
			year:      2002,
			facts:     100,
			stashedAt: start.Add(time.Duration(maxBaselineSettlements+i) * time.Second),
		})
	}
}
