// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"context"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// ClaimedGenerationCommitObserver is an optional post-commit hook a
// claim-aware ClaimedSource may implement to durably record source-owned
// progress state (for example ghactionsruntime's cross-cycle run watermark,
// issue #5429) only AFTER ClaimedService has durably committed the claimed
// generation's facts. See processClaimed in claimed_service.go for the call
// sites: it fires exactly once, immediately after commitCollected succeeds,
// before the claim itself is completed. A commit the committer refused returned
// nil too, but stored nothing, so it fires ClaimedGenerationRefusalObserver
// instead (#10037). An unchanged generation has no fact
// commit; ClaimedService fires the hook once that generation's completion
// mutation resolves, before the claim completes, so an unchanged result
// records progress exactly like a committed one (#9420).
//
// This ordering exists because NextClaimed only PRODUCES a generation;
// ClaimedService commits it afterward (commitCollected), and that commit can
// still fail and route the claim to a retry. A source that durably advances
// its own progress marker inside NextClaimed -- before the commit describing
// that marker is known to have succeeded -- can advance the marker even when
// the commit it was meant to describe never lands. On a later retry of the
// SAME work item, the source then compares its freshly fetched window
// against an ALREADY-ADVANCED marker and silently stops re-detecting the
// very gap the marker exists to catch. Issue #5429 was exactly this:
// ghactionsruntime saved its run watermark inside NextClaimed, ahead of the
// fact commit it was supposed to describe, so a commit failure followed by a
// retry made the watermark's own gap detection go silently blind on the
// retry.
//
// Absence is safe: a ClaimedSource that does not implement this interface is
// unaffected -- ClaimedService type-asserts optionally and calls nothing,
// mirroring GenerationDeadLetterReplayCompleter's optionality
// (claimed_service_dead_letter.go). The assertion is on s.Source itself, so a
// wrapping Source hides the hook unless it forwards it, and then nothing reports
// the absence (#10063).
type ClaimedGenerationCommitObserver interface {
	// ObserveClaimedGenerationCommitted is called once per successfully
	// committed (or unchanged-resolved) claim cycle, after the durable
	// commit and before claim completion. item is the SAME workflow.WorkItem NextClaimed received
	// for this cycle, so an implementation can look up whatever per-item
	// state it stashed during NextClaimed (see
	// ghactionsruntime.pendingWatermarks for the reference implementation).
	//
	// An error here MUST be treated as non-fatal by the caller: the facts
	// already committed durably, so there is nothing to roll back -- it
	// means only that the source's own progress marker did not advance this
	// cycle. That is safe by construction: the marker simply stays where it
	// was, so the source's next successful commit (for this or a later
	// generation of the same scope) re-evaluates against the same
	// not-yet-advanced marker and still catches any gap that would
	// otherwise have gone undetected. ClaimedService therefore records the
	// error as a span event and a counter and continues completing the claim; it does
	// NOT fail the claim and does NOT roll back the already-committed
	// facts.
	ObserveClaimedGenerationCommitted(ctx context.Context, item workflow.WorkItem) error
}

// ClaimedGenerationRefusalObserver is the optional hook a claim-aware
// ClaimedSource implements to hear that its generation was refused (#10037).
// A refusal returns a nil commit error, so the claim completes, but no facts
// landed: the source must not advance a progress marker or report a success.
// ClaimedService therefore calls this hook in place of
// ClaimedGenerationCommitObserver, never alongside it, at the same point in
// the cycle. A source that implements only the commit observer hears nothing
// on a refusal, and its marker holds.
type ClaimedGenerationRefusalObserver interface {
	// ObserveClaimedGenerationRefused is called once per refused commit, after
	// the rollback and before claim completion. item is the SAME
	// workflow.WorkItem NextClaimed received. An error is non-fatal for the
	// same reason a commit observer error is: nothing landed to roll back, and
	// the next tick recollects.
	ObserveClaimedGenerationRefused(ctx context.Context, item workflow.WorkItem, refusal CommitRefusal) error
}

// observeClaimedGenerationCommitted invokes the optional
// ClaimedGenerationCommitObserver hook for s.Source, if it implements one.
// Called from settleClaimedCommit after a commit that was not refused, and
// after an unchanged generation's completion mutation resolves (#9420). A
// hook error is recorded as a span event on the current claimed-run span,
// counted on eshu_dp_workflow_claim_commit_observer_failures_total, and
// swallowed -- see ClaimedGenerationCommitObserver's doc comment for why
// that is safe rather than a silent failure: the fact commit already
// happened, and the marker that didn't advance is self-healing on the
// source's next successful commit.
func (s ClaimedService) observeClaimedGenerationCommitted(ctx context.Context, item workflow.WorkItem) {
	observer, ok := s.Source.(ClaimedGenerationCommitObserver)
	if !ok {
		return
	}
	if err := observer.ObserveClaimedGenerationCommitted(ctx, item); err != nil {
		s.recordObserverFailure(ctx, item, "claimed_generation_commit_observer_failed", err)
	}
}

// observeClaimedGenerationRefused invokes the optional
// ClaimedGenerationRefusalObserver hook for s.Source, if it implements one. A
// hook error takes the same non-fatal path as a commit observer error.
func (s ClaimedService) observeClaimedGenerationRefused(ctx context.Context, item workflow.WorkItem, refusal CommitRefusal) {
	observer, ok := s.Source.(ClaimedGenerationRefusalObserver)
	if !ok {
		return
	}
	if err := observer.ObserveClaimedGenerationRefused(ctx, item, refusal); err != nil {
		s.recordObserverFailure(ctx, item, "claimed_generation_refusal_observer_failed", err)
	}
}

// settleClaimedCommit runs what follows a commit that returned nil and returns
// the claim's run outcome. A refused generation stored nothing (#10037), so it
// reaches only the refusal observer, records no fact volume, and reads as
// outcome refused; every other disposition keeps the committed path. The claim
// completes either way, and the next tick recollects.
func (s ClaimedService) settleClaimedCommit(
	ctx context.Context,
	item workflow.WorkItem,
	collected CollectedGeneration,
	disposition *CommitDisposition,
) string {
	if refusal, refused := disposition.Refusal(); refused {
		s.observeClaimedGenerationRefused(ctx, item, refusal)
		return telemetry.CollectorRunOutcomeRefused
	}
	s.observeClaimedGenerationCommitted(ctx, item)
	s.recordClaimFactsEmitted(ctx, item, collected)
	return telemetry.CollectorRunOutcomeSuccess
}

// recordObserverFailure reports a swallowed observer error as a span event and
// on eshu_dp_workflow_claim_commit_observer_failures_total.
func (s ClaimedService) recordObserverFailure(ctx context.Context, item workflow.WorkItem, event string, err error) {
	if s.Instruments != nil && s.Instruments.WorkflowClaimCommitObserverFailures != nil {
		s.Instruments.WorkflowClaimCommitObserverFailures.Add(ctx, 1, metric.WithAttributes(
			telemetry.AttrCollectorKind(string(s.CollectorKind)),
			telemetry.AttrSourceSystem(item.SourceSystem),
		))
	}
	trace.SpanFromContext(ctx).AddEvent(
		event,
		trace.WithAttributes(
			telemetry.AttrCollectorKind(string(s.CollectorKind)),
			telemetry.AttrSourceSystem(item.SourceSystem),
			attribute.String("error", err.Error()),
		),
	)
}
