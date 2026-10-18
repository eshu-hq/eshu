// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package deadline

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"os/exec"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

const (
	dnsStderr  = "fatal: unable to access 'https://github.com/o/r.git/': Could not resolve host: github.com"
	authStderr = "fatal: Authentication failed for 'https://github.com/o/r.git/'"
	hungUp     = "error: RPC failed; curl 56 Recv failure: Connection reset by peer"

	attemptsMetric  = "eshu_dp_git_command_attempts_total"
	retriesMetric   = "eshu_dp_git_command_retries_total"
	exhaustedMetric = "eshu_dp_git_command_retry_budget_exhausted_total"
	durationMetric  = "eshu_dp_git_command_attempt_duration_seconds"
)

var fastLimits = Limits{Fetch: time.Minute, Resolve: time.Minute, Clone: time.Minute}

// scripted runs one scripted outcome per attempt, in order, and keeps only the
// latest attempt's stderr, as the git package's runner does. One scripted value
// serves one command; goroutines each get their own.
type scripted struct {
	mu      sync.Mutex
	outcome []string // "ok", or the stderr text of a failing attempt
	calls   int
	stderr  bytes.Buffer
}

func (s *scripted) build(ctx context.Context) *exec.Cmd {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stderr.Reset()
	step := s.outcome[min(s.calls, len(s.outcome)-1)]
	s.calls++
	script := "exit 0"
	if step != "ok" {
		script = `printf '%s\n' "$0" >&2; exit 128`
	}
	cmd := exec.CommandContext(ctx, "sh", "-c", script, step)
	cmd.Stderr = &s.stderr
	return cmd
}

func (s *scripted) callCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

// retryHarness carries a context with instruments, a JSON logger, and the
// sweep budget, and builds commands whose backoff clock is injected: the sleep
// records the wait and returns at once, and the jitter is the identity. It
// keeps no package-level state, so cells and goroutines do not interact.
type retryHarness struct {
	ctx    context.Context
	reader *sdkmetric.ManualReader
	logs   *bytes.Buffer
	mu     sync.Mutex
	slept  []time.Duration
}

func newRetryHarness(t *testing.T, limits Limits) *retryHarness {
	t.Helper()
	instruments, reader := testInstruments(t)
	h := &retryHarness{reader: reader, logs: &bytes.Buffer{}}
	logger := slog.New(slog.NewJSONHandler(&lockedWriter{w: h.logs}, nil))
	h.ctx = WithSweep(WithMetrics(context.Background(), instruments), limits, logger)
	return h
}

// lockedWriter serializes log writes from concurrent cells.
type lockedWriter struct {
	mu sync.Mutex
	w  *bytes.Buffer
}

func (l *lockedWriter) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.w.Write(p)
}

// command makes a Command for run with the harness clock.
func (h *retryHarness) command(run *scripted, op string) Command {
	return Command{
		Operation: op, Host: "github.com", Scope: "github/o/r",
		Build: run.build,
		Stderr: func() string {
			run.mu.Lock()
			defer run.mu.Unlock()
			return run.stderr.String()
		},
		Sleep: func(_ context.Context, d time.Duration) error {
			h.mu.Lock()
			h.slept = append(h.slept, d)
			h.mu.Unlock()
			return nil
		},
		Jitter: func(d time.Duration) time.Duration { return d },
	}
}

func (h *retryHarness) sleeps() []time.Duration {
	h.mu.Lock()
	defer h.mu.Unlock()
	return slices.Clone(h.slept)
}

func attrKey(kvs []attribute.KeyValue) string {
	parts := make([]string, 0, len(kvs))
	for _, kv := range kvs {
		parts = append(parts, string(kv.Key)+"="+kv.Value.String())
	}
	slices.Sort(parts)
	return strings.Join(parts, ",")
}

// series returns one counter or histogram as "k=v,k=v" -> count.
func (h *retryHarness) series(t *testing.T, name string) map[string]int64 {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := h.reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	out := map[string]int64{}
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != name {
				continue
			}
			switch data := m.Data.(type) {
			case metricdata.Sum[int64]:
				for _, dp := range data.DataPoints {
					out[attrKey(dp.Attributes.ToSlice())] += dp.Value
				}
			case metricdata.Histogram[float64]:
				for _, dp := range data.DataPoints {
					// #nosec G115 -- bounded: dp.Count comes from the in-test metric reader over a handful of harness operations, far below MaxInt64.
					out[attrKey(dp.Attributes.ToSlice())] += int64(dp.Count)
				}
			default:
				t.Fatalf("%s data = %T", name, m.Data)
			}
		}
	}
	return out
}

func (h *retryHarness) recordsWithMessage(t *testing.T, prefix string) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(h.logs.String()), "\n") {
		if line == "" {
			continue
		}
		var rec map[string]any
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			t.Fatalf("log line %q: %v", line, err)
		}
		if msg, _ := rec["msg"].(string); strings.HasPrefix(msg, prefix) {
			out = append(out, rec)
		}
	}
	return out
}

func wantSeries(t *testing.T, got map[string]int64, name string, want map[string]int64) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s = %v, want %v", name, got, want)
	}
	for k, v := range want {
		if got[k] != v {
			t.Fatalf("%s = %v, want %v", name, got, want)
		}
	}
}
