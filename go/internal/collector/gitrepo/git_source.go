// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gitrepo

import (
	"context"
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/collector"
)

// Next returns the next collected repository generation from the current
// stream. When the stream is exhausted, it resets the source so the next call
// triggers a fresh discovery cycle.
func (s *GitSource) Next(ctx context.Context) (collector.CollectedGeneration, bool, error) {
	if !s.started {
		if s.Selector == nil {
			return collector.CollectedGeneration{}, false, fmt.Errorf("git repository selector is required")
		}
		if err := s.startStream(ctx); err != nil {
			return collector.CollectedGeneration{}, false, err
		}
		s.started = true
	}

	select {
	case gen, ok := <-s.stream:
		if !ok {
			// Channel closed: stream exhausted. Reset for next discovery cycle.
			s.started = false
			if s.streamErr != nil {
				err := s.streamErr
				s.streamErr = nil
				return collector.CollectedGeneration{}, false, err
			}
			return collector.CollectedGeneration{}, false, nil
		}
		return gen, true, nil
	case <-ctx.Done():
		return collector.CollectedGeneration{}, false, ctx.Err()
	}
}
