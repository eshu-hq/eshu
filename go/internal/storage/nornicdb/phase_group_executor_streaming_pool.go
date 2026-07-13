// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package nornicdb

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

type entityPhaseChunkJob struct {
	chunk []sourcecypher.Statement
	label string
}

type entityPhaseStreamingPool struct {
	ctx        context.Context
	poolCtx    context.Context
	cancel     context.CancelFunc
	executor   PhaseGroupExecutor
	ge         sourcecypher.GroupExecutor
	phase      string
	labelStats map[string]*entityPhaseLabelStats
	jobs       chan entityPhaseChunkJob
	errCh      chan error
	observerMu sync.Mutex
	inFlight   sync.WaitGroup
	workerWG   sync.WaitGroup
	chunkSeq   atomic.Int64
}

func newEntityPhaseStreamingPool(
	ctx context.Context,
	ge sourcecypher.GroupExecutor,
	executor PhaseGroupExecutor,
	phase string,
	labelStats map[string]*entityPhaseLabelStats,
) *entityPhaseStreamingPool {
	// poolCtx bounds admission only. In-flight writes keep the caller context
	// so one failed sibling cannot cancel another sibling's canonical write.
	poolCtx, cancel := context.WithCancel(ctx)
	pool := &entityPhaseStreamingPool{
		ctx:        ctx,
		poolCtx:    poolCtx,
		cancel:     cancel,
		executor:   executor,
		ge:         ge,
		phase:      phase,
		labelStats: labelStats,
		jobs:       make(chan entityPhaseChunkJob, executor.EntityPhaseConcurrency),
		errCh:      make(chan error, 1),
	}
	for range executor.EntityPhaseConcurrency {
		pool.workerWG.Add(1)
		go pool.runWorker()
	}
	return pool
}

func (p *entityPhaseStreamingPool) runWorker() {
	defer p.workerWG.Done()
	for job := range p.jobs {
		p.execute(job)
	}
}

func (p *entityPhaseStreamingPool) execute(job entityPhaseChunkJob) {
	defer p.inFlight.Done()
	if p.poolCtx.Err() != nil {
		return
	}
	idx := p.chunkSeq.Add(1)
	summary := summarizePhaseGroupChunk(job.chunk)
	start := time.Now()
	err := p.ge.ExecuteGroup(p.ctx, sanitizedPhaseGroupChunk(job.chunk))
	duration := time.Since(start)
	if err != nil {
		p.raiseError(fmt.Errorf(
			"phase-group chunk %d (size=%d, duration=%s, first_statement=%q): %w",
			idx, len(job.chunk), duration, summary, err,
		))
		return
	}
	p.observerMu.Lock()
	stats := ensureEntityPhaseLabelStats(p.labelStats, p.phase, job.label, job.chunk[0])
	stats.recordChunk(job.chunk, duration)
	logEntityPhaseLabelSummaryIfDue(stats, false)
	p.observerMu.Unlock()
	slog.Info(
		"nornicdb phase-group chunk completed",
		"chunk_index", idx,
		"statement_count", len(job.chunk),
		"duration_s", duration.Seconds(),
		"first_statement", summary,
		"concurrency", p.executor.EntityPhaseConcurrency,
		"streaming", true,
	)
}

func (p *entityPhaseStreamingPool) raiseError(err error) {
	select {
	case p.errCh <- err:
	default:
	}
	p.cancel()
}

func (p *entityPhaseStreamingPool) push(chunk []sourcecypher.Statement, label string) error {
	p.inFlight.Add(1)
	select {
	case p.jobs <- entityPhaseChunkJob{chunk: chunk, label: label}:
		return nil
	case <-p.poolCtx.Done():
		p.inFlight.Done()
		return fmt.Errorf("dispatch grouped chunks: %w", p.poolCtx.Err())
	}
}

func (p *entityPhaseStreamingPool) drain() error {
	p.inFlight.Wait()
	return p.preferError(nil)
}

func (p *entityPhaseStreamingPool) close() {
	close(p.jobs)
	p.workerWG.Wait()
}

func (p *entityPhaseStreamingPool) preferError(producerErr error) error {
	select {
	case err := <-p.errCh:
		return err
	default:
		return producerErr
	}
}

func (p *entityPhaseStreamingPool) recordSingleton(
	stmt sourcecypher.Statement,
	duration time.Duration,
) {
	p.observerMu.Lock()
	defer p.observerMu.Unlock()
	stats := ensureEntityPhaseLabelStats(p.labelStats, p.phase, entityStatementLabel(stmt), stmt)
	stats.recordSingleton(stmt, duration)
	logEntityPhaseLabelSummaryIfDue(stats, false)
}

func (p *entityPhaseStreamingPool) completeLabel(label string) {
	p.observerMu.Lock()
	stats := p.labelStats[label]
	p.observerMu.Unlock()
	if stats != nil {
		logEntityPhaseLabelSummaryIfDue(stats, true)
	}
}
