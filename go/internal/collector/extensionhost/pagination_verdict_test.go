// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package extensionhost

import (
	"context"
	"errors"
	"testing"
	"time"

	sdkcollector "github.com/eshu-hq/eshu-sdk/collector"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// verdictCall builds one continuation call that ends the stream with a
// failure verdict: no facts, one failure status, and the given page envelope
// (nil when the satellite decoded nothing and so has no page to pin).
func verdictCall(
	item workflow.WorkItem,
	state sdkcollector.ResultState,
	failureClass string,
	retryAfter int,
	page *ResultPage,
) CallResult {
	result := baseResult(item, state)
	result.ProtocolVersion = collectorSDKProtocolV1Alpha3
	result.Statuses = []sdkcollector.Status{{
		Class:             sdkcollector.StatusFailure,
		FailureClass:      failureClass,
		RetryAfterSeconds: retryAfter,
	}}
	return CallResult{Result: result, Page: page}
}

// verdictStream is page 0 (complete, facts, more=true, declared total 3)
// followed by the verdict page under test.
func verdictStream(item workflow.WorkItem, verdict CallResult) []CallResult {
	return []CallResult{
		pagedCall(item, []sdkcollector.Fact{testSDKFact(item)}, &ResultPage{Cursor: "cursor-1", More: true, TotalFacts: 3}),
		verdict,
	}
}

// TestCollectPagesContinuationVerdictKeepsItsClass proves a continuation page
// that ends the stream with a retryable or terminal verdict surfaces that
// verdict, not invalid_result, and commits nothing (eshu-hq/eshu#10780).
// Before the fix the assembled result carried page 0's facts, so the SDK
// validator rejected the retryable or terminal state as "must not include
// facts".
func TestCollectPagesContinuationVerdictKeepsItsClass(t *testing.T) {
	t.Parallel()

	item := testWorkItem()
	for _, tc := range []struct {
		name         string
		verdict      CallResult
		wantClass    string
		wantTerminal bool
		wantDelay    time.Duration
	}{
		{
			name:         "retryable with page envelope",
			verdict:      verdictCall(item, sdkcollector.ResultRetryable, "kev_catalogue_changed", 30, &ResultPage{PageIndex: 1, TotalFacts: 3}),
			wantClass:    "kev_catalogue_changed",
			wantDelay:    30 * time.Second,
			wantTerminal: false,
		},
		{
			name:         "retryable without page envelope",
			verdict:      verdictCall(item, sdkcollector.ResultRetryable, "kev_catalogue_changed", 45, nil),
			wantClass:    "kev_catalogue_changed",
			wantDelay:    45 * time.Second,
			wantTerminal: false,
		},
		{
			name:         "terminal with page envelope",
			verdict:      verdictCall(item, sdkcollector.ResultTerminal, "kev_cursor_invalid", 0, &ResultPage{PageIndex: 1, TotalFacts: 3}),
			wantClass:    "kev_cursor_invalid",
			wantTerminal: true,
		},
		{
			name:         "terminal without page envelope",
			verdict:      verdictCall(item, sdkcollector.ResultTerminal, "kev_cursor_invalid", 0, nil),
			wantClass:    "kev_cursor_invalid",
			wantTerminal: true,
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			script := &scriptRunner{calls: verdictStream(item, tc.verdict)}
			source := mustNewPagedSource(t, script, 0)
			collected, ok, err := source.NextClaimed(context.Background(), item)
			if err == nil {
				t.Fatalf("NextClaimed() error = nil, want %q verdict", tc.wantClass)
			}
			assertFailure(t, err, tc.wantClass, tc.wantTerminal)
			if ok {
				t.Fatalf("NextClaimed() ok = true, want false (nothing commits on a verdict)")
			}
			if collected.Facts != nil {
				t.Fatal("NextClaimed() returned a fact channel, want none")
			}
			if tc.wantDelay > 0 {
				var delayed interface{ RetryAfterDelay() time.Duration }
				if !errors.As(err, &delayed) {
					t.Fatalf("error %v does not expose RetryAfterDelay", err)
				}
				if got := delayed.RetryAfterDelay(); got != tc.wantDelay {
					t.Fatalf("RetryAfterDelay() = %s, want %s", got, tc.wantDelay)
				}
			}
			if len(script.requests) != 2 {
				t.Fatalf("runner calls = %d, want 2 (the verdict ends the stream)", len(script.requests))
			}
		})
	}
}

// TestCollectPagesVerdictDropsEarlierPages proves the assembled verdict carries
// only the verdict page's own statuses and no facts: page 0's complete status
// and fact must not ride along on a retryable or terminal result.
func TestCollectPagesVerdictDropsEarlierPages(t *testing.T) {
	t.Parallel()

	item := testWorkItem()
	verdict := verdictCall(item, sdkcollector.ResultRetryable, "kev_catalogue_changed", 30, nil)
	script := &scriptRunner{calls: verdictStream(item, verdict)}
	source := mustNewPagedSource(t, script, 0)
	request, err := source.requestForWorkItem(context.Background(), item)
	if err != nil {
		t.Fatalf("requestForWorkItem() error = %v, want nil", err)
	}
	result, err := source.collectPages(context.Background(), script, request, &claimRelay{})
	if err != nil {
		t.Fatalf("collectPages() error = %v, want nil verdict result", err)
	}
	if result.State != sdkcollector.ResultRetryable {
		t.Fatalf("collectPages() state = %q, want retryable", result.State)
	}
	if len(result.Facts) != 0 {
		t.Fatalf("collectPages() facts = %d, want 0", len(result.Facts))
	}
	if len(result.Statuses) != 1 || result.Statuses[0].FailureClass != "kev_catalogue_changed" {
		t.Fatalf("collectPages() statuses = %+v, want only the verdict page's failure status", result.Statuses)
	}
}

// TestCollectPagesVerdictPageSkipsStreamPins proves a verdict page is not held
// to the cross-page pins that only make sense for fact pages. A collector that
// refused an unreadable cursor never learned page 0's observed_at or declared
// total, and a verdict commits nothing, so neither pin can protect a commit.
// The claim echo and the SDK validator still bind it to the claim.
func TestCollectPagesVerdictPageSkipsStreamPins(t *testing.T) {
	t.Parallel()

	item := testWorkItem()
	verdict := verdictCall(item, sdkcollector.ResultTerminal, "kev_cursor_invalid", 0, &ResultPage{PageIndex: 1, TotalFacts: 9})
	verdict.Result.Generation.ObservedAt = testObservedAt().Add(time.Hour)
	script := &scriptRunner{calls: verdictStream(item, verdict)}
	source := mustNewPagedSource(t, script, 0)
	_, _, err := source.NextClaimed(context.Background(), item)
	assertFailure(t, err, "kev_cursor_invalid", true)
}

// TestCollectPagesRejectsUnframedNonVerdictPages pins what stays rejected: a
// page without an envelope after the stream opened is a verdict page only when
// it is retryable or terminal and carries no facts. Facts on it, or any other
// state, keep failing invalid_result, and verdict pages still answer to the
// sequence, version, and claim checks.
func TestCollectPagesRejectsUnframedNonVerdictPages(t *testing.T) {
	t.Parallel()

	item := testWorkItem()
	other := testSDKFact(item)
	other.StableKey = "scorecard:repo-eshu:other"
	other.SourceRef.FactKey = other.StableKey

	retryableWithFacts := verdictCall(item, sdkcollector.ResultRetryable, "kev_catalogue_changed", 30, nil)
	retryableWithFacts.Result.Facts = []sdkcollector.Fact{other}

	partialNoEnvelope := verdictCall(item, sdkcollector.ResultPartial, "warn", 0, nil)
	partialNoEnvelope.Result.Statuses = []sdkcollector.Status{{Class: sdkcollector.StatusWarning, Partial: true}}

	gapVerdict := verdictCall(item, sdkcollector.ResultTerminal, "kev_cursor_invalid", 0, &ResultPage{PageIndex: 5})
	legacyVerdict := verdictCall(item, sdkcollector.ResultTerminal, "kev_cursor_invalid", 0, &ResultPage{PageIndex: 1})
	legacyVerdict.Result.ProtocolVersion = sdkcollector.ProtocolVersionV1Alpha1

	for _, tc := range []struct {
		name      string
		call      CallResult
		wantClass string
	}{
		{name: "complete with facts and no envelope", call: CallResult{Result: completeResult(item, other)}, wantClass: FailureClassInvalidResult},
		{name: "complete without facts and no envelope", call: CallResult{Result: completeResult(item)}, wantClass: FailureClassInvalidResult},
		{name: "retryable with facts and no envelope", call: retryableWithFacts, wantClass: FailureClassInvalidResult},
		{name: "partial without envelope", call: partialNoEnvelope, wantClass: FailureClassInvalidResult},
		{name: "verdict page index gap", call: gapVerdict, wantClass: FailureClassInvalidResult},
		{name: "verdict page on v1alpha1", call: legacyVerdict, wantClass: FailureClassInvalidResult},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			script := &scriptRunner{calls: verdictStream(item, tc.call)}
			source := mustNewPagedSource(t, script, 0)
			_, _, err := source.NextClaimed(context.Background(), item)
			if err == nil {
				t.Fatalf("NextClaimed() error = nil, want %q", tc.wantClass)
			}
			assertFailure(t, err, tc.wantClass, true)
		})
	}
}

// TestCollectPagesFirstPageVerdictUnchanged proves a verdict on the very first
// call keeps the verdict it always had, with and without a page envelope.
func TestCollectPagesFirstPageVerdictUnchanged(t *testing.T) {
	t.Parallel()

	item := testWorkItem()
	for _, tc := range []struct {
		name string
		page *ResultPage
	}{
		{name: "no envelope"},
		{name: "final envelope", page: &ResultPage{}},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			script := &scriptRunner{calls: []CallResult{
				verdictCall(item, sdkcollector.ResultRetryable, "kev_catalogue_changed", 30, tc.page),
			}}
			source := mustNewPagedSource(t, script, 0)
			_, _, err := source.NextClaimed(context.Background(), item)
			assertFailure(t, err, "kev_catalogue_changed", false)
		})
	}
}
