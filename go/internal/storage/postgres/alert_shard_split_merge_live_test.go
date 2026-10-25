// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres_test

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #9019 disposable PostgreSQL 18 end-to-end proof of security-alert org-target sharding through the production planner, collector, claim-fenced commit, projector service, and query stores: an allowlist growing from 100 to 101 repositories splits into shard scopes and retires the base scope, shrinking back to 100 merges and retires both shards, and after every step list, count, and inventory return exactly one row per allowlisted repository while no unplanned scope contributes a row; a replayed reconciliation plans identical ids and no further retirement; two collector replicas commit the shards of one target concurrently; each proof creates and drops its own database from the bootstrap schema; about 3 to 8 s per subtest on PostgreSQL 18.6 locally; wall time is logged, not gated
//eshu:readiness:env ESHU_POSTGRES_DSN

import (
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/scope/org/shard"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

func shardItemIDs(items []workflow.WorkItem) (collect, retire []string) {
	for _, item := range items {
		if shard.IsRetirement(item.ScopeID, item.AcceptanceUnitID) {
			retire = append(retire, item.ScopeID)
		} else {
			collect = append(collect, item.ScopeID)
		}
	}
	slices.Sort(collect)
	slices.Sort(retire)
	return collect, retire
}

func sameWorkItem(a, b workflow.WorkItem) bool {
	return a.WorkItemID == b.WorkItemID && a.ScopeID == b.ScopeID && a.GenerationID == b.GenerationID &&
		a.AcceptanceUnitID == b.AcceptanceUnitID
}

func shardSplitMergeProof(replicas int) func(*testing.T) {
	return func(t *testing.T) {
		database, ctx := readStateIsolatedLiveDB(t)
		e := newShardE2E(t, database, ctx)
		start := time.Now()

		hundred := slices.Clone(e.organization[:100])
		collect, retire := shardItemIDs(e.scan(hundred, "schedule-20261025T060000Z", replicas))
		if !slices.Equal(collect, []string{shardLiveBaseScopeID}) || len(retire) != 0 {
			t.Fatalf("100 repositories planned %v and retired %v, want the base scope only", collect, retire)
		}
		e.assertReads("100 repositories", hundred)

		// Split: one more repository.
		hundredOne := slices.Clone(e.organization[:101])
		// Replay: planning one reconciliation twice yields identical ids.
		first := e.plan(hundredOne, "schedule-20261025T070000Z")
		items := e.scan(hundredOne, "schedule-20261025T070000Z", replicas)
		if !slices.EqualFunc(first, items, sameWorkItem) {
			t.Fatal("replayed reconciliation planned different work items")
		}
		collect, retire = shardItemIDs(items)
		if len(collect) != 2 || !slices.Equal(retire, []string{shardLiveBaseScopeID}) {
			t.Fatalf("101 repositories planned %v and retired %v, want two shards and the base retired", collect, retire)
		}
		e.assertReads("split to 101", hundredOne)
		splitShards := collect
		// Review F4: a token granted the target's base scope id keeps seeing
		// every alert of its shards; an ungranted base, a granted id that
		// itself carries the shard marker, and an id that merely shares the
		// base as a prefix see none; a repository-id grant still sees that
		// repository.
		for _, tc := range []struct {
			granted []string
			want    int
		}{
			{[]string{shardLiveBaseScopeID}, 101},
			{[]string{"security-alert:github-org:other"}, 0},
			{[]string{shardLiveBaseScopeID + "#shard-"}, 0},
			{[]string{"security-alert:github-org:ac"}, 0},
			{[]string{"security-alert:github:" + hundredOne[0]}, 1},
			{[]string{splitShards[0]}, len(shard.Partition(shardLiveBaseScopeID, "acme", hundredOne)[0].Repositories)},
		} {
			if listed, counted := e.grantedReads(tc.granted...); listed != tc.want || counted != tc.want {
				t.Fatalf("grant %v: list %d, count %d, want %d", tc.granted, listed, counted, tc.want)
			}
		}

		// After the retirement landed, the same reconciliation replans the same
		// shard items and no retirement: the retired base is skipped.
		replayed := e.plan(hundredOne, "schedule-20261025T070000Z")
		if want := items[:len(items)-1]; !slices.EqualFunc(want, replayed, sameWorkItem) {
			t.Fatalf("replay after the split planned %v, want the shard items %v only", replayed, want)
		}
		if _, retire = shardItemIDs(e.plan(hundredOne, "schedule-20261025T080000Z")); len(retire) != 0 {
			t.Fatalf("the scan after a split retires %v, want nothing", retire)
		}

		// Merge: drop one repository; its alerts must leave every read.
		merged := slices.Clone(e.organization[:100])
		// Review F1: once the shards' retirements are active and before the
		// parent's collection lands, the parent's active generation is still
		// its own retirement from the split. It must read as retired (the
		// classifier then reports collecting, not complete), and as a
		// collection once the parent's collection activates.
		parentBeforeCollection := func() {
			state := e.collectionState(shardLiveBaseScopeID, merged[0])
			if !state.Retired || !state.CommittedGeneration {
				t.Fatalf("merged parent before its collection = %+v, want a retired active generation", state)
			}
		}
		collect, retire = shardItemIDs(e.scan(merged, "schedule-20261025T090000Z", replicas, parentBeforeCollection))
		if state := e.collectionState(shardLiveBaseScopeID, merged[0]); state.Retired {
			t.Fatalf("merged parent after its collection = %+v, want not retired", state)
		}
		if !slices.Equal(collect, []string{shardLiveBaseScopeID}) || !slices.Equal(retire, splitShards) {
			t.Fatalf("merge planned %v and retired %v, want the base back and %v retired", collect, retire, splitShards)
		}
		e.assertReads("merge to 100", merged)
		if _, retire = shardItemIDs(e.plan(merged, "schedule-20261025T100000Z")); len(retire) != 0 {
			t.Fatalf("the scan after a merge retires %v, want nothing", retire)
		}
		t.Logf("split and merge with %d replica(s): %s", replicas, time.Since(start))
	}
}

func shardWideTargetProof(t *testing.T) {
	database, ctx := readStateIsolatedLiveDB(t)
	e := newShardE2E(t, database, ctx)
	for i := 120; i < 250; i++ {
		e.organization = append(e.organization, fmt.Sprintf("acme/wide-%03d", i))
	}
	all := slices.Clone(e.organization)
	collect, _ := shardItemIDs(e.scan(all, "schedule-20261025T060000Z", 2))
	if len(collect) < 3 {
		t.Fatalf("250 repositories planned %v, want at least 3 shards", collect)
	}
	e.assertReads("250 repositories, two replicas", all)
}

// TestShardSplitMergeLive runs the #9019 end-to-end proofs as subtests.
func TestShardSplitMergeLive(t *testing.T) {
	t.Run("SplitAndMergeOneReplica", shardSplitMergeProof(1))
	t.Run("SplitAndMergeTwoReplicas", shardSplitMergeProof(2))
	t.Run("WideTargetTwoReplicas", shardWideTargetProof)
}
