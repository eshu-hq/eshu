// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package contention

import (
	"testing"
)

// strandedCensus builds a Census with the given prefixed item counts, claim
// counts, and instance enabled flags.
func strandedCensus(pending, claimedItems int, claimStatuses map[string]int, instanceEnabled []bool) Census {
	c := Census{Prefixed: map[string]int{}, Other: map[string]int{}}
	if pending > 0 {
		c.Prefixed["workflow_work_items|pending"] = pending
	}
	if claimedItems > 0 {
		c.Prefixed["workflow_work_items|claimed"] = claimedItems
	}
	for status, n := range claimStatuses {
		c.Prefixed["workflow_claims|"+status] = n
	}
	for _, enabled := range instanceEnabled {
		key := "collector_instances|false"
		if enabled {
			key = "collector_instances|true"
		}
		c.Prefixed[key]++
	}
	return c
}

func TestCheckStrandedPreconditions(t *testing.T) {
	seedItems := func(seed int64, n int) []string {
		out := make([]string, 0, n)
		for i := range n {
			out = append(out, ids{seed: seed}.workItem(i)+":pending")
		}
		return out
	}
	clean := func() (Census, *Snapshot, []string, []string) {
		return strandedCensus(100000, 0, nil, []bool{false}),
			&Snapshot{ItemsByStatus: map[string]int{"pending": 100000}, ItemTotal: 100000},
			seedItems(1, 100000), nil
	}
	cases := map[string]func(Census, *Snapshot, []string, []string){
		"clean stranded leg passes": func(c Census, s *Snapshot, nt, dt []string) {},
		"wrong pending count fails": func(c Census, s *Snapshot, nt, dt []string) {
			c.Prefixed["workflow_work_items|pending"] = 99999
		},
		"claimed items fail": func(c Census, s *Snapshot, nt, dt []string) {
			c.Prefixed["workflow_work_items|pending"] = 99990
			c.Prefixed["workflow_work_items|claimed"] = 10
		},
		"claim rows fail": func(c Census, s *Snapshot, nt, dt []string) {
			c.Prefixed["workflow_claims|active"] = 3
		},
		"active claims snapshot fails": func(c Census, s *Snapshot, nt, dt []string) {
			s.ActiveClaims = 2
		},
		"missing instance row fails": func(c Census, s *Snapshot, nt, dt []string) {
			delete(c.Prefixed, "collector_instances|false")
		},
		"extra instance row fails": func(c Census, s *Snapshot, nt, dt []string) {
			c.Prefixed["collector_instances|true"]++
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			c, s, nt, dt := clean()
			mutate(c, s, nt, dt)
			err := checkStrandedPreconditions(c, *s, nt, dt, 1, 100000)
			if name == "clean stranded leg passes" && err != nil {
				t.Fatalf("clean: %v", err)
			}
			if name != "clean stranded leg passes" && err == nil {
				t.Fatalf("%s: want mismatch error, got nil", name)
			}
		})
	}
	t.Run("double terminal fails", func(t *testing.T) {
		c, s, nt, _ := clean()
		if err := checkStrandedPreconditions(c, *s, nt, []string{"x"}, 1, 100000); err == nil {
			t.Fatal("want mismatch error, got nil")
		}
	})
	t.Run("truncated nonterminal list fails", func(t *testing.T) {
		c, s, _, dt := clean()
		short := seedItems(1, 10)
		if err := checkStrandedPreconditions(c, *s, short, dt, 1, 100000); err == nil {
			t.Fatal("want mismatch error, got nil")
		}
	})
}
