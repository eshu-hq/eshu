// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cigates

import "strings"

// MatchGlob reports whether path matches the glob pattern. Supported syntax:
//   - `**` matches zero or more path segments (including none).
//   - `*` matches any sequence of characters within a single path segment
//     (does not cross `/`).
//   - All other characters are matched literally.
//
// Patterns with a leading `/` or trailing `/` never match any path. Empty
// pattern matches only empty path.
func MatchGlob(pattern, path string) bool {
	// Reject anchored or directory-style patterns.
	if strings.HasPrefix(pattern, "/") || strings.HasSuffix(pattern, "/") {
		return false
	}
	return matchSegments(strings.Split(pattern, "/"), strings.Split(path, "/"))
}

// matchSegments recursively matches pattern segments against path segments.
func matchSegments(pat, seg []string) bool {
	for {
		if len(pat) == 0 {
			return len(seg) == 0
		}
		if len(seg) == 0 {
			// Pattern segments remain; only valid if they are all "**"
			for _, p := range pat {
				if p != "**" {
					return false
				}
			}
			return true
		}

		head := pat[0]
		if head == "**" {
			rest := pat[1:]
			// "**" can match zero segments: try skipping it.
			if matchSegments(rest, seg) {
				return true
			}
			// "**" can match one or more segments: consume one path segment and retry.
			return matchSegments(pat, seg[1:])
		}

		// Single segment: must match the first path segment.
		if !matchSegment(head, seg[0]) {
			return false
		}
		pat = pat[1:]
		seg = seg[1:]
	}
}

// matchSegment reports whether the single-segment pattern p matches the string s.
// `*` matches any sequence of characters (no `/` in a segment, so no cross-segment
// issue here). All other characters are literal.
func matchSegment(p, s string) bool {
	// Fast paths.
	if p == "*" {
		return true
	}
	if !strings.ContainsRune(p, '*') {
		return p == s
	}
	return matchSegmentWild(p, s)
}

// matchSegmentWild handles segment patterns that contain at least one `*`.
// It walks through each wildcard, trying every possible split point in s.
func matchSegmentWild(p, s string) bool {
	idx := strings.IndexByte(p, '*')
	if idx == -1 {
		return p == s
	}
	prefix := p[:idx]
	if !strings.HasPrefix(s, prefix) {
		return false
	}
	tail := p[idx+1:]
	s = s[len(prefix):]
	// `*` consumed: try every position in s for the remaining pattern.
	for i := 0; i <= len(s); i++ {
		if matchSegmentWild(tail, s[i:]) {
			return true
		}
	}
	return false
}
