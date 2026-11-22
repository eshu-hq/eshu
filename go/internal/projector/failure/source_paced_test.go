// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package failure

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

// TestSourcePacedReleaseContract pins what the queue reads from a per-source
// release (#10882): it retries (never dead-letters), identifies itself as a
// source-paced cause, names the slowed source in its message, and carries the
// earliest (grant delay) and latest (release age) re-claim hints.
func TestSourcePacedReleaseContract(t *testing.T) {
	t.Parallel()

	release := SourcePacedRelease{Source: "git", RetryAfter: 45 * time.Second, ResumeBound: 15 * time.Minute}
	if !release.Retryable() {
		t.Fatal("Retryable() = false, want true so Fail releases instead of dead-lettering")
	}
	if !release.IsSourcePaced() {
		t.Fatal("IsSourcePaced() = false, want true")
	}
	if got := release.RetryAfterHint(); got != 45*time.Second {
		t.Fatalf("RetryAfterHint() = %s, want the grant delay 45s", got)
	}
	if got := release.ResumeBoundHint(); got != 15*time.Minute {
		t.Fatalf("ResumeBoundHint() = %s, want the release age 15m", got)
	}
	message := release.Error()
	for _, want := range []string{`"git"`, "45s"} {
		if !strings.Contains(message, want) {
			t.Fatalf("Error() = %q, want it to name the source and the retry delay (%s)", message, want)
		}
	}
	if !IsRetryable(release) {
		t.Fatal("IsRetryable(release) = false, want true")
	}
}

// TestSourcePacedHintsReadThroughWrapping proves the package-level hint readers
// find the hint under any %w wrapping, which is how the queue receives the
// cause, and return zero when the error carries none (a bound of zero keeps the
// queue's retry cap).
func TestSourcePacedHintsReadThroughWrapping(t *testing.T) {
	t.Parallel()

	wrapped := fmt.Errorf("project scope: %w", fmt.Errorf("release: %w",
		SourcePacedRelease{Source: "git", RetryAfter: 30 * time.Second, ResumeBound: 10 * time.Minute}))
	if got := RetryAfterHint(wrapped); got != 30*time.Second {
		t.Fatalf("RetryAfterHint(wrapped) = %s, want 30s", got)
	}
	if got := ResumeBoundHint(wrapped); got != 10*time.Minute {
		t.Fatalf("ResumeBoundHint(wrapped) = %s, want 10m", got)
	}
	for name, err := range map[string]error{"nil": nil, "plain": errors.New("graph write timed out")} {
		if got := RetryAfterHint(err); got != 0 {
			t.Fatalf("RetryAfterHint(%s) = %s, want 0", name, got)
		}
		if got := ResumeBoundHint(err); got != 0 {
			t.Fatalf("ResumeBoundHint(%s) = %s, want 0", name, got)
		}
	}
	unbounded := SourcePacedRelease{Source: "git", RetryAfter: time.Second}
	if got := ResumeBoundHint(unbounded); got != 0 {
		t.Fatalf("ResumeBoundHint(no bound) = %s, want 0 (the queue keeps its retry cap)", got)
	}
}
