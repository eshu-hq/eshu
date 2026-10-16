// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"fmt"
	"sort"
	"strings"
)

// severity orders what the gate prints.
type severity int

const (
	sevRed severity = iota
	sevNote
)

// findingKind says why a finding exists. Trailer-dependent kinds cannot be
// judged before the commit message exists, which matters in pre-commit.
type findingKind int

const (
	kindCap findingKind = iota
	kindWaiver
	kindTrailer
	kindNaming
	kindStale
	kindBaseline
)

// needsMessage reports whether the finding depends on commit messages.
func (k findingKind) needsMessage() bool {
	return k == kindCap || k == kindWaiver || k == kindTrailer
}

// finding is one line of gate output.
type finding struct {
	sev  severity
	kind findingKind
	dir  string
	file string
	msg  string
}

// evalInput is everything the rule needs. It is pure data so the rule is
// testable without git.
type evalInput struct {
	base, head snapshot
	// trailers are the waivers in the commits of base..head (w).
	trailers []trailer
	// history holds, per directory, the growth waived by every valid trailer in
	// the commits reachable from the base (W).
	history map[string]int
	// baseline holds the write-once baselines read from the head tree.
	baseline baselines
	markers  map[string]marker
	// only restricts evaluation to these go/-relative directories; nil means all.
	only map[string]bool
}

// evaluate applies the absolute-invariant ratchet and returns findings plus
// the number of directories it judged. For each directory D:
//
//	b, h         count at the base and at the head tree
//	B0           baseline cap row for D (0 if none)
//	W            growth waived by valid trailers in every commit reachable from the base
//	w            growth waived by valid trailers in the commits of base..head
//	delta_needed = max(0, h - max(40, b))
//	abs_needed   = max(0, h - max(40, B0) - W)
//	needed       = max(delta_needed, abs_needed)
//
// D is RED when w != needed: too little, too much, and an unused trailer all
// fail. delta_needed guards each merge; abs_needed catches growth that landed
// concurrently and was never waived.
func evaluate(in evalInput) ([]finding, int) {
	valid, out := validateTrailers(in)
	dirs := map[string]struct{}{}
	for d := range in.head.qualifying {
		dirs[d] = struct{}{}
	}
	for d := range in.base.qualifying {
		dirs[d] = struct{}{}
	}
	for d := range valid {
		dirs[d] = struct{}{}
	}
	var checked int
	for _, d := range sortedKeys(dirs) {
		if in.only != nil && !in.only[d] {
			continue
		}
		if in.head.count(d) > 0 || in.base.count(d) > 0 {
			checked++
		}
		out = append(out, capFindings(d, in, valid[d])...)
	}
	violators, naming := namingFindings(in)
	out = append(out, naming...)
	out = append(out, staleMarkers(in, violators)...)
	return out, checked
}

// validateTrailers splits trailers into per-directory valid waivers and RED
// findings for each invalid one.
func validateTrailers(in evalInput) (map[string][]trailer, []finding) {
	valid := map[string][]trailer{}
	var out []finding
	for _, t := range in.trailers {
		err := t.err
		if err == "" {
			if _, ok := in.head.allDirs[t.dir]; !ok {
				err = fmt.Sprintf("go/%s is not a directory in the merged tree", t.dir)
			}
		}
		if err != "" {
			out = append(out, finding{sev: sevRed, kind: kindTrailer, dir: t.dir, msg: fmt.Sprintf(
				"RED trailer in commit %s: %s: %q (write it as %q)", short(t.commit), err, t.line, trailerSyntax)})
			continue
		}
		valid[t.dir] = append(valid[t.dir], t)
	}
	return valid, out
}

// capFindings applies the cap and waiver arithmetic to one directory.
func capFindings(dir string, in evalInput, waivers []trailer) []finding {
	b, h := in.base.count(dir), in.head.count(dir)
	b0 := in.baseline.cap[dir]
	deltaNeeded := max(0, h-max(maxDirFiles, b))
	absNeeded := max(0, h-max(maxDirFiles, b0)-in.history[dir])
	needed := max(deltaNeeded, absNeeded)
	w := 0
	var commits []string
	for _, t := range waivers {
		w += t.n
		commits = append(commits, short(t.commit))
	}
	if w == needed {
		return nil
	}
	split := ""
	if absNeeded > deltaNeeded {
		split = fmt.Sprintf(" (+%d from this change, +%d already on base and never waived: concurrent merges)",
			deltaNeeded, needed-deltaNeeded)
	}
	head := fmt.Sprintf("RED go/%s: %d -> %d non-test .go files (base -> head)", dir, b, h)
	var msg string
	switch {
	case w == 0:
		msg = fmt.Sprintf("%s, needs +%d%s.\n"+
			"  Move files out, or add this line to the message of the commit that adds them\n"+
			"  (growth already on the base: put it in a one-commit repair PR):\n"+
			"    Dirgate-Growth: %s +%d #<issue> <reason>", head, needed, split, dir, needed)
	case w < needed:
		msg = fmt.Sprintf("%s, needs +%d%s, but the trailers in commit(s) %s waive only +%d; set the trailer to +%d.",
			head, needed, split, strings.Join(commits, ", "), w, needed)
	case needed == 0:
		msg = fmt.Sprintf("%s: unused trailer. Dirgate-Growth waives +%d in commit(s) %s, but the directory needs nothing "+
			"(cap max(%d, base count) = %d); remove it, or amend or rebase the history if its growth was reverted.",
			head, w, strings.Join(commits, ", "), maxDirFiles, max(maxDirFiles, b))
	default:
		msg = fmt.Sprintf("%s: over-waived. Dirgate-Growth waives +%d in commit(s) %s but the directory needs +%d%s; set the trailer to +%d.",
			head, w, strings.Join(commits, ", "), needed, split, needed)
	}
	kind := kindCap
	if w > 0 {
		kind = kindWaiver
	}
	return []finding{{sev: sevRed, kind: kind, dir: dir, msg: msg}}
}

// staleMarkers notes every nolint:dirgate marker that is not a live naming
// escape. The cap ignores the marker, so such a marker only misleads readers.
func staleMarkers(in evalInput, violators map[string]bool) []finding {
	var out []finding
	for _, p := range sortedKeys(in.markers) {
		m := in.markers[p]
		if violators[p] || skipDir(m.dir) {
			continue
		}
		if in.only != nil && !in.only[m.dir] {
			continue
		}
		out = append(out, finding{sev: sevNote, kind: kindStale, dir: m.dir, file: p, msg: fmt.Sprintf(
			"NOTE stale //nolint:dirgate in %s: the directory cap no longer honours it, and the file does not collide with a sibling package. Remove the marker.", p)})
	}
	return out
}

// sortedKeys returns the keys of m in order.
func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
