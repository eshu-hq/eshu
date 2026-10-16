// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package inventory_test

import (
	"context"
	"testing"
	"time"
)

func TestRetentionProbeRoundContextIsFreshAndBounded(t *testing.T) {
	first, cancelFirst := newRetentionProbeRoundContext()
	cancelFirst()
	second, cancelSecond := newRetentionProbeRoundContext()
	defer cancelSecond()
	if err := second.Err(); err != nil {
		t.Fatalf("next round inherited cancellation: %v", err)
	}
	deadline, ok := second.Deadline()
	remaining := time.Until(deadline)
	if !ok || remaining <= 0 || remaining > 90*time.Second {
		t.Fatalf("next round deadline = %v, bounded 90s deadline required", deadline)
	}
	if first.Err() != context.Canceled {
		t.Fatalf("prior round cancellation = %v, want canceled", first.Err())
	}
	cancelSecond()
	if second.Err() != context.Canceled {
		t.Fatalf("current round cancellation = %v, want canceled", second.Err())
	}
}
