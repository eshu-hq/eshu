// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package contention

import (
	"errors"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/eshu-hq/eshu/go/internal/workflow/parity"
)

// LatencySummary is the percentile summary of one operation's client-side
// call durations.
type LatencySummary struct {
	Count int     `json:"count"`
	P50MS float64 `json:"p50_ms"`
	P99MS float64 `json:"p99_ms"`
	MaxMS float64 `json:"max_ms"`
}

// recorder collects per-operation latencies and outcome codes. One mutex
// guards appends; the critical section is a slice append, so it adds no
// ordering between workers beyond the append itself.
type recorder struct {
	mu        sync.Mutex
	latencies map[string][]time.Duration
	codes     map[string]map[string]int
	deadlocks int
	slowest   []SlowCall
}

// SlowCall is one of the slowest calls of the run, kept with its start time
// so an operator can line it up with database logs and the sampler series.
type SlowCall struct {
	Op         string    `json:"op"`
	Start      time.Time `json:"start"`
	DurationMS float64   `json:"duration_ms"`
	Code       string    `json:"code"`
	Error      string    `json:"error,omitempty"`
}

// maxSlowCalls bounds the slow-call list in the report.
const maxSlowCalls = 10

func newRecorder() *recorder {
	return &recorder{latencies: map[string][]time.Duration{}, codes: map[string]map[string]int{}}
}

// observe records one call: its duration and its comparable code. A
// Postgres deadlock (SQLSTATE 40P01) anywhere in the error chain, or named
// in an HTTP leg's message, is counted separately so the run can abort.
func (r *recorder) observe(op string, d time.Duration, err error) parity.Code {
	code := parity.Classify(err)
	r.mu.Lock()
	defer r.mu.Unlock()
	r.latencies[op] = append(r.latencies[op], d)
	if r.codes[op] == nil {
		r.codes[op] = map[string]int{}
	}
	r.codes[op][string(code)]++
	if isDeadlock(err) {
		r.deadlocks++
	}
	r.keepSlow(op, d, code, err)
	return code
}

// keepSlow keeps the maxSlowCalls slowest calls; the caller holds r.mu.
func (r *recorder) keepSlow(op string, d time.Duration, code parity.Code, err error) {
	if len(r.slowest) == maxSlowCalls && ms(d) <= r.slowest[len(r.slowest)-1].DurationMS {
		return
	}
	call := SlowCall{Op: op, Start: time.Now().Add(-d).UTC(), DurationMS: ms(d), Code: string(code)}
	if err != nil {
		call.Error = err.Error()
	}
	r.slowest = append(r.slowest, call)
	sort.Slice(r.slowest, func(i, j int) bool { return r.slowest[i].DurationMS > r.slowest[j].DurationMS })
	if len(r.slowest) > maxSlowCalls {
		r.slowest = r.slowest[:maxSlowCalls]
	}
}

func (r *recorder) slowCalls() []SlowCall {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]SlowCall(nil), r.slowest...)
}

func (r *recorder) deadlockCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.deadlocks
}

func (r *recorder) summaries() (map[string]LatencySummary, map[string]map[string]int, int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make(map[string]LatencySummary, len(r.latencies))
	total := 0
	for op, ds := range r.latencies {
		out[op] = summarize(ds)
		total += len(ds)
	}
	codes := make(map[string]map[string]int, len(r.codes))
	for op, m := range r.codes {
		codes[op] = make(map[string]int, len(m))
		for k, v := range m {
			codes[op][k] = v
		}
	}
	return out, codes, total
}

func summarize(ds []time.Duration) LatencySummary {
	if len(ds) == 0 {
		return LatencySummary{}
	}
	sorted := append([]time.Duration(nil), ds...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	return LatencySummary{
		Count: len(sorted),
		P50MS: ms(percentile(sorted, 0.50)),
		P99MS: ms(percentile(sorted, 0.99)),
		MaxMS: ms(sorted[len(sorted)-1]),
	}
}

// percentile reads the nearest-rank percentile of an ascending slice.
func percentile(sorted []time.Duration, p float64) time.Duration {
	if len(sorted) == 0 {
		return 0
	}
	rank := int(float64(len(sorted))*p+0.999999) - 1
	if rank < 0 {
		rank = 0
	}
	if rank >= len(sorted) {
		rank = len(sorted) - 1
	}
	return sorted[rank]
}

func ms(d time.Duration) float64 { return float64(d.Microseconds()) / 1000 }

// isDeadlock reports a Postgres deadlock_detected in err.
func isDeadlock(err error) bool {
	if err == nil {
		return false
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "40P01" {
		return true
	}
	msg := err.Error()
	return strings.Contains(msg, "40P01") || strings.Contains(msg, "deadlock detected")
}
