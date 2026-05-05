package reducer

import (
	"context"
	"time"
)

// batchClaimSize bounds batch claims so workers receive fresh leases only when
// they are ready to start heartbeating work.
func (s Service) batchClaimSize() int {
	if s.BatchClaimSize > 0 {
		return s.BatchClaimSize
	}
	n := s.Workers * 4
	if n > 64 {
		n = 64
	}
	if n < 4 {
		n = 4
	}
	return n
}

// pollInterval returns the idle wait between empty claim attempts.
func (s Service) pollInterval() time.Duration {
	if s.PollInterval <= 0 {
		return defaultPollInterval
	}

	return s.PollInterval
}

// wait sleeps through the configured hook so tests and runtimes share the same
// cancellation behavior.
func (s Service) wait(ctx context.Context, interval time.Duration) error {
	if s.Wait != nil {
		return s.Wait(ctx, interval)
	}

	timer := time.NewTimer(interval)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
