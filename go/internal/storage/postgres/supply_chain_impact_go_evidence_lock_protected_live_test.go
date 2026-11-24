// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres_test

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #11327 real PostgreSQL proof, through the least-privilege runtime-writer login and the protected-scan authorization and publication, that a pass taking the shared go-evidence lock last and a Go-modules fence reopening that pass's own work item never deadlock in 40 rounds
//eshu:readiness:env ESHU_POSTGRES_DISPOSABLE ESHU_POSTGRES_DSN

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// TestGoEvidenceProtectedPassAndFenceNeverDeadlockLive is gate (2) of the #11327 lock-last
// ruling for the protected-scan pass: 40 rounds in which a pass that authorizes and
// publishes a protected scan result, runs as the least-privilege worker, and records its
// owed marker on its own work item races a fence that reopens that same work item. The
// pass's own scope is already a holder (an earlier pass wrote its supports). Three
// interleavings (the fence started right before the pass requests its lock and awaited,
// started and not awaited, started while the pass is paused holding the lock). The pass
// commits or is refused with the typed class or loses its lease to the reopen; none may
// fail with a deadlock, and every fence commits.
func TestGoEvidenceProtectedPassAndFenceNeverDeadlockLive(t *testing.T) {
	ctx, admin, worker := postgres.OpenProtectedPublishProofForTest(t)
	settleSupplyChainImpactSupports(t, ctx, admin)
	claimedAt := time.Date(2026, 11, 24, 12, 0, 0, 0, time.UTC)
	for round := 0; round < 40; round++ {
		tag := fmt.Sprintf("%02d", round)
		holder, generation := "scope:11327:prot:"+tag, "scope:11327:prot:"+tag+"/g1"
		repository := "repository:r_11327prot" + tag
		goScope := "go-reachability://" + repository
		goGeneration := fanOutGen(goScope, "g1")
		fanOutSeedScope(t, ctx, admin, goScope, "g1")
		mustExecProof(t, ctx, admin, `INSERT INTO ingestion_scopes
			(scope_id, scope_kind, source_system, source_key, collector_kind, partition_key, observed_at, ingested_at, status, payload)
			VALUES ($1,'container_image','synthetic',$1,'synthetic',$1,$2,$2,'active','{}'::jsonb)`, holder, claimedAt)
		mustExecProof(t, ctx, admin, `INSERT INTO scope_generations
			(generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, activated_at, payload)
			VALUES ($1,$2,'synthetic',$3,$3,'active',$3,'{}'::jsonb)`, generation, holder, claimedAt)
		mustExecProof(t, ctx, admin, `UPDATE ingestion_scopes SET active_generation_id=$1 WHERE scope_id=$2`, generation, holder)

		finding := func(cve string) supplychaincore.SupplyChainImpactFinding {
			return supplychaincore.SupplyChainImpactFinding{
				CVEID: cve, PackageID: "pkg:npm/prot-" + tag, Status: supplychaincore.SupplyChainImpactAffectedExact,
				RepositoryID: repository, EvidenceFactIDs: []string{"fact:prot:" + tag},
			}
		}
		write := supplychaincore.SupplyChainImpactWrite{
			FencingToken: 7, IntentID: "intent-prot-" + tag, ScopeID: holder, GenerationID: generation,
			SourceSystem: "vulnerability_intelligence", Findings: []supplychaincore.SupplyChainImpactFinding{finding("CVE-2026-7" + tag)},
			StaticNonAdvisoryEvidenceFactIDs: []string{"fact:prot:" + tag}, ProtectedCompleteSet: true,
		}
		plain := supplychaincore.PostgresSupplyChainImpactWriter{DB: postgres.SupplyChainImpactBeginner{Beginner: postgres.SQLDB{DB: worker}}}
		if _, err := plain.WriteSupplyChainImpactFindings(ctx, write); err != nil {
			t.Fatalf("round %s: first protected pass: %v", tag, err)
		}

		item := fanOutSeedItemAt(t, ctx, admin, holder, generation)
		mustExecProof(t, ctx, admin, `UPDATE fact_work_items SET status='claimed', lease_owner='prot-proof',
			claim_until=now()+interval '1 hour', last_attempt_at=$2, attempt_count=1 WHERE work_item_id=$1`, item, claimedAt)

		var wg sync.WaitGroup
		launch := func() {
			wg.Add(1)
			go func() {
				defer wg.Done()
				retryFence(ctx, t, admin, goScope, goGeneration)
			}()
		}
		beginner := recheckHookBeginner{inner: postgres.SupplyChainImpactBeginner{Beginner: postgres.SQLDB{DB: worker}}}
		switch round % 3 {
		case 0:
			beginner.beforeLock = func(context.Context) { launch(); wg.Wait() }
		case 1:
			beginner.beforeLock = func(context.Context) { launch() }
		default:
			beginner.hook = func(hookCtx context.Context) { launch(); waitForAdvisoryWaiter(hookCtx, t, admin, 1) }
		}
		claimed := claimedAt
		second := write
		second.FencingToken = 8
		second.Findings = append(append([]supplychaincore.SupplyChainImpactFinding(nil), write.Findings...), finding("CVE-2026-8"+tag))
		second.GoEvidence = []supplychaincore.GoEvidenceGeneration{{ScopeID: goScope, GenerationID: goGeneration}}
		second.Owed = postgres.SupplyChainImpactFanOutStore{LeaseOwner: "prot-proof"}.
			OwedRecorder(reducercontract.Intent{IntentID: item, ClaimedAt: &claimed})
		_, err := supplychaincore.PostgresSupplyChainImpactWriter{DB: beginner}.WriteSupplyChainImpactFindings(ctx, second)
		wg.Wait()
		var changed *supplychaincore.SupplyChainImpactGoEvidenceChangedError
		switch {
		case err == nil, errors.As(err, &changed):
		case strings.Contains(err.Error(), "no longer held"):
			// The fence reopened the work item before the marker: a retryable lease loss.
		default:
			t.Fatalf("round %s: protected pass failed with %v, want commit, the typed refusal or a lost lease (a deadlock reads 40P01)", tag, err)
		}
		if t.Failed() {
			t.Fatalf("round %s failed above", tag)
		}
	}
}

func mustExecProof(t *testing.T, ctx context.Context, db *sql.DB, query string, args ...any) {
	t.Helper()
	if _, err := db.ExecContext(ctx, query, args...); err != nil {
		t.Fatalf("exec %q: %v", strings.Fields(query)[0], err)
	}
}
