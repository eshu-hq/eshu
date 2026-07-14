// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// repoDependencyLeaseQuarantineError keeps an uncertain shard owner from
// re-entering before every canceled or ambiguous graph transaction has had the
// full lease window to quiesce.
type repoDependencyLeaseQuarantineError struct {
	delay time.Duration
	cause error
}

func (e *repoDependencyLeaseQuarantineError) Error() string {
	return fmt.Sprintf("quarantine repo dependency lease for %s: %v", e.delay, e.cause)
}

func (e *repoDependencyLeaseQuarantineError) Unwrap() error {
	return e.cause
}

func repoDependencyQuarantineDelay(err error, fallback time.Duration) time.Duration {
	var quarantineErr *repoDependencyLeaseQuarantineError
	if err != nil && errors.As(err, &quarantineErr) && quarantineErr.delay > fallback {
		return quarantineErr.delay
	}
	return fallback
}

func repoDependencyLeaseQuarantineReason(err error) string {
	var quarantineErr *repoDependencyLeaseQuarantineError
	if !errors.As(err, &quarantineErr) {
		return "not_quarantined"
	}
	if errors.Is(quarantineErr.cause, context.DeadlineExceeded) {
		return "cycle_deadline"
	}
	if strings.Contains(strings.ToLower(quarantineErr.cause.Error()), "heartbeat") {
		return "heartbeat_lost"
	}
	return "cycle_error"
}
