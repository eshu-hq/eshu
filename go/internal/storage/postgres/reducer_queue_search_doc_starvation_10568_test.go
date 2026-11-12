// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/projector/runtime"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
)

// TestClaimBatchSearchDocConflictDomainIsDisjointFromAdvisory pins the #10568
// mechanism premise: eshu_search_document claims fence on the scope conflict
// domain while advisory_group_projection claims fence on the advisory_group
// domain. The starvation is pure global ORDER BY starvation, not lock or
// conflict-key contention, so reserving claim slots for the search-doc domain
// cannot introduce a new deadlock edge between the two families.
func TestClaimBatchSearchDocConflictDomainIsDisjointFromAdvisory(t *testing.T) {
	t.Parallel()

	searchDomain, searchKey := reducerConflictDomainKey(runtime.ReducerIntent{
		ScopeID: "scope-10568",
		Domain:  reducercontract.DomainEshuSearchDocument,
	})
	advisoryDomain, _ := reducerConflictDomainKey(runtime.ReducerIntent{
		ScopeID:   "scope-10568",
		Domain:    reducer.DomainAdvisoryGroupProjection,
		EntityKey: "advisory-group:v1:CVE-2026-0001",
	})
	if searchDomain != reducerConflictDomainScope {
		t.Fatalf("eshu_search_document conflict domain = %q, want %q", searchDomain, reducerConflictDomainScope)
	}
	if advisoryDomain != reducerConflictDomainAdvisoryGroup {
		t.Fatalf("advisory_group_projection conflict domain = %q, want %q", advisoryDomain, reducerConflictDomainAdvisoryGroup)
	}
	if searchDomain == advisoryDomain {
		t.Fatalf("conflict domains overlap: search-doc and advisory share %q (contention, not starvation)", searchDomain)
	}
	if strings.TrimSpace(searchKey) == "" {
		t.Fatal("eshu_search_document conflict key is empty")
	}
}

// TestClaimBatchReservesSearchDocClaimSlots is the hermetic marker pin for
// #10568 (RED before the reserved slot landed, GREEN since): the batch claim
// must carry a reserved-slot mechanism so eshu_search_document rows are
// claimable while a priority-0 backlog deeper than the batch LIMIT exists.
// The mechanism's shape is pinned beside this marker by the query-shape
// tests; this pin only keeps the marker itself from vanishing silently.
func TestClaimBatchReservesSearchDocClaimSlots(t *testing.T) {
	t.Parallel()

	query := claimReducerWorkBatchQuery
	const marker = "search_doc_reserved"
	if !strings.Contains(query, marker) {
		t.Fatalf("batch claim query has no reserved-slot mechanism for eshu_search_document (want %q): "+
			"priority-1 rows starve behind any priority-0 backlog deeper than LIMIT (#10568)", marker)
	}
}

// TestClaimBatchSearchDocStarvationLiveReproduction is the live-fixture
// regression proof for #10568 acceptance criterion 1 (RED before the
// reserved slot): priority-1 search-doc rows must drain while a priority-0
// advisory backlog deeper than the batch LIMIT exists. Fixture mirrors the
// 2026-11-12 live census at small scale (40
// advisory rows vs 19.4k live; 4 search-doc rows vs 660 live, oldest ~72h).
// All rows share one scope (one active generation: ingestion_scopes carries
// UNIQUE(active_generation_id), so a scope per row cannot seed). Search-doc
// rows carry distinct scope conflict keys so the key fence never collapses
// them against each other; advisory rows carry distinct advisory-group keys
// for the same reason. The two families share a source_system, matching
// live, so the source-fairness lane cannot rescue the starved domain.
func TestClaimBatchSearchDocStarvationLiveReproduction(t *testing.T) {
	dsn := reducerDomainFairnessDSN()
	if dsn == "" {
		t.Skip("set ESHU_REDUCER_FAIRNESS_PROOF_DSN or ESHU_POSTGRES_DSN to run the search-doc starvation proof")
	}

	ctx := context.Background()
	db := openReducerFairnessDB(t, ctx, dsn)

	const (
		advisoryDomain  = reducer.DomainAdvisoryGroupProjection
		advisoryCount   = 40
		searchDocCount  = 4
		batchSize       = 4
		maxRounds       = 6
		sourceSystem    = "aws"
		searchDocDomain = reducercontract.DomainEshuSearchDocument
	)

	// 40 advisory slots claimed over 6 rounds of 4 stays below the 40-row
	// backlog, so under the current strict priority ordering no round can
	// reach a priority-1 row: the backlog never drains within the proof.
	now := time.Date(2026, time.November, 11, 8, 0, 0, 0, time.UTC)
	seedReducerFairnessScope(t, ctx, db, "scope-10568", now)
	for i := 0; i < advisoryCount; i++ {
		insertReducerFairnessWorkItem(t, ctx, db, reducerFairnessWorkItem{
			workItemID:     fmt.Sprintf("sd-starve-adv-%03d", i),
			scopeID:        "scope-10568",
			generationID:   "gen-fair",
			domain:         string(advisoryDomain),
			conflictDomain: reducerConflictDomainAdvisoryGroup,
			conflictKey:    fmt.Sprintf("sd-starve-adv-key-%03d", i),
			sourceSystem:   sourceSystem,
			updatedAt:      now.Add(time.Duration(i) * time.Second),
		})
	}
	// Search-doc rows are OLDER (~72h, the live oldest age) to prove the
	// point: even maximal age cannot overcome the priority sort today, and
	// the same fixture exercises a future priority-aging design.
	searchBase := now.Add(-72 * time.Hour)
	for i := 0; i < searchDocCount; i++ {
		insertReducerFairnessWorkItem(t, ctx, db, reducerFairnessWorkItem{
			workItemID:     fmt.Sprintf("sd-starved-%03d", i),
			scopeID:        "scope-10568",
			generationID:   "gen-fair",
			domain:         string(searchDocDomain),
			conflictDomain: reducerConflictDomainScope,
			conflictKey:    fmt.Sprintf("sd-starve-scope-key-%03d", i),
			sourceSystem:   sourceSystem,
			updatedAt:      searchBase.Add(time.Duration(i) * time.Second),
		})
	}

	queue := ReducerQueue{
		database:      SQLDB{DB: db},
		LeaseOwner:    "search-doc-starvation-test",
		LeaseDuration: time.Minute,
		Now:           func() time.Time { return now.Add(2 * time.Hour) },
		ClaimDomains:  []reducercontract.Domain{advisoryDomain, searchDocDomain},
	}

	searchClaimed := make(map[string]bool)
	totalClaimed := 0
	roundsRun := 0
	for round := 0; round < maxRounds && len(searchClaimed) < searchDocCount; round++ {
		intents, err := queue.ClaimBatch(ctx, batchSize)
		if err != nil {
			t.Fatalf("ClaimBatch() round %d error = %v", round, err)
		}
		if len(intents) == 0 {
			t.Fatalf("ClaimBatch() round %d returned 0 intents with %d advisory rows still pending", round, advisoryCount-totalClaimed)
		}
		roundsRun++
		totalClaimed += len(intents)
		for _, intent := range intents {
			if intent.Domain == searchDocDomain {
				searchClaimed[intent.IntentID] = true
			}
		}
	}
	// Guard against a vacuous pass: the advisory backlog must actually be
	// claimable (otherwise search-doc rows win by default and the fixture
	// proves nothing about ordering). Every round that ran must return a
	// full batch: the loop exits early once all search-doc rows drain, so
	// the bar is rounds-run times batch size, not all maxRounds.
	if totalClaimed < roundsRun*batchSize {
		t.Fatalf("claimed only %d rows in %d rounds of %d; advisory backlog is not claimable, fixture is vacuous (#10568)",
			totalClaimed, roundsRun, batchSize)
	}
	if len(searchClaimed) != searchDocCount {
		t.Fatalf("claimed %d/%d eshu_search_document rows in %d rounds of %d behind a %d-row advisory backlog, "+
			"want all (starved by reducer_domain_priority ordering, #10568)",
			len(searchClaimed), searchDocCount, maxRounds, batchSize, advisoryCount)
	}
}

// TestClaimBatchSearchDocSlotHoldsUnderConcurrentClaimers is the reserved-slot
// twin of TestClaimBatchLockRecheckDropsConcurrentlyClaimedRow for #10568:
// concurrent claimers racing an advisory backlog plus ready search-doc rows
// must take disjoint rows (no lease theft or duplicate work through either
// lock arm), fill every slot (no lost work from the split LIMIT), engage the
// reserved slot (at least one search-doc row wins a batch), and never take
// more than one search-doc row per batch (the k = 1 bound holds per claim,
// not just per round).
func TestClaimBatchSearchDocSlotHoldsUnderConcurrentClaimers(t *testing.T) {
	dsn := reducerDomainFairnessDSN()
	if dsn == "" {
		t.Skip("set ESHU_REDUCER_FAIRNESS_PROOF_DSN or ESHU_POSTGRES_DSN to run the reserved-slot concurrency proof")
	}

	ctx := context.Background()
	db, targetDSN := openReducerFairnessDBWithSchema(t, ctx, dsn)

	const (
		advisoryDomain  = reducer.DomainAdvisoryGroupProjection
		advisoryCount   = 24
		searchDocCount  = 4
		claimers        = 4
		perClaim        = 2
		sourceSystem    = "aws"
		searchDocDomain = reducercontract.DomainEshuSearchDocument
	)

	now := time.Date(2026, time.November, 11, 8, 0, 0, 0, time.UTC)
	seedReducerFairnessScope(t, ctx, db, "scope-10568-conc", now)
	for i := 0; i < advisoryCount; i++ {
		insertReducerFairnessWorkItem(t, ctx, db, reducerFairnessWorkItem{
			workItemID:     fmt.Sprintf("conc-adv-%03d", i),
			scopeID:        "scope-10568-conc",
			generationID:   "gen-fair",
			domain:         string(advisoryDomain),
			conflictDomain: reducerConflictDomainAdvisoryGroup,
			conflictKey:    fmt.Sprintf("conc-adv-key-%03d", i),
			sourceSystem:   sourceSystem,
			updatedAt:      now.Add(time.Duration(i) * time.Second),
		})
	}
	searchBase := now.Add(-72 * time.Hour)
	for i := 0; i < searchDocCount; i++ {
		insertReducerFairnessWorkItem(t, ctx, db, reducerFairnessWorkItem{
			workItemID:     fmt.Sprintf("conc-sd-%03d", i),
			scopeID:        "scope-10568-conc",
			generationID:   "gen-fair",
			domain:         string(searchDocDomain),
			conflictDomain: reducerConflictDomainScope,
			conflictKey:    fmt.Sprintf("conc-sd-key-%03d", i),
			sourceSystem:   sourceSystem,
			updatedAt:      searchBase.Add(time.Duration(i) * time.Second),
		})
	}

	claimAt := now.Add(2 * time.Hour)
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
			// Each claimer holds its own connection so the claim statements
			// truly interleave at the database instead of serializing behind
			// one pooled connection.
			cdb := openReducerFairnessClaimerDB(t, ctx, targetDSN)
			q := ReducerQueue{
				database:      SQLDB{DB: cdb},
				LeaseOwner:    fmt.Sprintf("search-doc-conc-%d", c),
				LeaseDuration: time.Minute,
				Now:           func() time.Time { return claimAt },
				ClaimDomains:  []reducercontract.Domain{advisoryDomain, searchDocDomain},
			}
			intents, err := q.ClaimBatch(ctx, perClaim)
			results[c] = claimResult{intents: intents, err: err}
		}(c)
	}
	wg.Wait()

	seen := make(map[string]string)
	perClaimerSearch := make([]int, claimers)
	searchTotal := 0
	for c, r := range results {
		if r.err != nil {
			t.Fatalf("claimer %d ClaimBatch() error = %v", c, r.err)
		}
		if len(r.intents) != perClaim {
			t.Fatalf("claimer %d claimed %d intents, want a full batch of %d (split LIMIT lost work, #10568)",
				c, len(r.intents), perClaim)
		}
		for _, intent := range r.intents {
			if owner, dup := seen[intent.IntentID]; dup {
				t.Fatalf("work item %q claimed twice (by %q and claimer %d): lease theft / duplicate work through the split lock arms (#10568)",
					intent.IntentID, owner, c)
			}
			seen[intent.IntentID] = fmt.Sprintf("search-doc-conc-%d", c)
			if intent.Domain == searchDocDomain {
				perClaimerSearch[c]++
				searchTotal++
			}
		}
		if perClaimerSearch[c] > 1 {
			t.Fatalf("claimer %d took %d eshu_search_document rows in one batch of %d, want at most 1 (k = 1 bound, #10568)",
				c, perClaimerSearch[c], perClaim)
		}
	}
	if len(seen) != claimers*perClaim {
		t.Fatalf("concurrent claimers took %d distinct rows, want %d (lost or duplicated work, #10568)",
			len(seen), claimers*perClaim)
	}
	if searchTotal == 0 {
		t.Fatalf("concurrent claimers took %d rows, none for %q (reserved slot does not engage under contention, #10568)",
			len(seen), searchDocDomain)
	}
}
