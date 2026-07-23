// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"fmt"
	"testing"
)

// BenchmarkSortWorkloadPlatformRowsWorstCase measures the #5644 in-memory sort
// at its true production worst case: the attached-platform collection is
// bounded by workloadPlatformEdgeLimit (contextStoryItemLimit *
// contextStoryItemLimit = 2500) with a queryLimit = 2501 sentinel, so
// sortWorkloadPlatformRows can be asked to order up to 2,501 rows once per
// service-story request.
//
// The row distribution mirrors the production invariant rather than an
// arbitrary 2,501 distinct keys. fetchWorkloadPlatformResult restricts
// `i.id IN $instance_ids` to the already-truncated runtime-topology result, so
// at most contextStoryItemLimit (50) DISTINCT instance ids can ever appear;
// the 2,500 bound is 50 instances x 50 platforms each. Generating 2,501
// distinct instance ids would let every comparison resolve on the first key
// and would silently never exercise the platform_name/platform_id/
// platform_kind tiebreakers the comparator exists for. Here each instance id
// repeats ~50 times, platform_name repeats within an instance, and a share of
// rows carry an empty platform_id so the comparator is driven all the way down
// to platform_kind.
func BenchmarkSortWorkloadPlatformRowsWorstCase(b *testing.B) {
	template := worstCaseWorkloadPlatformRows()

	b.ReportAllocs()
	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		b.StopTimer()
		rows := make([]map[string]any, len(template))
		copy(rows, template)
		b.StartTimer()
		sortWorkloadPlatformRows(rows)
	}
}

// worstCaseWorkloadPlatformRows builds the production-shaped worst case: 2,501
// rows spread over contextStoryItemLimit distinct instance ids, in a
// deliberately sort-hostile (reverse) order.
func worstCaseWorkloadPlatformRows() []map[string]any {
	const rowCount = workloadPlatformEdgeLimit + 1 // 2501, the query sentinel ceiling
	kinds := []string{"argocd_applicationset", "ecs_service"}
	rows := make([]map[string]any, 0, rowCount)
	for i := rowCount - 1; i >= 0; i-- {
		instance := i % contextStoryItemLimit // 50 distinct instance ids
		group := i / contextStoryItemLimit    // ~51 rows per instance
		platformID := fmt.Sprintf("platform:eks-%04d", i)
		if group%4 < 2 {
			// Two rows per (instance_id, platform_name) bucket carry an empty
			// platform_id, so they tie on the first three keys and the
			// comparator must fall through to platform_kind to order them.
			platformID = ""
		}
		rows = append(rows, map[string]any{
			"instance_id":   fmt.Sprintf("workload-instance:orders:inst-%03d", instance),
			"platform_name": fmt.Sprintf("platform-%02d", group/4),
			"platform_id":   platformID,
			// Keyed on group, not i: the two empty-platform_id rows in a bucket
			// sit 50 apart in i, so an i-keyed kind would make them identical
			// on every key and leave the tie genuinely unbreakable.
			"platform_kind": kinds[group%len(kinds)],
		})
	}
	return rows
}

// TestWorstCaseWorkloadPlatformRowsExerciseEveryComparatorKey guards the
// benchmark's premise. A previous revision generated 2,501 DISTINCT
// instance ids, which made every comparison resolve on the first key, so the
// benchmark silently measured the cheapest possible path while its comment
// claimed the opposite. This test fails if the fixture ever regresses to a
// distribution that cannot reach the secondary and tertiary keys.
func TestWorstCaseWorkloadPlatformRowsExerciseEveryComparatorKey(t *testing.T) {
	t.Parallel()
	rows := worstCaseWorkloadPlatformRows()

	if got, want := len(rows), workloadPlatformEdgeLimit+1; got != want {
		t.Fatalf("row count = %d, want %d", got, want)
	}

	instances := map[string]int{}
	nameTies, idTies := 0, 0
	seenInstanceName := map[string]int{}
	seenInstanceNameID := map[string]int{}
	for _, row := range rows {
		instances[StringVal(row, "instance_id")]++
		instanceName := StringVal(row, "instance_id") + "\x00" + StringVal(row, "platform_name")
		if seenInstanceName[instanceName] > 0 {
			nameTies++
		}
		seenInstanceName[instanceName]++
		instanceNameID := instanceName + "\x00" + StringVal(row, "platform_id")
		if seenInstanceNameID[instanceNameID] > 0 {
			idTies++
		}
		seenInstanceNameID[instanceNameID]++
	}

	if len(instances) > contextStoryItemLimit {
		t.Fatalf("distinct instance ids = %d, want <= %d (production caps instance_ids at the truncated topology)",
			len(instances), contextStoryItemLimit)
	}
	if nameTies == 0 {
		t.Fatal("no (instance_id, platform_name) ties: comparator never reaches the platform_id key")
	}
	if idTies == 0 {
		t.Fatal("no (instance_id, platform_name, platform_id) ties: comparator never reaches the platform_kind key")
	}
}
