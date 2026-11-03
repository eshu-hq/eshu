// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"testing"
	"time"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// TestClaimFenceSpanEndsAtCommit (#9376 review P3-1): the
// ingestion.commit.claim_fence span measures the fenced transaction, so it
// ends when COMMIT returns, not after the post-commit catalog merge and
// relationship backfill. The context handed to post-commit work no longer
// carries the fence span, and finish does not end it a second time.
func TestClaimFenceSpanEndsAtCommit(t *testing.T) {
	t.Parallel()
	recorder := tracetest.NewSpanRecorder()
	tracer := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder)).Tracer("claim-fence-span")
	now := time.Date(2026, time.November, 2, 12, 0, 0, 0, time.UTC)
	store := NewIngestionStore(&fakeTransactionalDB{tx: &fakeTx{}})
	store.Now = func() time.Time { return now }
	store.ClaimPlane = &ClaimPlaneFence{
		Pinner: &claimFenceFakePinner{}, Code: claimFenceTestCode, Tracer: tracer,
		Ticks: func(time.Duration) (<-chan time.Time, func()) { return make(chan time.Time), func() {} },
	}
	commitCtx, session, err := store.beginClaimFence(context.Background(),
		claimFenceTestMutation("scope-sp", "gen-sp", "claim-sp", 1), claimFenceTestScope("scope-sp"), "gen-sp")
	if err != nil {
		t.Fatalf("beginClaimFence() error = %v", err)
	}
	fenceSpan := trace.SpanFromContext(commitCtx).SpanContext()

	postCommit := session.afterCommit()
	ended := recorder.Ended()
	if len(ended) != 1 || ended[0].Name() != telemetry.SpanIngestionCommitClaimFence {
		t.Fatalf("ended spans after COMMIT = %d, want the fence span ended at COMMIT", len(ended))
	}
	var outcome string
	for _, attr := range ended[0].Attributes() {
		if attr.Key == "outcome" {
			outcome = attr.Value.AsString()
		}
	}
	if outcome != "committed" {
		t.Fatalf("fence span outcome = %q, want committed", outcome)
	}
	if trace.SpanFromContext(postCommit).SpanContext().Equal(fenceSpan) {
		t.Fatal("post-commit context still carries the ended fence span")
	}
	if err := session.finish(commitCtx, "gen-sp", nil); err != nil {
		t.Fatalf("finish() error = %v", err)
	}
	if got := len(recorder.Ended()); got != 1 {
		t.Fatalf("ended spans after finish = %d, want 1 (no second End)", got)
	}
}
