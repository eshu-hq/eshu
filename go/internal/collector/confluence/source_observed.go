package confluence

import (
	"context"

	"github.com/eshu-hq/eshu/go/internal/collector"
)

// Next collects one bounded Confluence generation.
func (s *Source) Next(ctx context.Context) (collector.CollectedGeneration, bool, error) {
	collected, ok, _, err := s.NextObserved(ctx, func(ctx context.Context) collector.CollectorObservation {
		return collector.CollectorObservation{Context: ctx}
	})
	return collected, ok, err
}

// NextObserved collects one bounded Confluence generation and starts
// collector.observe only for real collection attempts, not drained idle polls.
func (s *Source) NextObserved(
	ctx context.Context,
	startObserve collector.StartObserveFunc,
) (collector.CollectedGeneration, bool, collector.CollectorObservation, error) {
	if s.drained {
		s.drained = false
		return collector.CollectedGeneration{}, false, collector.CollectorObservation{}, nil
	}
	observation := startObserve(ctx)
	collected, ok, err := s.next(observation.Context)
	return collected, ok, observation, err
}
