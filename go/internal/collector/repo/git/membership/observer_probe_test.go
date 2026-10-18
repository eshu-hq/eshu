// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package membership

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/scope/selection"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// qaProbeStub answers the QA fixture's unlisted scopes the way the QA
// corpus behind #7625 answered: the 24 transferred scopes moved to the
// archive org (one 404s, one probe fails), and the renamed scope answers
// under its new name. Every listed scope is never consulted.
func qaProbeStub(calls *int) ProbeFunc {
	return func(_ context.Context, target ProbeTarget) (ProbeResult, error) {
		*calls++
		switch target.Slug {
		case "acme/renamed-service":
			return ProbeResult{Found: true, FullName: "acme/renamed-service-v2"}, nil
		case "acme/transferred-23":
			return ProbeResult{NotFound: true}, nil
		case "acme/transferred-22":
			return ProbeResult{}, errors.New("upstream 500")
		default:
			name := strings.TrimPrefix(target.Slug, "acme/")
			return ProbeResult{Found: true, FullName: "archive/" + name}, nil
		}
	}
}

// observeQATwoCycles runs the QA fixture through two evaluations, carrying
// the first projection as the second cycle's prior, with the QA probe stub.
func observeQATwoCycles(t *testing.T, h observerHarness, store *fakeStore, probe ProbeFunc) Result {
	t.Helper()
	known, listing := qaFixture()
	store.known = known
	first := evaluateAt(cycleOne, known, listing, nil)
	store.prior = first.Projected
	secondAt := cycleOne.Add(selection.ConfirmationMinSpan)
	request := qaRequest(listing)
	request.Now = secondAt
	request.Probe = probe
	return h.observer.Observe(context.Background(), request)
}

func TestObserverProbesConfirmedNotListedQA(t *testing.T) {
	t.Parallel()

	store := &fakeStore{}
	h := newObserverHarness(t, store)
	var calls int
	result := observeQATwoCycles(t, h, store, qaProbeStub(&calls))
	if result.Outcome != OutcomeEvaluated {
		t.Fatalf("outcome = %q, want %q", result.Outcome, OutcomeEvaluated)
	}
	if !result.ProbeRan {
		t.Fatal("ProbeRan = false, want the probe to run on an evaluated github_org cycle")
	}
	// 25 Confirmed not_listed scopes hit the cap exactly: 22 transferred, 1
	// renamed, 1 not_found, 1 inconclusive.
	if calls != 25 {
		t.Fatalf("probe calls = %d, want 25 (every confirmed not_listed scope, nothing else)", calls)
	}
	wantOutcomes := map[string]int{
		string(UpstreamTransferred): 22, string(UpstreamRenamed): 1,
		string(UpstreamNotFoundOrInaccessible): 1, telemetry.RepositorySelectionProbeOutcomeKeptPrior: 1,
	}
	if fmt.Sprintf("%v", result.ProbeOutcomes) != fmt.Sprintf("%v", wantOutcomes) {
		t.Fatalf("ProbeOutcomes = %v, want %v", result.ProbeOutcomes, wantOutcomes)
	}
	if len(store.classifications) != 1 {
		t.Fatalf("classification writes = %d, want 1", len(store.classifications))
	}
	write := store.classifications[0]
	if write.selectorID != testSelector.ID {
		t.Fatalf("classification selector = %q, want %q", write.selectorID, testSelector.ID)
	}
	if !write.classifiedAt.Equal(cycleOne.Add(selection.ConfirmationMinSpan)) {
		t.Fatalf("classified_at = %v, want the second evaluation time", write.classifiedAt)
	}
	if len(write.rows) != 24 {
		t.Fatalf("classified rows = %d, want 24 (the inconclusive probe writes nothing)", len(write.rows))
	}
	for i := 1; i < len(write.rows); i++ {
		if write.rows[i-1].ScopeID >= write.rows[i].ScopeID {
			t.Fatal("classified rows are not in scope_id order")
		}
	}
	seen := make(map[string]ClassificationRow, len(write.rows))
	for _, row := range write.rows {
		seen[row.ScopeID] = row
	}
	if row := seen["git-repository-scope:acme/renamed-service"]; row.Classification != UpstreamRenamed || row.SuccessorSlug != "acme/renamed-service-v2" {
		t.Fatalf("renamed-service row = %+v, want renamed with successor acme/renamed-service-v2", row)
	}
	if row := seen["git-repository-scope:acme/transferred-00"]; row.Classification != UpstreamTransferred || row.SuccessorSlug != "archive/transferred-00" {
		t.Fatalf("transferred-00 row = %+v, want transferred with successor archive/transferred-00", row)
	}
	if row := seen["git-repository-scope:acme/transferred-23"]; row.Classification != UpstreamNotFoundOrInaccessible || row.SuccessorSlug != "" {
		t.Fatalf("transferred-23 row = %+v, want not_found_or_inaccessible with no successor", row)
	}
	if _, ok := seen["git-repository-scope:acme/transferred-22"]; ok {
		t.Fatal("the inconclusive probe wrote a row; kept_prior writes nothing")
	}
	// Zero false positives: every classified scope is one the listing
	// dropped (the renamed scope or a transferred one); the 776 listed
	// scopes were never probed.
	for scopeID := range seen {
		if scopeID != "git-repository-scope:acme/renamed-service" && !strings.Contains(scopeID, "transferred-") {
			t.Fatalf("classified %s, which the listing still names", scopeID)
		}
	}
	wantClassifications := map[string]int{
		string(UpstreamTransferred): 22, string(UpstreamRenamed): 1,
		string(UpstreamNotFoundOrInaccessible): 1, telemetry.RepositorySelectionClassificationUnclassified: 1,
	}
	if fmt.Sprintf("%v", result.Classifications) != fmt.Sprintf("%v", wantClassifications) {
		t.Fatalf("Classifications = %v, want %v", result.Classifications, wantClassifications)
	}
	assertProbeCounter(t, h, wantOutcomes)
	assertClassificationGauge(t, h, wantClassifications)
}

func TestObserverNeverProbesExplicitOrGitLab(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name       string
		selector   Selector
		sourceMode string
		scope      string
		slug       string
		listing    Listing
	}{
		{
			name: "explicit", sourceMode: "explicit",
			selector: NewExplicitSelector("explicit", "acme", []Rule{{Kind: "exact", Value: "acme/web"}}, "anonymous"),
			scope:    "git-repository-scope:acme/web", slug: "acme/web",
			// The configured repository is listed and selected: an explicit
			// projection holds no excludable row at all.
			listing: Listing{Complete: true, Repositories: []ListedRepository{
				{ScopeID: "git-repository-scope:acme/web", Slug: "acme/web", State: StateSelected},
			}},
		},
		{
			name: "gitlab_group", sourceMode: "gitlabGroup",
			selector: NewGitLabGroupSelector("gitlabGroup", "gitlab.example.com", "acme/platform", nil, false, "anonymous"),
			scope:    "git-repository-scope:acme/platform-a", slug: "acme/platform/a",
			// The sibling scope is listed and selected while the target
			// stays unlisted: the gitlab_group evaluation confirms a
			// not_listed row the probe must still not touch.
			listing: Listing{Complete: true, Repositories: []ListedRepository{
				{ScopeID: "git-repository-scope:acme/platform-b", Slug: "acme/platform/b", State: StateSelected},
			}},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			store := &fakeStore{selector: tc.selector}
			h := newObserverHarness(t, store)
			since := cycleOne.Add(-selection.ConfirmationMinSpan)
			store.known = []KnownScope{{ScopeID: tc.scope, Slug: tc.slug}}
			store.prior = []Observation{{
				ScopeID: tc.scope, State: StateNotListed, StateSince: since,
				StateCycleCount: 2, EvaluatedAt: since, LivenessWindow: testWindow,
			}}
			var calls int
			request := Request{
				Selector: tc.selector, SourceMode: tc.sourceMode, Now: cycleOne,
				LivenessWindow: testWindow,
				Listing:        tc.listing,
				Probe:          qaProbeStub(&calls),
			}
			result := h.observer.Observe(context.Background(), request)
			if result.Outcome != OutcomeEvaluated {
				t.Fatalf("outcome = %q, want %q", result.Outcome, OutcomeEvaluated)
			}
			if result.ProbeRan || calls != 0 || len(store.classifications) != 0 {
				t.Fatalf("ProbeRan=%v calls=%d writes=%d; %s rows are never probed",
					result.ProbeRan, calls, len(store.classifications), tc.selector.Kind)
			}
		})
	}
}

func TestObserverClassificationWriteFailureKeepsEvaluated(t *testing.T) {
	t.Parallel()

	store := &fakeStore{classifyErr: errors.New("deadline")}
	h := newObserverHarness(t, store)
	var calls int
	result := observeQATwoCycles(t, h, store, qaProbeStub(&calls))
	if result.Outcome != OutcomeEvaluated {
		t.Fatalf("outcome = %q, want %q: a probe write failure never fails ingestion", result.Outcome, OutcomeEvaluated)
	}
	if calls != 25 {
		t.Fatalf("probe calls = %d, want 25: the probes ran, only the write failed", calls)
	}
	warn := h.logLine(t, "git_repository_selection_store_failed")
	if warn["level"] != "WARN" || warn["failure_class"] != FailureClassClassificationUpsert {
		t.Fatalf("store failure log = %v, want WARN failure_class %s", warn, FailureClassClassificationUpsert)
	}
	if result.Classifications != nil {
		t.Fatalf("Classifications = %v, want nil: the gauge never reports what the store does not hold", result.Classifications)
	}
	if len(result.ProbeOutcomes) == 0 {
		t.Fatal("ProbeOutcomes empty: the probes answered, only the write failed")
	}
}

func assertProbeCounter(t *testing.T, h observerHarness, want map[string]int) {
	t.Helper()
	got := map[string]int{}
	for _, scope := range h.collect(t).ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != "eshu_dp_collector_repository_selection_probe_total" {
				continue
			}
			for _, dp := range m.Data.(metricdata.Sum[int64]).DataPoints {
				outcome, _ := dp.Attributes.Value(telemetry.MetricDimensionOutcome)
				got[outcome.AsString()] = int(dp.Value)
			}
		}
	}
	if fmt.Sprintf("%v", got) != fmt.Sprintf("%v", want) {
		t.Fatalf("probe counter = %v, want %v", got, want)
	}
}

func assertClassificationGauge(t *testing.T, h observerHarness, want map[string]int) {
	t.Helper()
	got := map[string]int{}
	for _, scope := range h.collect(t).ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != "eshu_dp_collector_repository_selection_classifications" {
				continue
			}
			for _, dp := range m.Data.(metricdata.Gauge[int64]).DataPoints {
				classification, _ := dp.Attributes.Value(telemetry.MetricDimensionClassification)
				got[classification.AsString()] = int(dp.Value)
			}
		}
	}
	if fmt.Sprintf("%v", got) != fmt.Sprintf("%v", want) {
		t.Fatalf("classification gauge = %v, want %v", got, want)
	}
}
