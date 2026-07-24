// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ghactionsruntime

import (
	"context"
	"strconv"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/cicdrun/runwatermark"
)

// benchClient returns the same 10-run page on every FetchRuns call, so the
// benchmarks below isolate the touched NextClaimed path's own overhead
// (watermark Load/detect/Save) rather than provider fetch variance.
type benchClient struct{ page RunPage }

func (c benchClient) FetchRuns(context.Context, TargetConfig) (RunPage, error) {
	return c.page, nil
}

func benchPage() RunPage {
	snapshots := make([]RunSnapshot, 0, 10)
	for i := 0; i < 10; i++ {
		snapshots = append(snapshots, minimalRunSnapshot(strconv.Itoa(1000-i)))
	}
	return RunPage{Snapshots: snapshots}
}

// BenchmarkNextClaimedWithoutWatermarkStore measures the pre-#5429 shape:
// SourceConfig.Watermarks unset, so loadWatermark/detectRunBackfillGap/
// saveWatermark all short-circuit on the nil check with no extra work.
func BenchmarkNextClaimedWithoutWatermarkStore(b *testing.B) {
	source, err := NewClaimedSource(SourceConfig{
		CollectorInstanceID: "ci-cd-primary",
		Client:              benchClient{page: benchPage()},
		Targets:             []TargetConfig{watermarkTestTarget()},
	})
	if err != nil {
		b.Fatalf("NewClaimedSource() error = %v", err)
	}
	claim := watermarkTestClaim("generation-1", 1)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, _, err := source.NextClaimed(context.Background(), claim); err != nil {
			b.Fatalf("NextClaimed() error = %v", err)
		}
	}
}

// BenchmarkNextClaimedWithInMemoryWatermarkStore measures the #5429 shape:
// SourceConfig.Watermarks wired to an in-memory Store, so every call does a
// real Load, detectRunBackfillGap, and Save against it (the same amount of
// store work a Postgres-backed store would do, minus network/disk latency --
// see docs/internal/evidence/5429-cicd-run-watermark.md for the Postgres
// query-plan proof of that added latency, ~0.02-0.03ms per point query at
// 50k representative rows).
func BenchmarkNextClaimedWithInMemoryWatermarkStore(b *testing.B) {
	source, err := NewClaimedSource(SourceConfig{
		CollectorInstanceID: "ci-cd-primary",
		Client:              benchClient{page: benchPage()},
		Watermarks:          runwatermark.NewInMemoryStore(),
		Targets:             []TargetConfig{watermarkTestTarget()},
	})
	if err != nil {
		b.Fatalf("NewClaimedSource() error = %v", err)
	}
	claim := watermarkTestClaim("generation-1", 1)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		claim.CurrentFencingToken = int64(i) + 1
		if _, _, err := source.NextClaimed(context.Background(), claim); err != nil {
			b.Fatalf("NextClaimed() error = %v", err)
		}
	}
}
