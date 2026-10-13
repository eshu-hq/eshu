// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package deadline

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// Per-command git deadlines for the sync loop (#8052). A git command that
// waits on name resolution otherwise lasts until libcurl gives up after about
// 300 s, and the loop is sequential, so the whole fleet sync pauses with it.
const (
	// DefaultFetch bounds fetch, ls-remote, and every other
	// non-clone git command the sync loop runs.
	DefaultFetch = 90 * time.Second
	// DefaultClone bounds the initial clone. It is longer than the
	// fetch deadline because a slow clone that is still making progress is not
	// a DNS stall, and a short deadline would turn a large repository into a
	// permanent failure.
	DefaultClone = 600 * time.Second

	fetchTimeoutEnv = "ESHU_GIT_FETCH_TIMEOUT_SECONDS"
	cloneTimeoutEnv = "ESHU_GIT_CLONE_TIMEOUT_SECONDS"
	// maxTimeoutSeconds keeps seconds-to-Duration conversion far from
	// overflow and rejects a value that is a typo, not a deadline.
	maxTimeoutSeconds = 24 * 60 * 60
)

// Closed set of operation labels on eshu_dp_git_fetch_deadline_exceeded_total.
const (
	operationClone    = "clone"
	operationFetch    = "fetch"
	operationLsRemote = "ls_remote"
	operationOther    = "other"
)

// Error reports a git command the sync loop killed because it ran
// past its per-command deadline.
type Error struct {
	// Operation is the closed metric label: clone, fetch, ls_remote, or other.
	Operation string
	// Command is the git subcommand that was killed.
	Command string
	// Limit is the deadline that applied to the command.
	Limit time.Duration
	// Elapsed is how long the command ran before it was killed.
	Elapsed time.Duration
	// Err is the process error after the kill.
	Err error
}

// Error names the deadline and the elapsed time.
func (e *Error) Error() string {
	return fmt.Sprintf("git %s exceeded the %gs deadline after %.1fs", e.Command, e.Limit.Seconds(), e.Elapsed.Seconds())
}

// Unwrap returns the process error.
func (e *Error) Unwrap() error { return e.Err }

// Limits holds the two per-command deadlines. A zero field means the default,
// never unbounded, so a config built without FromEnv is still bounded.
type Limits struct {
	// Fetch bounds every non-clone git command.
	Fetch time.Duration
	// Clone bounds the initial clone.
	Clone time.Duration
}

// For returns the deadline that applies to a git subcommand.
func (l Limits) For(subcommand string) time.Duration {
	if subcommand == "clone" {
		if l.Clone > 0 {
			return l.Clone
		}
		return DefaultClone
	}
	if l.Fetch > 0 {
		return l.Fetch
	}
	return DefaultFetch
}

// FromEnv reads ESHU_GIT_FETCH_TIMEOUT_SECONDS and ESHU_GIT_CLONE_TIMEOUT_SECONDS.
// Unset or blank keeps the default. A set value must be a whole number of
// seconds from 1 to 86400; anything else (negative, zero, fractional,
// non-numeric) is a startup error naming the variable and the value. Zero is
// refused because a disabled deadline brings back the 300 s fleet pause of
// #8052.
func FromEnv(getenv func(string) string) (Limits, error) {
	fetch, err := secondsFromEnv(getenv, fetchTimeoutEnv, DefaultFetch)
	if err != nil {
		return Limits{}, err
	}
	clone, err := secondsFromEnv(getenv, cloneTimeoutEnv, DefaultClone)
	if err != nil {
		return Limits{}, err
	}
	return Limits{Fetch: fetch, Clone: clone}, nil
}

func secondsFromEnv(getenv func(string) string, name string, def time.Duration) (time.Duration, error) {
	trimmed := strings.TrimSpace(getenv(name))
	if trimmed == "" {
		return def, nil
	}
	seconds, err := strconv.Atoi(trimmed)
	if err != nil || seconds < 1 || seconds > maxTimeoutSeconds {
		return 0, fmt.Errorf("%s=%q: a set value must be a whole number of seconds from 1 to %d", name, trimmed, maxTimeoutSeconds)
	}
	return time.Duration(seconds) * time.Second, nil
}

// Operation maps a git subcommand to the closed operation label.
func Operation(subcommand string) string {
	switch subcommand {
	case "clone":
		return operationClone
	case "fetch":
		return operationFetch
	case "ls-remote":
		return operationLsRemote
	default:
		return operationOther
	}
}

type metricsKey struct{}

// WithMetrics carries the telemetry instruments to the shared git
// runner so every path that can hit a deadline (clone, fetch, the lock-recovery
// retry, ls-remote, pinned-ref fetches) counts it exactly once at the kill.
func WithMetrics(ctx context.Context, instruments *telemetry.Instruments) context.Context {
	if instruments == nil {
		return ctx
	}
	return context.WithValue(ctx, metricsKey{}, instruments)
}

// record counts one killed command and marks the active
// span, if any. The repository stays out of the metric labels; the paired
// "git repository sync failed" log line carries it with the elapsed time.
func record(ctx context.Context, deadlineErr *Error) {
	trace.SpanFromContext(ctx).AddEvent("git.deadline_exceeded", trace.WithAttributes(
		attribute.String(telemetry.MetricDimensionOperation, deadlineErr.Operation),
		attribute.String("git.command", deadlineErr.Command),
		attribute.Float64("git.deadline_seconds", deadlineErr.Limit.Seconds()),
		attribute.Float64("git.elapsed_seconds", deadlineErr.Elapsed.Seconds()),
	))
	instruments, _ := ctx.Value(metricsKey{}).(*telemetry.Instruments)
	if instruments == nil || instruments.GitFetchDeadlineExceeded == nil {
		return
	}
	instruments.GitFetchDeadlineExceeded.Add(ctx, 1, metric.WithAttributes(
		attribute.String(telemetry.MetricDimensionOperation, deadlineErr.Operation),
	))
}

// Run runs one git command under its per-command deadline. build must
// construct the command from the context it is handed; the command runs in its
// own process group, so the deadline kills git and its remote-https helper
// together (see runtime.NewProcessGroupCommand). A kill by the deadline returns
// an *Error. A kill by the parent context returns the plain process error, so
// a shutdown is never reported or counted as a deadline.
func Run(
	ctx context.Context,
	limits Limits,
	subcommand string,
	build func(context.Context) *exec.Cmd,
) error {
	limit := limits.For(subcommand)
	start := time.Now()
	deadlineCtx, cancel := context.WithTimeout(ctx, limit)
	defer cancel()
	err := build(deadlineCtx).Run()
	if err == nil || ctx.Err() != nil || !errors.Is(deadlineCtx.Err(), context.DeadlineExceeded) {
		return err //nolint:wrapcheck // the caller wraps it with the command and its stderr
	}
	deadlineErr := &Error{
		Operation: Operation(subcommand),
		Command:   subcommand,
		Limit:     limit,
		Elapsed:   time.Since(start),
		Err:       err,
	}
	record(ctx, deadlineErr)
	return deadlineErr
}
