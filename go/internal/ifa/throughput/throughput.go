// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package throughput

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/ifa"
	"github.com/eshu-hq/eshu/go/internal/replay/cassette"
	"github.com/eshu-hq/eshu/go/internal/replay/concurrentreplay"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

// Report summarizes one throughput Odù run.
type Report struct {
	// Slot is the scale-lab slot id the amplified corpus was driven at.
	Slot string
	// Workers is the concurrent driver worker count the run used.
	Workers int
	// ScopesCommitted is the number of amplified scope generations committed. It
	// must equal the slot's Scopes.
	ScopesCommitted int
	// FactsCommitted is the total facts drained through the commit boundary. It
	// is invariant to Workers for a given (family, slot, seed).
	FactsCommitted int64
	// Duration is the wall time the driver reported for the run. It is reported
	// informationally; the hermetic assertion is on committed counts, not wall
	// time, so the gate does not flake on a busy machine.
	Duration time.Duration
}

// Run amplifies the base Odù of the given family to slot and drives the
// amplified multi-scope corpus through the P2 concurrent replay driver with the
// requested worker count, returning throughput counts. It is hermetic: the
// amplified cassette is written to a temp file (removed on return) and committed
// into memory; no Postgres, graph backend, or network is touched.
//
// The base Odù is amplified via ifa.AmplifyAtSlot, so it inherits the family-aware
// disjoint-by-construction fan-out (a generic scope_id rewrite is rejected there,
// per the ADR Layer 3 landmine). A non-amplifiable slot (the schema-only smoke
// slot) or an unsupported family returns that seam's error unchanged.
func Run(ctx context.Context, family ifa.OduFamily, slot ifa.ScaleSlot, seed uint64, workers int) (Report, error) {
	raw, err := ifa.AmplifyAtSlot(family, slot, seed)
	if err != nil {
		return Report{}, fmt.Errorf("ifa throughput: amplify slot %q: %w", slot.ID, err)
	}

	dir, err := os.MkdirTemp("", "ifa-throughput-")
	if err != nil {
		return Report{}, fmt.Errorf("ifa throughput: temp dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(dir) }()

	path := filepath.Join(dir, "amplified.cassette.json")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		return Report{}, fmt.Errorf("ifa throughput: write amplified cassette: %w", err)
	}

	src, err := cassette.NewSource(path)
	if err != nil {
		return Report{}, fmt.Errorf("ifa throughput: load amplified cassette: %w", err)
	}

	committer := &countingCommitter{}
	driver := concurrentreplay.Driver{
		Source:    concurrentreplay.NewSource(src),
		Committer: committer,
		Workers:   workers,
	}
	report, err := driver.Run(ctx)
	if err != nil {
		return Report{}, fmt.Errorf("ifa throughput: drive slot %q: %w", slot.ID, err)
	}

	// The driver's GenerationsCommitted and the committer's own drained-scope
	// tally must agree: they count the same commit boundary from two sides. A
	// mismatch means the driver reported a commit the committer never drained (or
	// vice versa) — a real drop/double-count under concurrency — so fail loudly
	// rather than trust one count silently.
	if committed := committer.scopes.Load(); committed != int64(report.GenerationsCommitted) {
		return Report{}, fmt.Errorf(
			"ifa throughput: committed-scope count disagreement for slot %q: driver reported %d generations, committer drained %d scopes",
			slot.ID, report.GenerationsCommitted, committed)
	}

	return Report{
		Slot:            slot.ID,
		Workers:         report.Workers,
		ScopesCommitted: int(committer.scopes.Load()),
		FactsCommitted:  committer.facts.Load(),
		Duration:        report.Duration,
	}, nil
}

// countingCommitter is a hermetic collector.Committer that drains every scope
// generation's fact stream and tallies committed scopes and facts, so the
// throughput Odù measures drain completeness without a durable store.
type countingCommitter struct {
	facts  atomic.Int64
	scopes atomic.Int64
}

// CommitScopeGeneration drains factsCh and counts the committed scope and its
// facts. It returns ctx.Err() if the context is canceled mid-drain so a canceled
// run fails loudly rather than reporting a short count as success.
func (c *countingCommitter) CommitScopeGeneration(
	ctx context.Context,
	_ scope.IngestionScope,
	_ scope.ScopeGeneration,
	factsCh <-chan facts.Envelope,
) error {
	for {
		select {
		case _, ok := <-factsCh:
			if !ok {
				c.scopes.Add(1)
				return nil
			}
			c.facts.Add(1)
		case <-ctx.Done():
			return fmt.Errorf("ifa throughput: commit canceled: %w", ctx.Err())
		}
	}
}
