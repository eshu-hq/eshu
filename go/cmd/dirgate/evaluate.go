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
	// absScoped limits the absolute term to the directories this change touched:
	// those whose set of non-test .go files differs between base and head, plus
	// every directory a valid trailer names. It is set for a pull_request run
	// and for pre-push, which simulates it. Growth that landed unwaived is caught
	// where it was made (the push run on main, schedule, dispatch); a branch that
	// does not touch the directory is not made to repair it.
	absScoped bool
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
// concurrently and was never waived. When in.absScoped is set (pull_request and pre-push),
// abs_needed counts only for the directories the change touched; delta_needed
// and the trailer check always apply to every directory.
func evaluate(in evalInput) ([]finding, int) {
	valid, out := validateTrailers(in)
	var touched map[string]bool
	if in.absScoped {
		touched = touchedDirectories(in, valid)
	}
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
		out = append(out, capFindings(d, in, valid[d], touched == nil || touched[d])...)
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

// touchedDirectories returns the go/-relative directories a change touched: the
// ones whose set of non-test .go files differs between base and head (added,
// removed, or renamed files), and the ones a valid trailer names. A trailer
// names its directory into scope so a repair PR is judged on the directory it
// repairs, and a stray trailer on a settled directory is still an unused trailer.
func touchedDirectories(in evalInput, valid map[string][]trailer) map[string]bool {
	touched := map[string]bool{}
	for _, side := range []map[string]map[string]struct{}{in.base.qualifying, in.head.qualifying} {
		for d := range side {
			if !sameFileSet(in.base.qualifying[d], in.head.qualifying[d]) {
				touched[d] = true
			}
		}
	}
	for d := range valid {
		touched[d] = true
	}
	return touched
}

// sameFileSet reports whether two basename sets hold the same names.
func sameFileSet(a, b map[string]struct{}) bool {
	if len(a) != len(b) {
		return false
	}
	for name := range a {
		if _, ok := b[name]; !ok {
			return false
		}
	}
	return true
}

// capFindings applies the cap and waiver arithmetic to one directory. absInScope
// says whether the absolute term applies to it.
func capFindings(dir string, in evalInput, waivers []trailer, absInScope bool) []finding {
	b, h := in.base.count(dir), in.head.count(dir)
	b0 := in.baseline.cap[dir]
	deltaNeeded := max(0, h-max(maxDirFiles, b))
	absNeeded := 0
	if absInScope {
		absNeeded = max(0, h-max(maxDirFiles, b0)-in.history[dir])
	}
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
			"  (growth already on the base: check main first, a repair PR may already be in flight; otherwise put it in a one-commit repair PR):\n"+
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
