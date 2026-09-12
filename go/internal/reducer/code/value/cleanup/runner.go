// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cleanup

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer/code/taint"
	"github.com/eshu-hq/eshu/go/internal/reducer/sharedintent"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	log "github.com/eshu-hq/eshu/go/pkg/log"
)

const (
	defaultPollInterval     = time.Hour
	defaultLeaseTTL         = 5 * time.Minute
	defaultScopeBatchLimit  = 100
	defaultDeleteBatchLimit = 500
)

const (
	leaseDomain         = "code_value_flow_stale_cleanup"
	leasePartitionID    = 0
	leasePartitionCount = 1
)

// ErrCurrentGenerationsRequired reports missing active generation lookup
// wiring for value-flow stale cleanup.
var ErrCurrentGenerationsRequired = errors.New("code value-flow current generation reader is required")

// CurrentGeneration identifies one current source generation whose
// reducer-owned value-flow evidence may have stale graph rows from older
// generations.
type CurrentGeneration struct {
	ScopeID      string
	GenerationID string
}

// CurrentGenerationReader lists active repository-scope generations for
// bounded stale value-flow evidence cleanup.
type CurrentGenerationReader interface {
	ListCurrentCodeValueFlowGenerations(
		ctx context.Context,
		afterScopeID string,
		limit int,
	) ([]CurrentGeneration, error)
}

// TaintStaleEvidenceRetractor removes stale reducer-owned taint evidence
// for one current scope generation.
type TaintStaleEvidenceRetractor interface {
	RetractStaleCodeTaintEvidence(
		ctx context.Context,
		scopeID string,
		generationID string,
		evidenceSource string,
		limit int,
	) error
}

// InterprocStaleEvidenceRetractor removes stale reducer-owned interproc
// value-flow evidence for one current scope generation.
type InterprocStaleEvidenceRetractor interface {
	RetractStaleCodeInterprocEvidence(
		ctx context.Context,
		scopeID string,
		generationID string,
		evidenceSource string,
		limit int,
	) error
}

// RunnerConfig configures bounded value-flow graph stale-evidence cleanup.
type RunnerConfig struct {
	PollInterval     time.Duration
	LeaseOwner       string
	LeaseTTL         time.Duration
	ScopeBatchLimit  int
	DeleteBatchLimit int
}

func (c RunnerConfig) pollInterval() time.Duration {
	if c.PollInterval <= 0 {
		return defaultPollInterval
	}
	return c.PollInterval
}

func (c RunnerConfig) leaseTTL() time.Duration {
	if c.LeaseTTL <= 0 {
		return defaultLeaseTTL
	}
	return c.LeaseTTL
}

func (c RunnerConfig) scopeBatchLimit() int {
	if c.ScopeBatchLimit <= 0 {
		return defaultScopeBatchLimit
	}
	return c.ScopeBatchLimit
}

func (c RunnerConfig) deleteBatchLimit() int {
	if c.DeleteBatchLimit <= 0 {
		return defaultDeleteBatchLimit
	}
	return c.DeleteBatchLimit
}

// Result summarizes one bounded stale value-flow graph cleanup cycle.
type Result struct {
	LeaseAcquired   bool
	ScopesScanned   int
	ScopesSkipped   int
	TaintSweeps     int
	InterprocSweeps int
	CursorExhausted bool
	Duration        time.Duration
}

// Runner removes reducer-owned value-flow evidence from older generations
// beside the normal reducer intent loop.
type Runner struct {
	CurrentGenerations CurrentGenerationReader
	TaintEvidence      TaintStaleEvidenceRetractor
	TaintWriter        taint.EvidenceWriter
	TaintLedger        taint.ProjectedNodeLedger
	InterprocEvidence  InterprocStaleEvidenceRetractor
	InterprocWriter    taint.InterprocEvidenceWriter
	InterprocLedger    taint.InterprocProjectedEdgeLedger
	LeaseManager       sharedintent.PartitionLeaseManager
	Config             RunnerConfig
	Wait               func(context.Context, time.Duration) error

	Logger *slog.Logger

	cursorScopeID string
}

// Run scans active generation pages until the context is cancelled.
func (r *Runner) Run(ctx context.Context) error {
	if err := r.validate(); err != nil {
		return err
	}

	for {
		if ctx.Err() != nil {
			return nil
		}
		result, err := r.RunOnce(ctx)
		if err != nil {
			r.recordFailure(ctx, err)
			if waitErr := r.wait(ctx, r.Config.pollInterval()); waitErr != nil {
				if contextDone(ctx, waitErr) {
					return nil
				}
				return fmt.Errorf("wait for code value-flow stale cleanup retry: %w", waitErr)
			}
			continue
		}
		if !result.CursorExhausted {
			continue
		}
		if waitErr := r.wait(ctx, r.Config.pollInterval()); waitErr != nil {
			if contextDone(ctx, waitErr) {
				return nil
			}
			return fmt.Errorf("wait for code value-flow stale cleanup work: %w", waitErr)
		}
	}
}

// RunOnce executes one bounded stale value-flow graph cleanup cycle.
func (r *Runner) RunOnce(ctx context.Context) (Result, error) {
	if err := r.validate(); err != nil {
		return Result{}, err
	}
	if r.LeaseManager != nil {
		claimed, err := r.LeaseManager.ClaimPartitionLease(
			ctx,
			leaseDomain,
			leasePartitionID,
			leasePartitionCount,
			r.Config.LeaseOwner,
			r.Config.leaseTTL(),
		)
		if err != nil {
			return Result{}, fmt.Errorf("claim code value-flow stale cleanup lease: %w", err)
		}
		if !claimed {
			return Result{LeaseAcquired: false}, nil
		}
		defer func() {
			_ = r.LeaseManager.ReleasePartitionLease(
				ctx,
				leaseDomain,
				leasePartitionID,
				leasePartitionCount,
				r.Config.LeaseOwner,
			)
		}()
	}

	start := time.Now()
	result := Result{LeaseAcquired: true}
	candidates, err := r.CurrentGenerations.ListCurrentCodeValueFlowGenerations(
		ctx,
		r.cursorScopeID,
		r.Config.scopeBatchLimit(),
	)
	if err != nil {
		return Result{}, fmt.Errorf("list current code value-flow generations: %w", err)
	}
	if len(candidates) == 0 {
		r.cursorScopeID = ""
		result.CursorExhausted = true
		result.Duration = time.Since(start)
		r.recordResult(ctx, result)
		return result, nil
	}

	deleteLimit := r.Config.deleteBatchLimit()
	for _, candidate := range candidates {
		scopeID := strings.TrimSpace(candidate.ScopeID)
		generationID := strings.TrimSpace(candidate.GenerationID)
		if scopeID == "" || generationID == "" {
			result.ScopesSkipped++
			continue
		}
		if r.TaintLedger != nil && r.TaintWriter != nil {
			uids, err := r.TaintLedger.ListStaleNodeUIDs(
				ctx, taint.EvidenceSource(), scopeID, generationID, deleteLimit,
			)
			if err != nil {
				return Result{}, fmt.Errorf("list stale taint node uids: %w", err)
			}
			if err := r.TaintWriter.RetractStaleCodeTaintEvidenceByUIDs(
				ctx, uids, scopeID, generationID, taint.EvidenceSource(),
			); err != nil {
				return Result{}, fmt.Errorf("retract stale code taint evidence by uids: %w", err)
			}
			if len(uids) > 0 {
				if err := r.TaintLedger.PruneStaleForUIDs(
					ctx, taint.EvidenceSource(), scopeID, generationID, uids,
				); err != nil {
					return Result{}, fmt.Errorf("prune stale taint projected nodes for uids: %w", err)
				}
			}
		} else {
			if err := r.TaintEvidence.RetractStaleCodeTaintEvidence(
				ctx,
				scopeID,
				generationID,
				taint.EvidenceSource(),
				deleteLimit,
			); err != nil {
				return Result{}, fmt.Errorf("retract stale code taint evidence: %w", err)
			}
		}
		result.TaintSweeps++
		if r.InterprocLedger != nil && r.InterprocWriter != nil {
			uids, err := r.InterprocLedger.ListStaleSourceUIDs(
				ctx, taint.InterprocEvidenceSource(), scopeID, generationID, deleteLimit,
			)
			if err != nil {
				return Result{}, fmt.Errorf("list stale interproc source uids: %w", err)
			}
			if err := r.InterprocWriter.RetractStaleCodeInterprocEvidenceByUIDs(
				ctx, uids, scopeID, generationID, taint.InterprocEvidenceSource(),
			); err != nil {
				return Result{}, fmt.Errorf("retract stale code interproc evidence by uids: %w", err)
			}
			if len(uids) > 0 {
				if err := r.InterprocLedger.PruneStaleForUIDs(
					ctx, taint.InterprocEvidenceSource(), scopeID, generationID, uids,
				); err != nil {
					return Result{}, fmt.Errorf("prune stale interproc projected edges for uids: %w", err)
				}
			}
		} else {
			if err := r.InterprocEvidence.RetractStaleCodeInterprocEvidence(
				ctx,
				scopeID,
				generationID,
				taint.InterprocEvidenceSource(),
				deleteLimit,
			); err != nil {
				return Result{}, fmt.Errorf("retract stale code interproc evidence: %w", err)
			}
		}
		result.InterprocSweeps++
		result.ScopesScanned++
	}

	last := candidates[len(candidates)-1]
	if len(candidates) < r.Config.scopeBatchLimit() {
		r.cursorScopeID = ""
		result.CursorExhausted = true
	} else {
		r.cursorScopeID = strings.TrimSpace(last.ScopeID)
	}
	result.Duration = time.Since(start)
	r.recordResult(ctx, result)
	return result, nil
}

func (r *Runner) validate() error {
	if r.CurrentGenerations == nil {
		return ErrCurrentGenerationsRequired
	}
	if r.TaintEvidence == nil && (r.TaintLedger == nil || r.TaintWriter == nil) {
		return errors.New("code value-flow taint stale evidence retractor is required")
	}
	if r.InterprocEvidence == nil && (r.InterprocLedger == nil || r.InterprocWriter == nil) {
		return errors.New("code value-flow interproc stale evidence retractor is required")
	}
	if r.LeaseManager != nil && strings.TrimSpace(r.Config.LeaseOwner) == "" {
		return errors.New("code value-flow stale cleanup lease owner is required")
	}
	return nil
}

func (r *Runner) wait(ctx context.Context, d time.Duration) error {
	if r.Wait != nil {
		return r.Wait(ctx, d)
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (r *Runner) recordResult(ctx context.Context, result Result) {
	if r.Logger == nil {
		return
	}
	r.Logger.InfoContext(
		ctx,
		"code value-flow stale cleanup cycle completed",
		slog.Bool("lease_acquired", result.LeaseAcquired),
		slog.Int("scopes_scanned", result.ScopesScanned),
		slog.Int("scopes_skipped", result.ScopesSkipped),
		slog.Int("taint_sweeps", result.TaintSweeps),
		slog.Int("interproc_sweeps", result.InterprocSweeps),
		slog.Bool("cursor_exhausted", result.CursorExhausted),
		slog.Float64("duration_seconds", result.Duration.Seconds()),
		telemetry.PhaseAttr(telemetry.PhaseReduction),
	)
}

func (r *Runner) recordFailure(ctx context.Context, err error) {
	if r.Logger == nil {
		return
	}
	r.Logger.ErrorContext(
		ctx,
		"code value-flow stale cleanup cycle failed",
		log.Err(err),
		telemetry.FailureClassAttr("code_value_flow_stale_cleanup_error"),
		telemetry.PhaseAttr(telemetry.PhaseReduction),
	)
}

func contextDone(ctx context.Context, err error) bool {
	return errors.Is(err, context.Canceled) ||
		errors.Is(err, context.DeadlineExceeded) ||
		ctx.Err() != nil
}
