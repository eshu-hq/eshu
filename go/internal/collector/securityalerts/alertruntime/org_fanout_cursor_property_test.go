// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package alertruntime

import (
	"fmt"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/telemetry/contract"
)

// ringWalk runs the fan-out walk of one claim over allowed from start with the
// stoppers failing on every claim, the way fanOutOrganization does: it reads
// from start, wrapping, stops at the first stopper, and records every
// repository behind it as stopped. It returns the freshness-relevant outcome
// map as a string (complete, failed, or stopped per allowlist position), the set
// of positions read in full, and the stopper's walk index (-1 for a full read).
func ringWalk(size, start int, stoppers map[int]bool) (outcomes string, complete map[int]bool, stopIndex int) {
	var b strings.Builder
	cells := make([]byte, size)
	complete = map[int]bool{}
	stopIndex = -1
	for i := range size {
		position := (start + i) % size
		switch {
		case stopIndex >= 0:
			cells[position] = 's'
		case stoppers[position]:
			cells[position] = 'f'
			stopIndex = i
		default:
			cells[position] = 'c'
			complete[position] = true
		}
	}
	b.Write(cells)
	return b.String(), complete, stopIndex
}

// TestFanoutNextIsForwardOnlyBoundedAndNeverReadsLessThanToday is the property
// behind the start of the fan-out (#8946), checked exhaustively for every subset
// of persistent stoppers on rings of 1, 2, 3, 8, 10, and 12 repositories over
// three laps of claims. A stored generation is keyed on the full per-position
// outcome map, which is what the freshness hint covers, not on the stopper
// alone. The invariants:
//
//   - stored generations <= number of stoppers + 1;
//   - the outcome map reaches a fixed point by claim stoppers + 1;
//   - the settled read set is a superset of today's start-at-top read set (the
//     repositories before the first stopper);
//   - the start never decreases except for the single lap close, a stop at the
//     last position that returns the start to the top with the lap set, and a
//     full read that resets it.
func TestFanoutNextIsForwardOnlyBoundedAndNeverReadsLessThanToday(t *testing.T) {
	t.Parallel()

	for _, size := range []int{1, 2, 3, 8, 10, 12} {
		allowed := cursorAllowlist(size)
		for mask := range 1 << size {
			stoppers := map[int]bool{}
			count := 0
			firstStopper := size
			for position := range size {
				if mask&(1<<position) != 0 {
					stoppers[position] = true
					count++
					firstStopper = min(firstStopper, position)
				}
			}
			start, lap := 0, ""
			previous := ""
			stored, fixedAt := 0, 0
			var settled map[int]bool
			lapClosed := false
			for claim := 1; claim <= 3*size+3; claim++ {
				outcomes, complete, stopIndex := ringWalk(size, start, stoppers)
				if outcomes != previous {
					stored++
					fixedAt = claim
				}
				previous, settled = outcomes, complete
				next, nextLap := fanoutNext(allowed, start, lap, stopIndex, contract.SecurityAlertFanoutOutcomeFailed)
				closes := lap == "" && nextLap != "" && next == 0
				if closes {
					if lapClosed {
						t.Fatalf("size %d stoppers %b: the lap closed twice", size, mask)
					}
					lapClosed = true
				}
				if stopIndex >= 0 && next < start && !closes {
					t.Fatalf("size %d stoppers %b: start moved backward from %d to %d on claim %d without closing the lap", size, mask, start, next, claim)
				}
				if lap != "" && next != 0 {
					t.Fatalf("size %d stoppers %b: lapped start moved to %d", size, mask, next)
				}
				start, lap = next, nextLap
			}
			if stored > count+1 {
				t.Fatalf("size %d stoppers %b: stored %d generations, want at most %d", size, mask, stored, count+1)
			}
			if fixedAt > count+1 {
				t.Fatalf("size %d stoppers %b: the outcome map changed at claim %d, want a fixed point by claim %d", size, mask, fixedAt, count+1)
			}
			for position := range firstStopper {
				if !settled[position] {
					t.Fatalf("size %d stoppers %b: the settled read set misses %s, which the start-at-top fan-out reads", size, mask, fmt.Sprint(position))
				}
			}
		}
	}
}

// TestFanoutNextStaysAtTheTopForATargetOverTheCap pins that no stopper moves the
// start of a target over the per-claim cap.
func TestFanoutNextStaysAtTheTopForATargetOverTheCap(t *testing.T) {
	t.Parallel()

	allowed := cursorAllowlist(maxOrganizationFanoutRepositories + 1)
	for _, outcome := range []string{
		contract.SecurityAlertFanoutOutcomeFailed,
		contract.SecurityAlertFanoutOutcomeRateLimited,
		contract.SecurityAlertFanoutOutcomeStopped,
	} {
		if next, lap := fanoutNext(allowed, 0, "", 7, outcome); next != 0 || lap != "" {
			t.Errorf("outcome %s: next = %d, lap = %q; want the top and no lap for a target over the cap", outcome, next, lap)
		}
	}
}
