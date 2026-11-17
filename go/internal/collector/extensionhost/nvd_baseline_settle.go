// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package extensionhost

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector"
	vulnerabilitystore "github.com/eshu-hq/eshu/go/internal/storage/postgres/vulnerability"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// maxBaselineSettlements bounds the chunks waiting for their commit hook. A
// worker holds one claim at a time, so the steady state is a handful; an entry
// only lingers when a commit never lands, and a retry of the same item clears
// it on its next policy decision.
const maxBaselineSettlements = 64

// ClaimCommitSettler is the optional second half of a ClaimCommitPolicy: the
// policy decides before the commit transaction runs, so what is only true once
// the commit landed (a chunk was committed, a forward seed rotation took
// effect) is reported afterwards, from the same post-commit hooks the Source
// already implements. A policy that does not implement it reports nothing.
type ClaimCommitSettler interface {
	// SettleClaimCommitted runs once per claim whose generation durably
	// committed.
	SettleClaimCommitted(ctx context.Context, item workflow.WorkItem)
	// DiscardClaimSettlement runs when the generation was refused instead.
	DiscardClaimSettlement(item workflow.WorkItem)
}

var _ ClaimCommitSettler = (*NVDBaselineCommitPolicy)(nil)

// baselineSettlement is what the checkpoint writer saw for one armed chunk.
// It carries no scope id; the INFO line it feeds names the run, year, and
// position, never a scope.
type baselineSettlement struct {
	runID          string
	year           int
	resumeMicro    int64
	resumeCVEID    string
	facts          int
	complete       bool
	rotatedForward bool
	stashedAt      time.Time
}

// baselineSettlements stashes settlements between the checkpoint writer and
// the post-commit hook, keyed by work item id. Workers never contend on one
// key, since a work item has at most one live claim; they share only the map.
type baselineSettlements struct {
	mu   sync.Mutex
	rows map[string]baselineSettlement
}

func newBaselineSettlements() *baselineSettlements {
	return &baselineSettlements{rows: make(map[string]baselineSettlement)}
}

func (b *baselineSettlements) stash(item workflow.WorkItem, settlement baselineSettlement) {
	if b == nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	key := item.WorkItemID
	if _, exists := b.rows[key]; !exists && len(b.rows) >= maxBaselineSettlements {
		oldestKey := ""
		var oldest time.Time
		for candidate, row := range b.rows {
			if oldestKey == "" || row.stashedAt.Before(oldest) {
				oldestKey, oldest = candidate, row.stashedAt
			}
		}
		delete(b.rows, oldestKey)
	}
	b.rows[key] = settlement
}

func (b *baselineSettlements) take(item workflow.WorkItem) (baselineSettlement, bool) {
	if b == nil {
		return baselineSettlement{}, false
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	settlement, ok := b.rows[item.WorkItemID]
	delete(b.rows, item.WorkItemID)
	return settlement, ok
}

func (b *baselineSettlements) discard(item workflow.WorkItem) {
	if b == nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.rows, item.WorkItemID)
}

// checkpointWriter wraps the store's commit-transaction advance with the
// signals that belong to it. A seed rollback is certain the moment the guard
// refuses (the writer's error rolls the whole generation back), so its counter
// and WARN fire here. A forward rotation and the committed-chunk INFO line are
// only true once the transaction commits, so the writer stashes them for
// SettleClaimCommitted.
func (p *NVDBaselineCommitPolicy) checkpointWriter(
	item workflow.WorkItem,
	advance vulnerabilitystore.NVDBaselineAdvance,
	factCount int,
) collector.GenerationStateWriter {
	return func(ctx context.Context, exec collector.CommitExecutor) error {
		result, err := vulnerabilitystore.AdvanceNVDBaselineCheckpoint(ctx, exec, advance)
		if err != nil {
			var rollback *vulnerabilitystore.NVDBaselineSeedRollbackError
			if errors.As(err, &rollback) {
				p.instruments.RecordNVDBaselineSeedRotation(ctx, telemetry.NVDBaselineSeedRotationRollback)
				if p.logger != nil {
					p.logger.Warn("nvd baseline checkpoint refused a seed rollback, the whole generation rolls back",
						telemetry.LogKeyFailureClass, FailureClassNVDBaselineSeedRollback,
						"baseline_run_id", advance.BaselineRunID,
						"year", advance.Year,
						"seed_generated_unix_us", advance.YearGeneratedUnixMicro,
						"collector_instance_id", p.collectorInstanceID,
						"work_item_id", strings.TrimSpace(item.WorkItemID),
						"generation_id", strings.TrimSpace(item.GenerationID),
					)
				}
			}
			return err
		}
		p.settlements.stash(item, baselineSettlement{
			runID:          advance.BaselineRunID,
			year:           advance.Year,
			resumeMicro:    advance.ResumeUnixMicro,
			resumeCVEID:    advance.ResumeCVEID,
			facts:          factCount,
			complete:       advance.YearComplete,
			rotatedForward: result.RotatedForward,
			stashedAt:      p.clock().UTC(),
		})
		return nil
	}
}

// SettleClaimCommitted implements ClaimCommitSettler: the chunk's generation
// and its checkpoint advance committed together, so count a forward seed
// rotation and log the committed chunk. Absent a stashed settlement (a held or
// unchanged generation) it does nothing.
func (p *NVDBaselineCommitPolicy) SettleClaimCommitted(ctx context.Context, item workflow.WorkItem) {
	settlement, ok := p.settlements.take(item)
	if !ok {
		return
	}
	if settlement.rotatedForward {
		p.instruments.RecordNVDBaselineSeedRotation(ctx, telemetry.NVDBaselineSeedRotationForward)
	}
	if p.logger != nil {
		p.logger.Info("nvd baseline chunk committed",
			"baseline_run_id", settlement.runID,
			"year", settlement.year,
			"facts", settlement.facts,
			"resume_unix_us", settlement.resumeMicro,
			"resume_cve_id", settlement.resumeCVEID,
			"year_complete", settlement.complete,
			"seed_rotated_forward", settlement.rotatedForward,
			"collector_instance_id", p.collectorInstanceID,
			"work_item_id", strings.TrimSpace(item.WorkItemID),
			"generation_id", strings.TrimSpace(item.GenerationID),
		)
	}
}

// DiscardClaimSettlement implements ClaimCommitSettler: a refused generation
// stored nothing, so its stashed settlement must not read as a commit.
func (p *NVDBaselineCommitPolicy) DiscardClaimSettlement(item workflow.WorkItem) {
	p.settlements.discard(item)
}
