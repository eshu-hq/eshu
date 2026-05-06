package tags

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// Normalizer describes the reducer-owned tag normalization seam.
type Normalizer interface {
	Normalize(context.Context, ObservationBatch) (NormalizationResult, error)
}

// ObservationBatch captures the bounded raw observations handed to the
// normalizer.
type ObservationBatch struct {
	ScopeID      string
	GenerationID string
	Resources    []ObservedResource
}

// ObservedResource captures one canonical cloud resource and its raw observed
// tags prior to normalization.
type ObservedResource struct {
	CanonicalResourceID string
	RawTags             map[string]string
}

// NormalizedResource captures one canonical cloud resource after
// normalization.
type NormalizedResource struct {
	CanonicalResourceID string
	NormalizedTags      map[string]string
}

// NormalizationResult captures the bounded canonical updates produced by the
// normalizer.
type NormalizationResult struct {
	Resources []NormalizedResource
}

// Validate checks that the normalization result is well formed.
func (r NormalizationResult) Validate() error {
	for _, resource := range r.Resources {
		if strings.TrimSpace(resource.CanonicalResourceID) == "" {
			return fmt.Errorf("canonical_resource_id must not be blank")
		}
	}
	return nil
}

// PhaseStates converts the normalization result into exact canonical cloud
// readiness publications.
func (r NormalizationResult) PhaseStates(
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

	seen := make(map[string]struct{}, len(r.Resources))
	states := make([]reducer.GraphProjectionPhaseState, 0, len(r.Resources))
	for _, resource := range r.Resources {
		if _, ok := seen[resource.CanonicalResourceID]; ok {
			continue
		}
		seen[resource.CanonicalResourceID] = struct{}{}
		states = append(states, reducer.GraphProjectionPhaseState{
			Key: reducer.GraphProjectionPhaseKey{
				ScopeID:          scopeID,
				AcceptanceUnitID: resource.CanonicalResourceID,
				SourceRunID:      generationID,
				GenerationID:     generationID,
				Keyspace:         reducer.GraphProjectionKeyspaceCloudResourceUID,
			},
			Phase:       reducer.GraphProjectionPhaseCanonicalNodesCommitted,
			CommittedAt: observedAt,
			UpdatedAt:   observedAt,
		})
	}

	slices.SortFunc(states, func(left, right reducer.GraphProjectionPhaseState) int {
		return strings.Compare(left.Key.AcceptanceUnitID, right.Key.AcceptanceUnitID)
	})
	return states, nil
}

// PublishNormalizationResult converts the normalization result into durable
// readiness rows and publishes them through the shared phase publisher.
func PublishNormalizationResult(
	ctx context.Context,
	publisher reducer.GraphProjectionPhasePublisher,
	scopeID string,
	generationID string,
	result NormalizationResult,
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
		return fmt.Errorf("publish tag normalization result: %w", err)
	}
	return nil
}
