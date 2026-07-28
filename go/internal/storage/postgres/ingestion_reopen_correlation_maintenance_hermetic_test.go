// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"strings"
	"testing"
	"time"
)

// TestRunDeferredRelationshipMaintenanceIssuesCorrelationListingPerDomain is the
// CI-VISIBLE gate for the whole change. Its live sibling,
// TestRunDeferredRelationshipMaintenanceReopensCrossScopeCorrelationDomains, is
// gated on ESHU_DEFERRED_PARTITION_PROOF_DSN, which no workflow in
// .github/workflows sets — so it SKIPS in CI, and with only that test the
// ReopenSucceededReducerWorkItems call could be deleted from
// reopenMaintenanceWorkItemsInTransaction and every package would stay green.
// That is the same defect class this change exists to fix: evidence that only a
// runtime nobody runs produces.
//
// This test needs no Postgres. It drives the real
// RunDeferredRelationshipMaintenance against the package's fakeTx, which records
// every query issued inside a transaction, and asserts the ingester's ONE reopen
// transaction issues exactly one correlation listing per domain in
// CrossScopeCorrelationReopenDomains, with that domain bound to $1. Deleting the
// ReopenSucceededReducerWorkItems call fails it hermetically.
func TestRunDeferredRelationshipMaintenanceIssuesCorrelationListingPerDomain(t *testing.T) {
	t.Parallel()

	reopenTx, _ := runHermeticDeferredMaintenance(t)

	// The two relationship reopens and every correlation listing must land on the
	// SAME transaction: reopenMaintenanceWorkItemsInTransaction owes all of them
	// all-or-nothing, so a partial reopen is never observable.
	if !hasQueryContaining(reopenTx.queries, "domain = 'deployment_mapping'") {
		t.Fatal("reopen transaction issued no deployment_mapping listing; wrong transaction inspected")
	}
	if !hasQueryContaining(reopenTx.queries, "domain = 'code_import_repo_edge'") {
		t.Fatal("reopen transaction issued no code_import_repo_edge listing")
	}

	listedDomains := correlationListingDomains(reopenTx.queries)
	want := CrossScopeCorrelationReopenDomains()
	if len(listedDomains) != len(want) {
		t.Fatalf(
			"correlation listings issued = %v, want one per domain %v "+
				"(the ingester's maintenance pass must replay every cross-scope correlation domain, "+
				"not only eshu-bootstrap-index)",
			listedDomains, want,
		)
	}
	for i, domain := range want {
		if listedDomains[i] != domain {
			t.Fatalf("correlation listing[%d] domain = %q, want %q", i, listedDomains[i], domain)
		}
	}
}

// TestRunDeferredRelationshipMaintenanceCorrelationListingCarriesReplayBound is
// the CI-visible half of the per-drain bound (P1-2). The listing the ingester
// actually issues must be the replay-floor shape, not a corpus-wide selection:
// this pass runs on EVERY shard drain, and succeeded reducer rows are never
// terminalized, so an unbounded listing resurrects the whole ingestion history
// into 'pending' on every drain.
//
// Asserting on the query the maintenance pass issues — rather than on the
// listSucceededReducerWorkItemsByDomainQuery constant alone — is what makes this
// a gate on the call path and not just on a string.
func TestRunDeferredRelationshipMaintenanceCorrelationListingCarriesReplayBound(t *testing.T) {
	t.Parallel()

	reopenTx, _ := runHermeticDeferredMaintenance(t)

	var listing string
	for _, call := range reopenTx.queries {
		if isCorrelationListing(call.query) {
			listing = call.query
			break
		}
	}
	if listing == "" {
		t.Fatal("reopen transaction issued no correlation listing at all")
	}
	for _, fragment := range []string{
		"scope_replay_floor",
		"COALESCE(active_generation.ingested_at, latest_generation.ingested_at)",
		">= (floor.floor_ingested_at, floor.floor_generation_id)",
	} {
		if !strings.Contains(listing, fragment) {
			t.Fatalf(
				"correlation listing missing replay-floor bound fragment %q; "+
					"an unbounded per-drain listing resurrects every generation's succeeded rows.\nquery = %s",
				fragment, listing,
			)
		}
	}
}

// runHermeticDeferredMaintenance drives the real RunDeferredRelationshipMaintenance
// against fakes and returns the reopen transaction plus the backfill transaction,
// in the order the store begins them.
func runHermeticDeferredMaintenance(t *testing.T) (reopenTx, backfillTx *fakeTx) {
	t.Helper()

	now := time.Date(2026, time.July, 27, 12, 0, 0, 0, time.UTC)
	activeGenerations := [][]any{{"repo-alpha", "scope-alpha", "gen-alpha"}}
	backfillTx = &fakeTx{
		// The batch transaction re-loads active generations under its batch lock.
		queryResponses: []queueFakeRows{{rows: activeGenerations}},
	}
	reopenTx = &fakeTx{}
	db := &fakeTransactionalDB{
		txs: []*fakeTx{backfillTx, reopenTx},
		queryResponses: []queueFakeRows{
			// Snapshot reads on the store db: catalog, latest facts, active generations.
			{rows: [][]any{{[]byte(`{"repo_id":"repo-alpha","name":"alpha"}`), catalogFakeObservedAt}}},
			{rows: [][]any{}},
			{rows: activeGenerations},
		},
	}
	store := NewIngestionStore(db)
	store.Now = func() time.Time { return now }

	if err := store.RunDeferredRelationshipMaintenance(context.Background(), nil, nil); err != nil {
		t.Fatalf("RunDeferredRelationshipMaintenance() error = %v, want nil", err)
	}
	if !reopenTx.committed {
		t.Fatal("reopen transaction committed = false, want true")
	}
	return reopenTx, backfillTx
}

// isCorrelationListing reports whether a recorded query is the domain-parameterized
// succeeded-reducer listing ReopenSucceededReducerWorkItems issues.
//
// Matched by identity with the production constant, not by a fragment of its
// text. Keying on "work.domain = $1" made a legitimate rewrite of the listing
// (renaming the `work` alias, say) report "no correlation listing at all" —
// accusing the pass of the very regression these tests exist to catch, while the
// pass was in fact issuing the listing. Identity fails only when the call is
// genuinely gone.
func isCorrelationListing(query string) bool {
	return query == listSucceededReducerWorkItemsByDomainQuery
}

// correlationListingDomains returns the $1 domain argument of every correlation
// listing in call order.
func correlationListingDomains(calls []fakeQueryCall) []string {
	domains := make([]string, 0, len(calls))
	for _, call := range calls {
		if !isCorrelationListing(call.query) || len(call.args) == 0 {
			continue
		}
		domain, ok := call.args[0].(string)
		if !ok {
			continue
		}
		domains = append(domains, domain)
	}
	return domains
}

func hasQueryContaining(calls []fakeQueryCall, fragment string) bool {
	for _, call := range calls {
		if strings.Contains(call.query, fragment) {
			return true
		}
	}
	return false
}
