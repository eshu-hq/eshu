// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package deadline

import (
	"context"
	"errors"
	"os/exec"
	"slices"
	"strings"
	"testing"
	"time"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

const counterName = "eshu_dp_git_command_deadline_exceeded_total"

func testInstruments(t *testing.T) (*telemetry.Instruments, *sdkmetric.ManualReader) {
	t.Helper()
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	instruments, err := telemetry.NewInstruments(provider.Meter("deadline-test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v", err)
	}
	return instruments, reader
}

// operationCounts returns the counter value per operation label.
func operationCounts(t *testing.T, reader *sdkmetric.ManualReader) map[string]int64 {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	out := map[string]int64{}
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != counterName {
				continue
			}
			for _, dp := range m.Data.(metricdata.Sum[int64]).DataPoints {
				op, _ := dp.Attributes.Value(telemetry.MetricDimensionOperation)
				out[op.AsString()] += dp.Value
			}
		}
	}
	return out
}

func sleeper(seconds string) func(context.Context) *exec.Cmd {
	return func(ctx context.Context) *exec.Cmd { return exec.CommandContext(ctx, "sleep", seconds) }
}

// TestLimitsZeroValueKeepsDefaults proves a hand-built config is still
// bounded: zero means the default, never unbounded.
func TestLimitsZeroValueKeepsDefaults(t *testing.T) {
	var limits Limits
	want := map[string]time.Duration{Clone: DefaultClone, Fetch: DefaultFetch, LsRemote: DefaultResolve, Resolve: DefaultResolve}
	for op, def := range want {
		if got, ok := limits.For(op); !ok || got != def {
			t.Errorf("For(%q) = %s, %v; want %s, true", op, got, ok, def)
		}
	}
	if DefaultResolve != 60*time.Second || DefaultFetch != 90*time.Second || DefaultClone != 600*time.Second {
		t.Fatalf("defaults = %s/%s/%s, want 60s/90s/600s", DefaultResolve, DefaultFetch, DefaultClone)
	}
}

// TestLimitsForReachesEachOperation proves each configured value reaches only
// its own operation, and that the clone deadline is the longest default.
func TestLimitsForReachesEachOperation(t *testing.T) {
	limits := Limits{Resolve: 3 * time.Second, Fetch: 5 * time.Second, Clone: 7 * time.Second}
	want := map[string]time.Duration{Clone: 7 * time.Second, Fetch: 5 * time.Second, LsRemote: 3 * time.Second, Resolve: 3 * time.Second}
	for op, d := range want {
		if got, ok := limits.For(op); !ok || got != d {
			t.Errorf("For(%q) = %s, %v; want %s", op, got, ok, d)
		}
	}
	if DefaultClone <= DefaultFetch || DefaultFetch <= DefaultResolve {
		t.Fatalf("defaults must order clone > fetch > resolve, got %s/%s/%s", DefaultClone, DefaultFetch, DefaultResolve)
	}
}

func TestFromEnvDefaultsAndOverride(t *testing.T) {
	limits, err := FromEnv(func(string) string { return "" })
	if err != nil || limits.Resolve != 60*time.Second || limits.Fetch != 90*time.Second || limits.Clone != 600*time.Second {
		t.Fatalf("FromEnv(empty) = %+v, %v; want 60s/90s/600s", limits, err)
	}
	env := map[string]string{resolveTimeoutEnv: "30", fetchTimeoutEnv: " 45 ", cloneTimeoutEnv: "1200"}
	limits, err = FromEnv(func(k string) string { return env[k] })
	if err != nil || limits.Resolve != 30*time.Second || limits.Fetch != 45*time.Second || limits.Clone != 1200*time.Second {
		t.Fatalf("FromEnv(override) = %+v, %v; want 30s/45s/1200s", limits, err)
	}
}

func TestFromEnvRejectsBadValues(t *testing.T) {
	for _, name := range []string{resolveTimeoutEnv, fetchTimeoutEnv, cloneTimeoutEnv} {
		for _, value := range []string{"-1", "0", "abc", "1.5", "90s", "86401"} {
			_, err := FromEnv(func(k string) string {
				if k == name {
					return value
				}
				return ""
			})
			if err == nil || !strings.Contains(err.Error(), name) || !strings.Contains(err.Error(), value) {
				t.Errorf("FromEnv(%s=%q) error = %v, want one naming the variable and the value", name, value, err)
			}
		}
	}
}

func TestLowSpeedArgs(t *testing.T) {
	want := []string{"-c", "http.lowSpeedLimit=1", "-c", "http.lowSpeedTime=60"}
	if got := (Limits{}).LowSpeedArgs(); !slices.Equal(got, want) {
		t.Fatalf("LowSpeedArgs() = %v, want %v", got, want)
	}
	if got := (Limits{LowSpeed: 2 * time.Second}).LowSpeedArgs(); got[3] != "http.lowSpeedTime=2" {
		t.Fatalf("LowSpeedArgs(2s) = %v, want lowSpeedTime=2", got)
	}
}

func TestErrorTextNamesDeadlineAndElapsed(t *testing.T) {
	err := &Error{Operation: Fetch, Command: "fetch", Limit: 90 * time.Second, Elapsed: 90*time.Second + 40*time.Millisecond}
	if got, want := err.Error(), "git fetch exceeded the 90s deadline after 90.0s"; got != want {
		t.Fatalf("Error() = %q, want %q", got, want)
	}
}

// TestOperationSetIsClosed proves the metric label set is closed: every
// operation in the set records exactly its own label, an operation outside the
// set is refused before it runs and records nothing, and the set is the four
// values the telemetry docs name.
func TestOperationSetIsClosed(t *testing.T) {
	if want := []string{"clone", "fetch", "ls_remote", "resolve"}; !slices.Equal(Operations(), want) {
		t.Fatalf("Operations() = %v, want %v", Operations(), want)
	}
	instruments, reader := testInstruments(t)
	ctx := WithMetrics(context.Background(), instruments)
	limits := Limits{Resolve: 100 * time.Millisecond, Fetch: 100 * time.Millisecond, Clone: 100 * time.Millisecond}
	for _, op := range Operations() {
		var deadlineErr *Error
		if err := Run(ctx, limits, op, sleeper("5")); !errors.As(err, &deadlineErr) || deadlineErr.Operation != op {
			t.Fatalf("Run(%q) error = %v, want an *Error labelled %q", op, err, op)
		}
	}
	got := operationCounts(t, reader)
	if len(got) != 4 || got[Clone] != 1 || got[Fetch] != 1 || got[LsRemote] != 1 || got[Resolve] != 1 {
		t.Fatalf("%s = %v, want each of the four operations exactly 1", counterName, got)
	}
	ran := false
	err := Run(ctx, limits, "other", func(c context.Context) *exec.Cmd { ran = true; return sleeper("0")(c) })
	if err == nil || ran || len(operationCounts(t, reader)) != 4 {
		t.Fatalf("Run(other) = %v, ran=%v, counts=%v; want a refusal that runs nothing and adds no label", err, ran, operationCounts(t, reader))
	}
}

// TestRunPassesThroughOrdinaryFailure proves a command that fails on its own,
// well inside its deadline, is not relabelled a deadline.
func TestRunPassesThroughOrdinaryFailure(t *testing.T) {
	err := Run(context.Background(), Limits{Fetch: time.Minute}, Fetch, func(ctx context.Context) *exec.Cmd {
		return exec.CommandContext(ctx, "sh", "-c", "exit 3")
	})
	var deadlineErr *Error
	if err == nil || errors.As(err, &deadlineErr) {
		t.Fatalf("Run() error = %v, want a plain exit error", err)
	}
}

// TestRunParentCancelIsNotADeadline proves a cancelled parent returns the
// process error and records nothing.
func TestRunParentCancelIsNotADeadline(t *testing.T) {
	instruments, reader := testInstruments(t)
	parent, cancel := context.WithCancel(WithMetrics(context.Background(), instruments))
	time.AfterFunc(100*time.Millisecond, cancel)
	err := Run(parent, Limits{Fetch: time.Minute}, Fetch, sleeper("5"))
	var deadlineErr *Error
	if err == nil || errors.As(err, &deadlineErr) {
		t.Fatalf("Run() error = %v, want a plain kill error", err)
	}
	if got := operationCounts(t, reader); len(got) != 0 {
		t.Fatalf("%s = %v after a parent cancel, want none", counterName, got)
	}
}

// TestRunBoundsAnEscapedDescendant proves WaitDelay stops a descendant that
// kept the output pipes open (here: no process group, so the kill reaches only
// the shell) from holding Run past the grace.
func TestRunBoundsAnEscapedDescendant(t *testing.T) {
	old := killGrace
	killGrace = 300 * time.Millisecond
	t.Cleanup(func() { killGrace = old })
	var stderr strings.Builder
	start := time.Now()
	err := Run(context.Background(), Limits{Fetch: 200 * time.Millisecond}, Fetch, func(ctx context.Context) *exec.Cmd {
		cmd := exec.CommandContext(ctx, "sh", "-c", "sleep 20 & wait")
		cmd.Stderr = &stderr
		return cmd
	})
	var deadlineErr *Error
	if !errors.As(err, &deadlineErr) {
		t.Fatalf("Run() error = %v, want an *Error", err)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("Run() took %s, want it bounded by deadline plus grace", elapsed)
	}
}

// TestRunKeepsSuccessWhenPipesLinger proves a command that exited zero keeps
// its success even when a descendant still holds the pipes at the grace.
func TestRunKeepsSuccessWhenPipesLinger(t *testing.T) {
	old := killGrace
	killGrace = 300 * time.Millisecond
	t.Cleanup(func() { killGrace = old })
	var stderr strings.Builder
	err := Run(context.Background(), Limits{Fetch: time.Minute}, Fetch, func(ctx context.Context) *exec.Cmd {
		cmd := exec.CommandContext(ctx, "sh", "-c", "sleep 20 & exit 0")
		cmd.Stderr = &stderr
		return cmd
	})
	if err != nil {
		t.Fatalf("Run() error = %v, want nil: a zero exit is a success", err)
	}
}
