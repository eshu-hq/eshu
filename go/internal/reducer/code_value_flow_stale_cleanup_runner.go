// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
	log "github.com/eshu-hq/eshu/go/pkg/log"
)

const (
	defaultCodeValueFlowStaleCleanupPollInterval     = time.Hour
	defaultCodeValueFlowStaleCleanupLeaseTTL         = 5 * time.Minute
	defaultCodeValueFlowStaleCleanupScopeBatchLimit  = 100
	defaultCodeValueFlowStaleCleanupDeleteBatchLimit = 500
)

const (
	codeValueFlowStaleCleanupLeaseDomain         = "code_value_flow_stale_cleanup"
	codeValueFlowStaleCleanupLeasePartitionID    = 0
	codeValueFlowStaleCleanupLeasePartitionCount = 1
)

// ErrCodeValueFlowCurrentGenerationsRequired reports missing active generation
// lookup wiring for value-flow stale cleanup.
var ErrCodeValueFlowCurrentGenerationsRequired = errors.New("code value-flow current generation reader is required")

// CodeValueFlowCurrentGeneration identifies one current source generation whose
// reducer-owned value-flow evidence may have stale graph rows from older
// generations.
type CodeValueFlowCurrentGeneration struct {
	ScopeID      string
	GenerationID string
}

// CodeValueFlowCurrentGenerationReader lists active repository-scope
// generations for bounded stale value-flow evidence cleanup.
type CodeValueFlowCurrentGenerationReader interface {
	ListCurrentCodeValueFlowGenerations(
		ctx context.Context,
		afterScopeID string,
		limit int,
	) ([]CodeValueFlowCurrentGeneration, error)
}

// CodeTaintStaleEvidenceRetractor removes stale reducer-owned taint evidence
// for one current scope generation.
type CodeTaintStaleEvidenceRetractor interface {
	RetractStaleCodeTaintEvidence(
		ctx context.Context,
		scopeID string,
		generationID string,
		evidenceSource string,
		limit int,
	) error
}

// CodeInterprocStaleEvidenceRetractor removes stale reducer-owned interproc
// value-flow evidence for one current scope generation.
type CodeInterprocStaleEvidenceRetractor interface {
	RetractStaleCodeInterprocEvidence(
		ctx context.Context,
		scopeID string,
		generationID string,
		evidenceSource string,
		limit int,
	) error
}

// CodeValueFlowStaleCleanupRunnerConfig configures bounded value-flow graph
// stale-evidence cleanup.
type CodeValueFlowStaleCleanupRunnerConfig struct {
	PollInterval     time.Duration
	LeaseOwner       string
	LeaseTTL         time.Duration
	ScopeBatchLimit  int
	DeleteBatchLimit int
}

func (c CodeValueFlowStaleCleanupRunnerConfig) pollInterval() time.Duration {
	if c.PollInterval <= 0 {
		return defaultCodeValueFlowStaleCleanupPollInterval
	}
	return c.PollInterval
}

func (c CodeValueFlowStaleCleanupRunnerConfig) leaseTTL() time.Duration {
	if c.LeaseTTL <= 0 {
		return defaultCodeValueFlowStaleCleanupLeaseTTL
	}
	return c.LeaseTTL
}

func (c CodeValueFlowStaleCleanupRunnerConfig) scopeBatchLimit() int {
	if c.ScopeBatchLimit <= 0 {
		return defaultCodeValueFlowStaleCleanupScopeBatchLimit
	}
	return c.ScopeBatchLimit
}

func (c CodeValueFlowStaleCleanupRunnerConfig) deleteBatchLimit() int {
	if c.DeleteBatchLimit <= 0 {
		return defaultCodeValueFlowStaleCleanupDeleteBatchLimit
	}
	return c.DeleteBatchLimit
}

// CodeValueFlowStaleCleanupResult summarizes one bounded stale value-flow graph
// cleanup cycle.
type CodeValueFlowStaleCleanupResult struct {
	LeaseAcquired   bool
	ScopesScanned   int
	ScopesSkipped   int
	TaintSweeps     int
	InterprocSweeps int
	CursorExhausted bool
	Duration        time.Duration
}

// CodeValueFlowStaleCleanupRunner removes reducer-owned value-flow evidence
// from older generations beside the normal reducer intent loop.
type CodeValueFlowStaleCleanupRunner struct {
	CurrentGenerations CodeValueFlowCurrentGenerationReader
	TaintEvidence      CodeTaintStaleEvidenceRetractor
	TaintWriter        CodeTaintEvidenceWriter
	TaintLedger        CodeTaintEvidenceProjectedNodeLedger
	InterprocEvidence  CodeInterprocStaleEvidenceRetractor
	InterprocWriter    CodeInterprocEvidenceWriter
	InterprocLedger    CodeInterprocProjectedEdgeLedger
	LeaseManager       PartitionLeaseManager
	Config             CodeValueFlowStaleCleanupRunnerConfig
	Wait               func(context.Context, time.Duration) error

	Logger *slog.Logger

	cursorScopeID string
}

// Run scans active generation pages until the context is cancelled.
func (r *CodeValueFlowStaleCleanupRunner) Run(ctx context.Context) error {
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
				if codeValueFlowStaleCleanupContextDone(ctx, waitErr) {
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
			if codeValueFlowStaleCleanupContextDone(ctx, waitErr) {
				return nil
			}
			return fmt.Errorf("wait for code value-flow stale cleanup work: %w", waitErr)
		}
	}
}

// RunOnce executes one bounded stale value-flow graph cleanup cycle.
func (r *CodeValueFlowStaleCleanupRunner) RunOnce(ctx context.Context) (CodeValueFlowStaleCleanupResult, error) {
	if err := r.validate(); err != nil {
		return CodeValueFlowStaleCleanupResult{}, err
	}
	if r.LeaseManager != nil {
		claimed, err := r.LeaseManager.ClaimPartitionLease(
			ctx,
			codeValueFlowStaleCleanupLeaseDomain,
			codeValueFlowStaleCleanupLeasePartitionID,
			codeValueFlowStaleCleanupLeasePartitionCount,
			r.Config.LeaseOwner,
			r.Config.leaseTTL(),
		)
		if err != nil {
			return CodeValueFlowStaleCleanupResult{}, fmt.Errorf("claim code value-flow stale cleanup lease: %w", err)
		}
		if !claimed {
			return CodeValueFlowStaleCleanupResult{LeaseAcquired: false}, nil
		}
		defer func() {
			_ = r.LeaseManager.ReleasePartitionLease(
				ctx,
				codeValueFlowStaleCleanupLeaseDomain,
				codeValueFlowStaleCleanupLeasePartitionID,
				codeValueFlowStaleCleanupLeasePartitionCount,
				r.Config.LeaseOwner,
			)
		}()
	}

	start := time.Now()
	result := CodeValueFlowStaleCleanupResult{LeaseAcquired: true}
	candidates, err := r.CurrentGenerations.ListCurrentCodeValueFlowGenerations(
		ctx,
		r.cursorScopeID,
		r.Config.scopeBatchLimit(),
	)
	if err != nil {
		return CodeValueFlowStaleCleanupResult{}, fmt.Errorf("list current code value-flow generations: %w", err)
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
				ctx, codeTaintEvidenceSource, scopeID, generationID, deleteLimit,
			)
			if err != nil {
				return CodeValueFlowStaleCleanupResult{}, fmt.Errorf("list stale taint node uids: %w", err)
			}
			if err := r.TaintWriter.RetractStaleCodeTaintEvidenceByUIDs(
				ctx, uids, scopeID, generationID, codeTaintEvidenceSource,
			); err != nil {
				return CodeValueFlowStaleCleanupResult{}, fmt.Errorf("retract stale code taint evidence by uids: %w", err)
			}
			if len(uids) > 0 {
				if err := r.TaintLedger.PruneStaleForUIDs(
					ctx, codeTaintEvidenceSource, scopeID, generationID, uids,
				); err != nil {
					return CodeValueFlowStaleCleanupResult{}, fmt.Errorf("prune stale taint projected nodes for uids: %w", err)
				}
			}
		} else {
			if err := r.TaintEvidence.RetractStaleCodeTaintEvidence(
				ctx,
				scopeID,
				generationID,
				codeTaintEvidenceSource,
				deleteLimit,
			); err != nil {
				return CodeValueFlowStaleCleanupResult{}, fmt.Errorf("retract stale code taint evidence: %w", err)
			}
		}
		result.TaintSweeps++
		if r.InterprocLedger != nil && r.InterprocWriter != nil {
			uids, err := r.InterprocLedger.ListStaleSourceUIDs(
				ctx, codeInterprocEvidenceSource, scopeID, generationID, deleteLimit,
			)
			if err != nil {
				return CodeValueFlowStaleCleanupResult{}, fmt.Errorf("list stale interproc source uids: %w", err)
			}
			if err := r.InterprocWriter.RetractStaleCodeInterprocEvidenceByUIDs(
				ctx, uids, scopeID, generationID, codeInterprocEvidenceSource,
			); err != nil {
				return CodeValueFlowStaleCleanupResult{}, fmt.Errorf("retract stale code interproc evidence by uids: %w", err)
			}
			if len(uids) > 0 {
				if err := r.InterprocLedger.PruneStaleForUIDs(
					ctx, codeInterprocEvidenceSource, scopeID, generationID, uids,
				); err != nil {
					return CodeValueFlowStaleCleanupResult{}, fmt.Errorf("prune stale interproc projected edges for uids: %w", err)
				}
			}
		} else {
			if err := r.InterprocEvidence.RetractStaleCodeInterprocEvidence(
				ctx,
				scopeID,
				generationID,
				codeInterprocEvidenceSource,
				deleteLimit,
			); err != nil {
				return CodeValueFlowStaleCleanupResult{}, fmt.Errorf("retract stale code interproc evidence: %w", err)
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

func (r *CodeValueFlowStaleCleanupRunner) validate() error {
	if r.CurrentGenerations == nil {
		return ErrCodeValueFlowCurrentGenerationsRequired
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

func (r *CodeValueFlowStaleCleanupRunner) wait(ctx context.Context, d time.Duration) error {
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

func (r *CodeValueFlowStaleCleanupRunner) recordResult(ctx context.Context, result CodeValueFlowStaleCleanupResult) {
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

func (r *CodeValueFlowStaleCleanupRunner) recordFailure(ctx context.Context, err error) {
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

func codeValueFlowStaleCleanupContextDone(ctx context.Context, err error) bool {
	return errors.Is(err, context.Canceled) ||
		errors.Is(err, context.DeadlineExceeded) ||
		ctx.Err() != nil
}
