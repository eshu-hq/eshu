// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

// Small assertion helpers for the replatforming parity proof. They read the
// well-known fields the three replatforming surfaces emit and fail loudly on a
// missing or wrong-typed value rather than silently passing.

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query"
)

// assertCount fails unless got equals want for the named field.
func assertCount(t *testing.T, field string, got, want int) {
	t.Helper()
	if got != want {
		t.Fatalf("%s = %d, want %d", field, got, want)
	}
}

// intVal coerces a JSON number (float64) or int into an int, failing on any
// other type so a missing field never reads as zero.
func intVal(t *testing.T, v any) int {
	t.Helper()
	switch typed := v.(type) {
	case float64:
		return int(typed)
	case int:
		return typed
	default:
		t.Fatalf("value %#v is not a number", v)
		return 0
	}
}

// boolVal coerces a JSON bool, defaulting to false for absent or non-bool input.
func boolVal(v any) bool {
	b, _ := v.(bool)
	return b
}

// mapStringInt reduces a JSON object of numbers into a map[string]int.
func mapStringInt(t *testing.T, v any) map[string]int {
	t.Helper()
	raw, ok := v.(map[string]any)
	if !ok {
		t.Fatalf("value %#v is not an object", v)
	}
	out := make(map[string]int, len(raw))
	for k, val := range raw {
		out[k] = intVal(t, val)
	}
	return out
}

// mapVal asserts v is a JSON object and returns it.
func mapVal(t *testing.T, v any) map[string]any {
	t.Helper()
	m, ok := v.(map[string]any)
	if !ok {
		t.Fatalf("value %#v is not an object", v)
	}
	return m
}

// sliceOfMaps asserts v is a JSON array of objects and returns it.
func sliceOfMaps(t *testing.T, v any) []map[string]any {
	t.Helper()
	raw, ok := v.([]any)
	if !ok {
		t.Fatalf("value %#v is not an array", v)
	}
	out := make([]map[string]any, 0, len(raw))
	for _, item := range raw {
		m, ok := item.(map[string]any)
		if !ok {
			t.Fatalf("array item %#v is not an object", item)
		}
		out = append(out, m)
	}
	return out
}

// stringsOf reduces a JSON array of strings into a []string, ignoring non-string
// members.
func stringsOf(v any) []string {
	raw, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		if s, ok := item.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

// findPacketByItemID returns the ownership packet whose item_id matches, failing
// if it is absent so a dropped (silently omitted) finding is caught.
func findPacketByItemID(t *testing.T, packets []map[string]any, itemID string) map[string]any {
	t.Helper()
	for _, packet := range packets {
		if query.StringVal(packet, "item_id") == itemID {
			return packet
		}
	}
	t.Fatalf("ownership packet for item_id %q not found (silently omitted?)", itemID)
	return nil
}

// findItemByStableID returns the plan migration item whose stable_id matches,
// failing if it is absent.
func findItemByStableID(t *testing.T, items []map[string]any, stableID string) map[string]any {
	t.Helper()
	for _, item := range items {
		if query.StringVal(item, "stable_id") == stableID {
			return item
		}
	}
	t.Fatalf("plan item for stable_id %q not found (silently omitted?)", stableID)
	return nil
}

// ownershipPacketHasAmbiguousCandidate reports whether any owner candidate on the
// packet is marked ambiguous with explicit ambiguity reasons.
func ownershipPacketHasAmbiguousCandidate(packet map[string]any) bool {
	candidates, ok := packet["owner_candidates"].([]any)
	if !ok {
		return false
	}
	for _, raw := range candidates {
		candidate, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if query.StringVal(candidate, "confidence") == "ambiguous" {
			if reasons := stringsOf(candidate["ambiguity_reasons"]); len(reasons) > 0 {
				return true
			}
		}
	}
	return false
}
