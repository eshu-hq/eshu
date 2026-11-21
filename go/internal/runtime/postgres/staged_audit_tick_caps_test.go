// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// The staged audit's tick-total caps (#11117 A; asked because the #11103
// rename wave queues about 176,000 pending keys):
// StagedAuditMaxStagedFacts and StagedAuditMaxDistinctKeys bound the whole
// read phase, which reads every active scope generation, not the pending
// candidates. A corpus past either cap defers every tick on "budget" before
// the writer verify runs, whatever the dirty-log backlog is.

// capsPlan stages scopes batches of facts facts and keys distinct keys each and
// records whether the writer verify ran.
func capsPlan(scopes, facts, keys int, verified *bool) StagedAuditTickPlan {
	return StagedAuditTickPlan{
		Census: func(context.Context, db.Queryer) ([]StagedAuditCensusItem, error) {
			items := make([]StagedAuditCensusItem, scopes)
			for i := range items {
				items[i] = StagedAuditCensusItem{ScopeID: fmt.Sprintf("s%d", i), GenerationID: "g1"}
			}
			return items, nil
		},
		FetchBatch: func(context.Context, db.Queryer, StagedAuditCensusItem) (StagedAuditBatchResult, error) {
			return StagedAuditBatchResult{Facts: facts, DistinctKeys: keys}, nil
		},
		FetchStoredWinners: func(context.Context, db.Queryer) (int, error) { return 10, nil },
		Verify: func(context.Context) (int64, error) {
			*verified = true
			return 0, nil
		},
	}
}

func TestStagedAuditTickCapsDeferPastTheCorpusBudget(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name               string
		scopes, facts, key int
		wantDefer          bool
	}{
		{"staged facts at the cap", 50, StagedAuditMaxRowsPerBatch, 1, false},
		{"staged facts past the cap", 51, StagedAuditMaxRowsPerBatch, 1, true},
		{"distinct keys at the cap", 25, 10000, 10000, false},
		{"distinct keys past the cap", 26, 10000, 10000, true},
	} {
		verified := false
		runner := NewStagedAuditRunner(newLaneCFleet(), NewStagedAuditTelemetry(), StagedAuditRunnerOptions{})
		_, err := runner.RunTick(context.Background(), capsPlan(tc.scopes, tc.facts, tc.key, &verified))
		var deferred *StagedAuditDeferredError
		switch {
		case tc.wantDefer:
			if !errors.As(err, &deferred) || deferred.Reason != StagedAuditDeferBudget {
				t.Errorf("%s: err = %v, want a budget deferral", tc.name, err)
			}
			if verified {
				t.Errorf("%s: the writer verify ran on a deferred tick", tc.name)
			}
		default:
			if err != nil || !verified {
				t.Errorf("%s: err = %v, verified %v; want the tick to complete and verify", tc.name, err, verified)
			}
		}
	}
}
