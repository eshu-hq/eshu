// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package runtime

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/projector/decode"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/facts/cloud"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/reducer/gpphase"
)

func (r Runtime) publishCanonicalGraphPhases(ctx context.Context, generationID string, inputFacts []facts.Envelope) error {
	if r.PhasePublisher == nil {
		return nil
	}

	rows := canonicalGraphPhaseStates(ctx, r.Logger, generationID, inputFacts)
	if len(rows) == 0 {
		return nil
	}
	if err := r.PhasePublisher.PublishGraphProjectionPhases(ctx, rows); err != nil {
		if r.RepairQueue != nil {
			repairs := reducer.GraphProjectionPhaseRepairsFromStates(rows, err.Error(), time.Now().UTC())
			if enqueueErr := r.RepairQueue.Enqueue(ctx, repairs); enqueueErr != nil {
				return fmt.Errorf("publish canonical graph phases: %w (enqueue repairs: %v)", err, enqueueErr)
			}
		}
		return err
	}
	return nil
}

func canonicalGraphPhaseStates(ctx context.Context, logger *slog.Logger, generationID string, inputFacts []facts.Envelope) []gpphase.PhaseState {
	seen := make(map[string]struct{})
	rows := make([]gpphase.PhaseState, 0)

	log := logger
	if log == nil {
		log = slog.Default()
	}
	for _, fact := range inputFacts {
		switch decode.NormalizeFactKind(fact.FactKind) {
		case "repository":
			rows = appendCanonicalRepositoryGraphPhase(ctx, log, rows, seen, generationID, fact)
		}
		switch fact.FactKind {
		case cloud.TerraformStateSnapshotFactKind, cloud.TerraformStateWarningFactKind:
			rows = appendTerraformStateGraphPhase(
				rows,
				seen,
				generationID,
				fact,
				gpphase.KeyspaceTerraformResourceUID,
			)
			rows = appendTerraformStateGraphPhase(
				rows,
				seen,
				generationID,
				fact,
				gpphase.KeyspaceTerraformModuleUID,
			)
		case cloud.TerraformStateResourceFactKind:
			rows = appendTerraformStateGraphPhase(
				rows,
				seen,
				generationID,
				fact,
				gpphase.KeyspaceTerraformResourceUID,
			)
		case cloud.TerraformStateModuleFactKind:
			rows = appendTerraformStateGraphPhase(
				rows,
				seen,
				generationID,
				fact,
				gpphase.KeyspaceTerraformModuleUID,
			)
		}
	}

	return rows
}

func appendCanonicalRepositoryGraphPhase(
	ctx context.Context,
	log *slog.Logger,
	rows []gpphase.PhaseState,
	seen map[string]struct{},
	generationID string,
	fact facts.Envelope,
) []gpphase.PhaseState {
	repository, err := decode.CodegraphRepository(fact)
	if err != nil {
		// Already quarantined and logged upstream during BuildMaterialization;
		// debug only, so the phase path does not double-report the same fact.
		log.DebugContext(ctx, "projector canonical phase row dropped: repository fact undecodable",
			slog.String("fact_id", fact.FactID),
			slog.String("scope_id", fact.ScopeID),
			slog.String("generation_id", generationID),
		)
		return rows
	}
	repoID := repository.RepoID
	sourceRunID := decode.CodegraphDerefString(repository.SourceRunID)
	if strings.TrimSpace(fact.ScopeID) == "" || repoID == "" || sourceRunID == "" || strings.TrimSpace(generationID) == "" {
		// No other path dead-letters a decoded-but-identity-less repository
		// fact (issue #7671): without this warn a generation can succeed with
		// file facts but no code_entities_uid phase row and no log line.
		log.WarnContext(ctx, "projector canonical phase row dropped: repository fact missing identity",
			slog.String("fact_id", fact.FactID),
			slog.String("scope_id", fact.ScopeID),
			slog.String("generation_id", generationID),
			slog.String("repo_id", repoID),
			slog.String("source_run_id", sourceRunID),
		)
		return rows
	}

	return appendCanonicalGraphPhase(rows, seen, gpphase.PhaseKey{
		ScopeID:          fact.ScopeID,
		AcceptanceUnitID: repoID,
		SourceRunID:      sourceRunID,
		GenerationID:     generationID,
		Keyspace:         gpphase.KeyspaceCodeEntitiesUID,
	}, fact.ObservedAt)
}

func appendTerraformStateGraphPhase(
	rows []gpphase.PhaseState,
	seen map[string]struct{},
	generationID string,
	fact facts.Envelope,
	keyspace gpphase.Keyspace,
) []gpphase.PhaseState {
	if strings.TrimSpace(fact.ScopeID) == "" || strings.TrimSpace(generationID) == "" {
		return rows
	}
	return appendCanonicalGraphPhase(rows, seen, gpphase.PhaseKey{
		ScopeID:          fact.ScopeID,
		AcceptanceUnitID: fact.ScopeID,
		SourceRunID:      generationID,
		GenerationID:     generationID,
		Keyspace:         keyspace,
	}, fact.ObservedAt)
}

func appendCanonicalGraphPhase(
	rows []gpphase.PhaseState,
	seen map[string]struct{},
	key gpphase.PhaseKey,
	observedAt time.Time,
) []gpphase.PhaseState {
	composite := strings.Join([]string{
		key.ScopeID,
		key.AcceptanceUnitID,
		key.SourceRunID,
		key.GenerationID,
		string(key.Keyspace),
	}, "|")
	if _, ok := seen[composite]; ok {
		return rows
	}
	seen[composite] = struct{}{}
	rows = append(rows, gpphase.PhaseState{
		Key:         key,
		Phase:       gpphase.PhaseCanonicalNodesCommitted,
		CommittedAt: observedAt,
		UpdatedAt:   observedAt,
	})
	return rows
}
