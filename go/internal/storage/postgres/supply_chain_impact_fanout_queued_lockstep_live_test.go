// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres_test

import (
	"slices"
	"testing"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
)

// TestFindQueuedHolderScopesAgreesWithTheGoPassRuleAfterARealClaimLive is the
// #10972 Go/SQL lockstep. Each single-row queued shape of the cover matrix is
// read by the shipped prefilter with its stampOut, then claimed by a real
// ReducerQueue.Claim, and the claimed intent goes through the Go pass rule
// (ScopedPassCVEIDs). A dropped row's pass must run full, or scoped on a
// superset of a bounded stampOut; for an unbounded change only a full pass
// covers. A kept row whose Go pass would have covered the change is allowed
// (the disagreement falls on the keep side) and is logged.
func TestFindQueuedHolderScopesAgreesWithTheGoPassRuleAfterARealClaimLive(t *testing.T) {
	ctx, db := fanOutLiveDB(t)
	var shapes []coverShape
	for _, shape := range coverShapes() {
		if len(shape.rows) == 1 && coverAllQueued(shape.rows) {
			shapes = append(shapes, shape)
		}
	}
	seedCoverShapes(t, ctx, db, shapes)
	store := fanOutLiveStore(db)
	dropped := map[string]bool{}
	for _, shape := range shapes {
		got, err := store.FindQueuedHolderScopes(ctx, []string{shape.scope}, shape.stampOut)
		if err != nil {
			t.Fatal(err)
		}
		dropped[shape.scope] = len(got) == 1
	}

	claimed := map[string]reducercontract.Intent{}
	queue := fanOutLiveQueue(db)
	for range shapes {
		intent, ok, err := queue.Claim(ctx)
		if err != nil || !ok {
			t.Fatalf("claim %d of %d: ok=%v err=%v", len(claimed)+1, len(shapes), ok, err)
		}
		claimed[intent.ScopeID] = intent
	}

	for _, shape := range shapes {
		intent, ok := claimed[shape.scope]
		if !ok {
			t.Fatalf("%s: never claimed", shape.scope)
		}
		scope := supplychaincore.ScopedPassCVEIDs(intent)
		covers := scope == nil
		if bounded := len(shape.stampOut) > 0 && !slices.Contains(shape.stampOut, supplychaincore.FanOutStampSentinel); bounded && scope != nil {
			covers = true
			for _, id := range shape.stampOut {
				if !slices.Contains(scope, id) {
					covers = false
				}
			}
		}
		switch {
		case dropped[shape.scope] && !covers:
			t.Errorf("%s: dropped by the prefilter, but its claimed pass (attempt %d) is scoped to %v, which does not cover %v",
				shape.scope, intent.AttemptCount, scope, shape.stampOut)
		case !dropped[shape.scope] && covers:
			t.Logf("%s: kept, though its claimed pass (scope %v) covers %v: a keep-side disagreement", shape.scope, scope, shape.stampOut)
		}
		if dropped[shape.scope] != shape.dropped {
			t.Errorf("%s: dropped=%v, the matrix wants %v", shape.scope, dropped[shape.scope], shape.dropped)
		}
	}
}
