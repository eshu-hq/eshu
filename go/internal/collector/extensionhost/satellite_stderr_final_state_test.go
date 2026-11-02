// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package extensionhost

import (
	"context"
	"testing"

	sdkcollector "github.com/eshu-hq/eshu-sdk/collector"
)

// TestRelayFollowsFinalResultStateAcrossPages proves satellite figures reach
// the one ResultRecord only when the FINAL assembled result is neither
// complete nor unchanged. A partial first page whose stderr carried figures,
// followed by a complete page, must not log the figures on the complete claim
// (eshu-hq/eshu#9823).
func TestRelayFollowsFinalResultStateAcrossPages(t *testing.T) {
	t.Parallel()

	figures := &InputLimitFigures{InflatedBytes: 9, BudgetBytes: 8, CompressedBytes: 2}
	tests := []struct {
		name       string
		finalState sdkcollector.ResultState
		wantFigure bool
	}{
		{name: "partial page then complete page", finalState: sdkcollector.ResultComplete},
		{name: "partial page then partial page", finalState: sdkcollector.ResultPartial, wantFigure: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			item := testWorkItem()
			first := testSDKFact(item)
			second := testSDKFact(item)
			second.StableKey = "scorecard:repo-eshu:second"
			second.SourceRef.FactKey = second.StableKey

			page0 := pagedCall(item, []sdkcollector.Fact{first}, &ResultPage{Cursor: "cursor-1", More: true, TotalFacts: 2})
			page0.Result.State = sdkcollector.ResultPartial
			page0.Result.Statuses = []sdkcollector.Status{{
				Class: sdkcollector.StatusWarning, Partial: true, WarningCount: 1, FactCount: 1,
			}}
			page0.InputLimit = figures
			page1 := pagedCall(item, []sdkcollector.Fact{second}, &ResultPage{PageIndex: 1, TotalFacts: 2})
			page1.Result.State = tt.finalState
			if tt.finalState == sdkcollector.ResultPartial {
				page1.Result.Statuses = []sdkcollector.Status{{
					Class: sdkcollector.StatusWarning, Partial: true, WarningCount: 1, FactCount: 1,
				}}
			}

			recorder := &recordingResultRecorder{}
			source := mustNewPagedSource(t, &scriptRunner{calls: []CallResult{page0, page1}}, 0)
			source.statusRecorder = recorder
			source.resultRecorder = recorder
			if _, ok, err := source.NextClaimed(context.Background(), item); err != nil || !ok {
				t.Fatalf("NextClaimed() ok=%t error=%v, want true nil", ok, err)
			}
			if len(recorder.results) != 1 {
				t.Fatalf("results = %d, want 1", len(recorder.results))
			}
			record := recorder.results[0]
			if record.State != tt.finalState {
				t.Fatalf("state = %q, want %q", record.State, tt.finalState)
			}
			if got := record.InputLimit != nil; got != tt.wantFigure {
				t.Fatalf("InputLimit present = %t, want %t (%+v)", got, tt.wantFigure, record.InputLimit)
			}
		})
	}
}

// TestHostVerdictAfterPagesNeverCarriesFigures proves a host verdict raised
// after the pages are collected (here an unbounded status failure_class, which
// validateBoundedStatuses rejects) never carries satellite figures, even
// though collectPages already held them.
func TestHostVerdictAfterPagesNeverCarriesFigures(t *testing.T) {
	t.Parallel()

	item := testWorkItem()
	result := baseResult(item, sdkcollector.ResultTerminal)
	result.Statuses = []sdkcollector.Status{{Class: sdkcollector.StatusFailure, FailureClass: "Bad Class!"}}
	figures := &InputLimitFigures{InflatedBytes: 9, BudgetBytes: 8, CompressedBytes: 2}

	recorder := &recordingResultRecorder{}
	source := mustNewSource(t, figuresPagedRunner{call: CallResult{Result: result, InputLimit: figures}}, recorder)
	_, _, _ = source.NextClaimed(context.Background(), item)
	if len(recorder.results) != 1 {
		t.Fatalf("results = %d, want 1", len(recorder.results))
	}
	record := recorder.results[0]
	if record.Origin != ResultOriginHost {
		t.Fatalf("origin = %q, want host (the bounded-status check must reject the class)", record.Origin)
	}
	if record.InputLimit != nil {
		t.Fatalf("host verdict carried figures %+v, want none", *record.InputLimit)
	}
}
