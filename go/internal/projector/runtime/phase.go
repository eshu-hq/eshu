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
)

func (r Runtime) publishCanonicalGraphPhases(ctx context.Context, generationID string, inputFacts []facts.Envelope) error {
	if r.PhasePublisher == nil {
		return nil
	}

	rows := canonicalGraphPhaseStates(generationID, inputFacts)
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

func canonicalGraphPhaseStates(generationID string, inputFacts []facts.Envelope) []reducer.GraphProjectionPhaseState {
	seen := make(map[string]struct{})
	rows := make([]reducer.GraphProjectionPhaseState, 0)

	for _, fact := range inputFacts {
		switch decode.NormalizeFactKind(fact.FactKind) {
		case "repository":
			rows = appendCanonicalRepositoryGraphPhase(rows, seen, generationID, fact)
		}
		switch fact.FactKind {
		case cloud.TerraformStateSnapshotFactKind, cloud.TerraformStateWarningFactKind:
			rows = appendTerraformStateGraphPhase(
				rows,
				seen,
				generationID,
				fact,
				reducer.GraphProjectionKeyspaceTerraformResourceUID,
			)
			rows = appendTerraformStateGraphPhase(
				rows,
				seen,
				generationID,
				fact,
				reducer.GraphProjectionKeyspaceTerraformModuleUID,
			)
		case cloud.TerraformStateResourceFactKind:
			rows = appendTerraformStateGraphPhase(
				rows,
				seen,
				generationID,
				fact,
				reducer.GraphProjectionKeyspaceTerraformResourceUID,
			)
		case cloud.TerraformStateModuleFactKind:
			rows = appendTerraformStateGraphPhase(
				rows,
				seen,
				generationID,
				fact,
				reducer.GraphProjectionKeyspaceTerraformModuleUID,
			)
		}
	}

	return rows
}

func appendCanonicalRepositoryGraphPhase(
	rows []reducer.GraphProjectionPhaseState,
	seen map[string]struct{},
	generationID string,
	fact facts.Envelope,
) []reducer.GraphProjectionPhaseState {
	repository, err := decode.CodegraphRepository(fact)
	if err != nil {
		// Already quarantined and logged upstream during BuildMaterialization;
		// debug only, so the phase path does not double-report the same fact.
		slog.Debug("projector canonical phase row dropped: repository fact undecodable",
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
		slog.Warn("projector canonical phase row dropped: repository fact missing identity",
			slog.String("fact_id", fact.FactID),
			slog.String("scope_id", fact.ScopeID),
			slog.String("generation_id", generationID),
			slog.String("repo_id", repoID),
			slog.String("source_run_id", sourceRunID),
		)
		return rows
	}

	return appendCanonicalGraphPhase(rows, seen, reducer.GraphProjectionPhaseKey{
		ScopeID:          fact.ScopeID,
		AcceptanceUnitID: repoID,
		SourceRunID:      sourceRunID,
		GenerationID:     generationID,
		Keyspace:         reducer.GraphProjectionKeyspaceCodeEntitiesUID,
	}, fact.ObservedAt)
}

func appendTerraformStateGraphPhase(
	rows []reducer.GraphProjectionPhaseState,
	seen map[string]struct{},
	generationID string,
	fact facts.Envelope,
	keyspace reducer.GraphProjectionKeyspace,
) []reducer.GraphProjectionPhaseState {
	if strings.TrimSpace(fact.ScopeID) == "" || strings.TrimSpace(generationID) == "" {
		return rows
	}
	return appendCanonicalGraphPhase(rows, seen, reducer.GraphProjectionPhaseKey{
		ScopeID:          fact.ScopeID,
		AcceptanceUnitID: fact.ScopeID,
		SourceRunID:      generationID,
		GenerationID:     generationID,
		Keyspace:         keyspace,
	}, fact.ObservedAt)
}

func appendCanonicalGraphPhase(
	rows []reducer.GraphProjectionPhaseState,
	seen map[string]struct{},
	key reducer.GraphProjectionPhaseKey,
	observedAt time.Time,
) []reducer.GraphProjectionPhaseState {
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
	rows = append(rows, reducer.GraphProjectionPhaseState{
		Key:         key,
		Phase:       reducer.GraphProjectionPhaseCanonicalNodesCommitted,
		CommittedAt: observedAt,
		UpdatedAt:   observedAt,
	})
	return rows
}
