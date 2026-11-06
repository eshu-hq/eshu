// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #10063 disposable PostgreSQL proof of the CI/CD run watermark save guard on its real conflict filter: the fence decides inside one generation, the numeric run ID decides across generations, so a retried claim cannot wedge later first-attempt claims and an older generation cannot regress the row, also under concurrent saves on one key; reuses the #7978 bootstrapped proof schema; wall time is logged, not gated
//eshu:readiness:env ESHU_AWS_DRIFT_ARN_PROOF_DISPOSABLE ESHU_AWS_DRIFT_ARN_PROOF_DSN ESHU_DEFERRED_PARTITION_PROOF_DISPOSABLE ESHU_DEFERRED_PARTITION_PROOF_DSN ESHU_PROJECTOR_CLAIM_DEADLOCK_PROOF_DSN ESHU_PROJECTOR_SUPERSESSION_PROOF_DSN ESHU_REDUCER_FAIRNESS_PROOF_DSN

// Live proof for #10063 on a disposable PostgreSQL database
// (ESHU_PROJECTOR_SUPERSESSION_PROOF_DSN). The in-memory store states the same
// guard in Go; only this file proves the SQL conflict filter says the same.

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/cicdrun/runwatermark"
	cicdstore "github.com/eshu-hq/eshu/go/internal/storage/postgres/cicd"
)

// TestCICDRunWatermarkGuardAcrossGenerationsLive runs each guard row on a fresh
// key against the real upsert.
func TestCICDRunWatermarkGuardAcrossGenerationsLive(t *testing.T) {
	p := openReprojectProof(t)
	store := cicdstore.NewCICDRunWatermarkStore(SQLDB{DB: p.db})
	ctx := context.Background()

	tests := []struct {
		name       string
		generation string
		fence      int64
		runID      string
		wantErr    error
		wantRun    string
	}{
		{name: "same generation older fence newer run", generation: "gen-1", fence: 1, runID: "300", wantErr: runwatermark.ErrStaleFence, wantRun: "200"},
		{name: "same generation equal fence", generation: "gen-1", fence: 2, runID: "200", wantRun: "200"},
		{name: "same generation newer fence", generation: "gen-1", fence: 3, runID: "250", wantRun: "250"},
		{name: "next generation first attempt", generation: "gen-2", fence: 1, runID: "300", wantRun: "300"},
		{name: "other generation same run", generation: "gen-2", fence: 1, runID: "200", wantRun: "200"},
		{name: "other generation older run higher fence", generation: "gen-0", fence: 9, runID: "150", wantErr: runwatermark.ErrStaleFence, wantRun: "200"},
		{name: "run ids order numerically", generation: "gen-2", fence: 1, runID: "1000", wantRun: "1000"},
	}
	for index, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			key := runwatermark.Key{ScopeID: fmt.Sprintf("ci-cd:github-actions:guard/%d", index), Repository: "guard/repo"}
			if err := store.Save(ctx, runwatermark.Watermark{Key: key, LastRunID: "200", GenerationID: "gen-1", FencingToken: 2}); err != nil {
				t.Fatalf("seed Save() error = %v", err)
			}
			err := store.Save(ctx, runwatermark.Watermark{Key: key, LastRunID: tc.runID, GenerationID: tc.generation, FencingToken: tc.fence})
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("Save() error = %v, want %v", err, tc.wantErr)
			}
			got, ok, err := store.Load(ctx, key)
			if err != nil || !ok {
				t.Fatalf("Load() = %+v, %v, %v", got, ok, err)
			}
			if got.LastRunID != tc.wantRun {
				t.Fatalf("LastRunID = %q, want %q", got.LastRunID, tc.wantRun)
			}
		})
	}
}

// TestCICDRunWatermarkConcurrentGenerationsKeepTheNewestRunLive saves many
// generations on one key at once. The conflict domain is the (scope_id,
// repository) row, and ON CONFLICT re-evaluates the filter on the locked row, so
// every order must end on the newest run with no error but a stale refusal.
func TestCICDRunWatermarkConcurrentGenerationsKeepTheNewestRunLive(t *testing.T) {
	p := openReprojectProof(t)
	store := cicdstore.NewCICDRunWatermarkStore(SQLDB{DB: p.db})
	ctx := context.Background()
	key := runwatermark.Key{ScopeID: "ci-cd:github-actions:guard/concurrent", Repository: "guard/repo"}

	const writers = 32
	const newestRun = 1000 + writers - 1
	errs := make([]error, writers)
	var group sync.WaitGroup
	start := make(chan struct{})
	for writer := range writers {
		group.Add(1)
		go func() {
			defer group.Done()
			<-start
			errs[writer] = store.Save(ctx, runwatermark.Watermark{
				Key: key, LastRunID: strconv.Itoa(1000 + writer),
				GenerationID: fmt.Sprintf("gen-%d", writer), FencingToken: int64(writers - writer),
			})
		}()
	}
	close(start)
	group.Wait()

	for writer, err := range errs {
		if err != nil && !errors.Is(err, runwatermark.ErrStaleFence) {
			t.Fatalf("writer %d Save() error = %v, want nil or ErrStaleFence", writer, err)
		}
	}
	got, ok, err := store.Load(ctx, key)
	if err != nil || !ok {
		t.Fatalf("Load() = %+v, %v, %v", got, ok, err)
	}
	if want := strconv.Itoa(newestRun); got.LastRunID != want {
		t.Fatalf("LastRunID = %q, want the newest run %q", got.LastRunID, want)
	}
}
