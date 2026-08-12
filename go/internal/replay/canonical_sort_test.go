// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package replay

import (
	"fmt"
	"slices"
	"strings"
	"testing"
)

type countingCanonicalValue struct {
	value    string
	marshals *int
}

func (v countingCanonicalValue) MarshalJSON() ([]byte, error) {
	*v.marshals++
	return []byte(fmt.Sprintf("%q", v.value)), nil
}

func TestSortArraySkipsCanonicalTieBreakForUniqueKeys(t *testing.T) {
	const count = 1_000
	marshals := 0
	values := make([]any, 0, count)
	for i := count - 1; i >= 0; i-- {
		values = append(values, map[string]any{
			"id":      fmt.Sprintf("id-%04d", i),
			"payload": countingCanonicalValue{value: fmt.Sprintf("value-%04d", i), marshals: &marshals},
		})
	}

	sortArray(values, "id")
	if marshals != 0 {
		t.Fatalf("canonical tie-break marshals = %d, want 0 for unique primary keys", marshals)
	}
	for i, value := range values {
		got := elementField(value, "id")
		want := fmt.Sprintf("id-%04d", i)
		if got != want {
			t.Fatalf("values[%d] id = %q, want %q", i, got, want)
		}
	}
}

func TestSortArrayCanonicalTieBreakOrdersDuplicateKeys(t *testing.T) {
	marshals := 0
	values := []any{
		map[string]any{"id": "same", "payload": countingCanonicalValue{value: "z", marshals: &marshals}},
		map[string]any{"id": "same", "payload": countingCanonicalValue{value: "a", marshals: &marshals}},
		map[string]any{"id": "same", "payload": countingCanonicalValue{value: "m", marshals: &marshals}},
	}

	sortArray(values, "id")
	if marshals != len(values) {
		t.Fatalf("canonical tie-break marshals = %d, want one per duplicate-key element (%d)", marshals, len(values))
	}
	want := []string{"a", "m", "z"}
	for i, value := range values {
		got := value.(map[string]any)["payload"].(countingCanonicalValue).value
		if got != want[i] {
			t.Fatalf("values[%d] payload = %q, want %q", i, got, want[i])
		}
	}
}

func BenchmarkCanonicalizeUniqueFactKeys(b *testing.B) {
	for _, count := range []int{1_000, 5_000, 10_000} {
		b.Run(fmt.Sprintf("facts-%d", count), func(b *testing.B) {
			facts := make([]any, 0, count)
			payload := strings.Repeat("x", 3_072)
			for i := range count {
				facts = append(facts, map[string]any{
					"stable_fact_key": fmt.Sprintf("fact-%05d", i),
					"payload":         payload,
				})
			}
			doc := map[string]any{"facts": facts}
			opts := CanonicalOptions{SortArrays: map[string]string{"facts": "stable_fact_key"}}
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				slices.Reverse(facts)
				if _, err := CanonicalizeValue(doc, opts); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
