// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer/securityalert"
)

// withSupplyChainImpactSupportsCallMaxActiveRows shrinks the per-call working
// set budget for one test. The budget is a package var (not a const) so tests
// can model a multi-repository call tripping the budget with a few pages
// instead of hundreds of thousands of rows; the shipped binary keeps the
// default. Callers must not run in parallel while the budget is shrunk.
func withSupplyChainImpactSupportsCallMaxActiveRows(t *testing.T, max int) {
	t.Helper()
	old := supplyChainImpactSupportsCallMaxActiveRows
	supplyChainImpactSupportsCallMaxActiveRows = max
	t.Cleanup(func() { supplyChainImpactSupportsCallMaxActiveRows = old })
}

// callBudgetAdvisoryTail is one short trailing page for one repository: a
// single active row ends the repository's drain (raw page shorter than the
// page size) so the call moves on to the next repository.
func callBudgetAdvisoryTail(repo string, n int) [][]any {
	return [][]any{supplyChainImpactSupportRow(
		fmt.Sprintf("fact-%04d", n), repo,
		fmt.Sprintf(`{"repository_id":%q,"cve_id":"CVE-%04d","ecosystem":"npm","package_name":"p"}`,
			repo, n))}
}

// TestGetSupplyChainAdvisoriesForReposTruncatesOverCallBudget is the
// call-level OOM regression test (#11436): two repositories that each fit the
// per-drain cap must still fail fast when their sum passes the call budget.
// The call serves nothing (nil map) and reports the last delivered fact id,
// so a prefix served as complete can never silently drop findings.
func TestGetSupplyChainAdvisoriesForReposTruncatesOverCallBudget(t *testing.T) {
	withDrainMaxActiveRows(t, 10*listFactsByKindPageSize)
	withSupplyChainImpactSupportsCallMaxActiveRows(t, listFactsByKindPageSize+251)

	database := &fakeExecQueryer{queryResponses: []queueFakeRows{
		supportsReadyResponse(),
		{rows: boundAdvisoryPage("repo-a", 0)},
		{rows: callBudgetAdvisoryTail("repo-a", 500)},
		{rows: boundAdvisoryPage("repo-b", 1000)},
	}}
	store := NewFactStore(database)

	byRepo, err := store.GetSupplyChainAdvisoriesForRepos(context.Background(), []string{"repo-a", "repo-b"})
	var truncated *SupplyChainImpactSupportsDrainTruncatedError
	if !errors.As(err, &truncated) {
		t.Fatalf("GetSupplyChainAdvisoriesForRepos() error = %v, want *SupplyChainImpactSupportsDrainTruncatedError (the call must stop at the call budget)", err)
	}
	if byRepo != nil {
		t.Fatalf("GetSupplyChainAdvisoriesForRepos() = %d repos, want nil on call-budget truncation (no partial answer)", len(byRepo))
	}
	// repo-a delivers 501 rows; repo-b's page is cut to the first 250, so the
	// cursor is the last delivered id inside repo-b's page, not its end.
	if want := "fact-1249"; truncated.ResumeCursorFactID != want {
		t.Fatalf("resume cursor = %q, want %q (last delivered fact id of the cut page)", truncated.ResumeCursorFactID, want)
	}
	if got := truncated.Stats.activeRows; got != listFactsByKindPageSize+251 {
		t.Fatalf("truncated active rows = %d, want %d (repo-a 501 plus repo-b 250)", got, listFactsByKindPageSize+251)
	}
	if !truncated.Retryable() || truncated.FailureClass() != "supply_chain_impact_supports_drain_truncated" {
		t.Fatalf("truncation retryable=%v class=%q, want retryable supply_chain_impact_supports_drain_truncated",
			truncated.Retryable(), truncated.FailureClass())
	}
}

// TestGetSupplyChainAdvisoriesForReposServesUnderCallBudget pins the other
// half of the call bound: a multi-repository call under the budget behaves
// exactly as before, so the budget changes nothing for ordinary calls.
func TestGetSupplyChainAdvisoriesForReposServesUnderCallBudget(t *testing.T) {
	withSupplyChainImpactSupportsCallMaxActiveRows(t, listFactsByKindPageSize)

	database := &fakeExecQueryer{queryResponses: []queueFakeRows{
		supportsReadyResponse(),
		{rows: callBudgetAdvisoryTail("repo-a", 0)},
		{rows: callBudgetAdvisoryTail("repo-b", 1)},
	}}
	store := NewFactStore(database)

	byRepo, err := store.GetSupplyChainAdvisoriesForRepos(context.Background(), []string{"repo-a", "repo-b"})
	if err != nil {
		t.Fatalf("GetSupplyChainAdvisoriesForRepos() error = %v, want nil under the call budget", err)
	}
	if len(byRepo["repo-a"]) != 1 || len(byRepo["repo-b"]) != 1 {
		t.Fatalf("byRepo = %v, want one record per repository", byRepo)
	}
}

// callBudgetFactPage builds one full page of reconciliation fact rows, fact
// ids base+0000..base+0499, so the sibling fact stream can trip the call
// budget without a live database.
func callBudgetFactPage(base int) [][]any {
	rows := make([][]any, 0, listFactsByKindPageSize)
	for i := 0; i < listFactsByKindPageSize; i++ {
		n := base + i
		rows = append(rows, reconciliationFactRow(
			fmt.Sprintf("fact-%04d", n),
			"security_alert.repository_alert",
			fmt.Sprintf(`{"repository_id":"acme/app","cve_id":"CVE-%04d"}`, n)))
	}
	return rows
}

// TestListActiveSecurityAlertReconciliationFactsTruncatesFactStreamOverCallBudget
// covers the sibling fact stream (#11436): the alert and consumption pages
// accumulate without any bound today, so a hot filter must stop at the call
// budget with the resume cursor instead of appending every envelope.
func TestListActiveSecurityAlertReconciliationFactsTruncatesFactStreamOverCallBudget(t *testing.T) {
	withDrainMaxActiveRows(t, 10*listFactsByKindPageSize)
	withSupplyChainImpactSupportsCallMaxActiveRows(t, listFactsByKindPageSize+250)

	// The keys-readiness probe runs per fact page, so each page stages its
	// own probe answer ahead of its rows.
	database := &fakeExecQueryer{queryResponses: []queueFakeRows{
		{rows: [][]any{{true}}},  // supports readiness
		{rows: [][]any{{false}}}, // keyed sidecar not ready: the legacy fact statement
		{rows: callBudgetFactPage(0)},
		{rows: [][]any{{false}}}, // keyed probe for page 2
		{rows: callBudgetFactPage(500)},
	}}
	store := NewFactStore(database)

	got, err := store.ListActiveSecurityAlertReconciliationFacts(context.Background(),
		securityalert.SecurityAlertReconciliationFactFilter{RepositoryIDs: []string{"acme/app"}})
	var truncated *SupplyChainImpactSupportsDrainTruncatedError
	if !errors.As(err, &truncated) {
		t.Fatalf("ListActiveSecurityAlertReconciliationFacts() error = %v, want *SupplyChainImpactSupportsDrainTruncatedError (the fact stream must stop at the call budget)", err)
	}
	if got != nil {
		t.Fatalf("ListActiveSecurityAlertReconciliationFacts() = %d envelopes, want nil on truncation (no prefix as complete)", len(got))
	}
	// Page 1 delivers 500 rows; page 2 is cut to the first 250, so the cursor
	// is the last delivered id inside page 2, not its end.
	if want := "fact-0749"; truncated.ResumeCursorFactID != want {
		t.Fatalf("resume cursor = %q, want %q (last delivered fact id of the cut page)", truncated.ResumeCursorFactID, want)
	}
	if got := truncated.Stats.activeRows; got != listFactsByKindPageSize+250 {
		t.Fatalf("truncated active rows = %d, want %d", got, listFactsByKindPageSize+250)
	}
	// Fail fast means the supports drain never starts: no supports statement
	// may follow the truncated fact stream.
	for _, q := range database.queries {
		if strings.Contains(q.query, "supply_chain_impact_finding_supports AS s") {
			t.Fatalf("supports statement ran after the fact stream truncated (the call must fail fast)")
		}
	}
}

// TestListActiveSecurityAlertReconciliationFactsTruncatesImpactsOnRemainder
// pins that the two streams share one call budget: the fact stream spends
// part of it and the supports drain stops on what remains, not on a fresh
// full budget.
func TestListActiveSecurityAlertReconciliationFactsTruncatesImpactsOnRemainder(t *testing.T) {
	withDrainMaxActiveRows(t, 10*listFactsByKindPageSize)
	withSupplyChainImpactSupportsCallMaxActiveRows(t, 101)

	database := &fakeExecQueryer{queryResponses: []queueFakeRows{
		{rows: [][]any{{true}}},  // supports readiness
		{rows: [][]any{{false}}}, // keyed sidecar not ready: the legacy fact statement
		{rows: [][]any{
			reconciliationFactRow("fact-a", "security_alert.repository_alert", `{"repository_id":"acme/app"}`),
		}},
		{rows: boundTestActivePage(0)},
	}}
	store := NewFactStore(database)

	got, err := store.ListActiveSecurityAlertReconciliationFacts(context.Background(),
		securityalert.SecurityAlertReconciliationFactFilter{RepositoryIDs: []string{"acme/app"}})
	var truncated *SupplyChainImpactSupportsDrainTruncatedError
	if !errors.As(err, &truncated) {
		t.Fatalf("ListActiveSecurityAlertReconciliationFacts() error = %v, want *SupplyChainImpactSupportsDrainTruncatedError (the supports drain must stop on the call remainder)", err)
	}
	if got != nil {
		t.Fatalf("ListActiveSecurityAlertReconciliationFacts() = %d envelopes, want nil on truncation", len(got))
	}
	// One fact row spends 1 of the budget; the supports page is cut to the
	// first 100 of its 500 active rows.
	if want := "fact-0099"; truncated.ResumeCursorFactID != want {
		t.Fatalf("resume cursor = %q, want %q (last delivered supports id of the cut page)", truncated.ResumeCursorFactID, want)
	}
	if got := truncated.Stats.activeRows; got != 101 {
		t.Fatalf("truncated active rows = %d, want 101 (1 fact row plus 100 supports rows)", got)
	}
}

// TestSupplyChainImpactSupportsCallBudgetFromEnv pins the tuning contract:
// the env override wins when positive, and the per-drain default is the floor
// so an operator cannot set a call budget a single fully-capped drain would
// trip on its own.
func TestSupplyChainImpactSupportsCallBudgetFromEnv(t *testing.T) {
	t.Setenv(supplyChainImpactSupportsCallBudgetEnv, "250000")
	if got := supplyChainImpactSupportsCallBudget(); got != 250000 {
		t.Fatalf("budget with env 250000 = %d, want 250000", got)
	}
	t.Setenv(supplyChainImpactSupportsCallBudgetEnv, "10")
	if got, want := supplyChainImpactSupportsCallBudget(), supplyChainImpactSupportsDrainDefaultMaxActiveRows; got != want {
		t.Fatalf("budget with env 10 = %d, want the per-drain default floor %d", got, want)
	}
	t.Setenv(supplyChainImpactSupportsCallBudgetEnv, "not-a-number")
	if got, want := supplyChainImpactSupportsCallBudget(), supplyChainImpactSupportsCallDefaultMaxActiveRows; got != want {
		t.Fatalf("budget with malformed env = %d, want the default %d", got, want)
	}
}
