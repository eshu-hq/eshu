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

// matchSegments matches pattern segments against path segments.
//
// The recursion branches only at a "**" segment, which forks into "match zero
// segments here" and "consume one path segment and retry". With ONE "**" that
// fork is linear in the path length, but with two or more the same
// (pattern suffix, path suffix) state is reachable by many different splits and
// the naive recursion re-explores each one — exponential in the number of "**"
// segments, on a pattern that is perfectly legal and simply matches nothing.
// Measured on this repository before the memo below: a pattern of 14
// consecutive "**" segments followed by a literal the path does not contain
// took 396ms to answer "no match" for a SINGLE 15-segment candidate. Validate
// resolves every trigger against ~20k candidates, so one such trigger is not a
// slow gate, it is a gate that never finishes (#6223 review).
//
// failed memoizes the "**" states already proven not to match, which collapses
// that to O(len(pat) x len(seg)) distinct states. It is allocated only when the
// pattern actually carries two or more "**" segments, so the shapes the
// registry really holds — 85 triggers with no "**" and 414 with exactly one,
// measured against the committed registry — keep the original allocation-free
// walk. Normalizing consecutive "**" into one would fix only the adjacent
// spelling: the same blow-up reappears when the "**" segments are separated by
// literals (12 "**" segments interleaved with a literal, one 26-segment
// candidate: 254ms), which is why this memoizes states rather than rewriting
// the pattern.
func matchSegments(pat, seg []string) bool {
	return matchSegmentsBounded(pat, seg, doubleStarSegments(pat) > 1)
}

// matchSegmentsBounded is matchSegments with the memo decision already made.
// It exists so a caller resolving ONE pattern against many candidates —
// trackedPaths.matchesAny, ~20k candidates per trigger — counts the pattern's
// "**" segments once per pattern rather than once per candidate. Folding that
// count back into the per-candidate call cost a measured 49ms -> 62ms on the
// committed registry's full resolution pass.
func matchSegmentsBounded(pat, seg []string, memoize bool) bool {
	var failed []bool
	if memoize {
		failed = make([]bool, (len(pat)+1)*(len(seg)+1))
	}
	return matchSegmentsFrom(pat, seg, 0, 0, failed)
}

// doubleStarSegments counts the whole-segment "**" wildcards in pat. A segment
// that merely CONTAINS "**" ("collector-**") is a single-segment wildcard
// handled by matchSegment, not a fork, so it does not count here.
func doubleStarSegments(pat []string) int {
	n := 0
	for _, p := range pat {
		if p == "**" {
			n++
		}
	}
	return n
}

// matchSegmentsFrom matches pat[pi:] against seg[si:]. failed, when non-nil,
// records the "**" states already known not to match, indexed by (pi, si);
// a nil failed disables memoization for patterns that cannot revisit a state.
func matchSegmentsFrom(pat, seg []string, pi, si int, failed []bool) bool {
	stride := len(seg) + 1
	for {
		if pi == len(pat) {
			return si == len(seg)
		}
		if si == len(seg) {
			// Pattern segments remain; only valid if they are all "**"
			for _, p := range pat[pi:] {
				if p != "**" {
					return false
				}
			}
			return true
		}

		head := pat[pi]
		if head == "**" {
			if failed != nil && failed[pi*stride+si] {
				return false
			}
			// "**" can match zero segments: try skipping it.
			if matchSegmentsFrom(pat, seg, pi+1, si, failed) {
				return true
			}
			// "**" can match one or more segments: consume one path segment and retry.
			if matchSegmentsFrom(pat, seg, pi, si+1, failed) {
				return true
			}
			if failed != nil {
				failed[pi*stride+si] = true
			}
			return false
		}

		// Single segment: must match the first path segment.
		if !matchSegment(head, seg[si]) {
			return false
		}
		pi++
		si++
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
//
// It advances one pointer through the pattern and one through the string,
// remembering the most recent `*` and how much of s it had consumed. On a
// mismatch it returns to that `*` and lets it swallow one more character. Since
// only the LAST `*` is ever reconsidered, each character of s is revisited at
// most once per `*`, giving O(len(p)*len(s)) worst case.
//
// The previous form recursed per split point, which is exponential in the
// number of `*` in one segment: measured on this package with the pattern
// "*a" repeated n times against "a"*(n-1)+"b"*n, 385us at n=12, 5.9ms at n=16
// and 68.7ms at n=20 -- roughly 2x per added `*`, so seconds by n=30. The `**`
// fork above is memoized, but that memo does not reach inside a segment, so a
// pattern like "collector-*-*-*.go" ran unbounded against every candidate on
// the always-on Validate path (~20k tracked files). No committed trigger has
// that shape today; this removes the exposure rather than relying on that.
//
// Semantics are unchanged: `*` matches any run of characters within one
// segment, and TestMatchSegmentWild_AgreesWithTheExhaustiveForm pins this
// against a reference implementation.
func matchSegmentWild(p, s string) bool {
	var (
		pi, si     int
		star, mark = -1, 0
	)
	for si < len(s) {
		switch {
		case pi < len(p) && p[pi] == s[si]:
			pi++
			si++
		case pi < len(p) && p[pi] == '*':
			star, mark = pi, si
			pi++
		case star >= 0:
			// Back up to the last `*` and let it consume one more character.
			pi = star + 1
			mark++
			si = mark
		default:
			return false
		}
	}
	for pi < len(p) && p[pi] == '*' {
		pi++
	}
	return pi == len(p)
}
