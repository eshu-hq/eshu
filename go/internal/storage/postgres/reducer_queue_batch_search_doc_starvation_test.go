// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
)

// TestClaimBatchSearchDocumentStarvesBehindAdvisoryBacklog is the #10568
// regression proof (RED before the reserved slot landed, GREEN since): the
// batch claim's strict domain-priority ordering alone would starve
// eshu_search_document (the only priority-1 domain) behind the priority-0
// advisory backlog.
//
// The fixture mirrors the live census on #10567: a priority-0 backlog larger
// than the batch limit plus a smaller set of pending eshu_search_document
// rows. The search-doc rows are seeded OLDER than the advisory backlog, so a
// zero-search-doc claim cannot be explained by updated_at ordering — only by
// the reducer_domain_priority key sorting first. It drives the production
// ReducerQueue.ClaimBatch path (the shipped claimReducerWorkBatchQuery,
// including its LIMIT $8), so no ordering simulation stands between the
// fixture and the verdict: zero claimed search-doc rows is the starvation.
func TestClaimBatchSearchDocumentStarvesBehindAdvisoryBacklog(t *testing.T) {
	dsn := reducerDomainFairnessDSN()
	if dsn == "" {
		t.Skip("set ESHU_REDUCER_FAIRNESS_PROOF_DSN or ESHU_POSTGRES_DSN to run the search-document starvation proof")
	}

	ctx := context.Background()
	db := openReducerFairnessDB(t, ctx, dsn)

	const (
		busyDomain   = reducercontract.DomainAdvisoryGroupProjection
		searchDomain = reducercontract.DomainEshuSearchDocument
		busyCount    = 24
		searchCount  = 4
		batchSize    = 8
	)

	now := time.Date(2026, time.November, 12, 8, 0, 0, 0, time.UTC)
	seedReducerFairnessScope(t, ctx, db, "scope-search-starve", now)

	// Priority-0 advisory backlog, larger than the batch: every row in its own
	// advisory-group conflict group (the live domain for this family), so the
	// representative fence never collapses them.
	for i := 0; i < busyCount; i++ {
		insertReducerFairnessWorkItem(t, ctx, db, reducerFairnessWorkItem{
			workItemID:     fmt.Sprintf("advisory-%03d", i),
			scopeID:        "scope-search-starve",
			generationID:   "gen-fair",
			domain:         string(busyDomain),
			conflictDomain: reducerConflictDomainAdvisoryGroup,
			conflictKey:    fmt.Sprintf("advisory-key-%03d", i),
			updatedAt:      now.Add(time.Duration(i) * time.Second),
		})
	}
	// Priority-1 search-doc rows, OLDER than the advisory backlog (live: oldest
	// ~72 h pending, attempt_count 0, never claimed). Distinct conflict groups,
	// like the advisory rows, so only the domain-priority key can bury them.
	searchBase := now.Add(-72 * time.Hour)
	for i := 0; i < searchCount; i++ {
		insertReducerFairnessWorkItem(t, ctx, db, reducerFairnessWorkItem{
			workItemID:     fmt.Sprintf("searchdoc-%03d", i),
			scopeID:        "scope-search-starve",
			generationID:   "gen-fair",
			domain:         string(searchDomain),
			conflictDomain: reducerConflictDomainScope,
			conflictKey:    fmt.Sprintf("searchdoc-key-%03d", i),
			updatedAt:      searchBase.Add(time.Duration(i) * time.Second),
		})
	}

	queue := ReducerQueue{
		database:      SQLDB{DB: db},
		LeaseOwner:    "search-starvation-test",
		LeaseDuration: time.Minute,
		Now:           func() time.Time { return now.Add(2 * time.Hour) },
		ClaimDomains:  []reducercontract.Domain{busyDomain, searchDomain},
	}

	intents, err := queue.ClaimBatch(ctx, batchSize)
	if err != nil {
		t.Fatalf("ClaimBatch() error = %v", err)
	}
	if len(intents) != batchSize {
		t.Fatalf("ClaimBatch() claimed %d items, want a full batch of %d "+
			"(fixture must hold backlog pressure: %d advisory + %d search-doc rows)",
			len(intents), batchSize, busyCount, searchCount)
	}

	var searchClaimed int
	for _, intent := range intents {
		if intent.Domain == searchDomain {
			searchClaimed++
		}
	}
	if searchClaimed == 0 {
		t.Fatalf("ClaimBatch() claimed %d items, none for %q; "+
			"strict domain-priority ordering starves search-doc rows behind the "+
			"advisory backlog even though they are older (issue #10568)",
			len(intents), searchDomain)
	}
}

// TestClaimBatchSearchDocumentStarvationMechanismPinnedToShippedQuery is the
// hermetic companion to the live starvation proof above: it pins the exact
// shipped ordering fragments that bury priority-1 rows, so the fixture's
// faithfulness to claimReducerWorkBatchQuery is asserted without a database.
// The priority CASE maps eshu_search_document to 1 and everything else to 0;
// the locked_rest arm's ORDER BY sorts that key first. Since #10568 the batch
// bound is split across the reserved-slot arms (the reserved arm takes LIMIT
// 1, the rest arm takes LIMIT $8 minus the reserved take), so a batch still
// closes at $8 rows while a priority-1 row can always take one slot.
//
// If a later merge restructures this query, this pin fails loudly and the
// starvation fixture is re-verified against the new shape — the regression
// test must never silently validate a stale ordering.
func TestClaimBatchSearchDocumentStarvationMechanismPinnedToShippedQuery(t *testing.T) {
	t.Parallel()

	query := claimReducerWorkBatchQuery

	const priorityCASE = "CASE WHEN fact_work_items.domain = 'eshu_search_document' THEN 1 ELSE 0 END AS reducer_domain_priority"
	if !strings.Contains(query, priorityCASE) {
		t.Fatalf("batch claim query lost the domain-priority CASE %q", priorityCASE)
	}

	const priorityOrderLead = "ORDER BY candidate.reducer_domain_priority ASC"
	if !strings.Contains(query, priorityOrderLead) {
		t.Fatalf("batch claim query no longer leads its claim ORDER BY with %q", priorityOrderLead)
	}

	// Since #10568 the batch closes at $8 rows across the two lock arms, not
	// at a single LIMIT $8: the reserved arm takes at most one priority-1
	// row and the rest arm fills LIMIT minus that take.
	for _, want := range []string{
		"ORDER BY ordered_candidate.updated_at ASC, ordered_candidate.work_item_id ASC\n    LIMIT 1",
		"LIMIT ($8 - (SELECT COUNT(*) FROM locked_search_doc_reserved))",
	} {
		if !strings.Contains(query, want) {
			t.Fatalf("batch claim query lost its split batch bound %q", want)
		}
	}
}
