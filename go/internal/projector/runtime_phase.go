// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package projector

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
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
		switch NormalizeFactKind(fact.FactKind) {
		case "repository":
			rows = appendCanonicalRepositoryGraphPhase(rows, seen, generationID, fact)
		}
		switch fact.FactKind {
		case facts.TerraformStateSnapshotFactKind, facts.TerraformStateWarningFactKind:
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
		case facts.TerraformStateResourceFactKind:
			rows = appendTerraformStateGraphPhase(
				rows,
				seen,
				generationID,
				fact,
				reducer.GraphProjectionKeyspaceTerraformResourceUID,
			)
		case facts.TerraformStateModuleFactKind:
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
	repository, err := decodeCodegraphRepository(fact)
	if err != nil {
		return rows
	}
	repoID := repository.RepoID
	sourceRunID := codegraphDerefString(repository.SourceRunID)
	if strings.TrimSpace(fact.ScopeID) == "" || repoID == "" || sourceRunID == "" || strings.TrimSpace(generationID) == "" {
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
