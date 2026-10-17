// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// trailerKey is the commit-message trailer that waives directory growth.
const trailerKey = "Dirgate-Growth:"

// trailerSyntax is the exact form an operator must write.
const trailerSyntax = "Dirgate-Growth: <dir> +<N> #<issue> <reason>"

var issueRef = regexp.MustCompile(`^(?:[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+)?#[0-9]+$`)

// trailer is one Dirgate-Growth line found in a commit message.
type trailer struct {
	commit string
	line   string
	dir    string
	n      int
	issue  string
	reason string
	// err is non-empty when the line is not a valid waiver.
	err string
}

// parseTrailers finds every Dirgate-Growth line in one commit message.
func parseTrailers(commit, message string) []trailer {
	var out []trailer
	for _, raw := range strings.Split(message, "\n") {
		line := strings.TrimRight(raw, " \t\r")
		if len(line) < len(trailerKey) || !strings.EqualFold(line[:len(trailerKey)], trailerKey) {
			continue
		}
		out = append(out, parseTrailerLine(commit, line))
	}
	return out
}

// parseTrailerLine validates the fields of one trailer line.
func parseTrailerLine(commit, line string) trailer {
	t := trailer{commit: commit, line: line}
	rest := strings.TrimSpace(line[len(trailerKey):])
	fields := strings.Fields(rest)
	switch {
	case len(fields) == 0:
		t.err = "missing directory, +N, #issue and reason"
		return t
	case len(fields) == 1:
		t.dir = fields[0]
		t.err = "missing +N, #issue and reason"
		return t
	}
	t.dir = fields[0]
	n, ok := parseGrowth(fields[1])
	if !ok {
		t.err = fmt.Sprintf("growth %q must be written +N with N >= 1", fields[1])
		return t
	}
	t.n = n
	if len(fields) < 3 || !issueRef.MatchString(fields[2]) {
		t.err = "missing #issue reference after +N"
		return t
	}
	t.issue = fields[2]
	t.reason = skipFields(rest, 3)
	if t.reason == "" {
		t.err = "missing reason after the #issue"
	}
	return t
}

// parseGrowth reads "+N" with N >= 1.
func parseGrowth(field string) (int, bool) {
	digits, ok := strings.CutPrefix(field, "+")
	if !ok {
		return 0, false
	}
	n, err := strconv.Atoi(digits)
	if err != nil || n < 1 {
		return 0, false
	}
	return n, true
}

// loadTrailers reads every trailer from the commits in revRange.
func loadTrailers(g gitRepo, revRange string) ([]trailer, error) {
	out, err := g.output("log", "--format=%H%x1f%B%x1e", revRange)
	if err != nil {
		return nil, fmt.Errorf("read commit messages in %s: %w", revRange, err)
	}
	var all []trailer
	for _, rec := range strings.Split(out, "\x1e") {
		rec = strings.TrimLeft(rec, "\n")
		if rec == "" {
			continue
		}
		commit, message, _ := strings.Cut(rec, "\x1f")
		all = append(all, parseTrailers(commit, message)...)
	}
	return all, nil
}

// skipFields drops the first n whitespace-separated tokens of s.
func skipFields(s string, n int) string {
	for range n {
		s = strings.TrimLeft(s, " \t")
		i := strings.IndexAny(s, " \t")
		if i < 0 {
			return ""
		}
		s = s[i:]
	}
	return strings.TrimSpace(s)
}

// loadHistoryWaivers sums, per directory, the growth waived by every valid
// trailer in the commits reachable from base (W). It reads only commits whose
// message mentions the trailer key, so the cost stays flat as history grows.
func loadHistoryWaivers(g gitRepo, base string) (map[string]int, error) {
	out, err := g.output("log", "-i", "-F", "--grep="+strings.TrimSuffix(trailerKey, ":"), "--format=%H%x1f%B%x1e", base)
	if err != nil {
		return nil, fmt.Errorf("read waiver trailers reachable from %s: %w", short(base), err)
	}
	waived := map[string]int{}
	for _, rec := range strings.Split(out, "\x1e") {
		rec = strings.TrimLeft(rec, "\n")
		if rec == "" {
			continue
		}
		commit, message, _ := strings.Cut(rec, "\x1f")
		for _, t := range parseTrailers(commit, message) {
			if t.err == "" {
				waived[t.dir] += t.n
			}
		}
	}
	return waived, nil
}
