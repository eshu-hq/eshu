// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"sort"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
)

// The holder-list helpers the fan-out and the manifest marker share: the
// continuation list a claim carries, the capped batch split, and the sorted
// set operations. Split out of fanout.go to keep that file under the repo's
// 500-line cap.

// fanOutRemaining returns the continuation list stored on the intent's item,
// sorted and de-duplicated. The payload round-trips through JSON, so the list
// arrives as []any.
func fanOutRemaining(intent reducercontract.Intent) []string {
	var raw []string
	switch list := intent.Payload[PayloadKeyFanOutRemaining].(type) {
	case []string:
		raw = list
	case []any:
		for _, item := range list {
			if id, ok := item.(string); ok && id != "" {
				raw = append(raw, id)
			}
		}
	}
	return unionSortedStrings(nil, raw)
}

// nextFanOutChunk splits a sorted holder list into the first limit holders and
// the rest.
func nextFanOutChunk(sorted []string, limit int) ([]string, []string) {
	if len(sorted) <= limit {
		return sorted, nil
	}
	return sorted[:limit], sorted[limit:]
}

// chunkStrings splits values into slices of at most size, keeping order.
func chunkStrings(values []string, size int) [][]string {
	var chunks [][]string
	for start := 0; start < len(values); start += size {
		end := start + size
		if end > len(values) {
			end = len(values)
		}
		chunks = append(chunks, values[start:end])
	}
	return chunks
}

// overlapsStrings reports whether the sets a and b share an element. Both
// inputs are small stamp/derived sets, so a map probe is cheaper than a
// merge walk and needs no ordering precondition.
func overlapsStrings(a, b []string) bool {
	set := make(map[string]struct{}, len(a))
	for _, id := range a {
		set[id] = struct{}{}
	}
	for _, id := range b {
		if _, ok := set[id]; ok {
			return true
		}
	}
	return false
}

// unionSortedStrings returns the sorted, de-duplicated union of a and b with
// blanks dropped.
func unionSortedStrings(a, b []string) []string {
	seen := make(map[string]struct{}, len(a)+len(b))
	out := make([]string, 0, len(a)+len(b))
	for _, list := range [][]string{a, b} {
		for _, id := range list {
			if id == "" {
				continue
			}
			if _, ok := seen[id]; ok {
				continue
			}
			seen[id] = struct{}{}
			out = append(out, id)
		}
	}
	sort.Strings(out)
	return out
}
