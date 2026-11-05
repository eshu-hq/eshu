// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"context"
	"testing"
)

// TestCommitDispositionBox pins the box contract the committers and
// ClaimedService share (#10037): no mark reads as committed, each mark reads
// back, and the refusal is visible only on a refused box.
func TestCommitDispositionBox(t *testing.T) {
	t.Parallel()

	refusal := CommitRefusal{Reason: "destructive_successor_refused", Rule: "collector_ratio"}
	tests := []struct {
		name        string
		mark        func(context.Context)
		wantKind    CommitDispositionKind
		wantRefused bool
	}{
		{name: "no mark", mark: func(context.Context) {}, wantKind: CommitDispositionCommitted},
		{
			name:        "refused",
			mark:        func(ctx context.Context) { MarkCommitRefused(ctx, refusal) },
			wantKind:    CommitDispositionRefused,
			wantRefused: true,
		},
		{
			name:     "unchanged skip",
			mark:     MarkCommitUnchangedSkip,
			wantKind: CommitDispositionUnchangedSkip,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx, box := WithCommitDisposition(context.Background())
			tc.mark(ctx)
			if got := box.Kind(); got != tc.wantKind {
				t.Fatalf("Kind() = %q, want %q", got, tc.wantKind)
			}
			got, refused := box.Refusal()
			if refused != tc.wantRefused {
				t.Fatalf("Refusal() refused = %v, want %v", refused, tc.wantRefused)
			}
			if tc.wantRefused && got != refusal {
				t.Fatalf("Refusal() = %+v, want %+v", got, refusal)
			}
			if !tc.wantRefused && got != (CommitRefusal{}) {
				t.Fatalf("Refusal() = %+v, want zero", got)
			}
		})
	}
}

// TestCommitDispositionWithoutABox proves an unclaimed commit path, whose
// context carries no box, marks nothing and does not panic, and that a nil box
// reads as committed.
func TestCommitDispositionWithoutABox(t *testing.T) {
	t.Parallel()

	MarkCommitRefused(context.Background(), CommitRefusal{Reason: "destructive_successor_refused"})
	MarkCommitUnchangedSkip(context.Background())

	var box *CommitDisposition
	if got := box.Kind(); got != CommitDispositionCommitted {
		t.Fatalf("nil Kind() = %q, want committed", got)
	}
	if _, refused := box.Refusal(); refused {
		t.Fatal("nil Refusal() refused = true, want false")
	}
}

// TestCommitDispositionIsPerCommit proves one box never leaks into another
// commit's context: a worker's refusal cannot read as another worker's.
func TestCommitDispositionIsPerCommit(t *testing.T) {
	t.Parallel()

	first, firstBox := WithCommitDisposition(context.Background())
	_, secondBox := WithCommitDisposition(context.Background())
	MarkCommitRefused(first, CommitRefusal{Reason: "destructive_successor_refused"})
	if got := secondBox.Kind(); got != CommitDispositionCommitted {
		t.Fatalf("second box Kind() = %q, want committed", got)
	}
	if got := firstBox.Kind(); got != CommitDispositionRefused {
		t.Fatalf("first box Kind() = %q, want refused", got)
	}
}

// BenchmarkCommitDispositionPerClaim measures the per-claim cost ClaimedService
// adds on the committed path: one box on the context and one read of it.
func BenchmarkCommitDispositionPerClaim(b *testing.B) {
	base := context.Background()
	b.ReportAllocs()
	for b.Loop() {
		ctx, box := WithCommitDisposition(base)
		if _, refused := box.Refusal(); refused || ctx == nil {
			b.Fatal("a fresh box must read as committed")
		}
	}
}

// BenchmarkMarkCommitRefused measures the marking cost, paid only on a refusal.
func BenchmarkMarkCommitRefused(b *testing.B) {
	ctx, _ := WithCommitDisposition(context.Background())
	refusal := CommitRefusal{Reason: "destructive_successor_refused", Rule: "collector_ratio"}
	b.ReportAllocs()
	for b.Loop() {
		MarkCommitRefused(ctx, refusal)
	}
}
