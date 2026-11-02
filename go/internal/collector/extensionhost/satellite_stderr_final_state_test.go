// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package extensionhost

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
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

// wirePage is one satellite page as the runner sees it: the stdout document
// and the stderr buffer of that call.
type wirePage struct {
	call   CallResult
	stderr string
}

// decodingRunner is a PagedRunner that serialises each scripted page to wire
// stdout and sends it through decodeCallWithStderr, the exact parse gate both
// production runners use. Unlike scriptRunner it never hand-sets
// CallResult.InputLimit, so the per-call state gate is exercised for real.
type decodingRunner struct {
	pages []wirePage
	calls int
}

func (r *decodingRunner) RunCollector(ctx context.Context, request Request) (sdkcollector.Result, error) {
	call, err := r.RunPagedCollector(ctx, request)
	return call.Result, err
}

func (r *decodingRunner) RunPagedCollector(context.Context, Request) (CallResult, error) {
	if r.calls >= len(r.pages) {
		return CallResult{}, errors.New("decoding runner pages exhausted")
	}
	page := r.pages[r.calls]
	r.calls++
	stdout, err := json.Marshal(pagedWireResult{Result: page.call.Result, Page: page.call.Page})
	if err != nil {
		return CallResult{}, err
	}
	return decodeCallWithStderr(stdout, []byte(page.stderr))
}

// TestRelayParseGateAcrossPagesUsesRealDecode proves the relay through the
// real per-call parse gate. A partial page parses its figures; an unchanged
// final page never parses, so the final record carries none even though the
// partial page held a figure-bearing stderr line. (A partial page WITH facts
// followed by unchanged is a host invalid_result, since unchanged carries no
// facts, so it never reaches a record with figures.) A continuation page may only
// be complete or partial (checkPageEnvelope), so an unchanged continuation is
// rejected as an invalid result and never reaches a figure-bearing record
// (eshu-hq/eshu#9823).
func TestRelayParseGateAcrossPagesUsesRealDecode(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		firstState sdkcollector.ResultState
		finalState sdkcollector.ResultState
		firstMore  bool
		wantOrigin ResultOrigin
		wantState  sdkcollector.ResultState
		wantFigure bool
	}{
		{
			name: "partial page then partial page", firstState: sdkcollector.ResultPartial, firstMore: true,
			finalState: sdkcollector.ResultPartial, wantOrigin: ResultOriginSDK,
			wantState: sdkcollector.ResultPartial, wantFigure: true,
		},
		{
			name: "partial page then unchanged final page", firstState: sdkcollector.ResultPartial, firstMore: true,
			finalState: sdkcollector.ResultUnchanged, wantOrigin: ResultOriginSDK,
			wantState: sdkcollector.ResultUnchanged,
		},
		{
			name: "single partial page", firstState: sdkcollector.ResultPartial,
			finalState: sdkcollector.ResultPartial, wantOrigin: ResultOriginSDK,
			wantState: sdkcollector.ResultPartial, wantFigure: true,
		},
	}
	stderr := figuresLine + "\n"
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			item := testWorkItem()
			// Zero facts keep an unchanged final page a valid assembled
			// result: unchanged with facts is a host invalid_result.
			page0 := pagedCall(item, nil, &ResultPage{TotalFacts: 0})
			page0.Result.State = tt.firstState
			page0.Result.Statuses = []sdkcollector.Status{{Class: sdkcollector.StatusWarning, Partial: true, WarningCount: 1}}
			pages := []wirePage{{call: page0, stderr: stderr}}
			if tt.firstMore {
				page0.Page = &ResultPage{Cursor: "cursor-1", More: true}
				page1 := pagedCall(item, nil, &ResultPage{PageIndex: 1})
				page1.Result.State = tt.finalState
				page1.Result.Statuses = []sdkcollector.Status{{Class: sdkcollector.StatusComplete}}
				if tt.finalState == sdkcollector.ResultPartial {
					page1.Result.Statuses = []sdkcollector.Status{{Class: sdkcollector.StatusWarning, Partial: true, WarningCount: 1}}
				}
				pages = []wirePage{{call: page0, stderr: stderr}, {call: page1, stderr: strings.TrimSpace(stderr)}}
			}

			recorder := &recordingResultRecorder{}
			source := mustNewPagedSource(t, &decodingRunner{pages: pages}, 0)
			source.statusRecorder = recorder
			source.resultRecorder = recorder
			_, _, _ = source.NextClaimed(context.Background(), item)
			if len(recorder.results) != 1 {
				t.Fatalf("results = %d, want 1", len(recorder.results))
			}
			record := recorder.results[0]
			if record.Origin != tt.wantOrigin || record.State != tt.wantState {
				t.Fatalf("origin/state = %q/%q class=%q, want %q/%q (the case must reach a validated SDK result)",
					record.Origin, record.State, record.FailureClass, tt.wantOrigin, tt.wantState)
			}
			if got := record.InputLimit != nil; got != tt.wantFigure {
				t.Fatalf("InputLimit present = %t, want %t (%+v)", got, tt.wantFigure, record.InputLimit)
			}
		})
	}
}

// TestUnchangedContinuationPageIsRejectedBeforeAnyRelay pins the paged
// protocol: a page that asks for continuation while unchanged is an invalid
// result (a host verdict), so figures printed on it can never reach a record.
func TestUnchangedContinuationPageIsRejectedBeforeAnyRelay(t *testing.T) {
	t.Parallel()

	item := testWorkItem()
	page0 := pagedCall(item, nil, &ResultPage{Cursor: "cursor-1", More: true, TotalFacts: 0})
	page0.Result.State = sdkcollector.ResultUnchanged
	page0.Result.Statuses = []sdkcollector.Status{{Class: sdkcollector.StatusComplete}}

	recorder := &recordingResultRecorder{}
	source := mustNewPagedSource(t, &decodingRunner{pages: []wirePage{{call: page0, stderr: figuresLine + "\n"}}}, 0)
	source.statusRecorder = recorder
	source.resultRecorder = recorder
	_, _, _ = source.NextClaimed(context.Background(), item)
	if len(recorder.results) != 1 {
		t.Fatalf("results = %d, want 1", len(recorder.results))
	}
	record := recorder.results[0]
	if record.Origin != ResultOriginHost || record.FailureClass != FailureClassInvalidResult {
		t.Fatalf("origin/class = %q/%q, want host/%q", record.Origin, record.FailureClass, FailureClassInvalidResult)
	}
	if record.InputLimit != nil {
		t.Fatalf("host verdict carried figures %+v, want none", *record.InputLimit)
	}
}
