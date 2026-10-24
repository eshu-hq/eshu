// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package chain

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// TestSecurityAlertAnswerFollowsTheFanoutStartThroughTwoStoppers writes down the
// product truth of the forward-only fan-out start (#8946). With two repositories
// that stop the fan-out on every claim, the start moves past the first stopper
// and then past the second before it settles. A generation is a snapshot, so
// repository X, which generation N read, is target_incomplete with
// org_repository_fanout_stopped at N+1 when the walk stopped before it, and
// complete again at N+2 once the start settled on a window that reads it. The
// answers are honest at every generation, and the start settles so the
// committer then skips. The empty branch is covered on list, count, and
// inventory. The non-empty branch is covered on list and count; a non-empty
// inventory page carries no coverage object by design, so its rows are its own
// evidence.
func TestSecurityAlertAnswerFollowsTheFanoutStartThroughTwoStoppers(t *testing.T) {
	t.Parallel()

	readInFull := SecurityAlertTargetState{LatestWorkItemStatus: "completed", CommittedGeneration: true}
	stopped := readInFull
	stopped.UnreadTotal = 1
	stopped.UnreadReasons = []string{"org_repository_fanout_stopped"}
	stopped.UnreadRequested = map[string]string{"example-org/example-repo": "org_repository_fanout_stopped"}

	for route, target := range coverageRoutes(coverageTestRepositoryID) {
		t.Run("empty/"+route, func(t *testing.T) {
			t.Parallel()
			store := &fakeSecurityAlertCollectionState{
				configs: []string{coverageTestCoveringOrgTarget},
				states:  map[string]SecurityAlertTargetState{"sa-target-org-covering": readInFull},
			}
			handler := coverageTestHandler(store, 0)
			answers := []struct {
				generation string
				state      SecurityAlertTargetState
				want       coverageWant
			}{
				{"generation N reads the repository", readInFull, coverageWant{"complete", nil, "exact", "fresh", "", "ready_zero_results"}},
				{"generation N+1 stops before it", stopped, coverageWant{"target_incomplete", []string{"org_repository_fanout_stopped"}, "derived", "fresh", "", ""}},
				{"generation N+2 reads it again", readInFull, coverageWant{"complete", nil, "exact", "fresh", "", "ready_zero_results"}},
			}
			for _, answer := range answers {
				store.mu.Lock()
				store.states = map[string]SecurityAlertTargetState{"sa-target-org-covering": answer.state}
				store.mu.Unlock()
				code, body, resp := serveCoverage(context.Background(), t, handler, target)
				if code != http.StatusOK {
					t.Fatalf("%s: status = %d, want 200; body = %s", answer.generation, code, body)
				}
				assertCoverage(t, resp, answer.want)
				assertCoverageInvariant(t, resp)
			}
		})
	}

	for _, route := range []string{"list", "count"} {
		t.Run("rows/"+route, func(t *testing.T) {
			t.Parallel()
			target := coverageRoutes(coverageTestRepositoryID)[route]
			// Generation N: the repository's own read completed, so its alerts
			// are active rows. Generation N+1: the fan-out stopped before it, so
			// the alerts the org window held are partial rows carrying the
			// reason. Generation N+2: read again.
			complete := windowRowsStore{rows: 80, freshness: "active"}
			stoppedBefore := windowRowsStore{
				rows: 8, freshness: securityAlertSourceFreshnessPartial,
				reasons: []string{"provider_open_alert_page_limit_reached", "org_repository_fanout_stopped"},
			}
			for _, answer := range []struct {
				name       string
				store      windowRowsStore
				state      string
				partial    int
				considered int
			}{
				{"generation N", complete, securityAlertCoverageComplete, 0, 80},
				{"generation N+1", stoppedBefore, securityAlertCoverageTargetIncomplete, 8, 8},
				{"generation N+2", complete, securityAlertCoverageComplete, 0, 80},
			} {
				handler := &Handler{
					Content: selectorMatchContent{entries: []querycontract.RepositoryCatalogEntry{{
						ID: coverageTestRepositoryID, Name: "example-repo", RepoSlug: "example-org/example-repo",
					}}},
					SecurityAlerts:          answer.store,
					SecurityAlertAggregates: answer.store,
				}
				considered := answer.considered
				if route == "list" {
					considered = min(considered, 10) // the route asks for limit=10
				}
				code, body, _ := serveCoverage(context.Background(), t, handler, target)
				if code != http.StatusOK {
					t.Fatalf("%s: status = %d, want 200; body = %s", answer.name, code, body)
				}
				var envelope struct {
					Data struct {
						Coverage securityAlertReconciliationCoverageSummary `json:"coverage"`
					} `json:"data"`
				}
				if err := json.Unmarshal([]byte(body), &envelope); err != nil {
					t.Fatalf("%s: decode: %v", answer.name, err)
				}
				got := envelope.Data.Coverage
				if got.State != answer.state || got.PartialRows != answer.partial || got.RowsConsidered != considered {
					t.Errorf("%s coverage = %+v, want %s with %d partial of %d rows", answer.name, got, answer.state, answer.partial, considered)
				}
				// The list rows are their own evidence: the partial rows name the
				// reason the fan-out left the repository unread.
				if named := strings.Contains(body, "org_repository_fanout_stopped"); route == "list" && named != (answer.partial > 0) {
					t.Errorf("%s: list body names org_repository_fanout_stopped = %v, want %v", answer.name, named, answer.partial > 0)
				}
			}
		})
	}
}

// windowRowsStore answers a non-empty reconciliation read with rows of one
// source freshness and the collection reasons the collector stamped on them.
type windowRowsStore struct {
	rows      int
	freshness string
	reasons   []string
}

func (s windowRowsStore) ListSecurityAlertReconciliations(
	context.Context, SecurityAlertReconciliationFilter,
) ([]SecurityAlertReconciliationRow, error) {
	out := make([]SecurityAlertReconciliationRow, 0, s.rows)
	for i := 0; i < s.rows; i++ {
		row := SecurityAlertReconciliationRow{ReconciliationID: fmt.Sprintf("reconciliation-%d", i), SourceFreshness: s.freshness}
		row.ProviderAlert.CollectionIncompleteReasons = s.reasons
		out = append(out, row)
	}
	return out, nil
}

func (s windowRowsStore) CountSecurityAlertReconciliations(
	context.Context, SecurityAlertReconciliationAggregateFilter,
) (SecurityAlertReconciliationAggregateCount, error) {
	return SecurityAlertReconciliationAggregateCount{
		TotalReconciliations:   s.rows,
		ByReconciliationStatus: map[string]int{},
		ByProvider:             map[string]int{},
		ByProviderState:        map[string]int{},
		BySourceFreshness:      map[string]int{s.freshness: s.rows},
	}, nil
}

func (windowRowsStore) SecurityAlertReconciliationInventory(
	context.Context, SecurityAlertReconciliationAggregateFilter, SecurityAlertReconciliationInventoryDimension, int, int,
) ([]SecurityAlertReconciliationInventoryRow, error) {
	return nil, nil
}

func (windowRowsStore) SecurityAlertProviderRepositoryScopes(context.Context, string) ([]string, error) {
	return nil, nil
}
