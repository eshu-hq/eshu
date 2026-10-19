// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
)

// TestClaimReducerWorkBatchQueryOrdersIdleDomainsBeforeBusyBacklog is the
// hermetic shape guard for #8024: the batch claim must rank candidates from
// domains with no inflight work ahead of deeper same-source backlogs, so the
// key must stay in the locked CTE ORDER BY.
func TestClaimReducerWorkBatchQueryOrdersIdleDomainsBeforeBusyBacklog(t *testing.T) {
	t.Parallel()

	if !strings.Contains(claimReducerWorkBatchQuery, "reducer_domain_inflight_count ASC") {
		t.Fatalf("batch claim query lost the idle-domain ordering key (#8024):\n%s", claimReducerWorkBatchQuery)
	}
}

// TestClaimBatchDoesNotStarveIdleDomainBehindSameSourceBacklog reproduces
// #8024: after the #7805 maintenance pass, six ready
// deployable_unit_correlation rows waited 1.5h while every freed single-slot
// claim went to older same-source container_image_identity rows. The locked
// CTE ordered by (domain priority, source inflight, source fair rank, domain
// fair rank, updated_at); with one source and rank ties everywhere, the older
// backlog won every single-slot round.
//
// The fixture mirrors the incident: an older same-source
// container_image_identity backlog with inflight work, plus newer ready
// deployable_unit_correlation rows with no inflight work. Claim rounds use
// limit=1, the freed-slot regime. The agreed bound: the idle domain's head
// row claims in the first round, and all six ready rows claim within 12
// single-slot rounds.
func TestClaimBatchDoesNotStarveIdleDomainBehindSameSourceBacklog(t *testing.T) {
	dsn := reducerDomainFairnessDSN()
	if dsn == "" {
		t.Skip("set ESHU_REDUCER_FAIRNESS_PROOF_DSN or ESHU_POSTGRES_DSN to run the idle-domain starvation proof")
	}

	ctx := context.Background()
	db := openReducerFairnessDB(t, ctx, dsn)

	const (
		busyDomain    = reducer.DomainContainerImageIdentity
		starvedDomain = reducer.DomainDeployableUnitCorrelation
		busyCount     = 24
		starvedCount  = 6
		maxRounds     = 12
	)

	now := time.Date(2026, time.October, 10, 8, 0, 0, 0, time.UTC)
	seedReducerFairnessScope(t, ctx, db, "scope-8024", now)

	const sourceSystem = "img-src"
	for i := 0; i < busyCount; i++ {
		insertReducerFairnessWorkItem(t, ctx, db, reducerFairnessWorkItem{
			workItemID:     fmt.Sprintf("idle-busy-%03d", i),
			scopeID:        "scope-8024",
			generationID:   "gen-fair",
			domain:         string(busyDomain),
			conflictDomain: reducerConflictDomainScope,
			conflictKey:    fmt.Sprintf("idle-busy-key-%03d", i),
			sourceSystem:   sourceSystem,
			updatedAt:      now.Add(time.Duration(i) * time.Second),
		})
	}
	starvedBase := now.Add(time.Hour)
	for i := 0; i < starvedCount; i++ {
		insertReducerFairnessWorkItem(t, ctx, db, reducerFairnessWorkItem{
			workItemID:     fmt.Sprintf("idle-starved-%03d", i),
			scopeID:        "scope-8024",
			generationID:   "gen-fair",
			domain:         string(starvedDomain),
			conflictDomain: reducerConflictDomainScope,
			conflictKey:    fmt.Sprintf("idle-starved-key-%03d", i),
			sourceSystem:   sourceSystem,
			updatedAt:      starvedBase.Add(time.Duration(i) * time.Second),
		})
	}
	// The busy domain holds inflight leases (it is being worked); the starved
	// domain holds none. Seeded pending rows with no visible_at are
	// claim-time-ready, the steady-state equivalent of the incident's
	// post-deferral ready rows: deferral history does not enter the claim
	// query, only claim-time readiness does.
	claimAt := starvedBase.Add(2 * time.Hour)
	markReducerFairnessWorkClaimed(t, ctx, db, "busy-worker", claimAt.Add(time.Hour),
		"idle-busy-000", "idle-busy-001")

	queue := ReducerQueue{
		database:      SQLDB{DB: db},
		LeaseOwner:    "idle-starvation-test",
		LeaseDuration: time.Minute,
		Now:           func() time.Time { return claimAt },
		ClaimDomains:  []reducercontract.Domain{busyDomain, starvedDomain},
	}

	starvedClaimed := make(map[string]bool)
	var firstDomain reducercontract.Domain
	for round := 0; round < maxRounds && len(starvedClaimed) < starvedCount; round++ {
		intents, err := queue.ClaimBatch(ctx, 1)
		if err != nil {
			t.Fatalf("ClaimBatch() round %d error = %v", round, err)
		}
		if len(intents) != 1 {
			t.Fatalf("ClaimBatch() round %d returned %d intents, want 1", round, len(intents))
		}
		if round == 0 {
			firstDomain = intents[0].Domain
		}
		if intents[0].Domain == starvedDomain {
			starvedClaimed[intents[0].IntentID] = true
		}
	}
	if firstDomain != starvedDomain {
		t.Fatalf("first single-slot round claimed %q, want idle domain %q within 1 round (#8024)",
			firstDomain, starvedDomain)
	}
	if len(starvedClaimed) != starvedCount {
		t.Fatalf("claimed %d/%d idle-domain rows in %d single-slot rounds, want all (starved behind same-source backlog, #8024)",
			len(starvedClaimed), starvedCount, maxRounds)
	}
}

// TestClaimBatchIdleDomainPriorityHoldsUnderConcurrentClaimers is the AC3
// concurrency proof for #8024: four concurrent single-connection claimers take
// disjoint rows (no lease theft, no duplicate work) and the idle domain still
// takes slots while a same-source backlog competes.
func TestClaimBatchIdleDomainPriorityHoldsUnderConcurrentClaimers(t *testing.T) {
	dsn := reducerDomainFairnessDSN()
	if dsn == "" {
		t.Skip("set ESHU_REDUCER_FAIRNESS_PROOF_DSN or ESHU_POSTGRES_DSN to run the idle-domain concurrency proof")
	}

	ctx := context.Background()
	db, schemaName := openReducerFairnessDBWithSchema(t, ctx, dsn)

	const (
		busyDomain    = reducer.DomainContainerImageIdentity
		starvedDomain = reducer.DomainDeployableUnitCorrelation
		busyCount     = 12
		starvedCount  = 4
		claimers      = 4
		perClaim      = 2
	)

	now := time.Date(2026, time.October, 10, 9, 0, 0, 0, time.UTC)
	seedReducerFairnessScope(t, ctx, db, "scope-8024-conc", now)

	const sourceSystem = "img-src"
	for i := 0; i < busyCount; i++ {
		insertReducerFairnessWorkItem(t, ctx, db, reducerFairnessWorkItem{
			workItemID:     fmt.Sprintf("conc-busy-%03d", i),
			scopeID:        "scope-8024-conc",
			generationID:   "gen-fair",
			domain:         string(busyDomain),
			conflictDomain: reducerConflictDomainScope,
			conflictKey:    fmt.Sprintf("conc-busy-key-%03d", i),
			sourceSystem:   sourceSystem,
			updatedAt:      now.Add(time.Duration(i) * time.Second),
		})
	}
	starvedBase := now.Add(time.Hour)
	for i := 0; i < starvedCount; i++ {
		insertReducerFairnessWorkItem(t, ctx, db, reducerFairnessWorkItem{
			workItemID:     fmt.Sprintf("conc-starved-%03d", i),
			scopeID:        "scope-8024-conc",
			generationID:   "gen-fair",
			domain:         string(starvedDomain),
			conflictDomain: reducerConflictDomainScope,
			conflictKey:    fmt.Sprintf("conc-starved-key-%03d", i),
			sourceSystem:   sourceSystem,
			updatedAt:      starvedBase.Add(time.Duration(i) * time.Second),
		})
	}
	claimAt := starvedBase.Add(2 * time.Hour)
	markReducerFairnessWorkClaimed(t, ctx, db, "busy-worker", claimAt.Add(time.Hour), "conc-busy-000")

	type claimResult struct {
		intents []reducercontract.Intent
		err     error
	}
	results := make([]claimResult, claimers)
	var wg sync.WaitGroup
	for c := 0; c < claimers; c++ {
		wg.Add(1)
		go func(c int) {
			defer wg.Done()
			// Each claimer holds its own connection so the four claim
			// statements truly interleave at the database instead of
			// serializing behind one pooled connection.
			cdb := openReducerFairnessClaimerDB(t, ctx, dsn, schemaName)
			q := ReducerQueue{
				database:      SQLDB{DB: cdb},
				LeaseOwner:    fmt.Sprintf("conc-claimer-%d", c),
				LeaseDuration: time.Minute,
				Now:           func() time.Time { return claimAt },
				ClaimDomains:  []reducercontract.Domain{busyDomain, starvedDomain},
			}
			intents, err := q.ClaimBatch(ctx, perClaim)
			results[c] = claimResult{intents: intents, err: err}
		}(c)
	}
	wg.Wait()

	seen := make(map[string]string)
	starvedTotal := 0
	for c, r := range results {
		if r.err != nil {
			t.Fatalf("claimer %d ClaimBatch() error = %v", c, r.err)
		}
		for _, intent := range r.intents {
			if owner, dup := seen[intent.IntentID]; dup {
				t.Fatalf("work item %q claimed twice (by %q and claimer %d): lease theft / duplicate work under concurrency (#8024)",
					intent.IntentID, owner, c)
			}
			seen[intent.IntentID] = fmt.Sprintf("conc-claimer-%d", c)
			if intent.Domain == starvedDomain {
				starvedTotal++
			}
		}
	}
	if len(seen) != claimers*perClaim {
		t.Fatalf("concurrent claimers took %d distinct rows, want %d (lost or duplicated work, #8024)",
			len(seen), claimers*perClaim)
	}
	if starvedTotal == 0 {
		t.Fatalf("concurrent claimers took %d rows, none for idle domain %q (ordering collapses under contention, #8024)",
			len(seen), starvedDomain)
	}
}

// markReducerFairnessWorkClaimed parks the named rows as inflight leases held
// by another worker: they count toward source/domain inflight tallies while
// staying unclaimable until their leases expire.
func markReducerFairnessWorkClaimed(t *testing.T, ctx context.Context, db *sql.DB, owner string, claimUntil time.Time, workItemIDs ...string) {
	t.Helper()
	for _, id := range workItemIDs {
		if _, err := db.ExecContext(ctx, `
UPDATE fact_work_items
SET status = 'claimed', lease_owner = $2, claim_until = $3, last_attempt_at = $3
WHERE work_item_id = $1`, id, owner, claimUntil); err != nil {
			t.Fatalf("mark %q claimed: %v", id, err)
		}
	}
}
