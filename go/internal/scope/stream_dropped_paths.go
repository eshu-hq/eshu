// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package scope

import (
	"slices"
	"strings"
	"sync"
)

// MaxRecordedStreamDroppedPaths caps the repository-relative paths a
// generation records for files its fact stream could not read (#8185). The
// set identifies a persistent drop so the next equal snapshot can verify it
// still applies; it is not a fact inventory, so a bound keeps the generation
// payload small. A generation that drops more files records the first
// MaxRecordedStreamDroppedPaths in sort order and reports Truncated, which
// forces the next snapshot to parse: unrecorded drops cannot be re-checked,
// so any doubt parses.
const MaxRecordedStreamDroppedPaths = 32

// StreamDroppedPaths accumulates the repository-relative paths a generation's
// fact stream dropped because it could not read them between the parse and
// the emit (#8185). A pointer keeps ScopeGeneration comparable; it is never
// persisted directly. The store records Sorted in the generation payload, and
// the collector's skip probe re-checks those paths before skipping the next
// equal snapshot.
type StreamDroppedPaths struct {
	mu    sync.Mutex
	paths []string
}

// Note records one dropped path. Blank paths are ignored; normalization
// happens in Sorted.
func (s *StreamDroppedPaths) Note(relativePath string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.paths = append(s.paths, relativePath)
}

// Sorted reports the recorded paths trimmed, deduplicated, sorted, and
// capped at MaxRecordedStreamDroppedPaths, so two runs that dropped the same
// files produce the same record. It returns nil when nothing was recorded.
func (s *StreamDroppedPaths) Sorted() []string {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return NormalizeStreamDroppedPaths(s.paths)
}

// Truncated reports whether more distinct paths were recorded than Sorted
// keeps. A truncated set cannot prove every drop still applies, so the skip
// probe treats it as a refusal and the scope parses.
func (s *StreamDroppedPaths) Truncated() bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	seen := make(map[string]struct{})
	for _, path := range s.paths {
		if trimmed := strings.TrimSpace(path); trimmed != "" {
			seen[trimmed] = struct{}{}
		}
		if len(seen) > MaxRecordedStreamDroppedPaths {
			return true
		}
	}
	return false
}

// NormalizeStreamDroppedPaths trims, drops blanks, deduplicates, sorts, and
// caps paths at MaxRecordedStreamDroppedPaths. It is pure so the store and
// the collector agree on the recorded form.
func NormalizeStreamDroppedPaths(paths []string) []string {
	seen := make(map[string]struct{}, len(paths))
	out := make([]string, 0, len(paths))
	for _, path := range paths {
		trimmed := strings.TrimSpace(path)
		if trimmed == "" {
			continue
		}
		if _, dup := seen[trimmed]; dup {
			continue
		}
		seen[trimmed] = struct{}{}
		out = append(out, trimmed)
	}
	slices.Sort(out)
	if len(out) > MaxRecordedStreamDroppedPaths {
		out = out[:MaxRecordedStreamDroppedPaths]
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
