package collector

import (
	"errors"
	"time"
)

type retryAfterFailure interface {
	RetryAfterDelay() time.Duration
}

func (s ClaimedService) retryableVisibleAt(err error) time.Time {
	now := s.now()
	delay := s.PollInterval
	var retryAfter retryAfterFailure
	if errors.As(err, &retryAfter) {
		if providerDelay := retryAfter.RetryAfterDelay(); providerDelay > delay {
			delay = providerDelay
		}
	}
	return now.Add(delay)
}
