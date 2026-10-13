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

// Per-operation git deadlines for the sync loop (#8052). A git command that
// waits on name resolution otherwise lasts until libcurl gives up after about
// 300 s, and the loop is sequential, so the whole fleet sync pauses with it.
const (
	// DefaultResolve bounds ref discovery (git ls-remote).
	DefaultResolve = 60 * time.Second
	// DefaultFetch bounds a fetch of an already cloned repository.
	DefaultFetch = 90 * time.Second
	// DefaultClone bounds the initial clone. It is the longest deadline
	// because a large clone that is still making progress is not a stall.
	DefaultClone = 600 * time.Second

	resolveTimeoutEnv = "ESHU_GIT_RESOLVE_TIMEOUT_SECONDS"
	fetchTimeoutEnv   = "ESHU_GIT_FETCH_TIMEOUT_SECONDS"
	cloneTimeoutEnv   = "ESHU_GIT_CLONE_TIMEOUT_SECONDS"
	// maxTimeoutSeconds keeps seconds-to-Duration conversion far from
	// overflow and rejects a value that is a typo, not a deadline.
	maxTimeoutSeconds = 24 * 60 * 60
)

// The closed set of operations a deadline applies to. They are the only values
// of the operation label on eshu_dp_git_command_deadline_exceeded_total.
const (
	// Clone is the initial clone of a repository.
	Clone = "clone"
	// Fetch is a fetch into an existing checkout, including a pinned-ref fetch.
	Fetch = "fetch"
	// LsRemote is the remote ref listing a sync cycle runs per repository.
	LsRemote = "ls_remote"
	// Resolve is the remote default-branch lookup.
	Resolve = "resolve"
)

// killGrace bounds how long Run waits for the output pipes after the deadline
// has killed the process group. The group kill normally closes them at once;
// the grace only matters when a descendant escaped the group. A variable so a
// test can shorten it.
var killGrace = 10 * time.Second

// Operations returns the closed operation set in a stable order.
func Operations() []string { return []string{Clone, Fetch, LsRemote, Resolve} }

// Error reports a git command the sync loop killed because it ran past its
// deadline.
type Error struct {
	// Operation is the closed metric label, one of Operations.
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

// Limits holds the per-operation deadlines. A zero field means the default,
// never unbounded, so a config built without FromEnv is still bounded.
type Limits struct {
	// Resolve bounds ls-remote ref listing and default-branch lookup.
	Resolve time.Duration
	// Fetch bounds a fetch.
	Fetch time.Duration
	// Clone bounds the initial clone.
	Clone time.Duration
	// LowSpeed is how long a transfer may sit at 0 bytes per second before git
	// aborts it (http.lowSpeedTime). Zero means DefaultLowSpeed. It is not an
	// environment setting: it complements the process deadline for a stall
	// that starts after the connection is up.
	LowSpeed time.Duration
}

// DefaultLowSpeed is the default http.lowSpeedTime. A transfer that moves
// fewer than http.lowSpeedLimit (1) bytes per second for this long is aborted,
// so a slow clone that keeps making progress survives.
const DefaultLowSpeed = 60 * time.Second

// LowSpeedArgs returns the git -c options that abort a transfer stalled at 0
// bytes per second. Put them before the subcommand of a fetch or a clone. They
// are argv options on purpose: gitCommandEnv owns GIT_CONFIG_COUNT and
// GIT_CONFIG_KEY_0 for the credential header, and these must not touch it.
func (l Limits) LowSpeedArgs() []string {
	seconds := DefaultLowSpeed
	if l.LowSpeed > 0 {
		seconds = l.LowSpeed
	}
	return []string{
		"-c", "http.lowSpeedLimit=1",
		"-c", "http.lowSpeedTime=" + strconv.Itoa(int(seconds.Seconds())),
	}
}

// For returns the deadline of an operation and whether the operation is one of
// the closed set.
func (l Limits) For(operation string) (time.Duration, bool) {
	pick := func(set, def time.Duration) (time.Duration, bool) {
		if set > 0 {
			return set, true
		}
		return def, true
	}
	switch operation {
	case Clone:
		return pick(l.Clone, DefaultClone)
	case Fetch:
		return pick(l.Fetch, DefaultFetch)
	case LsRemote, Resolve:
		return pick(l.Resolve, DefaultResolve)
	default:
		return 0, false
	}
}

// command is the git subcommand an operation runs, for the error text.
func command(operation string) string {
	switch operation {
	case Clone:
		return "clone"
	case Fetch:
		return "fetch"
	default:
		return "ls-remote"
	}
}

// FromEnv reads ESHU_GIT_RESOLVE_TIMEOUT_SECONDS, ESHU_GIT_FETCH_TIMEOUT_SECONDS
// and ESHU_GIT_CLONE_TIMEOUT_SECONDS. Unset or blank keeps the default. A set
// value must be a whole number of seconds from 1 to 86400; anything else
// (negative, zero, fractional, non-numeric) is a startup error naming the
// variable and the value. Zero is refused because a disabled deadline brings
// back the 300 s fleet pause of #8052.
func FromEnv(getenv func(string) string) (Limits, error) {
	var limits Limits
	for _, item := range []struct {
		name string
		def  time.Duration
		dst  *time.Duration
	}{
		{resolveTimeoutEnv, DefaultResolve, &limits.Resolve},
		{fetchTimeoutEnv, DefaultFetch, &limits.Fetch},
		{cloneTimeoutEnv, DefaultClone, &limits.Clone},
	} {
		value, err := secondsFromEnv(getenv, item.name, item.def)
		if err != nil {
			return Limits{}, err
		}
		*item.dst = value
	}
	return limits, nil
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

type metricsKey struct{}

// WithMetrics carries the telemetry instruments to the shared git runner so
// every path that can hit a deadline (clone, fetch, the lock-recovery retry,
// ls-remote, pinned-ref fetches) counts it exactly once at the kill.
func WithMetrics(ctx context.Context, instruments *telemetry.Instruments) context.Context {
	if instruments == nil {
		return ctx
	}
	return context.WithValue(ctx, metricsKey{}, instruments)
}

// record counts one killed command and marks the active span, if any. The
// repository stays out of the metric labels; the paired "git repository sync
// failed" log line carries it with the elapsed time.
func record(ctx context.Context, deadlineErr *Error) {
	trace.SpanFromContext(ctx).AddEvent("git.deadline_exceeded", trace.WithAttributes(
		attribute.String(telemetry.MetricDimensionOperation, deadlineErr.Operation),
		attribute.Float64("git.deadline_seconds", deadlineErr.Limit.Seconds()),
		attribute.Float64("git.elapsed_seconds", deadlineErr.Elapsed.Seconds()),
	))
	instruments, _ := ctx.Value(metricsKey{}).(*telemetry.Instruments)
	if instruments == nil || instruments.GitCommandDeadlineExceeded == nil {
		return
	}
	instruments.GitCommandDeadlineExceeded.Add(ctx, 1, metric.WithAttributes(
		attribute.String(telemetry.MetricDimensionOperation, deadlineErr.Operation),
	))
}

// Run runs one git command under the deadline of its operation. build must
// construct the command from the context it is handed, and must build it with
// runtime.NewProcessGroupCommand: Run does not create the process group, that
// constructor does, and only a command in its own group lets the deadline kill
// git and its remote-https helper together. A kill by the deadline returns
// an *Error. A kill by the parent context returns the plain process error, so
// a shutdown is never reported or counted as a deadline. An operation outside
// the closed set is refused before anything runs.
//
// Run sets WaitDelay so a descendant that escaped the group cannot keep Wait
// blocked on the output pipes past killGrace. A command that exited zero and
// only left its pipes open still reports success.
func Run(
	ctx context.Context,
	limits Limits,
	operation string,
	build func(context.Context) *exec.Cmd,
) error {
	limit, ok := limits.For(operation)
	if !ok {
		return fmt.Errorf("git deadline: operation %q is not one of %v", operation, Operations())
	}
	start := time.Now()
	deadlineCtx, cancel := context.WithTimeout(ctx, limit)
	defer cancel()
	cmd := build(deadlineCtx)
	cmd.WaitDelay = killGrace
	err := cmd.Run()
	if errors.Is(err, exec.ErrWaitDelay) && cmd.ProcessState != nil && cmd.ProcessState.Success() {
		err = nil
	}
	if err == nil || ctx.Err() != nil || !errors.Is(deadlineCtx.Err(), context.DeadlineExceeded) {
		return err //nolint:wrapcheck // the caller wraps it with the command and its stderr
	}
	deadlineErr := &Error{
		Operation: operation,
		Command:   command(operation),
		Limit:     limit,
		Elapsed:   time.Since(start),
		Err:       err,
	}
	record(ctx, deadlineErr)
	return deadlineErr
}
