// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"database/sql"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// ownedPackageSlotProgressQueryTail is the progress-ordered rest of the
// owned-package statement (#9776). It follows the shared head and replaces the
// clock slot with the V/P least recently planned virtual slots, read in the same
// statement because P is only known from T here:
//
//   - reused_slots: the slots already recorded for this bucket. A repeat
//     reconcile inside one bucket must reproduce the same plan, because the plan
//     key and run id derive from the truncated clock.
//   - oldest_slots: otherwise the V/P slots with the smallest last_planned_bucket,
//     a missing row ordering first (-1) and virtual_slot breaking ties, so two
//     coordinators that both read "nothing recorded" pick the same set.
//   - slot_pick: the mode and the picked slots. When P exceeds V a window cannot
//     hold whole virtual slots, so the statement keeps the clock predicate and
//     reports clock_fallback.
//
// Binds beyond the clock statement: $5 V, $6 collector instance id, $7
// derivation kind. The final ORDER BY leads with virtual_slot so a read cut by
// LIMIT $2 cuts whole trailing slots. The six head verbs are the same.
const ownedPackageSlotProgressQueryTail = `slotted_targets AS (
  SELECT
    sized_targets.*,
    MOD(key_hash_low24::bigint, slot_count) AS slot_of_key,
    MOD(key_hash_low24::bigint, $5::bigint) AS virtual_slot_of_key
  FROM sized_targets
),
window_stats AS (
  SELECT
    total_targets,
    slot_count,
    MOD($3::bigint, slot_count) AS window_slot
  FROM (
    SELECT
      total_targets,
      %[5]s
    FROM (SELECT COUNT(*) AS total_targets FROM distinct_targets) AS counted_targets
  ) AS sized_stats
),
reused_slots AS (
  SELECT array_agg(reused.virtual_slot::bigint ORDER BY reused.virtual_slot) AS slots
  FROM owned_target_slot_progress AS reused
  WHERE reused.collector_instance_id = $6::text
    AND reused.derivation_kind = $7::text
    AND reused.last_planned_bucket = $3::bigint
),
oldest_slots AS (
  SELECT array_agg(oldest.virtual_slot ORDER BY oldest.virtual_slot) AS slots
  FROM (
    SELECT slots.virtual_slot
    FROM generate_series(0::bigint, $5::bigint - 1) AS slots(virtual_slot)
    LEFT JOIN owned_target_slot_progress AS progress
      ON progress.collector_instance_id = $6::text
     AND progress.derivation_kind = $7::text
     AND progress.virtual_slot = slots.virtual_slot
    ORDER BY COALESCE(progress.last_planned_bucket, -1) ASC, slots.virtual_slot ASC
    LIMIT (SELECT $5::bigint / window_stats.slot_count FROM window_stats)
  ) AS oldest
),
progress_stats AS (
  SELECT MIN(last_planned_bucket) AS oldest_bucket, COUNT(*) AS progress_rows
  FROM owned_target_slot_progress
  WHERE collector_instance_id = $6::text
    AND derivation_kind = $7::text
),
slot_pick AS (
  SELECT
    window_stats.total_targets,
    window_stats.slot_count,
    window_stats.window_slot,
    CASE WHEN window_stats.slot_count > $5::bigint THEN 'clock_fallback'
         WHEN reused_slots.slots IS NOT NULL THEN 'same_bucket_reuse'
         ELSE 'oldest' END AS selection_mode,
    CASE WHEN window_stats.slot_count > $5::bigint THEN ARRAY[]::bigint[]
         ELSE COALESCE(reused_slots.slots, oldest_slots.slots, ARRAY[]::bigint[]) END AS picked_slots,
    progress_stats.oldest_bucket,
    progress_stats.progress_rows
  FROM window_stats
  CROSS JOIN reused_slots
  CROSS JOIN oldest_slots
  CROSS JOIN progress_stats
),
window_targets AS (
  SELECT
    slotted_targets.*,
    COUNT(*) OVER () AS slot_size
  FROM slotted_targets
  CROSS JOIN slot_pick
  WHERE CASE WHEN slot_pick.selection_mode = 'clock_fallback'
        THEN slotted_targets.slot_of_key = slot_pick.window_slot
        ELSE slotted_targets.virtual_slot_of_key = ANY (slot_pick.picked_slots) END
)
SELECT
    window_targets.ecosystem,
    window_targets.package_name,
    window_targets.version,
    window_targets.lockfile,
    window_targets.repository_id,
    window_targets.fact_id,
    window_targets.source_location,
    window_targets.sbt_cross_version,
    window_targets.sbt_binary_artifact,
    window_targets.key_hash_low24,
    slot_pick.total_targets,
    slot_pick.slot_count,
    slot_pick.window_slot,
    COALESCE(window_targets.slot_size, 0) AS slot_size,
    slot_pick.selection_mode,
    array_to_string(slot_pick.picked_slots, ',') AS picked_slots,
    slot_pick.oldest_bucket,
    slot_pick.progress_rows
FROM slot_pick
LEFT JOIN window_targets ON TRUE
ORDER BY window_targets.virtual_slot_of_key ASC, window_targets.target_rank ASC
LIMIT $2
`

// listOwnedPackageDependencySlotProgressQuery renders the progress-ordered
// owned-package statement (#9776) for the same identity and alias modes as
// [listOwnedPackageDependencyTargetsQuery]. Binds: $1 raw package_manager
// values, $2 read limit, $3 rotation bucket, $4 target limit, $5 V, $6
// collector instance id, $7 derivation kind.
func listOwnedPackageDependencySlotProgressQuery(versionSpecific bool, expandAliases bool) string {
	return fmt.Sprintf(ownedPackageSlotQueryHead+ownedPackageSlotProgressQueryTail,
		ownedPackageStatementArgs(versionSpecific, expandAliases)...)
}

// slotProgressColumns holds the four extra columns the progress-ordered
// statement returns beside the clock statement's fourteen.
type slotProgressColumns struct {
	mode         string
	picked       string
	oldestBucket sql.NullInt64
	progressRows int64
}

// ownedPackageSlotProgressSelection turns the scanned progress columns and the
// virtual slot of every returned row into the read's selection: the picked
// slots, the slots admission may advance, and the oldest planned slot age.
func ownedPackageSlotProgressSelection(
	columns slotProgressColumns,
	rowSlots []int,
	window workflow.OwnedPackageDependencyTargetWindow,
	bucket int64,
	interval time.Duration,
	targetLimit int,
) (workflow.OwnedTargetSlotSelection, error) {
	picked, err := parsePickedVirtualSlots(columns.picked)
	if err != nil {
		return workflow.OwnedTargetSlotSelection{}, err
	}
	switch columns.mode {
	case workflow.SlotSelectionOldest, workflow.SlotSelectionSameBucketReuse, workflow.SlotSelectionClockFallback:
	default:
		return workflow.OwnedTargetSlotSelection{}, fmt.Errorf(
			"list owned package dependency targets: statement returned unknown slot selection mode %q", columns.mode)
	}
	selection := workflow.OwnedTargetSlotSelection{
		Mode:         columns.mode,
		PickedSlots:  picked,
		ProgressRows: columns.progressRows,
	}
	if columns.mode != workflow.SlotSelectionClockFallback {
		// The read limit is the target limit plus one lookahead row. A window of
		// exactly L+1 rows is not reader-truncated, yet the derivation drops the
		// lookahead row as budget exhausted, so treat it as truncated: the slot
		// holding that row stays oldest instead of being recorded as planned.
		selection.Truncated = window.Truncated() || (targetLimit > 0 && window.RowsRead > int64(targetLimit))
		selection.AdvanceSlots, selection.HeadTruncated = workflow.ResolveSlotAdvance(picked, rowSlots, selection.Truncated)
	}
	if columns.oldestBucket.Valid && columns.progressRows > 0 {
		selection.OldestPlannedBucket = columns.oldestBucket.Int64
		if age := bucket - columns.oldestBucket.Int64; age > 0 {
			selection.OldestSlotAge = time.Duration(age) * interval
		}
	}
	return selection, nil
}

// parsePickedVirtualSlots decodes the comma-separated slot list the statement
// returns. The list is the picked slots in ascending order; blank is none.
func parsePickedVirtualSlots(raw string) ([]int, error) {
	if raw == "" {
		return nil, nil
	}
	parts := strings.Split(raw, ",")
	slots := make([]int, 0, len(parts))
	for _, part := range parts {
		slot, err := strconv.Atoi(part)
		if err != nil || slot < 0 || slot >= workflow.OwnedPackageVirtualSlotCount {
			return nil, fmt.Errorf("list owned package dependency targets: statement returned invalid virtual slot %q", part)
		}
		slots = append(slots, slot)
	}
	return slots, nil
}
