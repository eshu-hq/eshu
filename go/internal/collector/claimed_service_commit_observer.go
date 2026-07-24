// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"context"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// ClaimedGenerationCommitObserver is an optional post-commit hook a
// claim-aware ClaimedSource may implement to durably record source-owned
// progress state (for example ghactionsruntime's cross-cycle run watermark,
// issue #5429) only AFTER ClaimedService has durably committed the claimed
// generation's facts. See processClaimed in claimed_service.go for the call
// site: it fires exactly once, immediately after commitCollected succeeds,
// before the claim itself is completed.
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
// (claimed_service_dead_letter.go).
type ClaimedGenerationCommitObserver interface {
	// ObserveClaimedGenerationCommitted is called once per successfully
	// committed claim cycle, after the durable commit and before claim
	// completion. item is the SAME workflow.WorkItem NextClaimed received
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
	// error as a span event and continues completing the claim; it does
	// NOT fail the claim and does NOT roll back the already-committed
	// facts.
	ObserveClaimedGenerationCommitted(ctx context.Context, item workflow.WorkItem) error
}

// observeClaimedGenerationCommitted invokes the optional
// ClaimedGenerationCommitObserver hook for s.Source, if it implements one.
// Called from processClaimed immediately after commitCollected succeeds. A
// hook error is recorded as a span event on the current claimed-run span and
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
		trace.SpanFromContext(ctx).AddEvent(
			"claimed_generation_commit_observer_failed",
			trace.WithAttributes(
				telemetry.AttrCollectorKind(string(s.CollectorKind)),
				telemetry.AttrSourceSystem(item.SourceSystem),
				attribute.String("error", err.Error()),
			),
		)
	}
}
