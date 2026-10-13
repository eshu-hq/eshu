// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package deadline

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// TestLimitsZeroValueKeepsDefaults proves a hand-built config is still
// bounded: zero means the default, never unbounded.
func TestLimitsZeroValueKeepsDefaults(t *testing.T) {
	var limits Limits
	if got := limits.For("fetch"); got != DefaultFetch {
		t.Fatalf("For(fetch) = %s, want %s", got, DefaultFetch)
	}
	if got := limits.For("clone"); got != DefaultClone {
		t.Fatalf("For(clone) = %s, want %s", got, DefaultClone)
	}
}

func TestLimitsForUsesCloneOnlyForClone(t *testing.T) {
	limits := Limits{Fetch: 5 * time.Second, Clone: 7 * time.Second}
	for _, sub := range []string{"fetch", "ls-remote", "checkout", "rev-parse", "worktree", ""} {
		if got := limits.For(sub); got != 5*time.Second {
			t.Errorf("For(%q) = %s, want the fetch deadline 5s", sub, got)
		}
	}
	if got := limits.For("clone"); got != 7*time.Second {
		t.Errorf("For(clone) = %s, want the clone deadline 7s", got)
	}
}

func TestOperationClassification(t *testing.T) {
	cases := map[string]string{
		"clone":        "clone",
		"fetch":        "fetch",
		"ls-remote":    "ls_remote",
		"checkout":     "other",
		"rev-parse":    "other",
		"symbolic-ref": "other",
		"worktree":     "other",
		"status":       "other",
		"":             "other",
	}
	for subcommand, want := range cases {
		if got := Operation(subcommand); got != want {
			t.Errorf("Operation(%q) = %q, want %q", subcommand, got, want)
		}
	}
}

func TestFromEnvDefaultsAndOverride(t *testing.T) {
	limits, err := FromEnv(func(string) string { return "" })
	if err != nil || limits.Fetch != 90*time.Second || limits.Clone != 600*time.Second {
		t.Fatalf("FromEnv(empty) = %+v, %v; want 90s/600s", limits, err)
	}
	env := map[string]string{fetchTimeoutEnv: " 45 ", cloneTimeoutEnv: "1200"}
	limits, err = FromEnv(func(k string) string { return env[k] })
	if err != nil || limits.Fetch != 45*time.Second || limits.Clone != 1200*time.Second {
		t.Fatalf("FromEnv(override) = %+v, %v; want 45s/1200s", limits, err)
	}
}

func TestFromEnvRejectsBadValues(t *testing.T) {
	for _, name := range []string{fetchTimeoutEnv, cloneTimeoutEnv} {
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

func TestErrorTextNamesDeadlineAndElapsed(t *testing.T) {
	err := &Error{Operation: "fetch", Command: "fetch", Limit: 90 * time.Second, Elapsed: 90*time.Second + 40*time.Millisecond}
	if got, want := err.Error(), "git fetch exceeded the 90s deadline after 90.0s"; got != want {
		t.Fatalf("Error() = %q, want %q", got, want)
	}
}

// TestRunPassesThroughOrdinaryFailure proves a command that fails on its own,
// well inside its deadline, is not relabelled a deadline.
func TestRunPassesThroughOrdinaryFailure(t *testing.T) {
	err := Run(context.Background(), Limits{Fetch: time.Minute}, "fetch", func(ctx context.Context) *exec.Cmd {
		return exec.CommandContext(ctx, "sh", "-c", "exit 3")
	})
	var deadlineErr *Error
	if err == nil || errors.As(err, &deadlineErr) {
		t.Fatalf("Run() error = %v, want a plain exit error", err)
	}
}
