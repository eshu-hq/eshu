// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package projector

import "time"

// maxProjectorPollInterval caps the idle poll backoff. It mirrors the
// shared-projection runner's 5s cap so an idle projector costs close to
// nothing while a productive cycle still re-polls immediately.
const maxProjectorPollInterval = 5 * time.Second

// projectorPollBackoff returns the wait after consecutiveEmpty consecutive
// empty polls from a base interval: base, 2x, 4x, then capped. The cap is
// max(5s, base) so an operator PollInterval above 5s is never shortened.
// A non-positive base falls back to the default poll interval.
func projectorPollBackoff(base time.Duration, consecutiveEmpty int) time.Duration {
	if base <= 0 {
		base = defaultPollInterval
	}
	backoff := base
	for i := 1; i < consecutiveEmpty && i < 4; i++ {
		backoff *= 2
	}
	if cap := maxProjectorPollInterval; cap > base {
		if backoff > cap {
			backoff = cap
		}
	} else if backoff > base {
		backoff = base
	}
	return backoff
}
