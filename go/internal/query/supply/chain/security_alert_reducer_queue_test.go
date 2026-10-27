// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package chain

import (
	"context"
	"errors"
	"net/http"
	"testing"
)

var errSecurityAlertQueueProbe = errors.New("reconciliation queue read failed")

// TestSecurityAlertReconciliationQueueCoverage pins the #8839 contract: a
// collection-healthy empty answer still reads as not complete while the
// reducer's security_alert_reconciliation work for the scope is outstanding
// (pending or retrying) or dead-lettered. Outstanding work reports building
// with cause reducer_backlog; dead-lettered work reports unavailable with
// cause dead_lettered_domain. A drained scope reads complete, work queued
// for a scope outside the covering set is ignored, and a
// collection-incomplete answer keeps its collector verdict without paying
// for the queue read.
func TestSecurityAlertReconciliationQueueCoverage(t *testing.T) {
	t.Parallel()

	healthy := SecurityAlertTargetState{LatestWorkItemStatus: "completed", CommittedGeneration: true}
	pending := SecurityAlertTargetState{LatestWorkItemStatus: "pending"}
	const backlogCause = "reducer_backlog"
	const deadCause = "dead_lettered_domain"
	const collectorCause = "missing_collector_completion"
	pendingQueue := map[string]SecurityAlertReconciliationQueueState{
		"sa-target-repo": {Outstanding: 3},
	}
	retryingQueue := map[string]SecurityAlertReconciliationQueueState{
		"sa-target-repo": {Outstanding: 1},
	}
	deadQueue := map[string]SecurityAlertReconciliationQueueState{
		"sa-target-repo": {DeadLetter: 2},
	}
	bothQueue := map[string]SecurityAlertReconciliationQueueState{
		"sa-target-repo": {Outstanding: 3, DeadLetter: 2},
	}
	otherScopeQueue := map[string]SecurityAlertReconciliationQueueState{
		"sa-target-org": {Outstanding: 3, DeadLetter: 2},
	}
	cases := []struct {
		name       string
		anchored   bool
		configs    []string
		states     map[string]SecurityAlertTargetState
		queue      map[string]SecurityAlertReconciliationQueueState
		queueErr   error
		want       coverageWant
		wantQueued bool
	}{
		{
			name:       "pending reconciliation work reads building",
			anchored:   true,
			configs:    []string{coverageTestRepoTarget},
			states:     map[string]SecurityAlertTargetState{"sa-target-repo": healthy},
			queue:      pendingQueue,
			want:       coverageWant{"target_incomplete", []string{"reducer_backlog"}, "derived", "building", backlogCause, ""},
			wantQueued: true,
		},
		{
			name:       "retrying reconciliation work reads building",
			anchored:   true,
			configs:    []string{coverageTestRepoTarget},
			states:     map[string]SecurityAlertTargetState{"sa-target-repo": healthy},
			queue:      retryingQueue,
			want:       coverageWant{"target_incomplete", []string{"reducer_backlog"}, "derived", "building", backlogCause, ""},
			wantQueued: true,
		},
		{
			name:       "dead-lettered reconciliation work reads unavailable",
			anchored:   true,
			configs:    []string{coverageTestRepoTarget},
			states:     map[string]SecurityAlertTargetState{"sa-target-repo": healthy},
			queue:      deadQueue,
			want:       coverageWant{"target_incomplete", []string{"dead_lettered_domain"}, "fallback", "unavailable", deadCause, ""},
			wantQueued: true,
		},
		{
			name:       "dead letter outranks outstanding work",
			anchored:   true,
			configs:    []string{coverageTestRepoTarget},
			states:     map[string]SecurityAlertTargetState{"sa-target-repo": healthy},
			queue:      bothQueue,
			want:       coverageWant{"target_incomplete", []string{"dead_lettered_domain"}, "fallback", "unavailable", deadCause, ""},
			wantQueued: true,
		},
		{
			name:       "drained reconciliation work reads complete",
			anchored:   true,
			configs:    []string{coverageTestRepoTarget},
			states:     map[string]SecurityAlertTargetState{"sa-target-repo": healthy},
			want:       coverageWant{"complete", nil, "exact", "fresh", "", "ready_zero_results"},
			wantQueued: true,
		},
		{
			name:       "queue work outside the covering set is ignored",
			anchored:   true,
			configs:    []string{coverageTestRepoTarget, coverageTestOrgTarget},
			states:     map[string]SecurityAlertTargetState{"sa-target-repo": healthy, "sa-target-org": healthy},
			queue:      otherScopeQueue,
			want:       coverageWant{"complete", nil, "exact", "fresh", "", "ready_zero_results"},
			wantQueued: true,
		},
		{
			name:       "collection-incomplete answer keeps its collector verdict",
			anchored:   true,
			configs:    []string{coverageTestRepoTarget},
			states:     map[string]SecurityAlertTargetState{"sa-target-repo": pending},
			queue:      pendingQueue,
			want:       coverageWant{"target_incomplete", []string{"no_committed_generation"}, "derived", "building", collectorCause, ""},
			wantQueued: false,
		},
		{
			name:       "unanchored healthy answer consults the queue too",
			anchored:   false,
			configs:    []string{coverageTestRepoTarget, coverageTestOrgTarget},
			states:     map[string]SecurityAlertTargetState{"sa-target-repo": healthy, "sa-target-org": healthy},
			queue:      deadQueue,
			want:       coverageWant{"target_incomplete", []string{"dead_lettered_domain"}, "fallback", "unavailable", deadCause, ""},
			wantQueued: true,
		},
		{
			name:       "queue read failure fails closed",
			anchored:   true,
			configs:    []string{coverageTestRepoTarget},
			states:     map[string]SecurityAlertTargetState{"sa-target-repo": healthy},
			queueErr:   errSecurityAlertQueueProbe,
			want:       coverageWant{"target_incomplete", []string{"collector_state_unavailable"}, "fallback", "unavailable", "", "readiness_unavailable"},
			wantQueued: true,
		},
	}
	for _, tc := range cases {
		target := "/api/v0/supply-chain/security-alerts/reconciliations?limit=10&cve_id=CVE-2026-0001"
		route := "list unanchored"
		if tc.anchored {
			target = "/api/v0/supply-chain/security-alerts/reconciliations?limit=10&repository_id=" + coverageTestRepositoryID
			route = "list anchored"
		}
		t.Run(tc.name+"/"+route, func(t *testing.T) {
			t.Parallel()
			store := &fakeSecurityAlertCollectionState{configs: tc.configs, states: tc.states, queueStates: tc.queue, queueErr: tc.queueErr}
			code, body, resp := serveCoverage(context.Background(), t, coverageTestHandler(store, 0), target)
			if code != http.StatusOK {
				t.Fatalf("status = %d, want 200; body = %s", code, body)
			}
			assertCoverage(t, resp, tc.want)
			assertCoverageInvariant(t, resp)
			assertNoTargetIdentity(t, body)
			if got := store.queueReadCalls(); (got > 0) != tc.wantQueued {
				t.Fatalf("queue reads = %d, want queued = %v", got, tc.wantQueued)
			}
		})
	}
}

// TestSecurityAlertReconciliationQueueSignals proves the operator-facing
// signals of the two new verdicts (#8839): each classified answer is
// counted once under its bounded state and reason, and the stage event
// carries the bounded collector state with no repository identity.
func TestSecurityAlertReconciliationQueueSignals(t *testing.T) {
	t.Parallel()

	healthy := SecurityAlertTargetState{LatestWorkItemStatus: "completed", CommittedGeneration: true}
	cases := []struct {
		name          string
		queue         map[string]SecurityAlertReconciliationQueueState
		wantCount     string
		wantCollector string
		wantCause     string
	}{
		{
			name:          "backlog",
			queue:         map[string]SecurityAlertReconciliationQueueState{"sa-target-repo": {Outstanding: 2}},
			wantCount:     "target_incomplete|reducer_backlog",
			wantCollector: "reconciling",
			wantCause:     "reducer_backlog",
		},
		{
			name:          "dead letter",
			queue:         map[string]SecurityAlertReconciliationQueueState{"sa-target-repo": {DeadLetter: 1}},
			wantCount:     "target_incomplete|dead_lettered_domain",
			wantCollector: "reconciliation_stuck",
			wantCause:     "dead_lettered_domain",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			store := &fakeSecurityAlertCollectionState{
				configs:     []string{coverageTestRepoTarget},
				states:      map[string]SecurityAlertTargetState{"sa-target-repo": healthy},
				queueStates: tc.queue,
			}
			signals := &coverageSignals{}
			h := signals.instrument(t, coverageTestHandler(store, 0))
			target := "/api/v0/supply-chain/security-alerts/reconciliations?limit=10&repository_id=" + coverageTestRepositoryID
			code, body, resp := serveCoverage(context.Background(), t, h, target)
			if code != http.StatusOK {
				t.Fatalf("status = %d; body = %s", code, body)
			}
			if got := resp.Truth.Freshness.Cause; got != tc.wantCause {
				t.Fatalf("truth freshness cause = %q, want %q", got, tc.wantCause)
			}
			counts := signals.emptyAnswerCounts(t)
			if got := counts[tc.wantCount]; got != 1 || len(counts) != 1 {
				t.Fatalf("empty answer counter = %v, want one %s", counts, tc.wantCount)
			}
			events := signals.collectionStateEvents(t)
			if len(events) != 1 {
				t.Fatalf("collection-state stage events = %d, want 1", len(events))
			}
			event := events[0]
			if got := event["collector_state"]; got != tc.wantCollector {
				t.Fatalf("collector_state = %v, want %s", got, tc.wantCollector)
			}
			assertNoTargetIdentity(t, body)
		})
	}
}
