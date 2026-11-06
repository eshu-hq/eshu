// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// allowedDispositions is the arbiter §3 disposition set proposed for the
// #9669 read-transport census. The mapping itself lives in
// READ_TRANSPORT_CENSUS.md; this test only pins the vocabulary and the
// coverage invariant (every CUTOVER_OWNERSHIP.md family has exactly one
// disposition row), so a new handler family fails here until the census
// gains a row. It never asserts which disposition a family gets.
var allowedDispositions = map[string]bool{
	"Native now":              true,
	"Native next in order":    true,
	"Native anytime":          true,
	"Dispatch until Leaf B-D": true,
	"Never satellite":         true,
}

var (
	cutoverRouteRow = regexp.MustCompile(`(?m)^\| ` + "`(GET|POST|PATCH|PUT|DELETE) [^`]+`" + ` \| ([^|]+?) \|`)
	// e.g. `| Code | 27 | read | Native anytime | ...`
	censusRow = regexp.MustCompile(`(?m)^\| ([A-Za-z]+) \| \d+ \| [^|]+? \| ([^|]+?) \|`)
)

func cutoverFamilies(t *testing.T) map[string]int {
	t.Helper()
	raw, err := os.ReadFile("CUTOVER_OWNERSHIP.md")
	if err != nil {
		t.Fatalf("read CUTOVER_OWNERSHIP.md: %v", err)
	}
	counts := map[string]int{}
	for _, m := range cutoverRouteRow.FindAllStringSubmatch(string(raw), -1) {
		fam := strings.TrimSpace(m[2])
		if fam == "" || strings.HasPrefix(fam, "-") {
			continue
		}
		counts[fam]++
	}
	if len(counts) == 0 {
		t.Fatal("no route rows parsed from CUTOVER_OWNERSHIP.md")
	}
	return counts
}

func censusDispositions(t *testing.T) map[string]string {
	t.Helper()
	raw, err := os.ReadFile("READ_TRANSPORT_CENSUS.md")
	if err != nil {
		t.Fatalf("read READ_TRANSPORT_CENSUS.md: %v", err)
	}
	got := map[string]string{}
	for _, m := range censusRow.FindAllStringSubmatch(string(raw), -1) {
		fam := strings.TrimSpace(m[1])
		disp := strings.TrimSpace(m[2])
		if fam == "Family" {
			continue
		}
		if _, dup := got[fam]; dup {
			t.Errorf("duplicate census row for family %q", fam)
		}
		if !allowedDispositions[disp] {
			t.Errorf("family %q has unknown disposition %q", fam, disp)
		}
		got[fam] = disp
	}
	if len(got) == 0 {
		t.Fatal("no disposition rows parsed from READ_TRANSPORT_CENSUS.md")
	}
	return got
}

// TestReadTransportCensusDrift fails when a handler family in the served
// route census has no per-family disposition row in the read-transport
// census, or when a census row names a family with no live route.
func TestReadTransportCensusDrift(t *testing.T) {
	t.Parallel()
	want := cutoverFamilies(t)
	got := censusDispositions(t)

	var missing, extra []string
	for fam := range want {
		if _, ok := got[fam]; !ok {
			missing = append(missing, fam)
		}
	}
	for fam := range got {
		if _, ok := want[fam]; !ok {
			extra = append(extra, fam)
		}
	}
	sort.Strings(missing)
	sort.Strings(extra)
	if len(missing) > 0 {
		t.Errorf("families without a census disposition row: %v", missing)
	}
	if len(extra) > 0 {
		t.Errorf("census rows without a live family: %v", extra)
	}
}
