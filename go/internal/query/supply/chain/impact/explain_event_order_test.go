// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

import "testing"

// eventsPayload builds an affected_ranges payload with one SEMVER range whose
// events are written "i:2.0.0", "f:2.5.0", "l:3.5.0".
func eventsPayload(events ...string) map[string]any {
	list := make([]any, 0, len(events))
	for _, raw := range events {
		key := map[string]string{"i": "introduced", "f": "fixed", "l": "last_affected"}[raw[:1]]
		list = append(list, map[string]any{key: raw[2:]})
	}
	return map[string]any{"affected_ranges": []any{map[string]any{"type": "SEMVER", "events": list}}}
}

// TestExplainRangeContainsReadsEventsInVersionOrder pins #9865 for the
// display walk: it must agree with the reducer matcher, so a range listed out
// of version order contains the same versions as its sorted twin.
func TestExplainRangeContainsReadsEventsInVersionOrder(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		events   []string
		observed string
		want     bool
	}{
		{"issue shape inside the higher pair", []string{"i:2.0.0", "f:2.5.0", "i:0", "f:1.5.0"}, "2.2.0", true},
		{"issue shape inside the lower pair", []string{"i:2.0.0", "f:2.5.0", "i:0", "f:1.5.0"}, "1.0.0", true},
		{"issue shape between the pairs", []string{"i:2.0.0", "f:2.5.0", "i:0", "f:1.5.0"}, "1.7.0", false},
		{"issue shape above both", []string{"i:2.0.0", "f:2.5.0", "i:0", "f:1.5.0"}, "3.0.0", false},
		{"sorted twin inside the higher pair", []string{"i:0", "f:1.5.0", "i:2.0.0", "f:2.5.0"}, "2.2.0", true},
		{"last_affected before a lower fixed pair", []string{"i:3.0.0", "l:3.5.0", "i:0", "f:1.5.0"}, "3.2.0", true},
		{"last_affected boundary itself is affected", []string{"i:0", "l:1.5.0"}, "1.5.0", true},
		{"just above last_affected is not", []string{"i:0", "l:1.5.0"}, "1.5.1", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ranges := decodeExplainAffectedRanges(eventsPayload(tc.events...))
			if len(ranges) != 1 {
				t.Fatalf("decoded %d ranges, want 1", len(ranges))
			}
			got, valid := explainRangeContains(ranges[0], tc.observed)
			if !valid || got != tc.want {
				t.Fatalf("contains=%v valid=%v, want %v true", got, valid, tc.want)
			}
		})
	}
}

// TestMatchingLineFixedVersionNamesTheEndOfTheContainingPair pins that the
// headline fix is the Fixed that ends the pair containing the observation: the
// lowest Fixed above it, not the first Fixed the feed happens to list.
func TestMatchingLineFixedVersionNamesTheEndOfTheContainingPair(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		events   []string
		observed string
		want     string
	}{
		{"sorted two pairs, inside the second", []string{"i:0", "f:1.5.0", "i:2.0.0", "f:2.5.0"}, "2.2.0", "2.5.0"},
		{"sorted two pairs, inside the first", []string{"i:0", "f:1.5.0", "i:2.0.0", "f:2.5.0"}, "1.0.0", "1.5.0"},
		{"unsorted two pairs, inside the higher", []string{"i:2.0.0", "f:2.5.0", "i:0", "f:1.5.0"}, "2.2.0", "2.5.0"},
		{"unsorted two pairs, inside the lower", []string{"i:2.0.0", "f:2.5.0", "i:0", "f:1.5.0"}, "1.0.0", "1.5.0"},
		{"between the pairs names nothing", []string{"i:0", "f:1.5.0", "i:2.0.0", "f:2.5.0"}, "1.7.0", ""},
		{"open line names nothing", []string{"i:0", "f:1.5.0", "i:2.0.0"}, "2.2.0", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			facts := []EvidenceFact{{FactID: "f", FactKind: "vulnerability.affected_package", Payload: eventsPayload(tc.events...)}}
			if got := matchingLineFixedVersion(facts, tc.observed); got != tc.want {
				t.Fatalf("matchingLineFixedVersion = %q, want %q", got, tc.want)
			}
		})
	}
}
