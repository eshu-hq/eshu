package dsl

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// OutputKind identifies the canonical DSL-owned output family.
type OutputKind string

const (
	// OutputKindResolvedRelationship identifies correlation outputs that
	// materialize resolved relationships.
	OutputKindResolvedRelationship OutputKind = "resolved_relationship"
	// OutputKindDriftObservation identifies drift-oriented outputs such as
	// unmanaged or orphaned observations.
	OutputKindDriftObservation OutputKind = "drift_observation"
)

// Publication captures one exact readiness publication the DSL-owned
// substrate intends to publish after a bounded evaluation pass completes.
type Publication struct {
	AcceptanceUnitID string
	Keyspace         reducer.GraphProjectionKeyspace
	Phase            reducer.GraphProjectionPhase
	OutputKind       OutputKind
}

// EvaluationResult captures the bounded phase-publication outputs produced by
// a DSL evaluation pass.
type EvaluationResult struct {
	Publications []Publication
}

// Evaluator describes the DSL-owned cross-source relationship evaluation seam.
type Evaluator interface {
	Evaluate(context.Context, CanonicalView) (EvaluationResult, error)
}

// DriftEvaluator describes the DSL-owned drift evaluation seam.
type DriftEvaluator interface {
	EvaluateDrift(context.Context, CanonicalView) (EvaluationResult, error)
}

// CanonicalView identifies the bounded canonical slice handed to the DSL layer.
type CanonicalView struct {
	ScopeID       string
	GenerationID  string
	CollectorKind string
}

// Validate checks that the publication carries the exact bounded identity the
// phase store requires.
func (p Publication) Validate() error {
	if strings.TrimSpace(p.AcceptanceUnitID) == "" {
		return fmt.Errorf("acceptance_unit_id must not be blank")
	}
	if strings.TrimSpace(string(p.Keyspace)) == "" {
		return fmt.Errorf("keyspace must not be blank")
	}
	if strings.TrimSpace(string(p.Phase)) == "" {
		return fmt.Errorf("phase must not be blank")
	}
	if strings.TrimSpace(string(p.OutputKind)) == "" {
		return fmt.Errorf("output_kind must not be blank")
	}
	return nil
}

// Validate checks that every publication in the result is well formed.
func (r EvaluationResult) Validate() error {
	for _, publication := range r.Publications {
		if err := publication.Validate(); err != nil {
			return err
		}
	}
	return nil
}

// PhaseStates converts an evaluation result into exact graph-projection phase
// rows, deduping identical publications for stable replay behavior.
func (r EvaluationResult) PhaseStates(
	scopeID string,
	generationID string,
	observedAt time.Time,
) ([]reducer.GraphProjectionPhaseState, error) {
	scopeID = strings.TrimSpace(scopeID)
	if scopeID == "" {
		return nil, fmt.Errorf("scope_id must not be blank")
	}
	generationID = strings.TrimSpace(generationID)
	if generationID == "" {
		return nil, fmt.Errorf("generation_id must not be blank")
	}
	if err := r.Validate(); err != nil {
		return nil, err
	}
	if observedAt.IsZero() {
		observedAt = time.Now().UTC()
	}
	observedAt = observedAt.UTC()

	type publicationKey struct {
		acceptanceUnitID string
		keyspace         reducer.GraphProjectionKeyspace
		phase            reducer.GraphProjectionPhase
	}

	seen := make(map[publicationKey]struct{}, len(r.Publications))
	states := make([]reducer.GraphProjectionPhaseState, 0, len(r.Publications))
	for _, publication := range r.Publications {
		key := publicationKey{
			acceptanceUnitID: publication.AcceptanceUnitID,
			keyspace:         publication.Keyspace,
			phase:            publication.Phase,
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		states = append(states, reducer.GraphProjectionPhaseState{
			Key: reducer.GraphProjectionPhaseKey{
				ScopeID:          scopeID,
				AcceptanceUnitID: publication.AcceptanceUnitID,
				SourceRunID:      generationID,
				GenerationID:     generationID,
				Keyspace:         publication.Keyspace,
			},
			Phase:       publication.Phase,
			CommittedAt: observedAt,
			UpdatedAt:   observedAt,
		})
	}

	slices.SortFunc(states, func(left, right reducer.GraphProjectionPhaseState) int {
		if cmp := strings.Compare(left.Key.AcceptanceUnitID, right.Key.AcceptanceUnitID); cmp != 0 {
			return cmp
		}
		if cmp := strings.Compare(string(left.Key.Keyspace), string(right.Key.Keyspace)); cmp != 0 {
			return cmp
		}
		return strings.Compare(string(left.Phase), string(right.Phase))
	})

	return states, nil
}

// PublishEvaluationResult converts the bounded result into durable phase rows
// and publishes them through the shared graph-projection publisher.
func PublishEvaluationResult(
	ctx context.Context,
	publisher reducer.GraphProjectionPhasePublisher,
	scopeID string,
	generationID string,
	result EvaluationResult,
	observedAt time.Time,
) error {
	if publisher == nil {
		return nil
	}
	states, err := result.PhaseStates(scopeID, generationID, observedAt)
	if err != nil {
		return err
	}
	if len(states) == 0 {
		return nil
	}
	if err := publisher.PublishGraphProjectionPhases(ctx, states); err != nil {
		return fmt.Errorf("publish dsl evaluation result: %w", err)
	}
	return nil
}
