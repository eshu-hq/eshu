// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres_test

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #11327 real PostgreSQL proof for the suppression family before the go-evidence lock: 40 rounds of a pass whose suppression diff really runs racing a fence that reopens its work item with zero deadlocks and a clean recorded footprint, and the production suppression candidate read returning under a 1 s lock_timeout while a side transaction holds LOCK TABLE fact_work_items IN EXCLUSIVE MODE
//eshu:readiness:env ESHU_POSTGRES_DISPOSABLE ESHU_POSTGRES_DSN

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts/supply/chain"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// suppressionCandidateMarker is a fragment of the production suppression candidate read
// (core selectSuppressionCandidatesQuery) that no other statement carries.
const suppressionCandidateMarker = "(g.activated_at IS NOT NULL)"

// suppressionPassFixture seeds a holder scope with a go scope, writes the holder's first
// pass (so its supports exist and it is a holder), claims its work item, and returns the
// second pass: a changed set, the suppression kind (so the diff runs before the lock), Go
// evidence for the go scope and the production owed recorder.
func suppressionPassFixture(c *convergence, tag string) (holder, goScope, goGeneration string, second supplychaincore.SupplyChainImpactWrite) {
	c.t.Helper()
	holder = "scope:11327:sup:" + tag
	repository := goEvidenceTestRepoID("11327:sup:" + tag)
	goScope = "go-reachability://" + repository
	goGeneration = fanOutGen(goScope, "g1")
	fanOutSeedScope(c.t, c.ctx, c.db, goScope, "g1")
	fanOutSeedScope(c.t, c.ctx, c.db, holder, "g1")
	generation := fanOutGen(holder, "g1")
	finding := func(cve string) supplychaincore.SupplyChainImpactFinding {
		f := fallbackFinding(cve, "pkg:npm/sup-"+tag+"@1")
		f.RepositoryID = repository
		return f
	}
	first := fenceWrite("intent:11327:sup1:"+tag, holder, generation, finding("CVE-2026-6"+tag))
	if _, err := (supplychaincore.PostgresSupplyChainImpactWriter{
		DB: postgres.SupplyChainImpactBeginner{Beginner: postgres.SQLDB{DB: c.db}}, Now: time.Now,
	}).WriteSupplyChainImpactFindings(c.ctx, first); err != nil {
		c.t.Fatalf("first pass of %s: %v", holder, err)
	}
	item := fanOutSeedItem(c.t, c.ctx, c.db, holder, "g1")
	claimedAt := time.Date(2026, 11, 24, 12, 0, 0, 0, time.UTC)
	if _, err := c.db.ExecContext(c.ctx, `UPDATE fact_work_items SET status='claimed', lease_owner='sup-proof',
		claim_until=now()+interval '1 hour', last_attempt_at=$2, attempt_count=1 WHERE work_item_id=$1`, item, claimedAt); err != nil {
		c.t.Fatalf("claim the holder's item: %v", err)
	}
	second = fenceWrite("intent:11327:sup2:"+tag, holder, generation, finding("CVE-2026-6"+tag), finding("CVE-2026-5"+tag))
	second.ScopeFactKinds = []string{chain.VulnerabilitySuppressionFactKind}
	second.GoEvidence = []supplychaincore.GoEvidenceGeneration{{ScopeID: goScope, GenerationID: goGeneration}}
	second.Owed = postgres.SupplyChainImpactFanOutStore{LeaseOwner: "sup-proof"}.
		OwedRecorder(reducercontract.Intent{IntentID: item, ClaimedAt: &claimedAt})
	return holder, goScope, goGeneration, second
}

// TestGoEvidenceSuppressionPassAndFenceNeverDeadlockLive is proof 4 of the plain-reads
// ruling: a pass whose suppression diff really runs before the lock, its own scope already a
// holder, races a fence that reopens that scope's work item. 40 rounds, three interleavings,
// zero 40P01; every fence commits; the pass commits or is refused with the typed class; the
// fence reopened the pass's item; and the recorded footprint of a committed pass is clean with
// the candidate read and the statement-timeout RESET both present before the lock.
func TestGoEvidenceSuppressionPassAndFenceNeverDeadlockLive(t *testing.T) {
	c := newGoEvidenceConvergence(t)
	committedWithRead := 0
	for round := 0; round < 40; round++ {
		tag := fmt.Sprintf("%02d", round)
		holder, goScope, goGeneration, write := suppressionPassFixture(c, tag)

		var wg sync.WaitGroup
		launch := func() {
			wg.Add(1)
			go func() {
				defer wg.Done()
				retryFence(c.ctx, c.t, c.db, goScope, goGeneration)
			}()
		}
		var mu sync.Mutex
		var footprint []string
		beginner := recheckHookBeginner{
			inner: postgres.SupplyChainImpactBeginner{Beginner: postgres.SQLDB{DB: c.db}},
			onEnd: func(statements []string, ok bool) {
				if ok {
					mu.Lock()
					footprint = append([]string(nil), statements...)
					mu.Unlock()
				}
			},
		}
		switch round % 3 {
		case 0:
			beginner.beforeLock = func(context.Context) { launch(); wg.Wait() }
		case 1:
			beginner.beforeLock = func(context.Context) { launch() }
		default:
			beginner.hook = func(ctx context.Context) { launch(); waitForAdvisoryWaiter(ctx, c.t, c.db, 1) }
		}
		_, err := supplychaincore.PostgresSupplyChainImpactWriter{DB: beginner, Now: time.Now}.WriteSupplyChainImpactFindings(c.ctx, write)
		wg.Wait()
		var changed *supplychaincore.SupplyChainImpactGoEvidenceChangedError
		if err != nil && !errors.As(err, &changed) {
			t.Fatalf("round %s: the suppression pass failed with %v, want a commit or the typed refusal (a deadlock reads 40P01, a lost lease is impossible here)", tag, err)
		}
		var replay bool
		if err := c.db.QueryRowContext(c.ctx, `SELECT cross_scope_replay_required FROM fact_work_items WHERE work_item_id = $1`,
			fanOutItemID(holder, fanOutGen(holder, "g1"))).Scan(&replay); err != nil || !replay {
			t.Fatalf("round %s: the fence did not reopen the pass's item (flag = %v, err = %v)", tag, replay, err)
		}
		if err == nil {
			mu.Lock()
			statements := footprint
			mu.Unlock()
			if why := footprintViolation(statements); why != "" {
				t.Fatalf("round %s: the committed suppression pass broke the footprint: %s", tag, why)
			}
			hasRead, hasReset := false, false
			for _, statement := range statements {
				hasRead = hasRead || strings.Contains(statement, suppressionCandidateMarker)
				hasReset = hasReset || statement == "RESET statement_timeout"
			}
			if !hasRead || !hasReset {
				t.Fatalf("round %s: the recorded pass ran no suppression diff (candidate read %v, RESET %v): the proof would be vacuous", tag, hasRead, hasReset)
			}
			committedWithRead++
		}
	}
	if committedWithRead == 0 {
		t.Fatal("no round committed a suppression pass: the footprint was never checked")
	}
}

// TestGoEvidenceSuppressionCandidateReadIgnoresATableLockLive is proof 5, one run: the
// retirement fence's LOCK TABLE fact_work_items IN EXCLUSIVE MODE is held in a side
// transaction while the production suppression candidate read, captured from a real pass,
// runs under a 1 s lock_timeout. ACCESS SHARE does not conflict with EXCLUSIVE, so it returns
// at once.
func TestGoEvidenceSuppressionCandidateReadIgnoresATableLockLive(t *testing.T) {
	c := newGoEvidenceConvergence(t)
	holder, _, _, write := suppressionPassFixture(c, "tl")
	var candidates string
	var mu sync.Mutex
	beginner := recheckHookBeginner{
		inner: postgres.SupplyChainImpactBeginner{Beginner: postgres.SQLDB{DB: c.db}},
		onEnd: func(statements []string, ok bool) {
			mu.Lock()
			defer mu.Unlock()
			for _, statement := range statements {
				if strings.Contains(statement, suppressionCandidateMarker) {
					candidates = statement
				}
			}
		},
	}
	if _, err := (supplychaincore.PostgresSupplyChainImpactWriter{DB: beginner, Now: time.Now}).WriteSupplyChainImpactFindings(c.ctx, write); err != nil {
		t.Fatalf("capture pass: %v", err)
	}
	mu.Lock()
	read := candidates
	mu.Unlock()
	if read == "" {
		t.Fatal("the production suppression candidate read was never captured")
	}

	side, err := c.db.BeginTx(c.ctx, nil)
	if err != nil {
		t.Fatalf("begin the side transaction: %v", err)
	}
	defer func() { _ = side.Rollback() }()
	if _, err := side.ExecContext(c.ctx, `LOCK TABLE fact_work_items IN EXCLUSIVE MODE`); err != nil {
		t.Fatalf("hold the retirement-style table lock: %v", err)
	}
	reader, err := c.db.BeginTx(c.ctx, nil)
	if err != nil {
		t.Fatalf("begin the reader: %v", err)
	}
	defer func() { _ = reader.Rollback() }()
	if _, err := reader.ExecContext(c.ctx, `SET LOCAL lock_timeout = '1s'`); err != nil {
		t.Fatalf("set the lock timeout: %v", err)
	}
	started := time.Now()
	rows, err := reader.QueryContext(c.ctx, read, holder, "supply_chain_impact")
	if err != nil {
		t.Fatalf("the candidate read failed under the table lock: %v", err)
	}
	n := 0
	for rows.Next() {
		n++
	}
	_ = rows.Close()
	if elapsed := time.Since(started); elapsed >= time.Second || n == 0 {
		t.Fatalf("candidate read returned %d rows after %v under a held EXCLUSIVE table lock, want rows at once", n, elapsed)
	}
}
