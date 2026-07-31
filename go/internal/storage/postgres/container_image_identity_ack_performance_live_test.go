//go:build perf5854_ack

// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"sort"
	"testing"
	"time"
)

type containerImageIdentityAckPerfStats struct {
	median time.Duration
	p95    time.Duration
}

func measureContainerImageIdentityAckPerfPair(
	t *testing.T,
	_ context.Context,
	_ *sql.DB,
	iterations int,
	reset func(),
	before func() error,
	after func() error,
) (containerImageIdentityAckPerfStats, containerImageIdentityAckPerfStats) {
	t.Helper()
	beforeSamples := make([]time.Duration, 0, iterations)
	afterSamples := make([]time.Duration, 0, iterations)
	run := func(name string, operation func() error) time.Duration {
		reset()
		started := time.Now()
		if err := operation(); err != nil {
			t.Fatalf("%s ACK performance operation: %v", name, err)
		}
		return time.Since(started)
	}
	for iteration := range iterations {
		if iteration%2 == 0 {
			beforeSamples = append(beforeSamples, run("before", before))
			afterSamples = append(afterSamples, run("after", after))
		} else {
			afterSamples = append(afterSamples, run("after", after))
			beforeSamples = append(beforeSamples, run("before", before))
		}
	}
	return ackPerfStats(beforeSamples), ackPerfStats(afterSamples)
}

func ackPerfStats(samples []time.Duration) containerImageIdentityAckPerfStats {
	sorted := append([]time.Duration(nil), samples...)
	sort.Slice(sorted, func(left, right int) bool { return sorted[left] < sorted[right] })
	return containerImageIdentityAckPerfStats{
		median: sorted[len(sorted)/2],
		p95:    sorted[(len(sorted)*95-1)/100],
	}
}

func assertContainerImageIdentityAckPerfBudget(
	t *testing.T,
	name string,
	before containerImageIdentityAckPerfStats,
	after containerImageIdentityAckPerfStats,
) {
	t.Helper()
	if after.median > time.Duration(float64(before.median)*1.05) {
		t.Errorf(
			"%s median = %s, exceeds 5%% budget over %s",
			name,
			after.median,
			before.median,
		)
	}
	if after.p95 > time.Duration(float64(before.p95)*1.10) {
		t.Errorf(
			"%s p95 = %s, exceeds 10%% budget over %s",
			name,
			after.p95,
			before.p95,
		)
	}
}

func ackPerfMillis(duration time.Duration) float64 {
	return float64(duration) / float64(time.Millisecond)
}
