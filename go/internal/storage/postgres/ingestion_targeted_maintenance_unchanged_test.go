// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"sort"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/backfill"
)

// The whole pass leaves a partition untouched when this pass did not change
// its backward evidence and its readiness row exists (#8082): it was a memo
// hit at pass start and received no inserted evidence row. The targeted pass
// still republishes every affected partition, so on such a partition the two
// arms differ only in the phase committed_at/updated_at and the memo
// committed_at the targeted arm rewrites. The helpers below tolerate exactly
// that difference, and only for partitions the fixture declares unchanged.

// unchangedTimestampOnly reports whether the arms may disagree on the row at
// key because the whole arm left a declared-unchanged partition alone. All of
// these must hold: the row exists in before, whole and targeted; its partition
// is in unchanged; it is a phase or memo row; the whole row is identical to
// before; and the targeted row differs only in timestamps: a phase value must be exactly two non-empty timestamps. A phase row's key
// carries every non-timestamp column (scope, acceptance unit, source run,
// generation, keyspace, phase) and its value is committed_at|updated_at. A
// memo row's value is catalog_fingerprint|committed_at, so the fingerprint
// must match.
func unchangedTimestampOnly(
	key string,
	before, whole, targeted targetedDiffState,
	unchanged map[backfill.ScopeGenerationPartition]struct{},
) bool {
	wholeRow, inWhole := whole[key]
	targetedRow, inTargeted := targeted[key]
	beforeRow, inBefore := before[key]
	if !inWhole || !inTargeted || !inBefore {
		return false
	}
	if _, ok := unchanged[wholeRow.partition]; !ok {
		return false
	}
	if wholeRow.value != beforeRow.value {
		return false
	}
	switch wholeRow.table {
	case "phase":
		targetedCommitted, targetedUpdated, okTargeted := strings.Cut(targetedRow.value, "|")
		return okTargeted && targetedCommitted != "" && targetedUpdated != "" &&
			!strings.Contains(targetedUpdated, "|")
	case "memo":
		wholeFingerprint, _, okWhole := strings.Cut(wholeRow.value, "|")
		targetedFingerprint, _, okTargeted := strings.Cut(targetedRow.value, "|")
		return okWhole && okTargeted && wholeFingerprint == targetedFingerprint
	}
	return false
}

// wholeRewroteUnchanged returns the phase and memo keys of declared-unchanged
// partitions that the whole arm does not hold byte-identical to before. Every
// declared-unchanged partition must already have both rows before the pass.
func wholeRewroteUnchanged(before, whole targetedDiffState, unchanged map[backfill.ScopeGenerationPartition]struct{}) []string {
	var violations []string
	for partition := range unchanged {
		for _, key := range []string{
			phaseKey(partition.ScopeID, partition.GenerationID),
			memoKey(partition.ScopeID, partition.GenerationID),
		} {
			beforeRow, inBefore := before[key]
			wholeRow, inWhole := whole[key]
			if !inBefore || !inWhole || beforeRow.value != wholeRow.value {
				violations = append(violations, key)
			}
		}
	}
	sort.Strings(violations)
	return violations
}

// unchangedDiffFixture builds a before/whole/targeted triple for one declared
// unchanged partition that the whole arm left as it was and the targeted arm
// republished at a later timestamp.
func unchangedDiffFixture() (before, whole, targeted targetedDiffState, partition backfill.ScopeGenerationPartition) {
	partition = backfill.ScopeGenerationPartition{ScopeID: "git:in", GenerationID: "in-1"}
	phase := phaseKey(partition.ScopeID, partition.GenerationID)
	memo := memoKey(partition.ScopeID, partition.GenerationID)
	row := func(table, key, value string) targetedDiffRow {
		return targetedDiffRow{table: table, key: strings.TrimPrefix(key, table+"|"), partition: partition, value: value}
	}
	before = targetedDiffState{
		phase: row("phase", phase, "T1|T1"),
		memo:  row("memo", memo, "sha256:fp|T1"),
	}
	whole = targetedDiffState{phase: before[phase], memo: before[memo]}
	targeted = targetedDiffState{
		phase: row("phase", phase, "T2|T2"),
		memo:  row("memo", memo, "sha256:fp|T2"),
	}
	return before, whole, targeted, partition
}

// plantTargetedValue overwrites the value of the targeted row at key.
func plantTargetedValue(targeted targetedDiffState, key, value string) {
	row := targeted[key]
	row.value = value
	targeted[key] = row
}

// TestTargetedDiffUnchangedToleranceSeededViolations is the seeded-violation
// pair for the adapted comparator. The clean case passes; every planted
// violation must still surface as an in-scope mismatch or a whole-arm rewrite.
func TestTargetedDiffUnchangedToleranceSeededViolations(t *testing.T) {
	t.Parallel()

	inScopeMismatch := func(before, whole, targeted targetedDiffState, compared, unchanged map[backfill.ScopeGenerationPartition]struct{}) bool {
		report := diffStates(before, whole, targeted, compared, unchanged)
		return len(report.wholeOnlyInScope) > 0 || len(report.targetedOnlyInScope) > 0
	}

	t.Run("clean timestamp-only difference is tolerated", func(t *testing.T) {
		before, whole, targeted, partition := unchangedDiffFixture()
		set := map[backfill.ScopeGenerationPartition]struct{}{partition: {}}
		if inScopeMismatch(before, whole, targeted, set, set) {
			t.Fatal("timestamp-only difference on a declared-unchanged partition was reported")
		}
		if got := wholeRewroteUnchanged(before, whole, set); len(got) != 0 {
			t.Fatalf("wholeRewroteUnchanged = %v, want none", got)
		}
	})

	violations := []struct {
		name  string
		plant func(before, whole, targeted targetedDiffState, partition backfill.ScopeGenerationPartition) (map[backfill.ScopeGenerationPartition]struct{}, targetedDiffState, targetedDiffState)
	}{
		{"targeted arm lost the phase row", func(_, whole, targeted targetedDiffState, p backfill.ScopeGenerationPartition) (map[backfill.ScopeGenerationPartition]struct{}, targetedDiffState, targetedDiffState) {
			delete(targeted, phaseKey(p.ScopeID, p.GenerationID))
			return map[backfill.ScopeGenerationPartition]struct{}{p: {}}, whole, targeted
		}},
		{"targeted memo fingerprint changed", func(_, whole, targeted targetedDiffState, p backfill.ScopeGenerationPartition) (map[backfill.ScopeGenerationPartition]struct{}, targetedDiffState, targetedDiffState) {
			key := memoKey(p.ScopeID, p.GenerationID)
			row := targeted[key]
			row.value = "sha256:other|T2"
			targeted[key] = row
			return map[backfill.ScopeGenerationPartition]struct{}{p: {}}, whole, targeted
		}},
		{"targeted phase value changed", func(_, whole, targeted targetedDiffState, p backfill.ScopeGenerationPartition) (map[backfill.ScopeGenerationPartition]struct{}, targetedDiffState, targetedDiffState) {
			key := phaseKey(p.ScopeID, p.GenerationID)
			row := targeted[key]
			delete(targeted, key)
			changed := strings.Replace(key, "backward_evidence_committed", "some_other_phase", 1)
			row.key = strings.TrimPrefix(changed, "phase|")
			targeted[changed] = row
			return map[backfill.ScopeGenerationPartition]struct{}{p: {}}, whole, targeted
		}},
		{"whole arm row not identical to before", func(_, whole, targeted targetedDiffState, p backfill.ScopeGenerationPartition) (map[backfill.ScopeGenerationPartition]struct{}, targetedDiffState, targetedDiffState) {
			key := phaseKey(p.ScopeID, p.GenerationID)
			row := whole[key]
			row.value = "T3|T3"
			whole[key] = row
			return map[backfill.ScopeGenerationPartition]struct{}{p: {}}, whole, targeted
		}},
		{"targeted phase value has three parts", func(_, whole, targeted targetedDiffState, p backfill.ScopeGenerationPartition) (map[backfill.ScopeGenerationPartition]struct{}, targetedDiffState, targetedDiffState) {
			plantTargetedValue(targeted, phaseKey(p.ScopeID, p.GenerationID), "T2|T2|T2")
			return map[backfill.ScopeGenerationPartition]struct{}{p: {}}, whole, targeted
		}},
		{"targeted phase timestamps are empty", func(_, whole, targeted targetedDiffState, p backfill.ScopeGenerationPartition) (map[backfill.ScopeGenerationPartition]struct{}, targetedDiffState, targetedDiffState) {
			plantTargetedValue(targeted, phaseKey(p.ScopeID, p.GenerationID), "|")
			return map[backfill.ScopeGenerationPartition]struct{}{p: {}}, whole, targeted
		}},
		{"partition not declared unchanged", func(_, whole, targeted targetedDiffState, _ backfill.ScopeGenerationPartition) (map[backfill.ScopeGenerationPartition]struct{}, targetedDiffState, targetedDiffState) {
			return nil, whole, targeted
		}},
	}
	for _, violation := range violations {
		t.Run(violation.name, func(t *testing.T) {
			before, whole, targeted, partition := unchangedDiffFixture()
			compared := map[backfill.ScopeGenerationPartition]struct{}{partition: {}}
			unchanged, whole, targeted := violation.plant(before, whole, targeted, partition)
			if !inScopeMismatch(before, whole, targeted, compared, unchanged) {
				t.Fatalf("planted violation %q was tolerated", violation.name)
			}
		})
	}

	t.Run("whole arm rewrote a declared-unchanged partition", func(t *testing.T) {
		before, whole, _, partition := unchangedDiffFixture()
		key := phaseKey(partition.ScopeID, partition.GenerationID)
		row := whole[key]
		row.value = "T2|T2"
		whole[key] = row
		set := map[backfill.ScopeGenerationPartition]struct{}{partition: {}}
		if got := wholeRewroteUnchanged(before, whole, set); len(got) != 1 || got[0] != key {
			t.Fatalf("wholeRewroteUnchanged = %v, want [%s]", got, key)
		}
	})

	t.Run("declared-unchanged partition missing its rows before the pass", func(t *testing.T) {
		_, whole, _, partition := unchangedDiffFixture()
		set := map[backfill.ScopeGenerationPartition]struct{}{partition: {}}
		if got := wholeRewroteUnchanged(targetedDiffState{}, whole, set); len(got) != 2 {
			t.Fatalf("wholeRewroteUnchanged = %v, want both phase and memo keys", got)
		}
	})
}
