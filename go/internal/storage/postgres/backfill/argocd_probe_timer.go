// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package backfill

import (
	"context"
	"sync/atomic"
	"time"
)

// argoCDProbeTimer sums the wall time of the fan-in's per-partition
// ListArgoCDBearingPartitionsQuery probes across all fan-in workers (#8082).
// The probes run concurrently, so the sum can exceed the fan-in wall time; it
// is the probe's cost, which the accepted per-partition design makes
// observable as argocd_probe_seconds on the fanin block. Observations arrive
// from concurrent workers, so the total is an atomic nanosecond counter.
type argoCDProbeTimer struct {
	nanos atomic.Int64
}

type argoCDProbeTimerKey struct{}

// withArgoCDProbeTimer returns ctx carrying timer for the fan-in publication
// step to fill. Without it the probe is not timed and costs nothing extra.
func withArgoCDProbeTimer(ctx context.Context, timer *argoCDProbeTimer) context.Context {
	return context.WithValue(ctx, argoCDProbeTimerKey{}, timer)
}

// recordArgoCDProbe adds one probe's duration to the timer carried by ctx and
// is a no-op when ctx carries none.
func recordArgoCDProbe(ctx context.Context, elapsed time.Duration) {
	if timer, ok := ctx.Value(argoCDProbeTimerKey{}).(*argoCDProbeTimer); ok && timer != nil {
		timer.nanos.Add(int64(elapsed))
	}
}

// seconds reports the summed probe time in seconds.
func (t *argoCDProbeTimer) seconds() float64 {
	return time.Duration(t.nanos.Load()).Seconds()
}
