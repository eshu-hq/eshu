package tfstateruntime

import (
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/collector/terraformstate"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

func (s ClaimedSource) stateTooLargeWarningGeneration(
	candidate terraformstate.DiscoveryCandidate,
	scopeValue scope.IngestionScope,
	candidateID string,
	sourceKey terraformstate.StateKey,
	fencingToken int64,
) (collector.CollectedGeneration, error) {
	observedAt := s.now()
	warningKind := "state_too_large"
	generationValue := scope.ScopeGeneration{
		GenerationID: fmt.Sprintf(
			"terraform_state:%s:warning:%s:%s",
			scopeValue.ScopeID,
			warningKind,
			terraformstate.LocatorHash(sourceKey),
		),
		ScopeID:       scopeValue.ScopeID,
		ObservedAt:    observedAt,
		IngestedAt:    observedAt,
		Status:        scope.GenerationStatusPending,
		TriggerKind:   scope.TriggerKindSnapshot,
		FreshnessHint: fmt.Sprintf("warning=%s candidate=%s", warningKind, candidateID),
	}
	if err := generationValue.ValidateForScope(scopeValue); err != nil {
		return collector.CollectedGeneration{}, err
	}

	warning, err := terraformstate.NewWarningFact(terraformstate.WarningFactOptions{
		Scope:        scopeValue,
		Generation:   generationValue,
		Source:       sourceKey,
		ObservedAt:   observedAt,
		FencingToken: fencingToken,
		Warning: terraformstate.SourceWarning{
			WarningKind: warningKind,
			Reason:      "terraform state exceeded configured size ceiling before snapshot identity could be read",
			Source:      string(candidate.Source),
		},
	})
	if err != nil {
		return collector.CollectedGeneration{}, err
	}
	return collector.FactsFromSlice(scopeValue, generationValue, []facts.Envelope{warning}), nil
}
