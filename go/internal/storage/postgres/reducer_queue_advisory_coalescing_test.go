// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"regexp"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/projector/runtime"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
)

// Advisory-coalescing (#10566) lifecycle pins: the re-homing design reuses
// the shipped enqueue/retouch/claim/supersede/retention primitives with no
// hot-path SQL change, so these tests pin the contract fragments the design
// depends on, DERIVED from the shipped constants. A failure means the
// primitive changed and the design needs a re-review, not a test update.
//
// Deliberately NOT pinned here: the exact live-state set RetouchWorkItem
// covers. The design amendment extends that set to reopen failed and
// dead-letter slots on a new touch; pinning today's set would contradict
// that change. The throwaway live probe proved the current exclusion.

func advisory10566Intent(scope, gen, group string) runtime.ReducerIntent {
	return runtime.ReducerIntent{
		ScopeID:      scope,
		GenerationID: gen,
		Domain:       reducercontract.DomainAdvisoryGroupProjection,
		EntityKey:    "advisory_group:" + group,
		Reason:       "supply_chain_impact_winners_touch",
		SourceSystem: "synthetic",
		Payload:      map[string]any{"group_id": group},
	}
}

// TestAdvisoryCoalescingSlotIdentity pins the A-1 primitive: one group maps
// to one work item id under the synthetic slot no matter which origin
// touched it, while origin-scoped intents diverge (today's duplication).
func TestAdvisoryCoalescingSlotIdentity(t *testing.T) {
	t.Parallel()

	const synth, genesis = "synthetic:advisory-groups", "synthetic:advisory-groups:genesis"
	a := advisory10566Intent(synth, genesis, "GHSA-0001")
	b := advisory10566Intent(synth, genesis, "GHSA-0001")
	b.Payload["origin_scope_id"] = "scope:real-a"
	b.Payload["origin_generation_id"] = "gen:real-a1"
	if reducerWorkItemID(a) != reducerWorkItemID(b) {
		t.Fatalf("synthetic-slot id diverges across origins: %q vs %q", reducerWorkItemID(a), reducerWorkItemID(b))
	}
	if got := reducerWorkItemID(a); !strings.HasPrefix(got, "reducer_") || strings.ContainsAny(got, ":/") {
		t.Fatalf("slot id not sanitized: %q", got)
	}
	other := advisory10566Intent("scope:real-b", "gen:real-b1", "GHSA-0001")
	if reducerWorkItemID(a) == reducerWorkItemID(other) {
		t.Fatalf("origin-scoped id collides with the synthetic slot: %q", reducerWorkItemID(a))
	}
	peer := advisory10566Intent(synth, genesis, "GHSA-0002")
	if reducerWorkItemID(a) == reducerWorkItemID(peer) {
		t.Fatalf("disjoint groups collide on one slot: %q", reducerWorkItemID(a))
	}
}

// rowFilterStatusSet extracts the quoted members of the row-filter
// `status IN (...)` list in shipped SQL: the WHERE-clause list that decides
// which rows a statement touches (not the CASE arms that only shape columns).
// It selects the list containing 'pending', which identifies the row filter
// in every queue statement this file pins.
func rowFilterStatusSet(t *testing.T, name, query string) map[string]bool {
	t.Helper()
	quoted := regexp.MustCompile(`'([a-z_]+)'`)
	for _, loc := range regexp.MustCompile(`(?i)status\s+IN\s*\(([^)]*)\)`).FindAllStringSubmatch(query, -1) {
		set := map[string]bool{}
		for _, m := range quoted.FindAllStringSubmatch(loc[1], -1) {
			set[m[1]] = true
		}
		if set["pending"] {
			return set
		}
	}
	t.Fatalf("%s: no row-filter status IN list found", name)
	return nil
}

// TestAdvisoryCoalescingRetouchAdvancesEveryLiveTouch pins the A-3 half of
// the retouch contract: reopened_at advances unconditionally, so any live
// touch moves the winners-watermark bound the claim read derives from
// COALESCE(reopened_at, created_at).
func TestAdvisoryCoalescingRetouchAdvancesEveryLiveTouch(t *testing.T) {
	t.Parallel()

	if !strings.Contains(retouchReducerWorkItemQuery, "reopened_at = $1,") {
		t.Fatalf("retouch no longer advances reopened_at unconditionally; the coalesced watermark bound stalls")
	}
	live := rowFilterStatusSet(t, "retouch", retouchReducerWorkItemQuery)
	for _, s := range []string{"pending", "retrying", "succeeded"} {
		if !live[s] {
			t.Fatalf("retouch dropped %q from its live set; a coalesced touch in that state stops advancing the bound", s)
		}
	}
	if !strings.Contains(claimReducerWorkQuery, "COALESCE(work.reopened_at, work.created_at) AS cycle_started_at") &&
		!strings.Contains(claimReducerWorkBatchQuery, "COALESCE(work.reopened_at, work.created_at) AS cycle_started_at") {
		t.Fatalf("claim no longer derives the cycle start from reopened_at; retouches stop moving the watermark bound")
	}
	if !strings.Contains(enqueueReducerBatchSuffix, "ON CONFLICT (work_item_id) DO NOTHING") {
		t.Fatalf("enqueue lost its DO NOTHING coalescing primitive")
	}
}

// TestAdvisoryCoalescingSweepAndRetentionGuards pins the A-2 halves that let
// re-homing dodge the P0/P1 by construction: the sweep only retires rows
// whose generation is older than the scope's active one, and retention only
// ever considers generations already terminally superseded.
func TestAdvisoryCoalescingSweepAndRetentionGuards(t *testing.T) {
	t.Parallel()

	if !strings.Contains(supersedeInactiveReducerGenerationsPredicate, "stale.generation_id <> scope.active_generation_id") {
		t.Fatalf("supersede sweep lost its active-generation guard; a homed row is no longer safe by construction")
	}
	sweep := rowFilterStatusSet(t, "supersede predicate", supersedeInactiveReducerGenerationsPredicate)
	for _, s := range []string{"claimed", "running", "succeeded"} {
		if sweep[s] {
			t.Fatalf("supersede sweep now takes %q rows; live and completed homed slots are no longer safe", s)
		}
	}
	if !strings.Contains(generationRetentionCandidateQuery, "generation.status = 'superseded'") {
		t.Fatalf("retention no longer gates on superseded generations; the always-active genesis is not safe by construction")
	}
}
