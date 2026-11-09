// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package audit

import (
	"context"
	"errors"
	"log/slog"
	"time"

	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
	pgaccess "github.com/eshu-hq/eshu/go/internal/runtime/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// StagedAuditor runs the staged stale-winners audit tick (#10451) and
// implements the reducer core's auditor interface, so the maintainer loop,
// lease handling, and deferred/skipped reporting stay exactly as they are for
// the writer-backed and split scans. Only the read phase differs: batches
// stream from one pinned standby member into a per-key winner map instead of
// arriving as one statement's cursor.
//
// Concurrency contract: the tick adds no writer lock, lease, or queue
// interaction beyond what the shared writer-side verification already takes.
// Every standby statement is its own short transaction with a fresh snapshot;
// per-batch snapshots may disagree, and the writer-side verification stays
// authoritative over every candidate, which is what makes the skew safe. The
// audit lease still single-flights ticks, so two winner maps never coexist.
type StagedAuditor struct {
	// Fleet pins the tick's reader member. Required.
	Fleet pgaccess.StagedAuditFleet
	// Store runs the writer-side verification. Required.
	Store postgres.SupplyChainImpactWinnersStore
	// Telemetry is the shared recorder deferrals, ticks, unresolvable keys,
	// and verify discards accumulate into. Nil uses a per-tick private
	// recorder; production wiring always shares one instance across ticks.
	Telemetry *pgaccess.StagedAuditTelemetry
	// BatchTimeout bounds one batch statement. Zero selects the production
	// default.
	BatchTimeout time.Duration
	// Logger receives one line per completed or deferred tick: the per-tick
	// gauges the post-deploy watch needs. Nil logs nothing.
	Logger *slog.Logger
}

var _ supplychaincore.SupplyChainImpactWinnersAuditor = StagedAuditor{}

// AuditWinners runs one staged tick and verifies its candidates. A refused or
// failed read phase defers with no writer contact; an empty candidate set
// returns without touching the writer either.
func (a StagedAuditor) AuditWinners(ctx context.Context) (supplychaincore.SupplyChainImpactWinnersAudit, error) {
	var none supplychaincore.SupplyChainImpactWinnersAudit
	// One instance for the whole tick: the runner's deferral and gauge
	// observations and the verify discard count below must accumulate in
	// the same recorder, never in two per-tick privates.
	telemetry := a.Telemetry
	if telemetry == nil {
		telemetry = pgaccess.NewStagedAuditTelemetry()
	}
	idents, unresolvable, read, result, err := a.runReadPhase(ctx, telemetry)
	if err != nil {
		var deferred *pgaccess.StagedAuditDeferredError
		if errors.As(err, &deferred) {
			if a.Logger != nil {
				a.Logger.InfoContext(ctx, "staged winners audit deferred",
					slog.String("reason", string(deferred.Reason)), slog.Float64("read_seconds", read.Seconds()))
			}
			return supplychaincore.SupplyChainImpactWinnersAudit{Deferred: true, ReadDuration: read}, nil
		}
		return none, err
	}
	if len(idents) == 0 {
		return supplychaincore.SupplyChainImpactWinnersAudit{ReadDuration: read}, nil
	}
	audit, err := a.Store.VerifyStagedCandidates(ctx, idents, read)
	if err != nil {
		return none, err
	}
	// Candidates the writer-side verification drops as clean are discards,
	// not misses: they prove the superset diff erred toward safety.
	discarded := int64(len(idents)) - audit.Repairs
	telemetry.ObserveVerify(discarded)
	if a.Logger != nil {
		a.Logger.InfoContext(ctx, "staged winners audit tick",
			slog.Int("batches", result.Batches), slog.Int("staged_facts", result.StagedFacts),
			slog.Int("distinct_keys", result.DistinctKeys), slog.Int("candidates", len(idents)),
			slog.Int("unresolvable", unresolvable), slog.Int64("repairs", audit.Repairs),
			slog.Int64("discards", discarded),
			slog.String("member", result.MemberID), slog.Float64("read_seconds", read.Seconds()))
	}
	return audit, nil
}

// runReadPhase streams every scope batch on the tick's pinned member into the
// compactor, diffs against the stored winners fetched on the same member,
// and resolves the candidate keys to verification identifiers with one
// point lookup per new key. The writer is never contacted here. The caller
// supplies the tick's telemetry instance so runner observations accumulate
// in it; nil takes a private recorder.
func (a StagedAuditor) runReadPhase(ctx context.Context, telemetry *pgaccess.StagedAuditTelemetry) ([]postgres.WinnersAuditIdent, int, time.Duration, pgaccess.StagedAuditTickResult, error) {
	started := time.Now()
	var empty pgaccess.StagedAuditTickResult
	if telemetry == nil {
		telemetry = pgaccess.NewStagedAuditTelemetry()
	}
	runner := pgaccess.NewStagedAuditRunner(a.Fleet, telemetry, pgaccess.StagedAuditRunnerOptions{BatchTimeout: a.BatchTimeout})
	compactor := postgres.NewStagedCompactor(
		pgaccess.StagedAuditMaxRowsPerBatch,
		pgaccess.StagedAuditMaxStagedFacts,
		pgaccess.StagedAuditMaxDistinctKeys,
	)
	var stored map[string]postgres.StoredWinner
	plan := pgaccess.StagedAuditTickPlan{
		Census: func(stmtCtx context.Context, reader db.Queryer) ([]pgaccess.StagedAuditCensusItem, error) {
			pairs, err := postgres.CensusPairs(stmtCtx, reader)
			if err != nil {
				return nil, err
			}
			items := make([]pgaccess.StagedAuditCensusItem, 0, len(pairs))
			for _, pair := range pairs {
				items = append(items, pgaccess.StagedAuditCensusItem{ScopeID: pair.ScopeID, GenerationID: pair.GenerationID})
			}
			return items, nil
		},
		FetchBatch: func(stmtCtx context.Context, reader db.Queryer, item pgaccess.StagedAuditCensusItem) (pgaccess.StagedAuditBatchResult, error) {
			var none pgaccess.StagedAuditBatchResult
			rows, err := postgres.StageBatch(stmtCtx, reader, item.ScopeID, item.GenerationID)
			if err != nil {
				return none, err
			}
			keysBefore := compactor.DistinctKeys()
			if addErr := compactor.AddBatch(item.ScopeID, rows); addErr != nil {
				// The compactor's budget aborts must surface under the
				// runner's budget reason, not batch-abort.
				var over *postgres.StagedCompactionOverBudgetError
				if errors.As(addErr, &over) {
					return none, &pgaccess.StagedAuditOverBudgetError{What: over.What, Got: over.Got, Limit: over.Limit}
				}
				return none, addErr
			}
			return pgaccess.StagedAuditBatchResult{Facts: len(rows), DistinctKeys: compactor.DistinctKeys() - keysBefore}, nil
		},
		FetchStoredWinners: func(stmtCtx context.Context, reader db.Queryer) (int, error) {
			var err error
			stored, err = postgres.FetchStoredWinners(stmtCtx, reader)
			if err != nil {
				return 0, err
			}
			return len(stored), nil
		},
	}
	result, err := runner.RunTick(ctx, plan)
	if err != nil {
		return nil, 0, time.Since(started), empty, err
	}
	keys := postgres.DiffCandidates(compactor.SourceWinners(), compactor.Overrides(), stored)
	// Ident resolution is still read phase: point lookups for new keys run on
	// the tick member, bounded like a batch, and any failure — including a
	// standby conflict mid-loop — defers under the single abort policy.
	resolveTimeout := a.BatchTimeout
	if resolveTimeout <= 0 {
		resolveTimeout = pgaccess.DefaultStagedAuditBatchTimeout
	}
	resolveCtx, cancel := context.WithTimeout(ctx, resolveTimeout)
	defer cancel()
	var idents []postgres.WinnersAuditIdent
	unresolvable := 0
	resolveErr := a.Fleet.RunOnMember(resolveCtx, result.MemberID, func(stmtCtx context.Context, reader db.Queryer) error {
		var err error
		idents, unresolvable, err = postgres.ResolveCandidateIdents(stmtCtx, reader, keys, compactor.SourceWinners(), compactor.Overrides(), stored)
		return err
	})
	if classified := pgaccess.ClassifyStagedAuditAbort(resolveErr); classified != nil {
		return nil, 0, time.Since(started), empty, classified
	}
	telemetry.ObserveUnresolvable(unresolvable)
	return idents, unresolvable, time.Since(started), result, nil
}
