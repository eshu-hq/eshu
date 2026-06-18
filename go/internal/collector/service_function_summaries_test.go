package collector

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/parser/summary"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

type stubSummaryCommitter struct {
	stubCommitter
	summaryCalls int
	summaries    []ValueFlowSummarySnapshot
}

func (s *stubSummaryCommitter) CommitScopeGenerationWithFunctionSummaries(
	_ context.Context,
	_ scope.IngestionScope,
	_ scope.ScopeGeneration,
	factStream <-chan facts.Envelope,
	summaries []ValueFlowSummarySnapshot,
) error {
	s.summaryCalls++
	s.summaries = append([]ValueFlowSummarySnapshot(nil), summaries...)
	for range factStream {
	}
	return nil
}

func TestServiceCommitWithTelemetryUsesSummaryCommitterForNonEmptySummaries(t *testing.T) {
	t.Parallel()

	scopeValue, generationValue, envelopes := testCollectedGeneration()
	committer := &stubSummaryCommitter{}
	collected := FactsFromSlice(scopeValue, generationValue, envelopes)
	collected.ValueFlowSummaries = []ValueFlowSummarySnapshot{{
		FunctionID: summary.NewFunctionID("repo-alpha", "pkg", "", "handler"),
		Effects:    summary.Effects{ParamToReturn: []int{0}},
		Language:   "go",
	}}

	service := Service{Committer: committer, PollInterval: time.Millisecond}
	if err := service.commitWithTelemetry(context.Background(), collected, time.Now()); err != nil {
		t.Fatalf("commitWithTelemetry() error = %v, want nil", err)
	}
	if got, want := committer.summaryCalls, 1; got != want {
		t.Fatalf("summary commit calls = %d, want %d", got, want)
	}
	if got, want := len(committer.summaries), 1; got != want {
		t.Fatalf("summary count = %d, want %d", got, want)
	}
	if committer.calls != 0 {
		t.Fatalf("plain commit calls = %d, want 0", committer.calls)
	}
}

func TestServiceCommitWithTelemetryUsesSummaryCommitterForObservedEmptySummaries(t *testing.T) {
	t.Parallel()

	scopeValue, generationValue, envelopes := testCollectedGeneration()
	committer := &stubSummaryCommitter{}
	collected := FactsFromSlice(scopeValue, generationValue, envelopes)
	collected.ValueFlowSummariesObserved = true

	service := Service{Committer: committer, PollInterval: time.Millisecond}
	if err := service.commitWithTelemetry(context.Background(), collected, time.Now()); err != nil {
		t.Fatalf("commitWithTelemetry() error = %v, want nil", err)
	}
	if got, want := committer.summaryCalls, 1; got != want {
		t.Fatalf("summary commit calls = %d, want %d", got, want)
	}
	if got := len(committer.summaries); got != 0 {
		t.Fatalf("summary count = %d, want 0", got)
	}
	if committer.calls != 0 {
		t.Fatalf("plain commit calls = %d, want 0", committer.calls)
	}
}
