// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package oci

import (
	"reflect"
	"testing"
)

func row(key, keyField string) map[string]any {
	return map[string]any{keyField: key}
}

func rows(keyField string, keys ...string) []map[string]any {
	out := make([]map[string]any, 0, len(keys))
	for _, key := range keys {
		out = append(out, row(key, keyField))
	}
	return out
}

func keysOf(rows []map[string]any, keyField string) []string {
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		out = append(out, r[keyField].(string))
	}
	return out
}

// TestAdvanceBoundedReadUnderLimitKeepsEverything covers the no-truncation
// path: fewer rows than the limit means nothing was cut off.
func TestAdvanceBoundedReadUnderLimitKeepsEverything(t *testing.T) {
	t.Parallel()
	keys := []string{"a", "b"}
	got := rows("k", "a", "a", "b")
	kept, truncated, next := AdvanceBoundedRead(keys, got, "k", 5)
	if !reflect.DeepEqual(kept, got) {
		t.Fatalf("kept = %#v, want %#v", kept, got)
	}
	if truncated != nil || next != nil {
		t.Fatalf("truncated=%#v next=%#v, want both nil", truncated, next)
	}
}

// TestAdvanceBoundedReadIrreducibleOverflow covers the case decision test 3
// depends on: every returned row shares the batch's first key, so that key
// alone filled the bound and must be withheld rather than resolved.
func TestAdvanceBoundedReadIrreducibleOverflow(t *testing.T) {
	t.Parallel()
	keys := []string{"a", "b", "c"}
	got := rows("k", "a", "a")
	kept, truncated, next := AdvanceBoundedRead(keys, got, "k", 2)
	if kept != nil {
		t.Fatalf("kept = %#v, want nil (never a placeholder row)", kept)
	}
	if want := []string{"a"}; !reflect.DeepEqual(truncated, want) {
		t.Fatalf("truncated = %#v, want %#v", truncated, want)
	}
	if want := []string{"b", "c"}; !reflect.DeepEqual(next, want) {
		t.Fatalf("next = %#v, want %#v (keys strictly after the truncated key)", next, want)
	}
}

// TestAdvanceBoundedReadContinuationKeepsCompleteKeys covers decision test 2:
// the last row's key is not the batch's first key, so every earlier key is
// guaranteed complete and kept; only the last key is retried.
func TestAdvanceBoundedReadContinuationKeepsCompleteKeys(t *testing.T) {
	t.Parallel()
	keys := []string{"a", "b"}
	got := rows("k", "a", "a", "b")
	kept, truncated, next := AdvanceBoundedRead(keys, got, "k", 3)
	if want := []string{"a", "a"}; !reflect.DeepEqual(keysOf(kept, "k"), want) {
		t.Fatalf("kept keys = %#v, want %#v", keysOf(kept, "k"), want)
	}
	if truncated != nil {
		t.Fatalf("truncated = %#v, want nil (not yet known to overflow)", truncated)
	}
	if want := []string{"b"}; !reflect.DeepEqual(next, want) {
		t.Fatalf("next = %#v, want %#v", next, want)
	}
}

// TestAdvanceBoundedReadMakesProgress proves the invariant every call site
// depends on to terminate: each call removes at least one key from the
// working set, whichever branch it takes.
func TestAdvanceBoundedReadMakesProgress(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		keys []string
		rows []map[string]any
	}{
		{"irreducible", []string{"a", "b"}, rows("k", "a", "a")},
		{"continuation", []string{"a", "b", "c"}, rows("k", "a", "b", "b")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, _, next := AdvanceBoundedRead(tc.keys, tc.rows, "k", len(tc.rows))
			if len(next) >= len(tc.keys) {
				t.Fatalf("next = %#v (%d keys), want fewer than the %d input keys", next, len(next), len(tc.keys))
			}
		})
	}
}

func TestKeyBatchesSplitsAtBound(t *testing.T) {
	t.Parallel()
	keys := make([]string, MaxKeysPerStatement+1)
	for i := range keys {
		keys[i] = string(rune('a' + i%26))
	}
	batches := KeyBatches(keys)
	if len(batches) != 2 {
		t.Fatalf("KeyBatches() returned %d batches, want 2", len(batches))
	}
	if len(batches[0]) != MaxKeysPerStatement || len(batches[1]) != 1 {
		t.Fatalf("batch sizes = %d, %d; want %d, 1", len(batches[0]), len(batches[1]), MaxKeysPerStatement)
	}
	if KeyBatches(nil) != nil {
		t.Fatalf("KeyBatches(nil) = %#v, want nil", KeyBatches(nil))
	}
}

func TestSortUniqueStringsDedupesAndSorts(t *testing.T) {
	t.Parallel()
	got := SortUniqueStrings([]string{"b", "", "a", "b", "a"})
	if want := []string{"a", "b"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("SortUniqueStrings() = %#v, want %#v", got, want)
	}
}

func TestRegistryTruthLimits(t *testing.T) {
	t.Parallel()
	complete := RegistryTruthLimits(nil)
	if got := complete["image_registry_truth_complete"]; got != true {
		t.Fatalf("image_registry_truth_complete = %#v, want true", got)
	}
	if _, ok := complete["truncated_image_refs"]; ok {
		t.Fatalf("truncated_image_refs present on a complete result: %#v", complete)
	}

	incomplete := RegistryTruthLimits([]string{"ref-a"})
	if got := incomplete["image_registry_truth_complete"]; got != false {
		t.Fatalf("image_registry_truth_complete = %#v, want false", got)
	}
	if got, want := incomplete["image_registry_truth_incomplete_reason"], RegistryTruthRowLimitReason; got != want {
		t.Fatalf("image_registry_truth_incomplete_reason = %#v, want %q", got, want)
	}
	if got, want := incomplete["truncated_image_refs"], []string{"ref-a"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("truncated_image_refs = %#v, want %#v", got, want)
	}
}
